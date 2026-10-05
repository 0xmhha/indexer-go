package fetch

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/feature"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// Recover brings the database in line with the node and the configuration
// before ingest starts. It is the single recovery entry point after a crash
// or a configuration change; every write it makes goes through the writer.
//
//  1. Reorg: if the newest indexed block is no longer on the node's chain
//     (it reorganized while the indexer was down), roll back to the fork.
//  2. Feature states: record which features process new blocks
//     (feature.Reconcile).
//  3. Backfill: features enabled after blocks were indexed process the
//     stored blocks they missed, in block order.
//
// enabled lists the enabled features in execution order; backfill is a
// pipeline of them that publishes no events.
func (f *Fetcher) Recover(ctx context.Context, enabled []string, backfill *feature.Pipeline) error {
	if err := f.recoverReorg(ctx); err != nil {
		return err
	}
	return f.recoverFeatures(ctx, enabled, backfill)
}

// recoverReorg compares the newest indexed block with the node's block at
// the same height and rolls back if they differ. A node that has not reached
// that height yet is not a reorg.
func (f *Fetcher) recoverReorg(ctx context.Context) error {
	latest, err := f.storage.GetLatestHeight(ctx)
	if errors.Is(err, storagepkg.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	stored, err := f.storedBlockHash(ctx, latest)
	if err != nil {
		if errors.Is(err, storagepkg.ErrNotFound) {
			return nil
		}
		return err
	}
	onNode, err := f.nodeBlockHash(ctx, latest)
	if err != nil {
		f.logger.Warn("Startup reorg check skipped: node has no block at the indexed height",
			zap.Uint64("height", latest), zap.Error(err))
		return nil
	}
	if onNode == stored {
		return nil
	}
	f.logger.Warn("Indexed head is no longer on the node's chain; rolling back",
		zap.Uint64("height", latest), zap.String("stored", stored.Hex()), zap.String("node", onNode.Hex()))
	fork, err := f.HandleReorg(ctx, latest)
	if err != nil {
		return fmt.Errorf("startup reorg: %w", err)
	}
	f.logger.Info("Rolled back to fork point", zap.Uint64("fork_point", fork))
	return nil
}

func (f *Fetcher) recoverFeatures(ctx context.Context, enabled []string, backfill *feature.Pipeline) error {
	fs, ok := f.storage.(storagepkg.FeatureStateStore)
	if !ok {
		return nil
	}
	states, err := fs.FeatureStates(ctx)
	if err != nil {
		return err
	}
	latest, err := f.storage.GetLatestHeight(ctx)
	hasData := err == nil
	if err != nil && !errors.Is(err, storagepkg.ErrNotFound) {
		return err
	}

	writes, jobs := feature.Reconcile(enabled, states, latest, hasData)
	for name, st := range writes {
		if !st.Active {
			f.logger.Warn("Feature disabled; its data stops at the current height", zap.String("feature", name), zap.Uint64("through", st.Through))
		}
		name, st := name, st
		if err := f.Exec(ctx, "featureState", func(ctx context.Context) error {
			return fs.SetFeatureState(ctx, name, st)
		}); err != nil {
			return err
		}
	}
	var online []feature.BackfillJob
	for _, job := range jobs {
		if job.Online {
			online = append(online, job)
			continue
		}
		f.logger.Info("Backfilling feature", zap.String("feature", job.Feature), zap.Uint64("from", job.From), zap.Uint64("to", job.To))
		name := job.Feature
		progress := func(ctx context.Context, h uint64) error {
			return fs.SetFeatureState(ctx, name, storagepkg.FeatureState{Through: h})
		}
		if err := f.Backfill(ctx, backfill.Only(name), job.From, job.To, progress); err != nil {
			return fmt.Errorf("backfill %s: %w", name, err)
		}
		if err := f.Exec(ctx, "featureState", func(ctx context.Context) error {
			return fs.SetFeatureState(ctx, name, storagepkg.FeatureState{Active: true})
		}); err != nil {
			return err
		}
	}
	if len(online) > 0 {
		f.startOnlineBackfill(online, backfill, fs)
	}
	return nil
}

// startOnlineBackfill fills the gaps of order-independent features in the
// background while ingest runs. Each block is one writer command, so live
// indexing commands interleave with it. Progress is recorded with each
// block; after a restart Recover resumes from the gap left.
func (f *Fetcher) startOnlineBackfill(jobs []feature.BackfillJob, backfill *feature.Pipeline, fs storagepkg.FeatureStateStore) {
	ctx := f.background()
	f.bgWG.Add(1)
	go func() {
		defer f.bgWG.Done()
		for _, job := range jobs {
			name, to := job.Feature, job.To
			f.logger.Info("Backfilling feature online", zap.String("feature", name), zap.Uint64("from", job.From), zap.Uint64("to", to))
			progress := func(ctx context.Context, h uint64) error {
				st := storagepkg.FeatureState{Active: true}
				if h < to {
					st.Gap = &storagepkg.BlockRange{From: h + 1, To: to}
				}
				return fs.SetFeatureState(ctx, name, st)
			}
			p := backfill.Only(name)
			for h := job.From; h <= to; {
				if ctx.Err() != nil {
					return
				}
				if err := f.Backfill(ctx, p, h, h, progress); err != nil {
					if ctx.Err() != nil {
						return
					}
					f.logger.Warn("Online backfill failed; retrying", zap.String("feature", name), zap.Uint64("height", h), zap.Error(err))
					if sleepCtx(ctx, f.config.RetryDelay) != nil {
						return
					}
					continue
				}
				h++
			}
			f.logger.Info("Online backfill done", zap.String("feature", name))
		}
	}()
}

// background returns the context of the fetcher's background work, which
// Close cancels.
func (f *Fetcher) background() context.Context {
	f.bgOnce.Do(func() { f.bgCtx, f.bgCancel = context.WithCancel(context.Background()) })
	return f.bgCtx
}

// WaitBackground waits for background work (online backfill) to finish. It
// is meant for tests and tooling.
func (f *Fetcher) WaitBackground() { f.bgWG.Wait() }
