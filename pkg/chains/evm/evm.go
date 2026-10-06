// Package evm is the chain profile for Ethereum-compatible chains. It decodes
// the standard transaction types with upstream go-ethereum (used here only as
// a codec), verifies transaction hashes and senders, and converts everything
// to pkg/core/model. Chain profiles built on top of it (for example StableNet)
// add decoders for their own transaction types with WithTxDecoder.
package evm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// ID is the generic EVM profile id.
const ID = "evm"

// ErrHashMismatch means a hash computed from the decoded data differs from the
// hash the node reported: the profile does not understand the encoding.
var ErrHashMismatch = errors.New("evm: computed hash differs from reported hash")

// ErrSenderMismatch means the recovered sender differs from the node's "from".
var ErrSenderMismatch = errors.New("evm: recovered sender differs from reported sender")

// TxDecoder decodes one transaction JSON object of a chain-specific type into
// the model, including hash verification and sender recovery.
type TxDecoder func(raw json.RawMessage) (*model.Transaction, error)

// Profile is an EVM chain profile.
type Profile struct {
	id               string
	detect           func(chains.NodeInfo) bool
	features         []string
	txDecoders       map[uint8]TxDecoder
	verifyHeaderHash bool
	headerHash       func(*types.Header) common.Hash

	binaryTxDecoders  map[uint8]BinaryTxDecoder
	effectiveGasPrice EffectiveGasPriceFunc

	accounting chains.NativeAccounting
	nativeCoin *common.Address
}

// Option configures a Profile.
type Option func(*Profile)

// WithDetect sets the detection rule (default: accept every node).
func WithDetect(f func(chains.NodeInfo) bool) Option { return func(p *Profile) { p.detect = f } }

// WithFeatures sets the features the chain enables by default.
func WithFeatures(fs ...string) Option { return func(p *Profile) { p.features = fs } }

// WithTxDecoder registers a decoder for a chain-specific transaction type.
// It takes precedence over the built-in decoding for that type.
func WithTxDecoder(typ uint8, d TxDecoder) Option {
	return func(p *Profile) { p.txDecoders[typ] = d }
}

// WithHeaderHashCheck controls whether the block hash is recomputed from the
// header and compared with the reported hash (default true). Chains whose
// header carries fields go-ethereum does not hash must disable it.
func WithHeaderHashCheck(on bool) Option { return func(p *Profile) { p.verifyHeaderHash = on } }

// WithHeaderHasher sets the rule that computes a block hash from its header,
// for chains whose consensus hashes a filtered header (for example WBFT).
func WithHeaderHasher(f func(*types.Header) common.Hash) Option {
	return func(p *Profile) { p.headerHash = f }
}

// WithNativeAccounting sets the rules that derive native balance changes
// (default: chains.EthereumAccounting).
func WithNativeAccounting(a chains.NativeAccounting) Option {
	return func(p *Profile) { p.accounting = a }
}

// WithNativeCoinContract declares the contract whose Transfer events report
// native value moves (chains.NativeCoinProfile).
func WithNativeCoinContract(addr common.Address) Option {
	return func(p *Profile) { p.nativeCoin = &addr }
}

// NativeCoinContract implements chains.NativeCoinProfile.
func (p *Profile) NativeCoinContract() (common.Address, bool) {
	if p.nativeCoin == nil {
		return common.Address{}, false
	}
	return *p.nativeCoin, true
}

// NativeAccounting implements chains.AccountingProfile.
func (p *Profile) NativeAccounting() chains.NativeAccounting {
	if p.accounting == nil {
		return chains.EthereumAccounting{}
	}
	return p.accounting
}

// New returns an EVM-based profile.
func New(id string, opts ...Option) *Profile {
	p := &Profile{
		id:               id,
		detect:           func(chains.NodeInfo) bool { return true },
		txDecoders:       map[uint8]TxDecoder{},
		verifyHeaderHash: true,
		headerHash:       (*types.Header).Hash,

		binaryTxDecoders:  map[uint8]BinaryTxDecoder{},
		effectiveGasPrice: LondonEffectiveGasPrice,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func init() {
	// Generic fallback: lowest priority, accepts any node.
	chains.Register(New(ID), 0)
}

func (p *Profile) ID() string                       { return p.id }
func (p *Profile) Detect(info chains.NodeInfo) bool { return p.detect(info) }
func (p *Profile) Features() []string               { return append([]string(nil), p.features...) }

// rpcBlock carries the parts of a block response that are not header fields.
type rpcBlock struct {
	Hash         common.Hash         `json:"hash"`
	Size         *hexutil.Uint64     `json:"size"`
	Transactions []json.RawMessage   `json:"transactions"`
	Uncles       []common.Hash       `json:"uncles"`
	Withdrawals  []*types.Withdrawal `json:"withdrawals"`
}

// DecodeBlock implements chains.Profile.
func (p *Profile) DecodeBlock(raw json.RawMessage) (*model.Block, error) {
	var head types.Header
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("evm: decode header: %w", err)
	}
	var body rpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("evm: decode block: %w", err)
	}
	if p.verifyHeaderHash {
		if h := p.headerHash(&head); h != body.Hash {
			return nil, fmt.Errorf("%w: block %d header %s reported %s", ErrHashMismatch, head.Number, h.Hex(), body.Hash.Hex())
		}
	}

	b := headerToModel(&head)
	b.Hash = body.Hash
	if body.Size != nil {
		b.Size = uint64(*body.Size)
	}
	b.Uncles = body.Uncles
	if body.Withdrawals != nil {
		b.Withdrawals = make([]model.Withdrawal, 0, len(body.Withdrawals))
		for _, w := range body.Withdrawals {
			b.Withdrawals = append(b.Withdrawals, model.Withdrawal{Index: w.Index, Validator: w.Validator, Address: w.Address, Amount: w.Amount})
		}
	}
	txs, err := p.decodeTxs(body.Transactions)
	if err != nil {
		return nil, fmt.Errorf("evm: block %d %w", b.Number, err)
	}
	for i, tx := range txs {
		tx.BlockHash, tx.BlockNumber, tx.Index = b.Hash, b.Number, uint(i)
	}
	b.Transactions = txs
	return b, nil
}

// txEnvelope reads the fields every transaction object carries.
type txEnvelope struct {
	Type *hexutil.Uint64 `json:"type"`
	Hash common.Hash     `json:"hash"`
	From *common.Address `json:"from"`
}

func (p *Profile) decodeTx(raw json.RawMessage) (*model.Transaction, error) {
	var env txEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	typ := uint8(types.LegacyTxType)
	if env.Type != nil {
		typ = uint8(*env.Type)
	}
	if d, ok := p.txDecoders[typ]; ok {
		return d(raw)
	}

	var tx types.Transaction
	if err := tx.UnmarshalJSON(raw); err != nil {
		if errors.Is(err, types.ErrTxTypeNotSupported) {
			return DecodeOpaque(raw)
		}
		return nil, fmt.Errorf("decode type %d: %w", typ, err)
	}
	m, err := gethconv.RecoverTx(&tx)
	if err != nil {
		return nil, err
	}
	if m.Hash != env.Hash {
		return nil, fmt.Errorf("%w: tx %s reported %s", ErrHashMismatch, m.Hash.Hex(), env.Hash.Hex())
	}
	if env.From != nil && *env.From != m.From {
		return nil, fmt.Errorf("%w: tx %s recovered %s reported %s", ErrSenderMismatch, m.Hash.Hex(), m.From.Hex(), env.From.Hex())
	}
	return m, nil
}

// opaqueTx reads the fields a node reports for any transaction type.
type opaqueTx struct {
	Type     hexutil.Uint64  `json:"type"`
	Hash     common.Hash     `json:"hash"`
	ChainID  *hexutil.Big    `json:"chainId"`
	Nonce    hexutil.Uint64  `json:"nonce"`
	From     common.Address  `json:"from"`
	To       *common.Address `json:"to"`
	Value    *hexutil.Big    `json:"value"`
	Gas      hexutil.Uint64  `json:"gas"`
	GasPrice *hexutil.Big    `json:"gasPrice"`
	TipCap   *hexutil.Big    `json:"maxPriorityFeePerGas"`
	FeeCap   *hexutil.Big    `json:"maxFeePerGas"`
	Input    hexutil.Bytes   `json:"input"`
}

// DecodeOpaque keeps a transaction whose type no profile understands: the
// node-reported hash and sender are used as is and Opaque is set. The block is
// still indexed; callers should count these (refactoring design 3.3).
func DecodeOpaque(raw json.RawMessage) (*model.Transaction, error) {
	var o struct {
		opaqueTx
		V *hexutil.Big `json:"v"`
		R *hexutil.Big `json:"r"`
		S *hexutil.Big `json:"s"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("decode opaque transaction: %w", err)
	}
	return &model.Transaction{
		Hash:      o.Hash,
		Type:      uint8(o.Type),
		ChainID:   (*big.Int)(o.ChainID),
		Nonce:     uint64(o.Nonce),
		From:      o.From,
		To:        o.To,
		Value:     (*big.Int)(o.Value),
		Gas:       uint64(o.Gas),
		GasPrice:  (*big.Int)(o.GasPrice),
		GasTipCap: (*big.Int)(o.TipCap),
		GasFeeCap: (*big.Int)(o.FeeCap),
		Input:     o.Input,
		Signature: model.Signature{V: (*big.Int)(o.V), R: (*big.Int)(o.R), S: (*big.Int)(o.S)},
		Opaque:    true,
	}, nil
}

func headerToModel(h *types.Header) *model.Block {
	return &model.Block{
		ParentHash:       h.ParentHash,
		UncleHash:        h.UncleHash,
		Miner:            h.Coinbase,
		StateRoot:        h.Root,
		TxRoot:           h.TxHash,
		ReceiptRoot:      h.ReceiptHash,
		Bloom:            h.Bloom.Bytes(),
		Difficulty:       h.Difficulty,
		Number:           h.Number.Uint64(),
		GasLimit:         h.GasLimit,
		GasUsed:          h.GasUsed,
		Time:             h.Time,
		Extra:            h.Extra,
		MixDigest:        h.MixDigest,
		Nonce:            h.Nonce.Uint64(),
		BaseFee:          h.BaseFee,
		WithdrawalsRoot:  h.WithdrawalsHash,
		BlobGasUsed:      h.BlobGasUsed,
		ExcessBlobGas:    h.ExcessBlobGas,
		ParentBeaconRoot: h.ParentBeaconRoot,
		RequestsHash:     h.RequestsHash,
	}
}

// DecodeReceipts implements chains.Profile.
func (p *Profile) DecodeReceipts(raw json.RawMessage) ([]*model.Receipt, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("evm: decode receipts: %w", err)
	}
	out := make([]*model.Receipt, len(items))
	err := parallelFor(len(items), func(i int) error {
		var r types.Receipt
		if err := r.UnmarshalJSON(items[i]); err != nil {
			return fmt.Errorf("evm: receipt %d: %w", i, err)
		}
		out[i] = gethconv.ReceiptFromGeth(&r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parallelDecodeMin is the transaction or receipt count from which a
// block's items are decoded on several cores. Decoding is dominated by
// signature recovery (tens of microseconds per transaction), so small blocks
// stay sequential to avoid scheduling overhead.
const parallelDecodeMin = 32

// decodeTxs decodes a block's transactions, in parallel for large blocks.
// Transactions are independent: each recovers and checks its own sender.
func (p *Profile) decodeTxs(raws []json.RawMessage) ([]*model.Transaction, error) {
	out := make([]*model.Transaction, len(raws))
	err := parallelFor(len(raws), func(i int) error {
		tx, err := p.decodeTx(raws[i])
		if err != nil {
			return fmt.Errorf("tx %d: %w", i, err)
		}
		out[i] = tx
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parallelFor runs fn for 0..n-1, on several cores when n reaches
// parallelDecodeMin, and returns the error of the lowest failing index.
func parallelFor(n int, fn func(i int) error) error {
	if n < parallelDecodeMin {
		for i := 0; i < n; i++ {
			if err := fn(i); err != nil {
				return err
			}
		}
		return nil
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	errs := make([]error, n)
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				errs[i] = fn(i)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
