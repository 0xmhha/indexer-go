package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// ErrReorg means the node's chain no longer contains an indexed block.
var ErrReorg = errors.New("fetch: chain reorganization")

// ErrReorgTooDeep means the indexed blocks that left the chain cannot all be
// rolled back (beyond the undo window or backfilled); reindex.
var ErrReorgTooDeep = errors.New("fetch: reorganization deeper than the indexer can roll back")

// ReorgError reports a reorganization detected at Height: the indexed block
// at that height is not an ancestor of the node's chain.
type ReorgError struct {
	Height uint64
}

func (e *ReorgError) Error() string {
	return fmt.Sprintf("%s: indexed block %d left the chain", ErrReorg, e.Height)
}

// Is makes errors.Is(err, ErrReorg) true.
func (e *ReorgError) Is(target error) bool { return target == ErrReorg }

// checkParent returns a ReorgError when fb does not extend the indexed block
// below it. A missing parent (start height, gap) is not checked.
func (f *Fetcher) checkParent(ctx context.Context, fb *fetchedBlock) error {
	h := fb.height()
	if h == 0 {
		return nil
	}
	parent, err := f.storedBlockHash(ctx, h-1)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check parent of block %d: %w", h, err)
	}
	if parent != fb.block.ParentHash {
		return &ReorgError{Height: h - 1}
	}
	return nil
}

// HandleReorg finds where the indexed chain and the node's chain fork, at or
// below from, rolls the database back to that block and returns its height.
// Indexing continues from the returned height + 1.
func (f *Fetcher) HandleReorg(ctx context.Context, from uint64) (uint64, error) {
	rb, ok := f.storage.(port.Rollbacker)
	if !ok {
		return 0, fmt.Errorf("%w: storage cannot roll back", ErrReorgTooDeep)
	}
	latest, err := f.storage.GetLatestHeight(ctx)
	if err != nil {
		return 0, err
	}
	if from > latest {
		from = latest
	}

	fork := from
	for {
		stored, storedErr := f.storedBlockHash(ctx, fork)
		if storedErr != nil && !errors.Is(storedErr, port.ErrNotFound) {
			return 0, storedErr
		}
		onNode, err := f.nodeBlockHash(ctx, fork)
		if err != nil {
			return 0, fmt.Errorf("read node block %d: %w", fork, err)
		}
		if storedErr == nil && stored == onNode {
			break // the newest block both chains share
		}
		if fork == 0 {
			return 0, fmt.Errorf("%w: genesis differs from the node's", ErrReorgTooDeep)
		}
		if latest-fork >= storagepkg.UndoWindow {
			return 0, fmt.Errorf("%w: no common block within %d blocks of %d", ErrReorgTooDeep, storagepkg.UndoWindow, latest)
		}
		fork--
	}

	f.logger.Warn("Chain reorganization: rolling back",
		zap.Uint64("fork_point", fork),
		zap.Uint64("indexed_head", latest),
		zap.Uint64("depth", latest-fork),
	)
	if err := f.rollbackTo(ctx, rb, fork); err != nil {
		if errors.Is(err, port.ErrNoUndo) {
			return 0, fmt.Errorf("%w: %v", ErrReorgTooDeep, err)
		}
		return 0, fmt.Errorf("roll back to %d: %w", fork, err)
	}
	f.metrics.RecordReorg(latest - fork)
	return fork, nil
}

// publishReorg delivers a committed rollback's events: the relay already
// has them in the outbox; without an outbox they are published directly.
func (f *Fetcher) publishReorg(r *port.Reorg) {
	if f.outbox != nil {
		f.notifyRelay()
		return
	}
	for i, ob := range r.Blocks {
		for _, ev := range reorgEvents(r, ob, i == 0) {
			if !f.publish(ev) {
				f.logger.Warn("Failed to publish reorg event (channel full)", zap.Uint64("seq", r.Seq), zap.String("type", string(ev.Type())))
			}
		}
	}
}

// nodeBlockHash returns the hash of the node's block at height.
func (f *Fetcher) nodeBlockHash(ctx context.Context, height uint64) (common.Hash, error) {
	rctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	if f.src == nil {
		return common.Hash{}, errNoSource
	}
	return f.src.HashAt(rctx, height)
}

// Reorgs returns how many reorganizations were rolled back and how many
// blocks they rolled back in total.
func (f *Fetcher) Reorgs() (count, blocks uint64) { return f.metrics.Reorgs() }
