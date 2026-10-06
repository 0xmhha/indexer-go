// Package wbft is the stablenet.wbft feature: it stores WBFT consensus data
// from each block header (round, seals, epoch info, validator signing
// statistics) and publishes consensus events.
package wbft

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/consensus"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// Name is the feature name.
const Name = "stablenet.wbft"

type wbftFeature struct{}

func (wbftFeature) Name() string       { return Name }
func (wbftFeature) Requires() []string { return nil }

func (wbftFeature) Register(r feature.Registrar) error {
	d := r.Deps()
	logger := d.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	store, err := consensus.Open(d.Storage, logger)
	if err != nil {
		return err
	}
	r.OnBlock(&handler{writer: store, reader: store, blocks: d.Blocks(), logger: logger, publishFn: d.Publish})
	return nil
}

func init() { feature.Register(wbftFeature{}) }

type handler struct {
	writer    consensus.WBFTWriter
	reader    consensus.WBFTReader
	blocks    chains.AccountingEnv
	epochs    stablenet.EpochTracker
	logger    *zap.Logger
	publishFn func(events.Event) bool
}

func (h *handler) publish(ev events.Event) bool {
	if h.publishFn == nil {
		return true
	}
	return h.publishFn(ev)
}

// HandleBlock stores the WBFT data of a block header.
//
//   - The seals of a block's own round are the proposer's local view and
//     differ between nodes; the canonical seals of block N-1 (merged late
//     signatures included) are the Prev seals carried by block N. Signing
//     statistics are therefore recorded for N-1 when N arrives, against the
//     validator set of N-1's epoch, in seal bitmap order.
//   - The validator set of block N is the one recorded in the last epoch
//     block at or below N-1.
//   - Epochs are numbered in order from genesis (epoch 0); epoch lengths
//     differ between networks and can change at transitions, so the number
//     is not derived from the height.
//
// A header that is not WBFT (for example genesis on some networks) is
// logged and skipped.
func (h *handler) HandleBlock(ctx context.Context, b *feature.Block) error {
	wbftExtra, err := consensus.ParseWBFTExtra(b.Geth.Header())
	if err != nil {
		h.logger.Warn("Failed to parse WBFT extra",
			zap.Uint64("height", b.Model.Number),
			zap.String("hash", b.Model.Hash.Hex()),
			zap.Error(err),
		)
		return nil
	}
	// The go-ethereum view recomputes the hash with the Ethereum rule; WBFT
	// blocks are identified by the hash the chain reports (D16).
	wbftExtra.BlockHash = b.Model.Hash

	// The validator set of the parent (for its canonical seals in this
	// block) and of this block.
	parentEpoch, known := h.epochs.Applied(b.Model.ParentHash)
	if !known && b.Model.Number > 0 {
		if parentEpoch, err = stablenet.EpochOf(ctx, b.Model.Number-1, h.blocks); err != nil {
			return fmt.Errorf("epoch of block %d: %w", b.Model.Number-1, err)
		}
	}
	epoch, err := h.epochs.For(ctx, b.Model, h.blocks)
	if err != nil {
		return fmt.Errorf("epoch of block %d: %w", b.Model.Number, err)
	}

	if wbftExtra.EpochInfo != nil {
		number, err := h.epochNumber(ctx, b.Model.Number)
		if err != nil {
			return err
		}
		wbftExtra.EpochInfo.EpochNumber = number
	}
	if err := h.writer.SaveWBFTBlockExtra(ctx, wbftExtra); err != nil {
		return fmt.Errorf("failed to save WBFT block extra: %w", err)
	}
	if wbftExtra.EpochInfo != nil {
		if err := h.writer.SaveEpochInfo(ctx, wbftExtra.EpochInfo); err != nil {
			return fmt.Errorf("failed to save epoch info: %w", err)
		}
	}

	if err := h.recordParentSigning(ctx, b, wbftExtra, parentEpoch); err != nil {
		return err
	}

	h.logger.Debug("Processed WBFT metadata",
		zap.Uint64("height", b.Model.Number),
		zap.Uint32("round", wbftExtra.Round),
		zap.Bool("has_epoch_info", wbftExtra.EpochInfo != nil),
	)

	h.publishConsensusBlockEvent(b, wbftExtra, epoch)
	return nil
}

// epochNumber numbers a new epoch block: one after the latest stored epoch,
// 0 for genesis. Without earlier epochs (indexing started after genesis) it
// falls back to the default epoch length.
func (h *handler) epochNumber(ctx context.Context, height uint64) (uint64, error) {
	if height == 0 {
		return 0, nil
	}
	latest, err := h.reader.GetLatestEpochInfo(ctx)
	switch {
	case err == nil && latest != nil && latest.BlockNumber < height:
		return latest.EpochNumber + 1, nil
	case err == nil && latest != nil && latest.BlockNumber == height:
		return latest.EpochNumber, nil
	case err != nil && !errors.Is(err, port.ErrNotFound):
		return 0, fmt.Errorf("latest epoch: %w", err)
	}
	h.logger.Warn("No earlier epoch indexed; numbering the epoch from the default epoch length",
		zap.Uint64("height", height), zap.Uint64("epoch_length", constants.DefaultEpochLength))
	return height / constants.DefaultEpochLength, nil
}

// recordParentSigning records who signed block N-1, from the Prev seals in
// block N.
func (h *handler) recordParentSigning(ctx context.Context, b *feature.Block, x *consensus.WBFTBlockExtra, parentEpoch *stablenet.EpochInfo) error {
	if b.Model.Number < 2 || parentEpoch == nil || (x.PrevPreparedSeal == nil && x.PrevCommittedSeal == nil) {
		return nil // genesis is not sealed
	}
	validators, err := parentEpoch.ValidatorAddresses()
	if err != nil {
		return fmt.Errorf("validators of block %d: %w", b.Model.Number-1, err)
	}
	var timestamp uint64
	if parent, err := h.blocks.Block(ctx, b.Model.Number-1); err == nil {
		timestamp = parent.Time
	}
	activities := make([]*consensus.ValidatorSigningActivity, len(validators))
	for i, addr := range validators {
		activities[i] = &consensus.ValidatorSigningActivity{
			BlockNumber:      b.Model.Number - 1,
			BlockHash:        b.Model.ParentHash,
			ValidatorAddress: addr,
			ValidatorIndex:   uint32(i),
			SignedPrepare:    x.PrevPreparedSeal != nil && sealed(x.PrevPreparedSeal.Sealers, i),
			SignedCommit:     x.PrevCommittedSeal != nil && sealed(x.PrevCommittedSeal.Sealers, i),
			Round:            x.PrevRound,
			Timestamp:        timestamp,
		}
	}
	if err := h.writer.UpdateValidatorSigningStats(ctx, b.Model.Number-1, activities); err != nil {
		return fmt.Errorf("failed to update validator signing stats: %w", err)
	}
	return nil
}

// sealed reports whether validator i is set in a seal bitmap (bit i of
// byte i/8, least significant first: go-stablenet SealerSet).
func sealed(bitmap []byte, i int) bool {
	return i/8 < len(bitmap) && bitmap[i/8]&(1<<(i%8)) != 0
}

// publishConsensusBlockEvent creates and publishes a ConsensusBlockEvent
// The counts use this block's own (local) seals, as the node that served
// the block saw them, against the validator set of the block's epoch.
func (h *handler) publishConsensusBlockEvent(b *feature.Block, wbftExtra *consensus.WBFTBlockExtra, epoch *stablenet.EpochInfo) {
	var validators []common.Address
	if epoch != nil {
		validators, _ = epoch.ValidatorAddresses()
	}
	validatorCount := len(validators)
	prepareCount := 0
	commitCount := 0

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
	consensusEvent := consensus.NewConsensusBlockEvent(
		wbftExtra.BlockNumber,
		wbftExtra.BlockHash,
		wbftExtra.Timestamp,
		wbftExtra.Round,
		wbftExtra.PrevRound,
		b.Model.Miner,
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
	if !h.publish(consensusEvent) {
		h.logger.Warn("Failed to publish consensus block event (channel full)",
			zap.Uint64("height", b.Model.Number),
		)
	}

	// Publish consensus error event if round changed (round > 0)
	if wbftExtra.Round > 0 {
		h.publishConsensusErrorEvent(b, wbftExtra, validators, "round_change", "medium",
			fmt.Sprintf("Consensus required %d rounds to finalize block", wbftExtra.Round+1),
			validatorCount, commitCount, participationRate)
	}

	// Publish consensus error event if low participation (< 67%)
	if participationRate < 67.0 && validatorCount > 0 {
		h.publishConsensusErrorEvent(b, wbftExtra, validators, "low_participation", "high",
			fmt.Sprintf("Low validator participation: %.2f%%", participationRate),
			validatorCount, commitCount, participationRate)
	}
}

// publishConsensusErrorEvent creates and publishes a ConsensusErrorEvent
func (h *handler) publishConsensusErrorEvent(b *feature.Block, wbftExtra *consensus.WBFTBlockExtra, validators []common.Address,
	errorType, severity, errorMessage string, expectedValidators, actualSigners int, participationRate float64) {

	// Validators of the block's epoch whose commit seal is missing
	var missedValidators []common.Address
	if wbftExtra.CommittedSeal != nil {
		for i, v := range validators {
			if !sealed(wbftExtra.CommittedSeal.Sealers, i) {
				missedValidators = append(missedValidators, v)
			}
		}
	}

	errorEvent := consensus.NewConsensusErrorEvent(
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

	if !h.publish(errorEvent) {
		h.logger.Warn("Failed to publish consensus error event (channel full)",
			zap.Uint64("height", b.Model.Number),
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
