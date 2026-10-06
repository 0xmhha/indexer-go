package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// ErrBlockConflict is returned when a block at an already indexed height has
// a different hash than the stored one. It is reported as a ReorgError (which
// matches both errors.Is(err, ErrReorg) and this error) so the caller rolls
// back instead of overwriting.
var ErrBlockConflict = errors.New("fetch: stored block differs from fetched block")

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

// storedBlockHash returns the hash of the block stored at height, as the
// chain reports it.
func (f *Fetcher) storedBlockHash(ctx context.Context, height uint64) (common.Hash, error) {
	b, err := f.storage.GetBlock(ctx, height)
	if err != nil {
		return common.Hash{}, err
	}
	return b.Hash, nil
}

// applyBlock performs every write for one block. It must run inside a block
// transaction (ctx from BeginBlock). Storage write failures abort the block;
// failures of external enrichment (RPC lookups, token metadata) are logged.
func (f *Fetcher) applyBlock(ctx context.Context, fb *fetchedBlock) error {
	height := fb.height()

	if err := f.storeBlock(ctx, fb); err != nil {
		return fmt.Errorf("failed to store block %d: %w", height, err)
	}
	if err := f.processBlockMetadata(ctx, fb); err != nil {
		return err
	}
	f.publish(f.blockEvent(fb))

	// Receipts are always processed sequentially here: a block batch must
	// not be written from several goroutines.
	if err := f.storeReceiptsSequential(ctx, fb); err != nil {
		return err
	}
	if err := f.runFeatures(ctx, fb); err != nil {
		return fmt.Errorf("block %d: %w", height, err)
	}
	f.publishBlockEvents(fb)
	f.processBlockWithProcessors(ctx, fb.geth, fb.gethReceipts)
	return nil
}

// storeBlock stores the block and its transactions under the hashes the
// chain reports.
func (f *Fetcher) storeBlock(ctx context.Context, fb *fetchedBlock) error {
	return f.storage.SetBlock(ctx, fb.block)
}

// blockEvent builds the block event with the chain's block hash.
func (f *Fetcher) blockEvent(fb *fetchedBlock) *events.BlockEvent {
	ev := events.NewBlockEvent(fb.geth)
	ev.Hash = fb.block.Hash
	return ev
}

// advanceCursor sets the latest height to height if it is higher than the
// stored one. Gap recovery therefore never moves the cursor backwards.
func (f *Fetcher) advanceCursor(ctx context.Context, height uint64) error {
	current, err := f.storage.GetLatestHeight(ctx)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
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
