package storage

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	consensustypes "github.com/0xmhha/indexer-go/types/consensus"
)

// ConsensusStorage provides consensus data storage operations
// This bridges the gap between fetch/consensus types and storage types
type ConsensusStorage struct {
	storage *PebbleStorage
	logger  *zap.Logger
}

// NewConsensusStorage creates a new ConsensusStorage instance
func NewConsensusStorage(storage *PebbleStorage, logger *zap.Logger) *ConsensusStorage {
	return &ConsensusStorage{
		storage: storage,
		logger:  logger,
	}
}

// SaveConsensusData saves consensus data extracted from a block
// This is the primary method for storing consensus information
func (cs *ConsensusStorage) SaveConsensusData(ctx context.Context, data *consensustypes.ConsensusData) error {
	if data == nil {
		return fmt.Errorf("consensus data is nil")
	}

	// Convert ConsensusData to WBFTBlockExtra for storage
	wbftExtra := cs.convertToWBFTBlockExtra(data)

	// Save WBFT block extra
	if err := cs.storage.SaveWBFTBlockExtra(ctx, wbftExtra); err != nil {
		return fmt.Errorf("failed to save WBFT block extra: %w", err)
	}

	// Save epoch info if this is an epoch boundary
	if data.IsEpochBoundary && data.EpochInfo != nil {
		epochInfo := cs.convertToEpochInfo(data.EpochInfo, data.BlockNumber)
		if err := cs.storage.SaveEpochInfo(ctx, epochInfo); err != nil {
			return fmt.Errorf("failed to save epoch info: %w", err)
		}
	}

	// Update validator signing statistics
	signingActivities := cs.createSigningActivities(data)
	if err := cs.storage.UpdateValidatorSigningStats(ctx, data.BlockNumber, signingActivities); err != nil {
		return fmt.Errorf("failed to update validator signing stats: %w", err)
	}

	cs.logger.Debug("Saved consensus data",
		zap.Uint64("block_number", data.BlockNumber),
		zap.String("block_hash", data.BlockHash.Hex()),
		zap.Uint32("round", data.Round),
		zap.Int("validator_count", len(data.Validators)),
		zap.Int("commit_count", data.CommitCount),
		zap.Bool("is_epoch_boundary", data.IsEpochBoundary),
	)

	return nil
}

// GetConsensusData retrieves consensus data for a specific block
func (cs *ConsensusStorage) GetConsensusData(ctx context.Context, blockNumber uint64) (*consensustypes.ConsensusData, error) {
	// Get WBFT block extra
	wbftExtra, err := cs.storage.GetWBFTBlockExtra(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get WBFT block extra: %w", err)
	}

	// Get block to extract proposer (coinbase)
	block, err := cs.storage.GetBlock(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	// Try to get epoch info for this block (it may not exist if this isn't an epoch boundary)
	// We need to get the latest epoch to find validator information
	latestEpoch, err := cs.storage.GetLatestEpochInfo(ctx)
	if err == nil && latestEpoch != nil {
		// Attach epoch info to wbftExtra for validator extraction
		wbftExtra.EpochInfo = latestEpoch
	}

	// Get block signers
	prepareSigners, commitSigners, err := cs.storage.GetBlockSigners(ctx, blockNumber)
	if err != nil {
		cs.logger.Warn("Failed to get block signers",
			zap.Uint64("block_number", blockNumber),
			zap.Error(err),
		)
		// Continue with empty signers
		prepareSigners = []common.Address{}
		commitSigners = []common.Address{}
	}

	// Get validator list from epoch info if available
	var validators []common.Address
	if wbftExtra.EpochInfo != nil && len(wbftExtra.EpochInfo.Candidates) > 0 {
		validators = make([]common.Address, 0, len(wbftExtra.EpochInfo.Validators))
		for _, validatorIndex := range wbftExtra.EpochInfo.Validators {
			if int(validatorIndex) < len(wbftExtra.EpochInfo.Candidates) {
				validators = append(validators, wbftExtra.EpochInfo.Candidates[validatorIndex].Address)
			}
		}
	}

	// Convert to ConsensusData
	data := cs.convertToConsensusData(wbftExtra, block.Header().Coinbase, validators, prepareSigners, commitSigners)

	return data, nil
}

// GetValidatorStats retrieves validator statistics over a block range
func (cs *ConsensusStorage) GetValidatorStats(
	ctx context.Context,
	validatorAddr common.Address,
	fromBlock, toBlock uint64,
) (*consensustypes.ValidatorStats, error) {
	// Get signing activities for this validator in the range
	activities, err := cs.storage.GetValidatorSigningActivity(ctx, validatorAddr, fromBlock, toBlock, 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator signing activity: %w", err)
	}

	// Aggregate statistics from activities
	stats := &consensustypes.ValidatorStats{
		Address:     validatorAddr,
		TotalBlocks: uint64(len(activities)),
	}

	for _, activity := range activities {
		if activity.SignedPrepare {
			stats.PreparesSigned++
		} else {
			stats.PreparesMissed++
		}

		if activity.SignedCommit {
			stats.CommitsSigned++
		} else {
			stats.CommitsMissed++
		}
	}

	// Calculate participation rate
	stats.CalculateParticipationRate()

	return stats, nil
}

// GetValidatorParticipation retrieves detailed validator participation over a block range
func (cs *ConsensusStorage) GetValidatorParticipation(
	ctx context.Context,
	validatorAddr common.Address,
	fromBlock, toBlock uint64,
	limit, offset int,
) (*consensustypes.ValidatorParticipation, error) {
	// Get signing activity from storage
	activities, err := cs.storage.GetValidatorSigningActivity(ctx, validatorAddr, fromBlock, toBlock, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator signing activity: %w", err)
	}

	// Convert to ValidatorParticipation
	participation := &consensustypes.ValidatorParticipation{
		Address:    validatorAddr,
		StartBlock: fromBlock,
		EndBlock:   toBlock,
		Blocks:     make([]consensustypes.BlockParticipation, 0, len(activities)),
	}

	var blocksProposed, blocksCommitted, blocksMissed uint64

	for _, activity := range activities {
		// Check if this validator was proposer (need to query block data)
		// For now, we'll leave this as false and update in a separate pass if needed
		wasProposer := false

		participation.Blocks = append(participation.Blocks, consensustypes.BlockParticipation{
			BlockNumber:   activity.BlockNumber,
			WasProposer:   wasProposer,
			SignedPrepare: activity.SignedPrepare,
			SignedCommit:  activity.SignedCommit,
			Round:         activity.Round,
		})

		if activity.SignedCommit {
			blocksCommitted++
		} else {
			blocksMissed++
		}
	}

	participation.TotalBlocks = uint64(len(activities))
	participation.BlocksProposed = blocksProposed
	participation.BlocksCommitted = blocksCommitted
	participation.BlocksMissed = blocksMissed

	if participation.TotalBlocks > 0 {
		participation.ParticipationRate = float64(blocksCommitted) / float64(participation.TotalBlocks) * 100.0
	}

	return participation, nil
}

// GetAllValidatorStats retrieves statistics for all validators in a block range
func (cs *ConsensusStorage) GetAllValidatorStats(
	ctx context.Context,
	fromBlock, toBlock uint64,
	limit, offset int,
) (map[common.Address]*consensustypes.ValidatorStats, error) {
	// Get all validators signing stats from storage
	signingStatsList, err := cs.storage.GetAllValidatorsSigningStats(ctx, fromBlock, toBlock, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get all validators signing stats: %w", err)
	}

	// Convert to map of ValidatorStats
	statsMap := make(map[common.Address]*consensustypes.ValidatorStats)

	for _, signingStats := range signingStatsList {
		stats := &consensustypes.ValidatorStats{
			Address:        signingStats.ValidatorAddress,
			TotalBlocks:    toBlock - fromBlock + 1,
			PreparesSigned: signingStats.PrepareSignCount,
			CommitsSigned:  signingStats.CommitSignCount,
			PreparesMissed: signingStats.PrepareMissCount,
			CommitsMissed:  signingStats.CommitMissCount,
		}

		stats.CalculateParticipationRate()
		statsMap[signingStats.ValidatorAddress] = stats
	}

	return statsMap, nil
}

// GetEpochInfo retrieves epoch information for a specific epoch
func (cs *ConsensusStorage) GetEpochInfo(ctx context.Context, epochNumber uint64) (*consensustypes.EpochData, error) {
	// Get epoch info from storage
	epochInfo, err := cs.storage.GetEpochInfo(ctx, epochNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get epoch info: %w", err)
	}

	// Convert to EpochData
	epochData := &consensustypes.EpochData{
		EpochNumber:    epochInfo.EpochNumber,
		ValidatorCount: len(epochInfo.Validators),
		CandidateCount: len(epochInfo.Candidates),
		Validators:     make([]consensustypes.ValidatorInfo, 0, len(epochInfo.Validators)),
		Candidates:     make([]consensustypes.CandidateInfo, 0, len(epochInfo.Candidates)),
	}

	// Convert validators
	for i, validatorIndex := range epochInfo.Validators {
		if int(validatorIndex) >= len(epochInfo.Candidates) {
			continue
		}

		candidate := epochInfo.Candidates[validatorIndex]
		var blsPubKey []byte
		if i < len(epochInfo.BLSPublicKeys) {
			blsPubKey = epochInfo.BLSPublicKeys[i]
		}

		epochData.Validators = append(epochData.Validators, consensustypes.ValidatorInfo{
			Address:   candidate.Address,
			Index:     validatorIndex,
			BLSPubKey: blsPubKey,
		})
	}

	// Convert candidates
	for _, candidate := range epochInfo.Candidates {
		epochData.Candidates = append(epochData.Candidates, consensustypes.CandidateInfo{
			Address:   candidate.Address,
			Diligence: candidate.Diligence,
		})
	}

	return epochData, nil
}

// GetLatestEpochInfo retrieves the most recent epoch information
func (cs *ConsensusStorage) GetLatestEpochInfo(ctx context.Context) (*consensustypes.EpochData, error) {
	epochInfo, err := cs.storage.GetLatestEpochInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest epoch info: %w", err)
	}

	// Get the epoch number and use GetEpochInfo for conversion
	return cs.GetEpochInfo(ctx, epochInfo.EpochNumber)
}

// Conversion helper functions

// convertToWBFTBlockExtra converts ConsensusData to WBFTBlockExtra for storage
func (cs *ConsensusStorage) convertToWBFTBlockExtra(data *consensustypes.ConsensusData) *WBFTBlockExtra {
	extra := &WBFTBlockExtra{
		BlockNumber:  data.BlockNumber,
		BlockHash:    data.BlockHash,
		RandaoReveal: data.RandaoReveal,
		PrevRound:    data.PrevRound,
		Round:        data.Round,
		GasTip:       data.GasTip,
		Timestamp:    data.Timestamp,
	}

	// Convert seals if present
	if data.VanityData != nil {
		extra.RandaoReveal = data.RandaoReveal
	}

	// Note: PrevPreparedSeal and PrevCommittedSeal are not in ConsensusData
	// They would need to be added to ConsensusData or retrieved separately

	// Convert prepared seal
	// PreparedSeal is not directly in ConsensusData
	// We would need to add it or calculate it from PrepareSigners

	// Convert committed seal
	// CommittedSeal is not directly in ConsensusData
	// We would need to add it or calculate it from CommitSigners

	return extra
}

// convertToConsensusData converts WBFTBlockExtra to ConsensusData
func (cs *ConsensusStorage) convertToConsensusData(
	extra *WBFTBlockExtra,
	proposer common.Address,
	validators []common.Address,
	prepareSigners, commitSigners []common.Address,
) *consensustypes.ConsensusData {
	data := &consensustypes.ConsensusData{
		BlockNumber:    extra.BlockNumber,
		BlockHash:      extra.BlockHash,
		Round:          extra.Round,
		PrevRound:      extra.PrevRound,
		RoundChanged:   extra.Round > 0,
		Proposer:       proposer,
		Validators:     validators,
		PrepareSigners: prepareSigners,
		CommitSigners:  commitSigners,
		PrepareCount:   len(prepareSigners),
		CommitCount:    len(commitSigners),
		RandaoReveal:   extra.RandaoReveal,
		GasTip:         extra.GasTip,
		Timestamp:      extra.Timestamp,
	}

	// Calculate missed validators
	data.CalculateMissedValidators()

	// Convert epoch info if present
	if extra.EpochInfo != nil {
		// Full epoch data conversion
		epochData := &consensustypes.EpochData{
			EpochNumber:    extra.EpochInfo.EpochNumber,
			ValidatorCount: len(extra.EpochInfo.Validators),
			CandidateCount: len(extra.EpochInfo.Candidates),
			Validators:     make([]consensustypes.ValidatorInfo, 0, len(extra.EpochInfo.Validators)),
			Candidates:     make([]consensustypes.CandidateInfo, 0, len(extra.EpochInfo.Candidates)),
		}

		// Convert validators
		for i, validatorIndex := range extra.EpochInfo.Validators {
			if int(validatorIndex) >= len(extra.EpochInfo.Candidates) {
				continue
			}

			candidate := extra.EpochInfo.Candidates[validatorIndex]
			var blsPubKey []byte
			if i < len(extra.EpochInfo.BLSPublicKeys) {
				blsPubKey = extra.EpochInfo.BLSPublicKeys[i]
			}

			epochData.Validators = append(epochData.Validators, consensustypes.ValidatorInfo{
				Address:   candidate.Address,
				Index:     validatorIndex,
				BLSPubKey: blsPubKey,
			})
		}

		// Convert candidates
		for _, candidate := range extra.EpochInfo.Candidates {
			epochData.Candidates = append(epochData.Candidates, consensustypes.CandidateInfo{
				Address:   candidate.Address,
				Diligence: candidate.Diligence,
			})
		}

		data.EpochInfo = epochData
		data.IsEpochBoundary = true
	}

	return data
}

// convertToEpochInfo converts consensus EpochData to storage EpochInfo
func (cs *ConsensusStorage) convertToEpochInfo(epochData *consensustypes.EpochData, blockNumber uint64) *EpochInfo {
	epochInfo := &EpochInfo{
		EpochNumber:   epochData.EpochNumber,
		BlockNumber:   blockNumber,
		Candidates:    make([]Candidate, 0, len(epochData.Candidates)),
		Validators:    make([]uint32, 0, len(epochData.Validators)),
		BLSPublicKeys: make([][]byte, 0, len(epochData.Validators)),
	}

	// Convert candidates
	for _, candidate := range epochData.Candidates {
		epochInfo.Candidates = append(epochInfo.Candidates, Candidate{
			Address:   candidate.Address,
			Diligence: candidate.Diligence,
		})
	}

	// Convert validators
	for _, validator := range epochData.Validators {
		epochInfo.Validators = append(epochInfo.Validators, validator.Index)
		epochInfo.BLSPublicKeys = append(epochInfo.BLSPublicKeys, validator.BLSPubKey)
	}

	return epochInfo
}

// createSigningActivities creates ValidatorSigningActivity records from ConsensusData
func (cs *ConsensusStorage) createSigningActivities(data *consensustypes.ConsensusData) []*ValidatorSigningActivity {
	activities := make([]*ValidatorSigningActivity, 0, len(data.Validators))

	// Create sets for efficient lookup
	prepareSigned := make(map[common.Address]bool)
	for _, addr := range data.PrepareSigners {
		prepareSigned[addr] = true
	}

	commitSigned := make(map[common.Address]bool)
	for _, addr := range data.CommitSigners {
		commitSigned[addr] = true
	}

	// Create activity for each validator
	for i, validatorAddr := range data.Validators {
		activity := &ValidatorSigningActivity{
			BlockNumber:      data.BlockNumber,
			BlockHash:        data.BlockHash,
			ValidatorAddress: validatorAddr,
			ValidatorIndex:   uint32(i),
			SignedPrepare:    prepareSigned[validatorAddr],
			SignedCommit:     commitSigned[validatorAddr],
			Round:            data.Round,
			Timestamp:        data.Timestamp,
		}
		activities = append(activities, activity)
	}

	return activities
}
