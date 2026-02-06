package fetch

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// ============================================================================
// Gap Detection and Recovery Methods
// ============================================================================

// GapRange represents a range of missing blocks
type GapRange struct {
	Start uint64
	End   uint64
}

// Size returns the number of blocks in the gap
func (g GapRange) Size() uint64 {
	if g.End < g.Start {
		return 0
	}
	return g.End - g.Start + 1
}

// ReceiptGapInfo contains information about missing receipts for a block
type ReceiptGapInfo struct {
	BlockNumber     uint64
	MissingReceipts []common.Hash
}

// DetectGaps scans the storage for missing blocks and returns gap ranges
func (f *Fetcher) DetectGaps(ctx context.Context, startHeight, endHeight uint64) ([]GapRange, error) {
	f.logger.Info("Scanning for gaps",
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	var gaps []GapRange
	var gapStart uint64
	inGap := false

	for height := startHeight; height <= endHeight; height++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return gaps, ctx.Err()
		default:
		}

		// Check if block exists
		exists, err := f.storage.HasBlock(ctx, height)
		if err != nil {
			return nil, fmt.Errorf("failed to check block %d: %w", height, err)
		}

		if !exists {
			// Start or continue gap
			if !inGap {
				gapStart = height
				inGap = true
			}
		} else {
			// End gap if we were in one
			if inGap {
				gaps = append(gaps, GapRange{
					Start: gapStart,
					End:   height - 1,
				})
				inGap = false
			}
		}

		// Log progress periodically
		if (height-startHeight+1)%1000 == 0 {
			f.logger.Debug("Gap detection progress",
				zap.Uint64("current", height),
				zap.Uint64("end", endHeight),
				zap.Int("gaps_found", len(gaps)),
			)
		}
	}

	// Handle gap at the end
	if inGap {
		gaps = append(gaps, GapRange{
			Start: gapStart,
			End:   endHeight,
		})
	}

	f.logger.Info("Gap detection completed",
		zap.Int("total_gaps", len(gaps)),
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	return gaps, nil
}

// FillGap fills a single gap range by fetching missing blocks
func (f *Fetcher) FillGap(ctx context.Context, gap GapRange) error {
	f.logger.Info("Filling gap",
		zap.Uint64("start", gap.Start),
		zap.Uint64("end", gap.End),
		zap.Uint64("size", gap.Size()),
	)

	// Use concurrent fetching for larger gaps
	if gap.Size() > 10 {
		return f.FetchRangeConcurrent(ctx, gap.Start, gap.End)
	}

	// Use sequential fetching for small gaps
	return f.FetchRange(ctx, gap.Start, gap.End)
}

// FillGaps fills all detected gaps concurrently
func (f *Fetcher) FillGaps(ctx context.Context, gaps []GapRange) error {
	if len(gaps) == 0 {
		f.logger.Info("No gaps to fill")
		return nil
	}

	f.logger.Info("Starting gap filling",
		zap.Int("total_gaps", len(gaps)),
	)

	// Fill each gap sequentially to maintain order and prevent resource exhaustion
	for i, gap := range gaps {
		f.logger.Info("Filling gap",
			zap.Int("gap_num", i+1),
			zap.Int("total_gaps", len(gaps)),
			zap.Uint64("start", gap.Start),
			zap.Uint64("end", gap.End),
			zap.Uint64("size", gap.Size()),
		)

		if err := f.FillGap(ctx, gap); err != nil {
			return fmt.Errorf("failed to fill gap [%d-%d]: %w", gap.Start, gap.End, err)
		}

		f.logger.Info("Gap filled successfully",
			zap.Uint64("start", gap.Start),
			zap.Uint64("end", gap.End),
		)
	}

	f.logger.Info("All gaps filled successfully",
		zap.Int("total_gaps", len(gaps)),
	)

	return nil
}

// DetectReceiptGaps scans stored blocks for missing receipts
func (f *Fetcher) DetectReceiptGaps(ctx context.Context, startHeight, endHeight uint64) ([]ReceiptGapInfo, error) {
	f.logger.Info("Scanning for receipt gaps",
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	var gaps []ReceiptGapInfo

	for height := startHeight; height <= endHeight; height++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return gaps, ctx.Err()
		default:
		}

		// Check if block exists first
		exists, err := f.storage.HasBlock(ctx, height)
		if err != nil {
			return nil, fmt.Errorf("failed to check block %d: %w", height, err)
		}
		if !exists {
			// Block doesn't exist, skip (will be caught by DetectGaps)
			continue
		}

		// Check for missing receipts in this block
		missingReceipts, err := f.storage.GetMissingReceipts(ctx, height)
		if err != nil {
			f.logger.Warn("failed to check missing receipts",
				zap.Uint64("height", height),
				zap.Error(err),
			)
			continue
		}

		if len(missingReceipts) > 0 {
			gaps = append(gaps, ReceiptGapInfo{
				BlockNumber:     height,
				MissingReceipts: missingReceipts,
			})
		}

		// Log progress periodically
		if (height-startHeight+1)%1000 == 0 {
			f.logger.Debug("Receipt gap detection progress",
				zap.Uint64("current", height),
				zap.Uint64("end", endHeight),
				zap.Int("blocks_with_missing_receipts", len(gaps)),
			)
		}
	}

	totalMissing := 0
	for _, gap := range gaps {
		totalMissing += len(gap.MissingReceipts)
	}

	f.logger.Info("Receipt gap detection completed",
		zap.Int("blocks_with_missing_receipts", len(gaps)),
		zap.Int("total_missing_receipts", totalMissing),
		zap.Uint64("start", startHeight),
		zap.Uint64("end", endHeight),
	)

	return gaps, nil
}

// FillReceiptGap fetches and stores missing receipts for a single block
func (f *Fetcher) FillReceiptGap(ctx context.Context, gap ReceiptGapInfo) error {
	f.logger.Info("Filling receipt gap",
		zap.Uint64("block", gap.BlockNumber),
		zap.Int("missing_count", len(gap.MissingReceipts)),
	)

	// Fetch receipts from RPC
	receipts, err := f.client.GetBlockReceipts(ctx, gap.BlockNumber)
	if err != nil {
		return fmt.Errorf("failed to fetch receipts for block %d: %w", gap.BlockNumber, err)
	}

	// Create a map for quick lookup
	receiptMap := make(map[common.Hash]*types.Receipt)
	for _, receipt := range receipts {
		receiptMap[receipt.TxHash] = receipt
	}

	// Store only the missing receipts
	storedCount := 0
	for _, txHash := range gap.MissingReceipts {
		receipt, exists := receiptMap[txHash]
		if !exists {
			f.logger.Warn("receipt not found from RPC",
				zap.String("tx_hash", txHash.Hex()),
				zap.Uint64("block", gap.BlockNumber),
			)
			continue
		}

		if err := f.storage.SetReceipt(ctx, receipt); err != nil {
			return fmt.Errorf("failed to store receipt for tx %s: %w", txHash.Hex(), err)
		}
		storedCount++
	}

	f.logger.Info("Receipt gap filled",
		zap.Uint64("block", gap.BlockNumber),
		zap.Int("stored", storedCount),
		zap.Int("expected", len(gap.MissingReceipts)),
	)

	return nil
}

// FillReceiptGaps fills all detected receipt gaps
func (f *Fetcher) FillReceiptGaps(ctx context.Context, gaps []ReceiptGapInfo) error {
	if len(gaps) == 0 {
		f.logger.Info("No receipt gaps to fill")
		return nil
	}

	totalMissing := 0
	for _, gap := range gaps {
		totalMissing += len(gap.MissingReceipts)
	}

	f.logger.Info("Starting receipt gap filling",
		zap.Int("blocks_with_gaps", len(gaps)),
		zap.Int("total_missing_receipts", totalMissing),
	)

	for i, gap := range gaps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		f.logger.Debug("Filling receipt gap",
			zap.Int("gap_num", i+1),
			zap.Int("total_gaps", len(gaps)),
			zap.Uint64("block", gap.BlockNumber),
			zap.Int("missing", len(gap.MissingReceipts)),
		)

		if err := f.FillReceiptGap(ctx, gap); err != nil {
			return fmt.Errorf("failed to fill receipt gap for block %d: %w", gap.BlockNumber, err)
		}
	}

	f.logger.Info("All receipt gaps filled successfully",
		zap.Int("blocks_processed", len(gaps)),
		zap.Int("receipts_recovered", totalMissing),
	)

	return nil
}

// RunWithGapRecovery starts the fetcher with automatic gap detection and recovery
func (f *Fetcher) RunWithGapRecovery(ctx context.Context) error {
	f.logger.Info("Starting fetcher with gap recovery enabled",
		zap.Uint64("start_height", f.config.StartHeight),
		zap.Int("batch_size", f.config.BatchSize),
	)

	// First, check for gaps in existing data
	latestHeight, err := f.storage.GetLatestHeight(ctx)
	if err == nil && latestHeight > f.config.StartHeight {
		f.logger.Info("Checking for gaps in existing data",
			zap.Uint64("start", f.config.StartHeight),
			zap.Uint64("end", latestHeight),
		)

		// Check for block gaps
		gaps, err := f.DetectGaps(ctx, f.config.StartHeight, latestHeight)
		if err != nil {
			f.logger.Error("Failed to detect block gaps", zap.Error(err))
		} else if len(gaps) > 0 {
			f.logger.Info("Found block gaps in existing data, filling them first",
				zap.Int("gap_count", len(gaps)),
			)
			if err := f.FillGaps(ctx, gaps); err != nil {
				f.logger.Error("Failed to fill block gaps", zap.Error(err))
				// Continue anyway - gaps will be retried later
			}
		}

		// Check for receipt gaps (blocks exist but receipts missing)
		receiptGaps, err := f.DetectReceiptGaps(ctx, f.config.StartHeight, latestHeight)
		if err != nil {
			f.logger.Error("Failed to detect receipt gaps", zap.Error(err))
		} else if len(receiptGaps) > 0 {
			f.logger.Info("Found receipt gaps in existing data, filling them",
				zap.Int("blocks_with_missing_receipts", len(receiptGaps)),
			)
			if err := f.FillReceiptGaps(ctx, receiptGaps); err != nil {
				f.logger.Error("Failed to fill receipt gaps", zap.Error(err))
				// Continue anyway - gaps will be retried later
			}
		}
	}

	// Run normal fetching loop
	return f.Run(ctx)
}
