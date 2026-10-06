package fetch

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ============================================================================
// Block Processing Internal Methods
// ============================================================================

// fetchBlockAndReceiptsWithRetry fetches block and receipts with exponential backoff retry logic
func (f *Fetcher) fetchBlockAndReceiptsWithRetry(ctx context.Context, height uint64, startTime time.Time) (*fetchedBlock, bool, error) {
	var hadError bool

	// Retry logic with exponential backoff
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoffDelay := f.config.RetryDelay * time.Duration(1<<uint(attempt-1))
			f.logger.Warn("Retrying block fetch",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", f.config.MaxRetries),
				zap.Duration("backoff_delay", backoffDelay),
			)
			if err := sleepCtx(ctx, backoffDelay); err != nil {
				return nil, true, err
			}
		}

		fb, err := f.fetchOnce(ctx, height)
		if err == nil {
			return fb, hadError, nil
		}
		hadError = true
		f.logger.Error("Failed to fetch block",
			zap.Uint64("height", height),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		f.metrics.RecordRequest(time.Since(startTime), true, false)
		if attempt == f.config.MaxRetries {
			return nil, hadError, fmt.Errorf("failed to fetch block %d after %d attempts: %w", height, f.config.MaxRetries, err)
		}
	}
	return nil, hadError, fmt.Errorf("failed to fetch block %d: no attempts", height)
}

// processBlockMetadata processes WBFT metadata, address indexing, balance tracking, and genesis initialization
func (f *Fetcher) processBlockMetadata(ctx context.Context, fb *fetchedBlock) error {
	// Address indexing, balances and the other per-block indexes are
	// features (pkg/features); they run after the core data is stored.
	if fb.height() == 0 {
		// Initialize genesis token metadata for system contracts
		if err := f.initializeGenesisTokenMetadata(ctx); err != nil {
			f.logger.Warn("Failed to initialize genesis token metadata",
				zap.Uint64("height", 0),
				zap.Error(err),
			)
		}
	}
	return nil
}

// storeReceiptsSequential stores receipts, indexes their logs and parses
// system contract events one receipt at a time.
func (f *Fetcher) storeReceiptsSequential(ctx context.Context, fb *fetchedBlock) error {
	for _, receipt := range fb.receipts {
		if err := f.storage.SetReceipt(ctx, receipt); err != nil {
			return fmt.Errorf("failed to store receipt for tx %s: %w", receipt.TxHash.Hex(), err)
		}

		// Index logs from this receipt
		if logWriter, ok := f.storage.(port.LogWriter); ok && len(receipt.Logs) > 0 {
			if err := logWriter.IndexLogs(ctx, receipt.Logs); err != nil {
				return fmt.Errorf("failed to index logs of tx %s: %w", receipt.TxHash.Hex(), err)
			}
		}
	}

	return nil
}

// fetchBlockJob fetches a single block and its receipts with retry logic
func (f *Fetcher) fetchBlockJob(ctx context.Context, height uint64) *jobResult {
	for attempt := 0; attempt <= f.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: delay = baseDelay * 2^(attempt-1)
			backoffDelay := f.config.RetryDelay * time.Duration(1<<uint(attempt-1))
			f.logger.Warn("Retrying block fetch",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Int("max_retries", f.config.MaxRetries),
				zap.Duration("backoff_delay", backoffDelay),
			)
			if err := sleepCtx(ctx, backoffDelay); err != nil {
				return &jobResult{height: height, err: err}
			}
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return &jobResult{height: height, err: ctx.Err()}
		default:
		}

		fb, err := f.fetchOnce(ctx, height)
		if err == nil {
			return &jobResult{height: height, block: fb}
		}
		f.logger.Error("Failed to fetch block",
			zap.Uint64("height", height),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		if attempt == f.config.MaxRetries {
			return &jobResult{
				height: height,
				err:    fmt.Errorf("failed to fetch block after %d attempts: %w", f.config.MaxRetries, err),
			}
		}
	}
	return &jobResult{height: height, err: fmt.Errorf("failed to fetch block %d: no attempts", height)}
}

// GetNextHeight determines the next block height to fetch
func (f *Fetcher) GetNextHeight(ctx context.Context) uint64 {
	// Try to get the latest indexed height
	latestHeight, err := f.storage.GetLatestHeight(ctx)
	if err != nil {
		// No blocks indexed yet, start from configured start height
		f.logger.Info("No blocks indexed yet, starting from configured height",
			zap.Uint64("start_height", f.config.StartHeight),
		)
		return f.config.StartHeight
	}

	// If configured start height is higher than latest indexed, use start height
	if f.config.StartHeight > latestHeight {
		f.logger.Info("Configured start height is higher than latest indexed",
			zap.Uint64("start_height", f.config.StartHeight),
			zap.Uint64("latest_height", latestHeight),
		)
		return f.config.StartHeight
	}

	// Continue from next block after latest indexed
	nextHeight := latestHeight + 1
	f.logger.Info("Continuing from latest indexed block",
		zap.Uint64("latest_height", latestHeight),
		zap.Uint64("next_height", nextHeight),
	)
	return nextHeight
}
