package fetch

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/feature"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
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
	mr, ok := f.storage.(storagepkg.ModelReader)
	if !ok {
		return fmt.Errorf("backfill needs storage that keeps the chain-neutral model")
	}
	for h := from; h <= to; h++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := mr.GetModelBlock(ctx, h)
		if err != nil {
			return fmt.Errorf("backfill: read block %d: %w", h, err)
		}
		rs := make([]*model.Receipt, 0, len(b.Transactions))
		for _, tx := range b.Transactions {
			r, err := mr.GetModelReceipt(ctx, tx.Hash)
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
