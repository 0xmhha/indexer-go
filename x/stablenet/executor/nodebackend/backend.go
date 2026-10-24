// Package nodebackend adapts the unmodified pinned StableNet execution client.
// It is isolated from the indexer's geth version and PostgreSQL dependency.
package nodebackend

import (
	"context"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/exporter"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

type chainReader interface {
	executor.Chain
	CurrentBlock() *types.Header
	GetHeaderByNumber(uint64) *types.Header
	GetBlock(common.Hash, uint64) *types.Block
	GetReceiptsByHash(common.Hash) types.Receipts
	StateCache() state.Database
}
type Backend struct {
	chain  chainReader
	replay func(context.Context, executor.ReplayInput) (execwire.Result, error)
}

var _ exporter.Backend = (*Backend)(nil)

func (b *Backend) Head(ctx context.Context) (execwire.Ref, error) {
	if err := ctx.Err(); err != nil {
		return execwire.Ref{}, err
	}
	if b == nil || b.chain == nil || b.chain.CurrentBlock() == nil {
		return execwire.Ref{}, exporter.ErrCheckpointRecovery
	}
	h := b.chain.CurrentBlock()
	return execwire.Ref{Height: h.Number.Uint64(), Hash: execwire.Hash(h.Hash())}, nil
}
func (b *Backend) Canonical(ctx context.Context, n uint64) (execwire.Hash, error) {
	if err := ctx.Err(); err != nil {
		return execwire.Hash{}, err
	}
	if b == nil || b.chain == nil {
		return execwire.Hash{}, exporter.ErrCheckpointRecovery
	}
	h := b.chain.GetHeaderByNumber(n)
	if h == nil {
		return execwire.Hash{}, fmt.Errorf("%w: header %d", exporter.ErrCheckpointRecovery, n)
	}
	return execwire.Hash(h.Hash()), nil
}
func (b *Backend) Replay(ctx context.Context, n, generation uint64) (execwire.Result, error) {
	if err := ctx.Err(); err != nil {
		return execwire.Result{}, err
	}
	if n == 0 || b == nil || b.chain == nil {
		return execwire.Result{}, exporter.ErrCheckpointRecovery
	}
	h := b.chain.GetHeaderByNumber(n)
	parent := b.chain.GetHeaderByNumber(n - 1)
	if h == nil || parent == nil || h.ParentHash != parent.Hash() {
		return execwire.Result{}, fmt.Errorf("%w: block/parent %d", exporter.ErrCheckpointRecovery, n)
	}
	block := b.chain.GetBlock(h.Hash(), n)
	if block == nil {
		return execwire.Result{}, exporter.ErrCheckpointRecovery
	}
	replay := b.replay
	if replay == nil {
		replay = executor.Replay
	}
	return replay(ctx, executor.ReplayInput{Chain: b.chain, Database: b.chain.StateCache(), Parent: parent, Block: block, Receipts: b.chain.GetReceiptsByHash(h.Hash()), Generation: generation})
}
