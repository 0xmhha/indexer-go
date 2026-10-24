package feature

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// WriterPlan owns one block's feature-unit writers. Apply runs their SQL in
// dependency order in the caller's publication transaction, never committing.
type WriterPlan struct {
	height             uint64
	hash, parent, root common.Hash
	writers            []unitWriter
}

type unitWriter struct {
	unit     Unit
	handlers []BlockHandler
}

func (p *Pipeline) PrepareWriters(ctx context.Context, b *Block, prepare bool, maxBytes int64) (*WriterPlan, error) {
	if b == nil || b.Model == nil || maxBytes <= 0 {
		return nil, port.ErrInvalidData
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := &WriterPlan{height: b.Model.Number, hash: b.Model.Hash, parent: b.Model.ParentHash, root: b.Model.StateRoot}
	if p == nil {
		return out, nil
	}
	var retained int64
	seen := map[string]bool{}
	for _, h := range p.handlers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if h.h == nil || h.unit() == "" {
			return nil, port.ErrInvalidData
		}
		if len(out.writers) == 0 || out.writers[len(out.writers)-1].unit.Name != h.unit() {
			if seen[h.unit()] {
				return nil, fmt.Errorf("%w: discontiguous feature unit %s", port.ErrInvalidData, h.unit())
			}
			seen[h.unit()] = true
			out.writers = append(out.writers, unitWriter{unit: Unit{Name: h.unit(), Definition: h.def}})
		}
		writer := &out.writers[len(out.writers)-1]
		if writer.unit.Definition != h.def {
			return nil, port.ErrInvalidData
		}
		handler := h.h
		if preparer, ok := handler.(BlockPreparer); prepare && ok {
			input, err := preparer.PrepareBlock(ctx, b)
			if err != nil {
				return nil, fmt.Errorf("prepare feature writer %q: %w", h.unit(), err)
			}
			if input.Bytes < 0 || input.Bytes > maxBytes-retained {
				return nil, fmt.Errorf("feature writers exceed state_max_bytes")
			}
			retained += input.Bytes
			if input.Handler != nil {
				handler = input.Handler
			}
		}
		if !out.matches(b) {
			return nil, port.ErrExecutionConflict
		}
		writer.handlers = append(writer.handlers, handler)
	}
	return out, nil
}

func (p *WriterPlan) matches(b *Block) bool {
	return p != nil && b != nil && b.Model != nil && b.Model.Number == p.height && b.Model.Hash == p.hash && b.Model.ParentHash == p.parent && b.Model.StateRoot == p.root
}

func (p *WriterPlan) Apply(ctx context.Context, b *Block, checkpoint func(context.Context, Unit) error) error {
	if !p.matches(b) {
		return port.ErrExecutionConflict
	}
	if checkpoint == nil {
		return port.ErrInvalidData
	}
	for _, writer := range p.writers {
		for _, h := range writer.handlers {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !p.matches(b) {
				return port.ErrExecutionConflict
			}
			if err := h.HandleBlock(ctx, b); err != nil {
				return fmt.Errorf("feature writer %q: %w", writer.unit.Name, err)
			}
			if !p.matches(b) {
				return port.ErrExecutionConflict
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkpoint(ctx, writer.unit); err != nil {
			return fmt.Errorf("checkpoint feature writer %q: %w", writer.unit.Name, err)
		}
		if !p.matches(b) {
			return port.ErrExecutionConflict
		}
	}
	return ctx.Err()
}
