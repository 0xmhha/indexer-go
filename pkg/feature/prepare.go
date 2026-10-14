package feature

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/errgroup"
)

// BlockPreparer optionally acquires immutable, block-specific inputs outside
// the writer. Calls for different blocks may overlap. It must not write storage,
// publish events, or change the block. The returned handler runs in the usual
// ordered block transaction. A nil Handler retains the original handler.
type BlockPreparer interface {
	PrepareBlock(context.Context, *Block) (PreparedHandler, error)
}

// PreparedHandler owns one block's inputs, acquired by hash when reading state.
// ReadState asks the fetcher to recheck canonicality immediately before writing.
// Bytes accounts for retained state payload, not the Go heap or block data.
type PreparedHandler struct {
	Handler   BlockHandler
	ReadState bool
	Bytes     int64
}

// PreparedBlock is bound to one pipeline and one block. Sending it through a
// channel transfers ownership: neither producer nor consumer mutates its inputs.
type PreparedBlock struct {
	number    uint64
	hash      common.Hash
	handlers  []namedHandler
	readState bool
	bytes     int64
}

func (p *PreparedBlock) ReadsState() bool { return p != nil && p.readState }
func (p *PreparedBlock) Bytes() int64 {
	if p == nil {
		return 0
	}
	return p.bytes
}

// PrepareBlock prepares eligible handlers in feature order. Legacy handlers
// remain unchanged, including custom handlers without BlockPreparer.
func (p *Pipeline) PrepareBlock(ctx context.Context, b *Block) (*PreparedBlock, error) {
	if p == nil {
		return nil, nil
	}
	out := &PreparedBlock{number: b.Model.Number, hash: b.Model.Hash}
	for _, h := range p.handlers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if preparer, ok := h.h.(BlockPreparer); ok {
			prepared, err := preparer.PrepareBlock(ctx, b)
			if err != nil {
				return nil, fmt.Errorf("prepare feature %q: %w", h.unit(), err)
			}
			if prepared.Bytes < 0 || prepared.Bytes > (1<<63-1)-out.bytes {
				return nil, fmt.Errorf("prepare feature %q: invalid payload size", h.unit())
			}
			if prepared.Handler != nil {
				h.h = prepared.Handler
			}
			out.readState = out.readState || prepared.ReadState
			out.bytes += prepared.Bytes
		}
		out.handlers = append(out.handlers, h)
	}
	return out, nil
}

// HandleBlock applies prepared inputs inside the writer transaction. Never
// reuse inputs for another height or fork, even when transactions are identical.
func (p *PreparedBlock) HandleBlock(ctx context.Context, b *Block) error {
	if p.number != b.Model.Number || p.hash != b.Model.Hash {
		return fmt.Errorf("prepared inputs do not belong to block %d (%s)", b.Model.Number, b.Model.Hash)
	}
	for _, h := range p.handlers {
		if err := h.h.HandleBlock(ctx, b); err != nil {
			return fmt.Errorf("feature %q: %w", h.unit(), err)
		}
	}
	return nil
}

// PrepareReads runs a bounded group of read-only jobs, sharing the pipeline's
// permits across blocks and features. Every goroutine finishes before return.
// Jobs may retain speculative errors for the writer to decide if needed.
func (d Deps) PrepareReads(ctx context.Context, count int, read func(context.Context, int) error) error {
	g, jobCtx := errgroup.WithContext(ctx)
	g.SetLimit(max(1, d.PrepareConcurrency))
	for i := 0; i < count; i++ {
		if err := jobCtx.Err(); err != nil {
			break
		}
		g.Go(func() error {
			if d.prepareSlots != nil {
				select {
				case d.prepareSlots <- struct{}{}:
				case <-jobCtx.Done():
					return jobCtx.Err()
				}
				defer func() { <-d.prepareSlots }()
			}
			return read(jobCtx, i)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}
