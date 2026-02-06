package fetch

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// ============================================================================
// Block Processing Internal Methods
// ============================================================================

// fetchBlockAndReceiptsWithRetry fetches block and receipts with exponential backoff retry logic
func (f *Fetcher) fetchBlockAndReceiptsWithRetry(ctx context.Context, height uint64, startTime time.Time) (*types.Block, types.Receipts, bool, error) {
	var block *types.Block
	var receipts types.Receipts
	var err error
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
			time.Sleep(backoffDelay)
		}

		// Fetch block - use chain adapter if available (for EIP-4844 compatibility)
		if f.chainAdapter != nil {
			block, err = f.chainAdapter.BlockFetcher().GetBlockByNumber(ctx, height)
		} else {
			block, err = f.client.GetBlockByNumber(ctx, height)
		}
		if err != nil {
			hadError = true
			f.logger.Error("Failed to fetch block",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			f.metrics.RecordRequest(time.Since(startTime), true, false)
			if attempt == f.config.MaxRetries {
				return nil, nil, hadError, fmt.Errorf("failed to fetch block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Fetch receipts - use chain adapter if available
		if f.chainAdapter != nil {
			receipts, err = f.chainAdapter.BlockFetcher().GetBlockReceipts(ctx, height)
		} else {
			receipts, err = f.client.GetBlockReceipts(ctx, height)
		}
		if err != nil {
			hadError = true
			f.logger.Error("Failed to fetch receipts",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			f.metrics.RecordRequest(time.Since(startTime), true, false)
			if attempt == f.config.MaxRetries {
				return nil, nil, hadError, fmt.Errorf("failed to fetch receipts for block %d after %d attempts: %w", height, f.config.MaxRetries, err)
			}
			continue
		}

		// Success - break retry loop
		break
	}

	return block, receipts, hadError, nil
}

// processFeeDelegationMetadata extracts and stores fee delegation metadata for a block
func (f *Fetcher) processFeeDelegationMetadata(ctx context.Context, height uint64) error {
	// Check if storage supports fee delegation
	fdStorage, ok := f.storage.(FeeDelegationStorage)
	if !ok {
		return nil // Storage doesn't support fee delegation, skip silently
	}

	// Check if client supports fee delegation metadata extraction
	fdClient, ok := f.client.(FeeDelegationClient)
	if !ok {
		return nil // Client doesn't support fee delegation metadata extraction, skip silently
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
func (f *Fetcher) processBlockMetadata(ctx context.Context, block *types.Block, receipts types.Receipts, height uint64) error {
	// Process WBFT metadata
	if err := f.processWBFTMetadata(ctx, block); err != nil {
		return fmt.Errorf("failed to process WBFT metadata for block %d: %w", height, err)
	}

	// Process address indexing (contract creation, token transfers)
	if err := f.processAddressIndexing(ctx, block, receipts); err != nil {
		return fmt.Errorf("failed to process address indexing for block %d: %w", height, err)
	}

	// Process native balance tracking
	if err := f.processBalanceTracking(ctx, block, receipts); err != nil {
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
func (f *Fetcher) storeAndProcessReceipts(ctx context.Context, block *types.Block, receipts types.Receipts, height uint64) error {
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

		// Parse system contract events from large block receipts
		if f.systemContractEventParser != nil {
			for _, receipt := range receipts {
				if len(receipt.Logs) > 0 {
					if err := f.systemContractEventParser.ParseAndIndexLogs(ctx, receipt.Logs); err != nil {
						f.logger.Warn("failed to parse system contract events",
							zap.String("tx", receipt.TxHash.Hex()),
							zap.Int("logs", len(receipt.Logs)),
							zap.Error(err),
						)
					}
				}
			}
		}
	} else {
		// Standard sequential processing for normal blocks
		for _, receipt := range receipts {
			if err := f.storage.SetReceipt(ctx, receipt); err != nil {
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
				}
			}

			// Parse system contract events from this receipt
			if f.systemContractEventParser != nil && len(receipt.Logs) > 0 {
				if err := f.systemContractEventParser.ParseAndIndexLogs(ctx, receipt.Logs); err != nil {
					f.logger.Warn("failed to parse system contract events",
						zap.String("tx", receipt.TxHash.Hex()),
						zap.Int("logs", len(receipt.Logs)),
						zap.Error(err),
					)
				}
			}
		}
	}

	return nil
}

// fetchBlockJob fetches a single block and its receipts with retry logic
func (f *Fetcher) fetchBlockJob(ctx context.Context, height uint64) *jobResult {
	var block *types.Block
	var receipts types.Receipts
	var err error

	// Retry logic for fetching block with exponential backoff
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
			time.Sleep(backoffDelay)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return &jobResult{height: height, err: ctx.Err()}
		default:
		}

		// Fetch block - use chain adapter if available (for EIP-4844 compatibility)
		if f.chainAdapter != nil {
			block, err = f.chainAdapter.BlockFetcher().GetBlockByNumber(ctx, height)
		} else {
			block, err = f.client.GetBlockByNumber(ctx, height)
		}
		if err != nil {
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
			continue
		}

		// Fetch receipts - use chain adapter if available
		if f.chainAdapter != nil {
			receipts, err = f.chainAdapter.BlockFetcher().GetBlockReceipts(ctx, height)
		} else {
			receipts, err = f.client.GetBlockReceipts(ctx, height)
		}
		if err != nil {
			f.logger.Error("Failed to fetch receipts",
				zap.Uint64("height", height),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			if attempt == f.config.MaxRetries {
				return &jobResult{
					height: height,
					err:    fmt.Errorf("failed to fetch receipts after %d attempts: %w", f.config.MaxRetries, err),
				}
			}
			continue
		}

		// Success - break retry loop
		break
	}

	return &jobResult{
		height:   height,
		block:    block,
		receipts: receipts,
		err:      nil,
	}
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
