package fetch

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// Backfill runs p's handlers for the stored blocks from..to, in order, one
// block transaction per block. progress is called inside each block's
// transaction after the handlers, so the caller can record how far the
// backfill got atomically with the block's writes; a backfill stopped
// half-way therefore resumes after the last committed block. It reads blocks
// and receipts from storage, never from the node.
func (f *Fetcher) Backfill(ctx context.Context, p *feature.Pipeline, from, to uint64, progress func(ctx context.Context, height uint64) error) error {
	if f.txr == nil {
		return fmt.Errorf("backfill needs storage with block transactions")
	}
	if f.declared {
		return f.backfillFromNode(ctx, p, from, to, progress)
	}
	mr := f.storage
	for h := from; h <= to; h++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := mr.GetBlock(ctx, h)
		if err != nil {
			return fmt.Errorf("backfill: read block %d: %w", h, err)
		}
		rs := make([]*model.Receipt, 0, len(b.Transactions))
		for _, tx := range b.Transactions {
			r, err := mr.GetReceipt(ctx, tx.Hash)
			if err != nil {
				return fmt.Errorf("backfill: read receipt %s: %w", tx.Hash.Hex(), err)
			}
			rs = append(rs, r)
		}
		fb, err := fetchedFromModel(b, rs)
		if err != nil {
			return fmt.Errorf("backfill: block %d: %w", h, err)
		}

		if err := f.backfillBlock(ctx, p, fb, progress); err != nil {
			return err
		}
		if h%1000 == 0 || h == to {
			f.logger.Info("Backfill progress", zap.Strings("features", p.Features()), zap.Uint64("height", h), zap.Uint64("to", to))
		}
	}
	return nil
}

type undoDropper interface {
	DropUndo(ctx context.Context, height uint64) error
}

// backfillFromNode is Backfill in the declared mode: the storage keeps no
// logs, so the declared logs of [from, to] are read again from the node a
// LogRange at a time, and the features run on each block that has some.
// The progress of the heights without logs is recorded with the range's
// end, so a stopped backfill resumes after the last range it finished.
// Heights below the start height are not read: ingest starts there.
func (f *Fetcher) backfillFromNode(ctx context.Context, p *feature.Pipeline, from, to uint64, progress func(ctx context.Context, height uint64) error) error {
	from = max(from, min(f.config.StartHeight, to))
	for start := from; start <= to; {
		end := min(to, start+LogRange-1)
		blocks, err := f.readLogBlocks(ctx, start, end)
		if err != nil {
			return fmt.Errorf("backfill: read blocks %d..%d: %w", start, end, err)
		}
		last := start - 1
		for _, fb := range blocks {
			if err := f.backfillBlock(ctx, p, fb, progress); err != nil {
				return err
			}
			last = fb.height()
		}
		if progress != nil && (last < end || len(blocks) == 0) {
			if err := f.write().do(ctx, "backfillProgress", func(ctx context.Context) error {
				return f.writeBackfillProgress(ctx, end, progress)
			}); err != nil {
				return err
			}
		}
		f.logger.Info("Backfill progress", zap.Strings("features", p.Features()), zap.Uint64("height", end), zap.Uint64("to", to))
		if end == to {
			break
		}
		start = end + 1
	}
	return nil
}
