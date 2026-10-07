package fetch

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ============================================================================
// Block Processing Internal Methods
// ============================================================================

// processBlockMetadata processes WBFT metadata, address indexing, balance tracking, and genesis initialization
func (f *Fetcher) processBlockMetadata(ctx context.Context, fb *fetchedBlock) error {
	// Address indexing, balances, token metadata and the other per-block
	// indexes are features (pkg/features); they run after the core data is
	// stored.
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
