package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
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

// gapRecoveryRounds is how many times RunWithGapRecovery detects and fills
// the remaining gaps before it gives up.
const gapRecoveryRounds = 3

// RunWithGapRecovery fills the gaps below the indexed head, then runs the
// live loop. Each round detects the remaining gaps and fills them (blocks
// filled by an earlier round stay filled); after gapRecoveryRounds failed
// rounds, or when the gaps cannot be filled (ErrGapBelowIndexed), it
// returns the error instead of following the node with the gaps left
// behind, so the failure is visible and a restart tries again.
func (f *Fetcher) RunWithGapRecovery(ctx context.Context) error {
	f.logger.Info("Starting fetcher with gap recovery enabled",
		zap.Uint64("start_height", f.config.StartHeight),
		zap.Int("batch_size", f.config.BatchSize),
	)
	if err := f.recoverGaps(ctx); err != nil {
		return err
	}
	return f.Run(ctx)
}

// recoverGaps detects and fills the gaps between the start height and the
// latest indexed block, in rounds.
func (f *Fetcher) recoverGaps(ctx context.Context) error {
	latestHeight, err := f.storage.GetLatestHeight(ctx)
	if errors.Is(err, port.ErrNotFound) || (err == nil && latestHeight <= f.config.StartHeight) {
		return nil // nothing indexed below the head
	}
	if err != nil {
		return fmt.Errorf("gap recovery: latest height: %w", err)
	}
	for round := 1; ; round++ {
		gaps, err := f.DetectGaps(ctx, f.config.StartHeight, latestHeight)
		if err == nil {
			if len(gaps) == 0 {
				return nil
			}
			f.logger.Info("Found block gaps in existing data, filling them first",
				zap.Int("gap_count", len(gaps)), zap.Int("round", round))
			if err = f.FillGaps(ctx, gaps); err == nil {
				return nil
			}
		}
		if errors.Is(err, ErrGapBelowIndexed) || ctx.Err() != nil || round == gapRecoveryRounds {
			return fmt.Errorf("gap recovery between blocks %d and %d: %w", f.config.StartHeight, latestHeight, err)
		}
		f.logger.Warn("Gap recovery failed; retrying", zap.Int("round", round), zap.Error(err))
		if err := sleepCtx(ctx, f.config.RetryDelay); err != nil {
			return err
		}
	}
}
