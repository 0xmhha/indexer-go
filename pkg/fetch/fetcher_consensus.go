package fetch

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
)

// ============================================================================
// WBFT Consensus Metadata Methods
// ============================================================================

// processWBFTMetadata parses and stores WBFT consensus metadata from block header
func (f *Fetcher) processWBFTMetadata(ctx context.Context, block *types.Block) error {
	// Check if chain adapter indicates non-WBFT consensus - skip silently
	if f.chainAdapter != nil {
		info := f.chainAdapter.Info()
		if info != nil && info.ConsensusType != chain.ConsensusTypeWBFT {
			// Not a WBFT chain, skip WBFT metadata processing
			return nil
		}
	}

	// Check if storage implements WBFTWriter
	wbftWriter, ok := f.storage.(storagepkg.WBFTWriter)
	if !ok {
		// Storage doesn't support WBFT metadata - skip silently
		return nil
	}

	// Parse WBFT Extra from block header
	wbftExtra, err := storagepkg.ParseWBFTExtra(block.Header())
	if err != nil {
		// Log warning but don't fail the entire block indexing
		f.logger.Warn("Failed to parse WBFT extra",
			zap.Uint64("height", block.NumberU64()),
			zap.String("hash", block.Hash().Hex()),
			zap.Error(err),
		)
		return nil
	}

	// Save WBFT block extra
	if err := wbftWriter.SaveWBFTBlockExtra(ctx, wbftExtra); err != nil {
		return fmt.Errorf("failed to save WBFT block extra: %w", err)
	}

	// Save epoch info if present
	if wbftExtra.EpochInfo != nil {
		if err := wbftWriter.SaveEpochInfo(ctx, wbftExtra.EpochInfo); err != nil {
			return fmt.Errorf("failed to save epoch info: %w", err)
		}
	}

	// Extract and save validator signing activities
	if wbftExtra.EpochInfo != nil && len(wbftExtra.EpochInfo.Candidates) > 0 {
		var signingActivities []*storagepkg.ValidatorSigningActivity

		// Extract prepare signers
		if wbftExtra.PreparedSeal != nil {
			preparers, err := storagepkg.ExtractSigners(
				wbftExtra.PreparedSeal.Sealers,
				wbftExtra.EpochInfo.Validators,
				wbftExtra.EpochInfo.Candidates,
			)
			if err != nil {
				f.logger.Warn("Failed to extract prepare signers",
					zap.Uint64("height", block.NumberU64()),
					zap.Error(err),
				)
			} else {
				// Create signing activities for preparers
				for i, validator := range wbftExtra.EpochInfo.Candidates {
					activity := &storagepkg.ValidatorSigningActivity{
						BlockNumber:      wbftExtra.BlockNumber,
						BlockHash:        wbftExtra.BlockHash,
						ValidatorAddress: validator.Address,
						ValidatorIndex:   uint32(i),
						SignedPrepare:    containsAddress(preparers, validator.Address),
						SignedCommit:     false, // Will be updated below
						Round:            wbftExtra.Round,
						Timestamp:        wbftExtra.Timestamp,
					}
					signingActivities = append(signingActivities, activity)
				}
			}
		}

		// Extract commit signers
		if wbftExtra.CommittedSeal != nil {
			committers, err := storagepkg.ExtractSigners(
				wbftExtra.CommittedSeal.Sealers,
				wbftExtra.EpochInfo.Validators,
				wbftExtra.EpochInfo.Candidates,
			)
			if err != nil {
				f.logger.Warn("Failed to extract commit signers",
					zap.Uint64("height", block.NumberU64()),
					zap.Error(err),
				)
			} else {
				// Update commit status for existing activities
				for _, activity := range signingActivities {
					activity.SignedCommit = containsAddress(committers, activity.ValidatorAddress)
				}
			}
		}

		// Save validator signing activities
		if len(signingActivities) > 0 {
			if err := wbftWriter.UpdateValidatorSigningStats(ctx, wbftExtra.BlockNumber, signingActivities); err != nil {
				return fmt.Errorf("failed to update validator signing stats: %w", err)
			}
		}
	}

	f.logger.Debug("Processed WBFT metadata",
		zap.Uint64("height", block.NumberU64()),
		zap.Uint32("round", wbftExtra.Round),
		zap.Bool("has_epoch_info", wbftExtra.EpochInfo != nil),
	)

	// Publish ConsensusBlockEvent to EventBus for WebSocket subscriptions
	if f.eventBus != nil {
		f.publishConsensusBlockEvent(block, wbftExtra)
	}

	return nil
}

// publishConsensusBlockEvent creates and publishes a ConsensusBlockEvent
func (f *Fetcher) publishConsensusBlockEvent(block *types.Block, wbftExtra *storagepkg.WBFTBlockExtra) {
	// Calculate validator counts
	validatorCount := 0
	prepareCount := 0
	commitCount := 0

	if wbftExtra.EpochInfo != nil {
		validatorCount = len(wbftExtra.EpochInfo.Candidates)
	}

	if wbftExtra.PreparedSeal != nil && wbftExtra.PreparedSeal.Sealers != nil {
		prepareCount = countBitsInBitmap(wbftExtra.PreparedSeal.Sealers)
	}

	if wbftExtra.CommittedSeal != nil && wbftExtra.CommittedSeal.Sealers != nil {
		commitCount = countBitsInBitmap(wbftExtra.CommittedSeal.Sealers)
	}

	// Calculate participation rate
	participationRate := 0.0
	missedValidatorRate := 0.0
	if validatorCount > 0 {
		participationRate = float64(commitCount) / float64(validatorCount) * 100.0
		missedValidatorRate = float64(validatorCount-commitCount) / float64(validatorCount) * 100.0
	}

	// Determine epoch boundary and extract epoch info
	isEpochBoundary := wbftExtra.EpochInfo != nil && len(wbftExtra.EpochInfo.Validators) > 0
	var epochNumber *uint64
	var epochValidators []common.Address

	if isEpochBoundary && wbftExtra.EpochInfo != nil {
		epochNum := wbftExtra.EpochInfo.EpochNumber
		epochNumber = &epochNum
		// Extract validator addresses from candidates using validator indices
		for _, idx := range wbftExtra.EpochInfo.Validators {
			if int(idx) < len(wbftExtra.EpochInfo.Candidates) {
				epochValidators = append(epochValidators, wbftExtra.EpochInfo.Candidates[idx].Address)
			}
		}
	}

	// Create consensus block event
	consensusEvent := events.NewConsensusBlockEvent(
		wbftExtra.BlockNumber,
		wbftExtra.BlockHash,
		wbftExtra.Timestamp,
		wbftExtra.Round,
		wbftExtra.PrevRound,
		block.Coinbase(),
		validatorCount,
		prepareCount,
		commitCount,
		participationRate,
		missedValidatorRate,
		isEpochBoundary,
		epochNumber,
		epochValidators,
	)

	// Publish to EventBus
	if !f.eventBus.Publish(consensusEvent) {
		f.logger.Warn("Failed to publish consensus block event (channel full)",
			zap.Uint64("height", block.NumberU64()),
		)
	}

	// Publish consensus error event if round changed (round > 0)
	if wbftExtra.Round > 0 {
		f.publishConsensusErrorEvent(block, wbftExtra, "round_change", "medium",
			fmt.Sprintf("Consensus required %d rounds to finalize block", wbftExtra.Round+1),
			validatorCount, commitCount, participationRate)
	}

	// Publish consensus error event if low participation (< 67%)
	if participationRate < 67.0 && validatorCount > 0 {
		f.publishConsensusErrorEvent(block, wbftExtra, "low_participation", "high",
			fmt.Sprintf("Low validator participation: %.2f%%", participationRate),
			validatorCount, commitCount, participationRate)
	}
}

// publishConsensusErrorEvent creates and publishes a ConsensusErrorEvent
func (f *Fetcher) publishConsensusErrorEvent(block *types.Block, wbftExtra *storagepkg.WBFTBlockExtra,
	errorType, severity, errorMessage string, expectedValidators, actualSigners int, participationRate float64) {

	// Extract missed validators
	var missedValidators []common.Address
	if wbftExtra.EpochInfo != nil && wbftExtra.CommittedSeal != nil {
		committers, err := storagepkg.ExtractSigners(
			wbftExtra.CommittedSeal.Sealers,
			wbftExtra.EpochInfo.Validators,
			wbftExtra.EpochInfo.Candidates,
		)
		if err == nil {
			for _, candidate := range wbftExtra.EpochInfo.Candidates {
				if !containsAddress(committers, candidate.Address) {
					missedValidators = append(missedValidators, candidate.Address)
				}
			}
		}
	}

	errorEvent := events.NewConsensusErrorEvent(
		wbftExtra.BlockNumber,
		wbftExtra.BlockHash,
		wbftExtra.Timestamp,
		errorType,
		severity,
		errorMessage,
		wbftExtra.Round,
		expectedValidators,
		actualSigners,
		missedValidators,
		participationRate,
		false, // consensusImpacted - block was still finalized
		nil,   // errorDetails
	)

	if !f.eventBus.Publish(errorEvent) {
		f.logger.Warn("Failed to publish consensus error event (channel full)",
			zap.Uint64("height", block.NumberU64()),
			zap.String("errorType", errorType),
		)
	}
}

// containsAddress checks if an address is in a slice of addresses
func containsAddress(addresses []common.Address, target common.Address) bool {
	for _, addr := range addresses {
		if addr == target {
			return true
		}
	}
	return false
}

// countBitsInBitmap counts the number of set bits in a bitmap byte slice
func countBitsInBitmap(bitmap []byte) int {
	count := 0
	for _, b := range bitmap {
		// Count bits using Brian Kernighan's algorithm
		for b != 0 {
			count++
			b &= b - 1
		}
	}
	return count
}
