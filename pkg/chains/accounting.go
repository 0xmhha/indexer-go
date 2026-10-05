package chains

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Native balance accounting. How a block changes native balances is a
// chain rule: who pays gas, where the tip and the base fee go, which value
// moves are visible. Chain profiles provide it (AccountingProfile); the
// balance feature only applies the deltas.

// BalanceDelta is one native balance change caused by a block.
type BalanceDelta struct {
	Address common.Address
	Delta   *big.Int
	TxHash  common.Hash // zero for block-level changes (fee distribution)
	Reason  string      // short label for logs: "value", "gas", "tip", ...
}

// AccountingEnv gives accounting rules access to earlier blocks.
type AccountingEnv interface {
	// Block returns block number, from the index or the node.
	Block(ctx context.Context, number uint64) (*model.Block, error)
}

// NativeAccounting derives the native balance changes of a block. rs are the
// block's receipts in transaction order.
type NativeAccounting interface {
	NativeDeltas(ctx context.Context, b *model.Block, rs []*model.Receipt, env AccountingEnv) ([]BalanceDelta, error)
}

// AccountingProfile is implemented by profiles with their own accounting
// rules.
type AccountingProfile interface {
	NativeAccounting() NativeAccounting
}

// NativeCoinProfile is implemented by profiles whose native coin is also
// exposed as a token contract that emits Transfer events for native value
// moves (StableNet's NativeCoinAdapter). Those events are native transfers,
// not token transfers.
type NativeCoinProfile interface {
	NativeCoinContract() (common.Address, bool)
}

// NativeCoinContract returns the profile's native coin contract, if any.
func NativeCoinContract(p Profile) (common.Address, bool) {
	if np, ok := p.(NativeCoinProfile); ok {
		return np.NativeCoinContract()
	}
	return common.Address{}, false
}

// AccountingOf returns the profile's accounting rules, or the Ethereum rules
// when the profile has none or is unknown.
func AccountingOf(p Profile) NativeAccounting {
	if ap, ok := p.(AccountingProfile); ok {
		if a := ap.NativeAccounting(); a != nil {
			return a
		}
	}
	return EthereumAccounting{}
}

// EthereumAccounting implements the Ethereum execution rules visible from a
// block and its receipts:
//
//   - a successful transaction moves its value from the sender to the
//     recipient (or the created contract); a failed one moves nothing;
//   - the gas payer (GasPayer) pays gasUsed * effective gas price, plus the
//     blob gas;
//   - the miner receives gasUsed * (price - baseFee); the base fee and blob
//     gas are burned (before London the whole fee goes to the miner);
//   - withdrawals credit their amount (in Gwei).
//
// Value moved by internal calls is not visible without traces and block
// rewards of proof-of-work chains are not derived; accounts affected by them
// drift from the node's balance.
type EthereumAccounting struct{}

var gwei = big.NewInt(1_000_000_000)

// NativeDeltas implements NativeAccounting.
func (EthereumAccounting) NativeDeltas(_ context.Context, b *model.Block, rs []*model.Receipt, _ AccountingEnv) ([]BalanceDelta, error) {
	var out []BalanceDelta
	minerFees := new(big.Int)
	for i, tx := range b.Transactions {
		if i >= len(rs) || rs[i] == nil {
			continue
		}
		r := rs[i]
		out = append(out, TxFeeDeltas(b, tx, r, minerFees)...)
		if r.Status == 1 {
			out = append(out, ValueDeltas(tx, r)...)
		}
	}
	if minerFees.Sign() > 0 {
		out = append(out, BalanceDelta{Address: b.Miner, Delta: minerFees, Reason: "tip"})
	}
	for _, w := range b.Withdrawals {
		amount := new(big.Int).Mul(new(big.Int).SetUint64(w.Amount), gwei)
		out = append(out, BalanceDelta{Address: w.Address, Delta: amount, Reason: "withdrawal"})
	}
	return out, nil
}

// TxFeeDeltas returns the gas payment of a transaction and adds the miner's
// share (the tip, or the whole fee before London) to minerFees.
func TxFeeDeltas(b *model.Block, tx *model.Transaction, r *model.Receipt, minerFees *big.Int) []BalanceDelta {
	price := r.EffectiveGasPrice
	if price == nil {
		price = tx.GasPrice
	}
	if price == nil {
		return nil
	}
	gasUsed := new(big.Int).SetUint64(r.GasUsed)
	cost := new(big.Int).Mul(gasUsed, price)
	if r.BlobGasPrice != nil && r.BlobGasUsed > 0 {
		cost.Add(cost, new(big.Int).Mul(new(big.Int).SetUint64(r.BlobGasUsed), r.BlobGasPrice))
	}
	if b.BaseFee == nil {
		minerFees.Add(minerFees, new(big.Int).Mul(gasUsed, price))
	} else if tip := new(big.Int).Sub(price, b.BaseFee); tip.Sign() > 0 {
		minerFees.Add(minerFees, tip.Mul(tip, gasUsed))
	}
	if cost.Sign() == 0 {
		return nil
	}
	return []BalanceDelta{{Address: GasPayer(tx), Delta: cost.Neg(cost), TxHash: tx.Hash, Reason: "gas"}}
}

// ValueDeltas returns the top-level value move of a successful transaction.
func ValueDeltas(tx *model.Transaction, r *model.Receipt) []BalanceDelta {
	if tx.Value == nil || tx.Value.Sign() <= 0 {
		return nil
	}
	to := tx.To
	if to == nil {
		to = r.ContractAddress
	}
	if to == nil {
		return nil
	}
	return []BalanceDelta{
		{Address: tx.From, Delta: new(big.Int).Neg(tx.Value), TxHash: tx.Hash, Reason: "value"},
		{Address: *to, Delta: new(big.Int).Set(tx.Value), TxHash: tx.Hash, Reason: "value"},
	}
}
