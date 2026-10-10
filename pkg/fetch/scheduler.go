package fetch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// indexRange indexes blocks start..end in height order through the one
// ingest pipeline (refactoring plan R2-2) that the live loop, gap recovery
// and single-block fetches share, so every path indexes a block the same
// way:
//
//   - workers fetch blocks concurrently (each with the retry policy);
//   - a reorder buffer hands them to the writer in height order;
//   - at most window = 2*workers heights are in flight beyond the next
//     block to index, so a slow block holds back fetching instead of
//     growing the buffer (backpressure).
//
// An error stops the pipeline at the lowest failing height: the blocks
// below it are indexed, nothing above, and its error is returned. A reorganization found while indexing comes
// back as a ReorgError for the caller to handle.
func (f *Fetcher) indexRange(ctx context.Context, start, end uint64, workers int) error {
	if end < start {
		return nil
	}
	total := end - start + 1
	if workers < 1 {
		workers = 1
	}
	if uint64(workers) > total {
		workers = int(total)
	}
	window := 2 * workers

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	slots := make(chan struct{}, window) // heights fetched or being fetched, not yet indexed
	heights := make(chan uint64)
	results := make(chan *jobResult, window)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // dispatcher
		defer wg.Done()
		defer close(heights)
		for h := start; h <= end; h++ {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			select {
			case heights <- h:
			case <-ctx.Done():
				return
			}
		}
	}()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for h := range heights {
				r := f.fetchJob(ctx, h)
				select {
				case results <- r:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	// On return, stop and wait for the goroutines so none outlives the call.
	defer func() {
		cancel()
		go func() {
			for range results {
			}
		}()
		wg.Wait()
		close(results)
	}()

	pending := make(map[uint64]*jobResult, window)
	next := start
	for next <= end {
		var r *jobResult
		select {
		case r = <-results:
		case <-ctx.Done():
			return ctx.Err()
		}
		pending[r.height] = r
		for res, ok := pending[next]; ok; res, ok = pending[next] {
			// Errors are handled in height order too: every block below a
			// failed one is indexed first, as on the sequential path.
			if res.err != nil {
				return fmt.Errorf("failed to fetch block %d: %w", res.height, res.err)
			}
			f.offerBlock(res.block) // fast path: before the commit
			if err := f.indexBlock(ctx, res.block); err != nil {
				return err
			}
			delete(pending, next)
			<-slots // the height left the window
			if done := next - start + 1; done%100 == 0 || next == end {
				f.logger.Info("Index progress",
					zap.Uint64("height", next), zap.Uint64("done", done), zap.Uint64("total", total))
			}
			next++
		}
	}
	return nil
}

// fetchJob fetches one block with the retry policy: up to MaxRetries
// retries with exponential backoff from RetryDelay.
func (f *Fetcher) fetchJob(ctx context.Context, height uint64) *jobResult {
	start := time.Now()
	var lastErr error
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := f.config.RetryDelay * time.Duration(1<<uint(attempt-1))
			f.logger.Warn("Retrying block fetch",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", f.config.MaxRetries),
				zap.Duration("backoff_delay", backoff),
			)
			if err := sleepCtx(ctx, backoff); err != nil {
				return &jobResult{height: height, err: err}
			}
		}
		fb, err := f.fetchOnce(ctx, height)
		if err == nil {
			f.metrics.RecordRequest(time.Since(start), false, false)
			return &jobResult{height: height, block: fb}
		}
		if ctx.Err() != nil {
			return &jobResult{height: height, err: ctx.Err()}
		}
		lastErr = err
		f.logger.Error("Failed to fetch block",
			zap.Uint64("height", height),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		f.metrics.RecordRequest(time.Since(start), true, false)
	}
	return &jobResult{height: height, err: fmt.Errorf("failed after %d attempts: %w", f.config.MaxRetries+1, lastErr)}
}
