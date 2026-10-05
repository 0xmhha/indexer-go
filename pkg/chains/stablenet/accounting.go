package stablenet

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Native balance accounting under go-stablenet's rules (Anzeon, WBFT):
//
//   - every native value move, top-level or internal, mint or burn, emits
//     Transfer(from, to, value) from the NativeCoinAdapter contract
//     (core/vm/evm.go AddTransferLog); a reverted move leaves no log, so the
//     logs are the complete list of moves;
//   - the gas payer (the fee payer of a fee delegation transaction) pays
//     gasUsed * effective gas price; the coinbase receives the tip,
//     gasUsed * (price - baseFee) (core/state_transition.go);
//   - the base fee of the block, baseFee * gasUsed, is split among the
//     validators of the epoch the parent block belongs to by diligence, the
//     remainder (dust) going to the coinbase (consensus/wbft/engine
//     distributeBaseFee). This happens at finalization and leaves no log.
//
// Known gap: a pre-Cancun SELFDESTRUCT to the destructed contract itself
// burns its balance, but the log shows a move from the contract to itself.

// NativeCoinAdapterAddress is the default NativeCoinAdapter (genesis config
// Anzeon.SystemContracts.NativeCoinAdapter).
var NativeCoinAdapterAddress = common.HexToAddress("0x0000000000000000000000000000000000001000")

var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

// Accounting implements chains.NativeAccounting for StableNet.
type Accounting struct {
	NativeCoin common.Address

	mu    sync.Mutex
	epoch *epochCache
}

// epochCache remembers the newest epoch block seen while processing blocks
// in order, so the next block's validator set needs no lookup.
type epochCache struct {
	after     common.Hash // hash of the last processed block
	afterNum  uint64
	epochInfo *epochInfo // epoch info valid for the block after it
}

var _ chains.NativeAccounting = (*Accounting)(nil)

// NewAccounting returns the StableNet accounting rules.
func NewAccounting() *Accounting { return &Accounting{NativeCoin: NativeCoinAdapterAddress} }

// NativeDeltas implements chains.NativeAccounting.
func (a *Accounting) NativeDeltas(ctx context.Context, b *model.Block, rs []*model.Receipt, env chains.AccountingEnv) ([]chains.BalanceDelta, error) {
	var out []chains.BalanceDelta
	tips := new(big.Int)
	for i, tx := range b.Transactions {
		if i >= len(rs) || rs[i] == nil {
			continue
		}
		out = append(out, chains.TxFeeDeltas(b, tx, rs[i], tips)...)
		for _, l := range rs[i].Logs {
			from, to, value, ok := a.nativeTransfer(l)
			if !ok {
				continue
			}
			if from != (common.Address{}) {
				out = append(out, chains.BalanceDelta{Address: from, Delta: new(big.Int).Neg(value), TxHash: tx.Hash, Reason: "transfer"})
			}
			if to != (common.Address{}) {
				out = append(out, chains.BalanceDelta{Address: to, Delta: value, TxHash: tx.Hash, Reason: "transfer"})
			}
		}
	}
	if tips.Sign() > 0 {
		out = append(out, chains.BalanceDelta{Address: b.Miner, Delta: tips, Reason: "tip"})
	}
	dist, err := a.baseFeeDistribution(ctx, b, env)
	if err != nil {
		return nil, err
	}
	return append(out, dist...), nil
}

// nativeTransfer decodes a NativeCoinAdapter Transfer log.
func (a *Accounting) nativeTransfer(l *model.Log) (from, to common.Address, value *big.Int, ok bool) {
	if l.Address != a.NativeCoin || len(l.Topics) != 3 || l.Topics[0] != transferTopic || len(l.Data) != 32 {
		return from, to, nil, false
	}
	value = new(big.Int).SetBytes(l.Data)
	if value.Sign() == 0 {
		return from, to, nil, false
	}
	return common.BytesToAddress(l.Topics[1].Bytes()), common.BytesToAddress(l.Topics[2].Bytes()), value, true
}

// baseFeeDistribution splits the block's base fee among the validators of
// the epoch of block-1 by diligence; integer division leaves dust for the
// coinbase.
func (a *Accounting) baseFeeDistribution(ctx context.Context, b *model.Block, env chains.AccountingEnv) ([]chains.BalanceDelta, error) {
	info, err := a.epochFor(ctx, b, env)
	if err != nil {
		return nil, err
	}
	if b.Number == 0 || b.GasUsed == 0 || b.BaseFee == nil {
		return nil, nil
	}
	if info == nil {
		return nil, fmt.Errorf("stablenet: no epoch info for block %d", b.Number)
	}
	total := new(big.Int).Mul(b.BaseFee, new(big.Int).SetUint64(b.GasUsed))
	var sum uint64
	for _, idx := range info.Validators {
		if int(idx) >= len(info.Candidates) {
			return nil, fmt.Errorf("stablenet: validator index %d out of range in epoch info for block %d", idx, b.Number)
		}
		sum += info.Candidates[idx].Diligence
	}
	var out []chains.BalanceDelta
	dust := new(big.Int).Set(total)
	if sum != 0 {
		for _, idx := range info.Validators {
			c := info.Candidates[idx]
			share := new(big.Int).Mul(total, new(big.Int).SetUint64(c.Diligence))
			share.Div(share, new(big.Int).SetUint64(sum))
			if share.Sign() == 0 {
				continue
			}
			dust.Sub(dust, share)
			out = append(out, chains.BalanceDelta{Address: c.Addr, Delta: share, Reason: "base fee"})
		}
	}
	if dust.Sign() > 0 {
		out = append(out, chains.BalanceDelta{Address: b.Miner, Delta: dust, Reason: "base fee dust"})
	}
	return out, nil
}

// epochFor returns the epoch info that applies to block b: the one recorded
// in the last epoch block at or below b-1 (go-stablenet getEpochInfo). It
// keeps the newest epoch info while blocks are processed in order and looks
// back through earlier blocks otherwise (restart, gap, rollback).
func (a *Accounting) epochFor(ctx context.Context, b *model.Block, env chains.AccountingEnv) (*epochInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var info *epochInfo
	switch {
	case b.Number == 0:
		// Genesis: nothing to distribute.
	case a.epoch != nil && a.epoch.afterNum == b.Number-1 && a.epoch.after == b.ParentHash:
		info = a.epoch.epochInfo
	default:
		var err error
		if info, err = a.lookBack(ctx, b.Number-1, env); err != nil {
			return nil, err
		}
	}
	// What the next block will use: this block's epoch info if it is an
	// epoch block, otherwise the same as this block.
	next := info
	if own, err := ExtraEpochInfo(b.Extra); err != nil {
		return nil, fmt.Errorf("stablenet: block %d: %w", b.Number, err)
	} else if own != nil {
		next = own
	}
	a.epoch = &epochCache{after: b.Hash, afterNum: b.Number, epochInfo: next}
	return info, nil
}

// lookBack finds the newest block at or below n that carries epoch info.
func (a *Accounting) lookBack(ctx context.Context, n uint64, env chains.AccountingEnv) (*epochInfo, error) {
	if env == nil {
		return nil, fmt.Errorf("stablenet: cannot read earlier blocks for epoch info")
	}
	for h := n; ; h-- {
		blk, err := env.Block(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("stablenet: read block %d for epoch info: %w", h, err)
		}
		info, err := ExtraEpochInfo(blk.Extra)
		if err != nil {
			return nil, fmt.Errorf("stablenet: block %d: %w", h, err)
		}
		if info != nil {
			return info, nil
		}
		if h == 0 {
			return nil, nil
		}
	}
}

// Reset forgets the remembered epoch (after a rollback).
func (a *Accounting) Reset() {
	a.mu.Lock()
	a.epoch = nil
	a.mu.Unlock()
}

// epochInfo is the EpochInfo element of WBFT extra data
// (core/types/istanbul.go).
type epochInfo struct {
	Candidates    []*epochCandidate
	Validators    []uint32
	BLSPublicKeys [][]byte
}

type epochCandidate struct {
	Addr      common.Address
	Diligence uint64
}

// extraEpochInfo is the position of the epoch info in the WBFT extra list.
const extraEpochInfo = 9

// ExtraEpochInfo decodes the epoch info of WBFT extra data; it is nil for
// blocks that are not epoch blocks and for extra data that is not WBFT.
func ExtraEpochInfo(extra []byte) (*epochInfo, error) {
	var elems []rlp.RawValue
	if err := rlp.DecodeBytes(extra, &elems); err != nil || len(elems) != extraFields {
		return nil, nil
	}
	raw := elems[extraEpochInfo]
	if len(raw) == 1 && (raw[0] == 0xc0 || raw[0] == 0x80) {
		return nil, nil // absent
	}
	var info epochInfo
	if err := rlp.DecodeBytes(raw, &info); err != nil {
		return nil, fmt.Errorf("decode WBFT epoch info: %w", err)
	}
	return &info, nil
}
