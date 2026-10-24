package balance

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/rpcbatch"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

type preparedBalance struct {
	value   *big.Int
	err     error
	skipped bool
}

// PrepareBlock computes profile deltas and acquires initial balances before
// entering the writer. The writer rechecks record existence; speculative
// errors for accounts initialized by earlier in-flight blocks are ignored.
func (h *handler) PrepareBlock(ctx context.Context, b *feature.Block) (feature.PreparedHandler, error) {
	if h.deps.NativeBalances != nil {
		return feature.PreparedHandler{}, ctx.Err()
	}
	parallel, ok := h.accounting.(chains.ConcurrentAccounting)
	if !ok || !parallel.ConcurrentAccounting() || h.deps.BalanceAtHash == nil {
		return feature.PreparedHandler{}, nil
	}
	deltas, err := h.accounting.NativeDeltas(ctx, b.Model, b.Receipts, h.blocks)
	if err != nil {
		return feature.PreparedHandler{}, fmt.Errorf("native balance changes of block %d: %w", b.Model.Number, err)
	}
	merged := mergeDeltas(deltas)
	changed := map[common.Address]bool{}
	for _, d := range merged {
		if d.Delta.Sign() != 0 {
			changed[d.Address] = true
		}
	}
	if b.Model.Number == 0 {
		changed[b.Model.Miner] = true
	}
	var addresses []common.Address
	for _, addr := range sortedAddresses(changed) {
		recorded, err := h.records.HasBalanceRecord(ctx, addr)
		if err != nil {
			return feature.PreparedHandler{}, fmt.Errorf("prepare balance record %s: %w", addr, err)
		}
		if !recorded {
			addresses = append(addresses, addr)
		}
	}
	at := new(big.Int).SetUint64(b.Model.Number)
	if b.Model.Number > 0 {
		at.Sub(at, big.NewInt(1))
	}
	values := make([]preparedBalance, len(addresses))
	hash := b.Model.Hash
	if b.Model.Number > 0 {
		hash = b.Model.ParentHash
	}
	readBalance := func(ctx context.Context, i int) error {
		values[i].value, values[i].err = h.deps.BalanceAtHash(ctx, addresses[i], hash)
		return preparationReadError(ctx, values[i].err)
	}
	// Check after obtaining the shared read permit: an earlier block may
	// initialize the account while this job waits. Do not reuse another
	// block's speculative state; only committed records suppress a read.
	needsRead := func(ctx context.Context, i int) (bool, error) {
		recorded, err := h.records.HasBalanceRecord(ctx, addresses[i])
		if err != nil {
			return false, fmt.Errorf("prepare balance record %s: %w", addresses[i], err)
		}
		values[i].skipped = recorded
		return !recorded, nil
	}
	readOne := func(ctx context.Context, i int) error {
		needed, err := needsRead(ctx, i)
		if err != nil || !needed {
			return err
		}
		return readBalance(ctx, i)
	}
	batchSize := max(1, min(64, h.deps.StateBatchSize))
	var readErr error
	if batchSize == 1 || h.deps.BalanceBatchAtHash == nil {
		readErr = h.deps.PrepareReads(ctx, len(addresses), readOne)
	} else {
		readErr = h.deps.PrepareReads(ctx, (len(addresses)+batchSize-1)/batchSize, func(ctx context.Context, batch int) error {
			from, to := batch*batchSize, min((batch+1)*batchSize, len(addresses))
			var indices []int
			var pending []common.Address
			for i := from; i < to; i++ {
				needed, err := needsRead(ctx, i)
				if err != nil {
					return err
				}
				if needed {
					indices = append(indices, i)
					pending = append(pending, addresses[i])
				}
			}
			if len(pending) == 0 {
				return nil
			}
			out, failures, err := h.deps.BalanceBatchAtHash(ctx, pending, hash)
			if errors.Is(err, rpcbatch.ErrUnsupported) {
				// The outer job already holds a shared permit. Re-entering
				// PrepareReads here would deadlock all permits on fallback.
				for _, i := range indices {
					if err := readBalance(ctx, i); err != nil {
						return err
					}
				}
				return nil
			}
			if err == nil && (len(out) != len(pending) || len(failures) != len(pending)) {
				err = fmt.Errorf("invalid balance batch result length")
			}
			for result, i := range indices {
				if err != nil {
					values[i].err = err
				} else {
					values[i].value, values[i].err = out[result], failures[result]
					if values[i].value == nil && values[i].err == nil {
						values[i].err = fmt.Errorf("balance batch has no result for %s", addresses[i])
					}
					if values[i].value != nil && values[i].value.Sign() < 0 && values[i].err == nil {
						values[i].err = fmt.Errorf("balance batch has a negative result for %s", addresses[i])
					}
				}
				if err := preparationReadError(ctx, values[i].err); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if readErr != nil {
		return feature.PreparedHandler{}, readErr
	}
	balances := make(map[common.Address]preparedBalance, len(addresses))
	var size int64
	for i, addr := range addresses {
		if values[i].skipped {
			continue // recorded after waiting; rollback uses the writer fallback
		}
		balances[addr] = values[i]
		size += 96
		if values[i].value != nil {
			size += int64(len(values[i].value.Bytes()))
		}
		if values[i].err != nil {
			size += int64(len(values[i].err.Error()))
		}
	}
	for _, d := range merged {
		size += 96 + int64(len(d.Reason)+len(d.Delta.Bytes()))
	}
	copyHandler := *h
	copyHandler.balanceAt = func(ctx context.Context, addr common.Address, block *big.Int) (*big.Int, error) {
		if block != nil && block.Cmp(at) == 0 {
			if v, ok := balances[addr]; ok {
				if v.value == nil {
					return nil, v.err
				}
				return new(big.Int).Set(v.value), v.err
			}
		}
		// Resync depends on ordered DB updates; absent speculative inputs
		// (e.g. a concurrent rollback) are read on the writer as before.
		return h.balanceAt(ctx, addr, block)
	}
	return feature.PreparedHandler{Handler: feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		return copyHandler.applyDeltas(ctx, b, merged)
	}), ReadState: len(balances) > 0, Bytes: size}, nil
}

func preparationReadError(ctx context.Context, err error) error {
	var reply rpc.Error
	if errors.As(err, &reply) && (reply.ErrorCode() == -32601 || reply.ErrorCode() == -32602) {
		return err // RPC capability errors must not become zero balances.
	}
	return ctx.Err()
}
