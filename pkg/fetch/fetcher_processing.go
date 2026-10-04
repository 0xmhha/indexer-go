package fetch

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
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

// processFeeDelegationMetadata extracts and stores fee delegation metadata
// for blocks read through the legacy client, by fetching the block again
// through a fee delegation aware client. Blocks decoded by a chain profile
// carry the fee payer themselves and are handled by the
// stablenet.fee_delegation feature. Removed with the legacy client path.
func (f *Fetcher) processFeeDelegationMetadata(ctx context.Context, fb *fetchedBlock) error {
	if f.src != nil {
		return nil
	}
	height := fb.height()
	// Check if storage supports fee delegation
	fdStorage, ok := f.storage.(FeeDelegationStorage)
	if !ok {
		return nil // Storage doesn't support fee delegation, skip silently
	}

	// Check if a client supporting fee delegation metadata extraction is set
	fdClient := f.fdClient
	if fdClient == nil {
		var ok bool
		if fdClient, ok = f.client.(FeeDelegationClient); !ok {
			return nil // Client doesn't support fee delegation metadata extraction, skip silently
		}
	}

	// Fetch block with fee delegation metadata
	_, metas, err := fdClient.GetBlockWithFeeDelegationMeta(ctx, height)
	if err != nil {
		f.logger.Warn("Failed to fetch fee delegation metadata",
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return nil // Don't fail block processing for fee delegation metadata extraction failure
	}

	// Store each fee delegation metadata
	for _, meta := range metas {
		storageMeta := &storagepkg.FeeDelegationTxMeta{
			TxHash:       meta.TxHash,
			BlockNumber:  meta.BlockNumber,
			OriginalType: meta.OriginalType,
			FeePayer:     meta.FeePayer,
			FeePayerV:    meta.FeePayerV,
			FeePayerR:    meta.FeePayerR,
			FeePayerS:    meta.FeePayerS,
		}
		if err := fdStorage.SetFeeDelegationTxMeta(ctx, storageMeta); err != nil {
			f.logger.Warn("Failed to store fee delegation metadata",
				zap.String("txHash", meta.TxHash.Hex()),
				zap.Uint64("height", height),
				zap.Error(err),
			)
			if f.strictStorageErrors {
				return fmt.Errorf("failed to store fee delegation metadata: %w", err)
			}
			// Continue processing other metadata even if one fails
		}
	}

	if len(metas) > 0 {
		f.logger.Debug("Stored fee delegation metadata",
			zap.Uint64("height", height),
			zap.Int("count", len(metas)),
		)
	}

	return nil
}

// processBlockMetadata processes WBFT metadata, address indexing, balance tracking, and genesis initialization
func (f *Fetcher) processBlockMetadata(ctx context.Context, fb *fetchedBlock) error {
	height := fb.height()
	block := fb.geth

	// Process address indexing (contract creation, token transfers)
	if err := f.processAddressIndexing(ctx, fb); err != nil {
		return fmt.Errorf("failed to process address indexing for block %d: %w", height, err)
	}

	// Process native balance tracking
	if err := f.processBalanceTracking(ctx, fb); err != nil {
		return fmt.Errorf("failed to process balance tracking for block %d: %w", height, err)
	}

	// Initialize genesis allocation balances (block 0 only)
	if height == 0 {
		if err := f.initializeGenesisBalances(ctx, block); err != nil {
			f.logger.Warn("Failed to initialize genesis balances",
				zap.Uint64("height", height),
				zap.Error(err),
			)
			// Don't fail the entire block processing for genesis balance initialization
		}

		// Initialize genesis token metadata for system contracts
		if err := f.initializeGenesisTokenMetadata(ctx); err != nil {
			f.logger.Warn("Failed to initialize genesis token metadata",
				zap.Uint64("height", height),
				zap.Error(err),
			)
			// Don't fail the entire block processing for genesis token initialization
		}
	}

	return nil
}

// storeAndProcessReceipts stores receipts and indexes logs using appropriate processing strategy
func (f *Fetcher) storeAndProcessReceipts(ctx context.Context, fb *fetchedBlock) error {
	block, receipts, height := fb.geth, fb.gethReceipts, fb.height()
	// Use large block processor for blocks exceeding threshold
	if f.largeBlockProcessor.ShouldProcessInBatches(block, receipts) {
		f.logger.Info("Using parallel processing for large block",
			zap.Uint64("height", height),
			zap.Uint64("gas_used", block.GasUsed()),
			zap.Int("receipt_count", len(receipts)),
		)
		if err := f.largeBlockProcessor.ProcessReceiptsParallel(ctx, block, receipts); err != nil {
			return fmt.Errorf("failed to process large block receipts: %w", err)
		}
	} else {
		if err := f.storeReceiptsSequential(ctx, fb); err != nil {
			return err
		}
	}

	return nil
}

// storeReceiptsSequential stores receipts, indexes their logs and parses
// system contract events one receipt at a time.
func (f *Fetcher) storeReceiptsSequential(ctx context.Context, fb *fetchedBlock) error {
	mw, modelStore := f.storage.(storagepkg.ModelWriter)
	for i, receipt := range fb.gethReceipts {
		var err error
		if modelStore {
			err = mw.SetModelReceipt(ctx, fb.receipts[i])
		} else {
			err = f.storage.SetReceipt(ctx, receipt)
		}
		if err != nil {
			return fmt.Errorf("failed to store receipt for tx %s: %w", receipt.TxHash.Hex(), err)
		}

		// Index logs from this receipt
		if logWriter, ok := f.storage.(storagepkg.LogWriter); ok && len(receipt.Logs) > 0 {
			if err := logWriter.IndexLogs(ctx, receipt.Logs); err != nil {
				f.logger.Warn("failed to index logs",
					zap.String("tx", receipt.TxHash.Hex()),
					zap.Int("logs", len(receipt.Logs)),
					zap.Error(err),
				)
				if f.strictStorageErrors {
					return fmt.Errorf("failed to index logs: %w", err)
				}
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
