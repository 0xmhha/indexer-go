package execution

import (
	"bytes"
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
	Origin    port.ExecutionIngressOriginResolver
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
	if err := ctx.Err(); err != nil {
		return err
	}
	var archive *port.ExecutionIngressArchive
	if w == port.ExecutionBlocks && b.Origin != nil {
		input := port.ExecutionIngressInput{Chain: p.chain, Ref: p.result.Ref, Parent: p.result.Parent, Digest: p.digest, Root: p.result.StateRoot, ReceiptsRoot: p.result.ReceiptsRoot, BlockRLP: bytes.Clone(p.result.BlockRLP), ReceiptsRLP: bytes.Clone(p.result.ReceiptsRLP)}
		origin, release, err := b.Origin.ResolveExecutionOrigin(ctx, input)
		if release != nil {
			defer release()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if release == nil {
			return port.ErrExecutionConflict
		}
		original := port.ExecutionIngressInput{Ref: p.result.Ref, Parent: p.result.Parent, Digest: p.digest, BlockRLP: p.result.BlockRLP, ReceiptsRLP: p.result.ReceiptsRLP}
		if err := matchIngressArchive(origin, original); err != nil {
			return err
		}
		archive = &origin
	}
	part := port.ExecutionPart{Writer: w, Ref: p.result.Ref, Parent: p.result.Parent, Digest: p.digest}
	return b.Store.WriteExecutionPart(ctx, part, func(bound context.Context) error {
		switch w {
		case port.ExecutionTransactions:
			if err := b.Store.ApplyExecutionCore(bound, w, p.block, p.receipts); err != nil {
				return err
			}
			if p.result.CallTraces != nil {
				writer, ok := b.Store.(port.ExecutionCallWriter)
				if !ok {
					return port.ErrInvalidData
				}
				if e := writer.ApplyExecutionCalls(bound, p.result, p.block, p.receipts); e != nil {
					return e
				}
			}
			if p.result.BlobBundle == nil {
				return nil
			}
			writer, ok := b.Store.(port.ExecutionBlobWriter)
			if !ok {
				return port.ErrInvalidData
			}
			return writer.ApplyExecutionBlobs(bound, p.chain, p.result, p.block)
		case port.ExecutionBlocks:
			if err := b.Store.ApplyExecutionCore(bound, w, p.block, p.receipts); err != nil {
				return err
			}
			if archive == nil {
				return nil
			}
			writer, ok := b.Store.(port.ExecutionIngressArchiveWriter)
			if !ok {
				return port.ErrInvalidData
			}
			return writer.ApplyExecutionIngress(bound, *archive)
		case port.ExecutionReceipts, port.ExecutionLogs:
			return b.Store.ApplyExecutionCore(bound, w, p.block, p.receipts)
		case port.ExecutionAccounts:
			if e := b.Store.ApplyExecutionState(bound, w, p.result); e != nil {
				return e
			}
			if p.result.StateProofBundle == nil {
				return nil
			}
			writer, ok := b.Store.(port.ExecutionProofWriter)
			if !ok {
				return port.ErrInvalidData
			}
			return writer.ApplyExecutionProofs(bound, p.chain, p.result)
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
