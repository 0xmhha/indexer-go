package execution

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type PartStore interface {
	WriteExecutionPart(context.Context, port.ExecutionPart, func(context.Context) error) error
	port.ExecutionCoreWriter
	port.ExecutionStateStore
}
type Publisher interface {
	PublishExecution(context.Context, execwire.Result, *model.Block, []*model.Receipt, execwire.Hash) error
}
type StoreBackend struct {
	Store     PartStore
	Publisher Publisher
}

func (b *StoreBackend) Part(ctx context.Context, w port.ExecutionWriter, p *Prepared) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("execution part %s: %w", w, err)
		}
	}()
	if b == nil || b.Store == nil || p == nil {
		return port.ErrInvalidData
	}
	valid := false
	for _, writer := range port.ExecutionWriters() {
		valid = valid || writer == w
	}
	if !valid {
		return port.ErrInvalidData
	}
	part := port.ExecutionPart{Writer: w, Ref: p.result.Ref, Parent: p.result.Parent, Digest: p.digest}
	return b.Store.WriteExecutionPart(ctx, part, func(bound context.Context) error {
		switch w {
		case port.ExecutionBlocks, port.ExecutionTransactions, port.ExecutionReceipts, port.ExecutionLogs:
			return b.Store.ApplyExecutionCore(bound, w, p.block, p.receipts)
		case port.ExecutionContracts:
			return b.Store.ApplyExecutionState(bound, w, p.result, p.creations...)
		default:
			return b.Store.ApplyExecutionState(bound, w, p.result)
		}
	})
}
func (b *StoreBackend) Publish(ctx context.Context, p *Prepared) error {
	if b == nil || b.Publisher == nil || p == nil {
		return port.ErrInvalidData
	}
	return b.Publisher.PublishExecution(ctx, p.result, p.block, p.receipts, p.digest)
}
func (p *Prepared) Ref() execwire.Ref     { return p.result.Ref }
func (p *Prepared) Digest() execwire.Hash { return p.digest }
