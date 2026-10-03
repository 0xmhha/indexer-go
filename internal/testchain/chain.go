// Package testchain provides a deterministic in-process EVM chain served over
// JSON-RPC. It lets tests drive the real indexer wiring (client, adapter
// detection, fetcher, storage) without a node, and produces the same chain for
// the same scenario so that index results can be compared byte for byte.
package testchain

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
)

// Account is a deterministic key pair used to sign scenario transactions.
type Account struct {
	Key     *ecdsa.PrivateKey
	Address common.Address
}

// NewAccount derives an account from a small integer seed so that addresses
// are stable across runs.
func NewAccount(seed uint64) Account {
	d := new(big.Int).SetUint64(seed + 1) // private key must be non-zero
	key, err := crypto.ToECDSA(common.LeftPadBytes(d.Bytes(), 32))
	if err != nil {
		panic(err)
	}
	return Account{Key: key, Address: crypto.PubkeyToAddress(key.PublicKey)}
}

// TxSpec describes one transaction and the receipt it should produce.
type TxSpec struct {
	From    Account
	Tx      types.TxData // nonce and chain id are filled by the builder
	Failed  bool         // receipt status 0
	GasUsed uint64       // defaults to 21000
	Logs    []*types.Log // address/topics/data only; positions are filled in
	Creates bool         // contract creation: receipt carries ContractAddress
}

// Block is one built block with its receipts.
type Block struct {
	Block    *types.Block
	Receipts types.Receipts
}

// ContractMock answers eth_call for one address. Keys are hex call data
// without 0x: the full call data is matched first, then the 4-byte selector.
type ContractMock map[string][]byte

// Chain is an append-only list of blocks plus the account state needed to
// answer eth_getBalance at any height.
type Chain struct {
	mu        sync.RWMutex
	chainID   *big.Int
	blocks    []*Block
	byHash    map[common.Hash]*Block
	txIndex   map[common.Hash]txLocation
	nonces    map[common.Address]uint64
	balances  []map[common.Address]*big.Int // balances[h] = state after block h
	contracts map[common.Address]ContractMock
	code      map[common.Address][]byte
	head      uint64 // highest block visible over RPC
	baseTime  uint64
}

type txLocation struct {
	block *Block
	index int
}

// NewChain creates a chain whose genesis allocates the given balances.
func NewChain(chainID int64, alloc map[common.Address]*big.Int) *Chain {
	c := &Chain{
		chainID:   big.NewInt(chainID),
		byHash:    map[common.Hash]*Block{},
		txIndex:   map[common.Hash]txLocation{},
		nonces:    map[common.Address]uint64{},
		contracts: map[common.Address]ContractMock{},
		code:      map[common.Address][]byte{},
		baseTime:  1_700_000_000,
	}
	state := map[common.Address]*big.Int{}
	for a, v := range alloc {
		state[a] = new(big.Int).Set(v)
	}
	c.balances = append(c.balances, state)
	c.appendBlock(nil, nil)
	return c
}

// ChainID returns the chain id.
func (c *Chain) ChainID() *big.Int { return new(big.Int).Set(c.chainID) }

// SetContract registers eth_call answers for an address.
func (c *Chain) SetContract(addr common.Address, mock ContractMock) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.contracts[addr] = mock
}

// SetCode sets the bytecode returned by eth_getCode for an address.
func (c *Chain) SetCode(addr common.Address, code []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.code[addr] = code
}

// NextNonce returns the nonce the next transaction from addr will use.
func (c *Chain) NextNonce(addr common.Address) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nonces[addr]
}

// SetHead limits which blocks are visible over RPC. Blocks above head behave
// as if they were not mined yet.
func (c *Chain) SetHead(h uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h >= uint64(len(c.blocks)) {
		h = uint64(len(c.blocks) - 1)
	}
	c.head = h
}

// Head returns the highest visible block number.
func (c *Chain) Head() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.head
}

// Len returns the number of built blocks (head may be lower).
func (c *Chain) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.blocks)
}

// AddBlock signs and appends a block containing the given transactions and
// makes it visible.
func (c *Chain) AddBlock(specs ...TxSpec) *Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.appendBlock(specs, nil)
	c.head = b.Block.NumberU64()
	return b
}

// AddBlockWithExtra is AddBlock with a custom header extra-data field.
func (c *Chain) AddBlockWithExtra(extra []byte, specs ...TxSpec) *Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.appendBlock(specs, extra)
	c.head = b.Block.NumberU64()
	return b
}

// appendBlock must be called with c.mu held (or during construction).
func (c *Chain) appendBlock(specs []TxSpec, extra []byte) *Block {
	number := uint64(len(c.blocks))
	parent := common.Hash{}
	if number > 0 {
		parent = c.blocks[number-1].Block.Hash()
	}
	state := map[common.Address]*big.Int{}
	if number > 0 {
		for a, v := range c.balances[number-1] {
			state[a] = new(big.Int).Set(v)
		}
	} else {
		state = c.balances[0]
	}

	signer := types.LatestSignerForChainID(c.chainID)
	var (
		txs      []*types.Transaction
		receipts types.Receipts
		cumGas   uint64
		logIndex uint
	)
	for i, s := range specs {
		tx := types.MustSignNewTx(s.From.Key, signer, withNonce(s.Tx, c.nonces[s.From.Address], c.chainID))
		c.nonces[s.From.Address]++

		gasUsed := s.GasUsed
		if gasUsed == 0 {
			gasUsed = 21000
		}
		cumGas += gasUsed
		status := types.ReceiptStatusSuccessful
		if s.Failed {
			status = types.ReceiptStatusFailed
		}
		r := &types.Receipt{
			Type:              tx.Type(),
			Status:            status,
			CumulativeGasUsed: cumGas,
			TxHash:            tx.Hash(),
			GasUsed:           gasUsed,
			EffectiveGasPrice: tx.GasPrice(),
			BlockNumber:       new(big.Int).SetUint64(number),
			TransactionIndex:  uint(i),
			Logs:              []*types.Log{},
		}
		if s.Creates {
			r.ContractAddress = crypto.CreateAddress(s.From.Address, tx.Nonce())
		}
		if !s.Failed {
			for _, l := range s.Logs {
				cp := *l
				cp.BlockNumber = number
				cp.TxHash = tx.Hash()
				cp.TxIndex = uint(i)
				cp.Index = logIndex
				logIndex++
				r.Logs = append(r.Logs, &cp)
			}
		}
		r.Bloom = types.CreateBloom(r)
		txs = append(txs, tx)
		receipts = append(receipts, r)

		// Same balance rule the indexer applies: value + gasUsed * tx.GasPrice().
		cost := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), tx.GasPrice())
		value := tx.Value()
		if s.Failed {
			value = new(big.Int)
		}
		sub(state, s.From.Address, new(big.Int).Add(cost, value))
		if to := tx.To(); to != nil {
			add(state, *to, value)
		} else if s.Creates {
			add(state, r.ContractAddress, value)
		}
	}

	header := &types.Header{
		ParentHash: parent,
		Coinbase:   common.Address{},
		Root:       common.Hash{},
		Difficulty: big.NewInt(1),
		Number:     new(big.Int).SetUint64(number),
		GasLimit:   30_000_000,
		GasUsed:    cumGas,
		Time:       c.baseTime + number*2,
		Extra:      extra,
		BaseFee:    big.NewInt(1_000_000_000),
	}
	blk := types.NewBlock(header, &types.Body{Transactions: txs}, receipts, trie.NewStackTrie(nil))
	for _, r := range receipts {
		r.BlockHash = blk.Hash()
		for _, l := range r.Logs {
			l.BlockHash = blk.Hash()
		}
	}

	b := &Block{Block: blk, Receipts: receipts}
	c.blocks = append(c.blocks, b)
	c.byHash[blk.Hash()] = b
	for i, tx := range txs {
		c.txIndex[tx.Hash()] = txLocation{block: b, index: i}
	}
	if number > 0 {
		c.balances = append(c.balances, state)
	}
	return b
}

func withNonce(tx types.TxData, nonce uint64, chainID *big.Int) types.TxData {
	switch t := tx.(type) {
	case *types.LegacyTx:
		cp := *t
		cp.Nonce = nonce
		return &cp
	case *types.DynamicFeeTx:
		cp := *t
		cp.Nonce = nonce
		cp.ChainID = chainID
		return &cp
	case *types.SetCodeTx:
		cp := *t
		cp.Nonce = nonce
		return &cp
	default:
		panic(fmt.Sprintf("testchain: unsupported tx type %T", tx))
	}
}

func add(state map[common.Address]*big.Int, a common.Address, v *big.Int) {
	if state[a] == nil {
		state[a] = new(big.Int)
	}
	state[a].Add(state[a], v)
}

func sub(state map[common.Address]*big.Int, a common.Address, v *big.Int) {
	if state[a] == nil {
		state[a] = new(big.Int)
	}
	state[a].Sub(state[a], v)
	if state[a].Sign() < 0 {
		panic(fmt.Sprintf("testchain: negative balance for %s", a.Hex()))
	}
}

// blockAt returns a visible block, or nil.
func (c *Chain) blockAt(n uint64) *Block {
	if n > c.head || n >= uint64(len(c.blocks)) {
		return nil
	}
	return c.blocks[n]
}

// balanceAt returns the balance after block n (clamped to head).
func (c *Chain) balanceAt(a common.Address, n uint64) *big.Int {
	if n > c.head {
		n = c.head
	}
	if v := c.balances[n][a]; v != nil {
		return new(big.Int).Set(v)
	}
	return new(big.Int)
}
