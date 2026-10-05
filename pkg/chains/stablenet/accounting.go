package stablenet

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

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
	epochs     EpochTracker
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
	info, err := a.epochs.For(ctx, b, env)
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
