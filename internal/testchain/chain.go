// Package testchain provides a deterministic in-process EVM chain served over
// JSON-RPC. It lets tests drive the real indexer wiring (client, adapter
// detection, fetcher, storage) without a node, and produces the same chain for
// the same scenario so that index results can be compared byte for byte.
package testchain

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sort"
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

	// StableNet mode only: native value moves inside the transaction
	// (internal calls, mints, burns), and whether the sender is an
	// authorized account (pays its own tip, ends with AuthorizedTxExecuted).
	NativeTransfers []NativeTransfer
	Authorized      bool
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

	finalized   *uint64 // finalized/safe height; nil follows head
	noFinalized bool    // the node does not know the finalized tag

	coinbase common.Address   // header coinbase; receives tips
	sn       *StableNetConfig // StableNet mode, or nil
}

type txLocation struct {
	block *Block
	index int
}

// NewChain creates a chain whose genesis allocates the given balances.
func NewChain(chainID int64, alloc map[common.Address]*big.Int) *Chain {
	return newChain(chainID, alloc, nil)
}

func newChain(chainID int64, alloc map[common.Address]*big.Int, sn *StableNetConfig) *Chain {
	c := &Chain{
		chainID:   big.NewInt(chainID),
		byHash:    map[common.Hash]*Block{},
		txIndex:   map[common.Hash]txLocation{},
		nonces:    map[common.Address]uint64{},
		contracts: map[common.Address]ContractMock{},
		code:      map[common.Address][]byte{},
		baseTime:  1_700_000_000,
		sn:        sn,
	}
	if sn != nil {
		c.coinbase = sn.Coinbase
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
	baseFee := big.NewInt(1_000_000_000)
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
		// London rule, as nodes report it: base fee plus tip, capped by the
		// fee cap (legacy transactions pay their gas price). StableNet
		// replaces the tip with the governance tip (Anzeon).
		price := c.txPrice(tx, s, baseFee)
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
			EffectiveGasPrice: price,
			BlockNumber:       new(big.Int).SetUint64(number),
			TransactionIndex:  uint(i),
			Logs:              []*types.Log{},
		}
		if s.Creates {
			r.ContractAddress = crypto.CreateAddress(s.From.Address, tx.Nonce())
		}
		if !s.Failed {
			logs := c.nativeLogs(s.From.Address, createdOrTo(tx, r), tx.Value(), s)
			logs = append(logs, s.Logs...)
			for _, l := range logs {
				cp := *l
				cp.BlockNumber = number
				cp.TxHash = tx.Hash()
				cp.TxIndex = uint(i)
				cp.Index = logIndex
				logIndex++
				r.Logs = append(r.Logs, &cp)
			}
		}
		if s.Authorized && c.sn != nil {
			// Emitted even when the transaction fails, as the last log.
			r.Logs = append(r.Logs, &types.Log{
				Address: AccountManagerAddress, Topics: []common.Hash{SigAuthorizedTxExecuted},
				BlockNumber: number, TxHash: tx.Hash(), TxIndex: uint(i), Index: logIndex,
			})
			logIndex++
		}
		r.Bloom = types.CreateBloom(r)
		txs = append(txs, tx)
		receipts = append(receipts, r)

		// The chain's balance rules: the sender pays gasUsed * price and,
		// if the transaction succeeds, the value (and internal moves); the
		// coinbase receives the tip, gasUsed * (price - baseFee). The base
		// fee is burned, or distributed in StableNet mode (settleBlock).
		cost := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), price)
		sub(state, s.From.Address, cost)
		tip := new(big.Int).Sub(price, baseFee)
		if tip.Sign() > 0 {
			add(state, c.coinbase, new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), tip))
		}
		if !s.Failed {
			value := tx.Value()
			if to := createdOrTo(tx, r); to != nil && value.Sign() > 0 {
				sub(state, s.From.Address, value)
				add(state, *to, value)
			}
			for _, nt := range s.NativeTransfers {
				if nt.From != (common.Address{}) {
					sub(state, nt.From, nt.Value)
				}
				if nt.To != (common.Address{}) {
					add(state, nt.To, nt.Value)
				}
			}
		}
	}
	c.settleBlock(state, number, cumGas, baseFee, c.coinbase)
	if c.sn != nil {
		extra = c.headerExtra(number, extra)
	}

	header := &types.Header{
		ParentHash: parent,
		Coinbase:   c.coinbase,
		Root:       common.Hash{},
		Difficulty: big.NewInt(1),
		Number:     new(big.Int).SetUint64(number),
		GasLimit:   30_000_000,
		GasUsed:    cumGas,
		Time:       c.baseTime + number*2,
		Extra:      extra,
		BaseFee:    baseFee,
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

// createdOrTo is the account a transaction's value goes to: its recipient,
// or the created contract.
func createdOrTo(tx *types.Transaction, r *types.Receipt) *common.Address {
	if to := tx.To(); to != nil {
		return to
	}
	if r.ContractAddress != (common.Address{}) {
		addr := r.ContractAddress
		return &addr
	}
	return nil
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

// SetFinalized makes the finalized and safe tags answer block n (capped at
// the head).
func (c *Chain) SetFinalized(n uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finalized = &n
	c.noFinalized = false
}

// DisableFinalizedTag makes the node answer null for the finalized and safe
// tags, as nodes without finality information do.
func (c *Chain) DisableFinalizedTag() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noFinalized = true
}

// Block returns block n regardless of the head, or nil if it was not built.
func (c *Chain) Block(n uint64) *Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n >= uint64(len(c.blocks)) {
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

// BalanceAt returns an account's balance after block n (regardless of the
// head), for comparing indexed balances with the chain's state.
func (c *Chain) BalanceAt(a common.Address, n uint64) *big.Int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n >= uint64(len(c.balances)) {
		n = uint64(len(c.balances)) - 1
	}
	if v := c.balances[n][a]; v != nil {
		return new(big.Int).Set(v)
	}
	return new(big.Int)
}

// Accounts returns every account that ever held a balance, sorted.
func (c *Chain) Accounts() []common.Address {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := map[common.Address]bool{}
	for _, st := range c.balances {
		for a := range st {
			seen[a] = true
		}
	}
	out := make([]common.Address, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out
}

// TxCount returns the number of transactions in all built blocks.
func (c *Chain) TxCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n := 0
	for _, b := range c.blocks {
		n += len(b.Block.Transactions())
	}
	return n
}

// Reorg drops every block above keep, as a chain reorganization does, so the
// next AddBlock builds a competing block at keep+1. Nonces and balances return
// to their state after block keep.
func (c *Chain) Reorg(keep uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if keep >= uint64(len(c.blocks))-1 {
		return
	}
	for _, b := range c.blocks[keep+1:] {
		delete(c.byHash, b.Block.Hash())
		for _, tx := range b.Block.Transactions() {
			delete(c.txIndex, tx.Hash())
		}
	}
	c.blocks = c.blocks[:keep+1]
	c.balances = c.balances[:keep+1]
	c.nonces = map[common.Address]uint64{}
	for _, b := range c.blocks {
		for _, tx := range b.Block.Transactions() {
			from, err := types.Sender(types.LatestSignerForChainID(c.chainID), tx)
			if err == nil {
				c.nonces[from]++
			}
		}
	}
	if c.head > keep {
		c.head = keep
	}
}
