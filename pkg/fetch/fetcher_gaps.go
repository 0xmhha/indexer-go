package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/feature"
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

// ErrGapBelowIndexed reports missing blocks below blocks already indexed
// while order-dependent features are enabled. Filling them now would apply
// those features out of order (balances, address sequences, latest-state
// records such as module installs would be wrong, defect D12); the database
// must be reindexed instead.
var ErrGapBelowIndexed = errors.New("missing blocks below indexed blocks")

// orderDependentFeatures returns the enabled features whose result depends
// on processing blocks in order.
func (f *Fetcher) orderDependentFeatures() []string {
	if f.features == nil {
		return nil
	}
	var out []string
	for _, n := range f.features.Features() {
		if !feature.IsOrderIndependent(n) {
			out = append(out, n)
		}
	}
	return out
}

// FillGaps fills all detected gaps concurrently. Detected gaps lie below the
// latest indexed block, so they are filled only if every enabled feature is
// order-independent; otherwise it returns ErrGapBelowIndexed and writes
// nothing.
func (f *Fetcher) FillGaps(ctx context.Context, gaps []GapRange) error {
	if len(gaps) == 0 {
		f.logger.Info("No gaps to fill")
		return nil
	}
	if names := f.orderDependentFeatures(); len(names) > 0 {
		return fmt.Errorf("%w: blocks %d-%d (and %d gaps in all) cannot be indexed after later blocks with order-dependent features %v enabled; reindex the database",
			ErrGapBelowIndexed, gaps[0].Start, gaps[0].End, len(gaps), names)
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
				if errors.Is(err, ErrGapBelowIndexed) {
					return err
				}
				f.logger.Error("Failed to fill block gaps", zap.Error(err))
				// Continue anyway - gaps will be retried later
			}
		}

	}

	// Run normal fetching loop
	return f.Run(ctx)
}
