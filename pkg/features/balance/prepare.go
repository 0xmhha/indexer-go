package balance

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

type preparedBalance struct {
	value *big.Int
	err   error
}

// PrepareBlock computes profile deltas and acquires initial balances before
// entering the writer. The writer rechecks record existence; speculative
// errors for accounts initialized by earlier in-flight blocks are ignored.
func (h *handler) PrepareBlock(ctx context.Context, b *feature.Block) (feature.PreparedHandler, error) {
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
	if err := h.deps.PrepareReads(ctx, len(addresses), func(ctx context.Context, i int) error {
		values[i].value, values[i].err = h.deps.BalanceAtHash(ctx, addresses[i], hash)
		var reply rpc.Error
		if errors.As(values[i].err, &reply) && (reply.ErrorCode() == -32601 || reply.ErrorCode() == -32602) {
			return values[i].err // RPC capability errors must not become zero balances.
		}
		return ctx.Err()
	}); err != nil {
		return feature.PreparedHandler{}, err
	}
	balances := make(map[common.Address]preparedBalance, len(addresses))
	var size int64
	for i, addr := range addresses {
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
	}), ReadState: len(addresses) > 0, Bytes: size}, nil
}
