package balance

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/ethereum/go-ethereum/common"
)

// applyExecutionBalances projects the verified execution journal. It must
// never recalculate native accounting or repair disagreement through RPC.
func (h *handler) applyExecutionBalances(ctx context.Context, b *feature.Block) error {
	changes, err := h.deps.NativeBalances(ctx, b)
	if err != nil {
		return err
	}
	if err = feature.ValidateNativeBalances(changes); err != nil {
		return err
	}
	if b == nil || b.Model == nil {
		return port.ErrInvalidData
	}
	current := make(map[common.Address]*big.Int)
	for _, change := range changes {
		if err = ctx.Err(); err != nil {
			return err
		}
		if change.Before.Cmp(change.After) == 0 {
			continue
		}
		before, known := current[change.Address]
		if !known {
			recorded, e := h.records.HasBalanceRecord(ctx, change.Address)
			if e != nil {
				return e
			}
			if recorded {
				before, err = h.r.GetAddressBalance(ctx, change.Address, b.Model.Number)
				if err != nil {
					return err
				}
			} else {
				before = new(big.Int).Set(change.Before)
				if err = h.w.SetBalance(ctx, change.Address, b.Model.Number, before); err != nil {
					return err
				}
			}
		}
		if before == nil || before.Cmp(change.Before) != 0 {
			return fmt.Errorf("%w: native balance before %s at %d/%d", port.ErrExecutionConflict, change.Address.Hex(), b.Model.Number, change.Ordinal)
		}
		delta := new(big.Int).Sub(change.After, change.Before)
		if err = h.w.UpdateBalance(ctx, change.Address, b.Model.Number, delta, change.TxHash); err != nil {
			return err
		}
		current[change.Address] = new(big.Int).Set(change.After)
	}
	return nil
}
