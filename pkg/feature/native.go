package feature

import (
	"bytes"
	"context"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

type NativeBalanceChange struct {
	Address       common.Address
	Before, After *big.Int
	TxHash        common.Hash
	Ordinal       uint32
}

type nativeBalanceKey struct{}
type nativeBalanceInput struct {
	height  uint64
	hash    common.Hash
	changes []NativeBalanceChange
}

// ValidateNativeBalances requires deterministic ordinal/address order and
// unsigned 256-bit amounts. Finalize changes use the zero transaction hash.
func ValidateNativeBalances(changes []NativeBalanceChange) error {
	for i, c := range changes {
		if c.Before == nil || c.After == nil || c.Before.Sign() < 0 || c.After.Sign() < 0 || c.Before.BitLen() > 256 || c.After.BitLen() > 256 {
			return port.ErrInvalidData
		}
		if i > 0 {
			prev := changes[i-1]
			if c.Ordinal < prev.Ordinal || c.Ordinal == prev.Ordinal && bytes.Compare(c.Address[:], prev.Address[:]) <= 0 {
				return port.ErrInvalidData
			}
		}
	}
	return nil
}
func copyNativeBalances(changes []NativeBalanceChange) []NativeBalanceChange {
	copy := make([]NativeBalanceChange, len(changes))
	for i, c := range changes {
		copy[i] = c
		copy[i].Before = new(big.Int).Set(c.Before)
		copy[i].After = new(big.Int).Set(c.After)
	}
	return copy
}
func WithNativeBalances(ctx context.Context, height uint64, hash common.Hash, changes []NativeBalanceChange) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateNativeBalances(changes); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, nativeBalanceKey{}, nativeBalanceInput{height: height, hash: hash, changes: copyNativeBalances(changes)}), nil
}
func NativeBalancesFromContext(ctx context.Context, b *Block) ([]NativeBalanceChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input, ok := ctx.Value(nativeBalanceKey{}).(nativeBalanceInput)
	if !ok {
		return nil, port.ErrExecutionCoverage
	}
	if b == nil || b.Model == nil || b.Model.Number != input.height || b.Model.Hash != input.hash {
		return nil, port.ErrExecutionConflict
	}
	return copyNativeBalances(input.changes), nil
}
