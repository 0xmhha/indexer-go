package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

const defaultStateMaxBytes int64 = 16 << 20

func (f *Fetcher) prepareBlock(ctx context.Context, fb *fetchedBlock) error {
	if !f.config.StatePrefetch || f.features == nil {
		return nil
	}
	// Replay of an already stored block must not require pruned state.
	// A concurrent rollback is handled by the normal writer path instead.
	_, err := f.storage.GetBlock(ctx, fb.height())
	if err == nil {
		return nil
	}
	if !errors.Is(err, port.ErrNotFound) {
		return err
	}
	prepared, err := f.features.PrepareBlock(ctx, fb.featureBlock())
	if err != nil {
		if errors.Is(err, chains.ErrAccountingFork) {
			height := fb.height()
			if height > 0 {
				height--
			}
			return fmt.Errorf("prepare block %d: %w: %w", fb.height(), err, &ReorgError{Height: height})
		}
		return err
	}
	limit := f.config.StateMaxBytes
	if limit <= 0 {
		limit = defaultStateMaxBytes
	}
	if prepared.Bytes() > limit {
		return fmt.Errorf("prepared state payload %d exceeds state_max_bytes %d at block %d", prepared.Bytes(), limit, fb.height())
	}
	fb.prepared = prepared
	return nil
}

// Hash-bound RPC reads identify the acquired fork without an extra header
// lookup during preparation. Recheck canonicality on the writer before using
// queued results. A fork change cancels
// the range and discards every speculative result; no shared cache survives it.
func (f *Fetcher) checkPreparedState(ctx context.Context, fb *fetchedBlock) error {
	if !fb.prepared.ReadsState() {
		return nil
	}
	rctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	stateSource := f.stateSource
	if stateSource == nil {
		stateSource = f.src
	}
	hash, err := stateSource.HashAt(rctx, fb.height())
	if err != nil {
		return err
	}
	if hash != fb.block.Hash {
		return fmt.Errorf("prepared state block changed at %d: %w", fb.height(), &ReorgError{Height: fb.height()})
	}
	return nil
}
