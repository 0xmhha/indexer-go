package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// ErrBlockConflict is returned when a block at an already indexed height has
// a different hash than the stored one. Reorg handling is not implemented yet
// (refactoring plan R2-4), so the fetcher stops instead of overwriting.
var ErrBlockConflict = errors.New("fetch: stored block differs from fetched block (reorg handling not implemented)")

// atomic reports whether blocks are indexed with one storage transaction each.
func (f *Fetcher) atomic() bool {
	return f.config.AtomicBlock && f.txr != nil
}

// SetBeforeCommitHook installs a function called after a block's writes are
// staged and before they commit. Returning an error aborts the block as a
// crash at that point would. It exists for fault-injection tests only.
func (f *Fetcher) SetBeforeCommitHook(hook func(height uint64) error) {
	f.beforeCommitHook = hook
}

// publish sends an event, or buffers it while a block transaction is open so
// that subscribers only see events for committed blocks.
func (f *Fetcher) publish(ev events.Event) bool {
	if f.pendingEvents != nil {
		*f.pendingEvents = append(*f.pendingEvents, ev)
		return true
	}
	if f.eventBus == nil {
		return false
	}
	return f.eventBus.Publish(ev)
}

// indexBlock stores one fetched block with all derived indexes and advances
// the cursor in a single storage transaction. It is used by both the live
// loop and gap recovery.
//
//   - A block whose hash is already stored at its height is skipped, so
//     processing a block twice never changes storage.
//   - The cursor only moves forward, so filling a gap below it does not
//     rewind it.
//   - Events are published only after the transaction commits.
func (f *Fetcher) indexBlock(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	height := block.NumberU64()

	stored, err := f.storage.GetBlock(ctx, height)
	switch {
	case err == nil && stored.Hash() == block.Hash():
		f.logger.Debug("Block already indexed, skipping", zap.Uint64("height", height))
		return f.advanceCursorOnly(ctx, height)
	case err == nil:
		return fmt.Errorf("%w: height %d stored %s fetched %s", ErrBlockConflict, height, stored.Hash().Hex(), block.Hash().Hex())
	case !errors.Is(err, storagepkg.ErrNotFound):
		return fmt.Errorf("check stored block %d: %w", height, err)
	}

	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return fmt.Errorf("begin block %d: %w", height, err)
	}
	defer tx.Rollback() // no-op after Commit

	var pending []events.Event
	f.pendingEvents = &pending
	defer func() { f.pendingEvents = nil }()

	if err := f.applyBlock(txCtx, block, receipts); err != nil {
		return err
	}
	if err := f.advanceCursor(txCtx, height); err != nil {
		return err
	}
	if f.beforeCommitHook != nil {
		if err := f.beforeCommitHook(height); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit block %d: %w", height, err)
	}

	f.pendingEvents = nil
	for _, ev := range pending {
		if !f.publish(ev) {
			f.logger.Warn("Failed to publish event (channel full)",
				zap.Uint64("height", height),
				zap.String("type", string(ev.Type())),
			)
		}
	}

	f.metrics.RecordBlockProcessed(len(receipts))
	f.logger.Info("Successfully indexed block",
		zap.Uint64("height", height),
		zap.String("hash", block.Hash().Hex()),
		zap.Int("txs", len(block.Transactions())),
		zap.Int("receipts", len(receipts)),
	)
	return nil
}

// applyBlock performs every write for one block. It must run inside a block
// transaction (ctx from BeginBlock). Storage write failures abort the block;
// failures of external enrichment (RPC lookups, token metadata) are logged.
func (f *Fetcher) applyBlock(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	height := block.NumberU64()

	if err := f.storage.SetBlock(ctx, block); err != nil {
		return fmt.Errorf("failed to store block %d: %w", height, err)
	}
	if err := f.processBlockMetadata(ctx, block, receipts, height); err != nil {
		return err
	}
	if err := f.processFeeDelegationMetadata(ctx, height); err != nil {
		f.logger.Warn("Fee delegation metadata processing failed",
			zap.Uint64("height", height),
			zap.Error(err),
		)
	}
	f.publish(events.NewBlockEvent(block))

	// Receipts are always processed sequentially here: a block batch must
	// not be written from several goroutines.
	if err := f.storeReceiptsSequential(ctx, receipts); err != nil {
		return err
	}
	f.publishBlockEvents(block, receipts, height)
	f.processBlockWithProcessors(ctx, block, receipts)
	return nil
}

// advanceCursor sets the latest height to height if it is higher than the
// stored one. Gap recovery therefore never moves the cursor backwards.
func (f *Fetcher) advanceCursor(ctx context.Context, height uint64) error {
	current, err := f.storage.GetLatestHeight(ctx)
	if err != nil && !errors.Is(err, storagepkg.ErrNotFound) {
		return fmt.Errorf("read latest height: %w", err)
	}
	if err == nil && current >= height {
		return nil
	}
	if err := f.storage.SetLatestHeight(ctx, height); err != nil {
		return fmt.Errorf("failed to update latest height to %d: %w", height, err)
	}
	return nil
}

// advanceCursorOnly moves the cursor for a skipped, already stored block in
// its own small transaction.
func (f *Fetcher) advanceCursorOnly(ctx context.Context, height uint64) error {
	txCtx, tx, err := f.txr.BeginBlock(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f.advanceCursor(txCtx, height); err != nil {
		return err
	}
	return tx.Commit()
}
