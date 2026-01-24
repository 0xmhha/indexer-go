package fetch

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/events"
	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
)

// ConsensusClient extends the base Client with consensus-specific operations
type ConsensusClient interface {
	Client

	// GetConsensusData extracts consensus data from a block
	GetConsensusData(ctx context.Context, blockNumber uint64) (*consensustypes.ConsensusData, error)

	// GetValidatorsAtBlock returns the active validator set at a specific block
	GetValidatorsAtBlock(ctx context.Context, blockNumber uint64) ([]common.Address, error)
}

// ConsensusFetcher handles fetching and parsing WBFT consensus data from blocks
// It implements the ConsensusDataProvider interface
type ConsensusFetcher struct {
	client   Client
	parser   *WBFTParser
	logger   *zap.Logger
	eventBus *events.EventBus
}

// NewConsensusFetcher creates a new ConsensusFetcher instance
func NewConsensusFetcher(client Client, logger *zap.Logger) *ConsensusFetcher {
	return &ConsensusFetcher{
		client:   client,
		parser:   NewWBFTParser(logger),
		logger:   logger,
		eventBus: nil,
	}
}

// SetEventBus sets the EventBus for publishing consensus events
func (cf *ConsensusFetcher) SetEventBus(eventBus *events.EventBus) {
	cf.eventBus = eventBus
}

// GetConsensusData extracts and parses consensus data from a block
// This implements the primary method for fetching consensus information
func (cf *ConsensusFetcher) GetConsensusData(ctx context.Context, blockNumber uint64) (*consensustypes.ConsensusData, error) {
	// Fetch the block
	block, err := cf.client.GetBlockByNumber(ctx, blockNumber)
	if err != nil {
		cf.logger.Error("Failed to fetch block for consensus data",
			zap.Uint64("block_number", blockNumber),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to fetch block %d: %w", blockNumber, err)
	}

	// Extract consensus data from block header
	consensusData, err := cf.ExtractConsensusData(block.Header())
	if err != nil {
		cf.logger.Error("Failed to extract consensus data from block",
			zap.Uint64("block_number", blockNumber),
			zap.String("block_hash", block.Hash().Hex()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to extract consensus data from block %d: %w", blockNumber, err)
	}

	cf.logger.Debug("Successfully extracted consensus data",
		zap.Uint64("block_number", blockNumber),
		zap.Uint32("round", consensusData.Round),
		zap.Int("validator_count", len(consensusData.Validators)),
		zap.Int("commit_count", consensusData.CommitCount),
		zap.Float64("participation_rate", consensusData.ParticipationRate()),
	)

	// Publish consensus block event
	if cf.eventBus != nil {
		cf.publishConsensusBlockEvent(consensusData)
	}

	// Check for validator changes at epoch boundaries
	if consensusData.IsEpochBoundary && cf.eventBus != nil {
		cf.publishValidatorChangeEvent(consensusData)
	}

	// Check for consensus errors or anomalies
	if cf.eventBus != nil {
		cf.checkAndPublishConsensusErrors(consensusData)
	}

	return consensusData, nil
}

// ExtractConsensusData extracts consensus data from a block header
// This implements the ConsensusDataProvider interface method
func (cf *ConsensusFetcher) ExtractConsensusData(header *types.Header) (*consensustypes.ConsensusData, error) {
	if header == nil {
		return nil, fmt.Errorf("header is nil")
	}

	// Parse WBFT extra data from header
	wbftExtra, err := cf.parser.ParseWBFTExtra(header)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WBFT extra data: %w", err)
	}

	// Extract validators from the current committed seal
	validators, err := cf.parser.ExtractValidators(wbftExtra)
	if err != nil {
		cf.logger.Warn("Failed to extract validators from WBFT extra",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Error(err),
		)
		// Continue with empty validator list
		validators = []common.Address{}
	}

	// Extract prepare signers from prepared seal
	prepareSigners, err := cf.parser.ExtractSignersFromSeal(wbftExtra.PreparedSeal, validators)
	if err != nil {
		cf.logger.Warn("Failed to extract prepare signers",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Error(err),
		)
		prepareSigners = []common.Address{}
	}

	// Extract commit signers from committed seal
	commitSigners, err := cf.parser.ExtractSignersFromSeal(wbftExtra.CommittedSeal, validators)
	if err != nil {
		cf.logger.Warn("Failed to extract commit signers",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Error(err),
		)
		commitSigners = []common.Address{}
	}

	// Build ConsensusData structure
	consensusData := &consensustypes.ConsensusData{
		BlockNumber:    header.Number.Uint64(),
		BlockHash:      header.Hash(),
		Round:          wbftExtra.Round,
		PrevRound:      wbftExtra.PrevRound,
		RoundChanged:   wbftExtra.Round > 0,
		Proposer:       header.Coinbase, // Block proposer is in Coinbase field
		Validators:     validators,
		PrepareSigners: prepareSigners,
		CommitSigners:  commitSigners,
		PrepareCount:   len(prepareSigners),
		CommitCount:    len(commitSigners),
		VanityData:     wbftExtra.VanityData,
		RandaoReveal:   wbftExtra.RandaoReveal,
		GasTip:         wbftExtra.GasTip,
		Timestamp:      header.Time,
	}

	// Calculate missed validators
	consensusData.CalculateMissedValidators()

	// Parse epoch info if this is an epoch boundary
	if wbftExtra.EpochInfo != nil {
		epochData, err := cf.parser.ParseEpochInfo(header, wbftExtra.EpochInfo)
		if err != nil {
			cf.logger.Warn("Failed to parse epoch info",
				zap.Uint64("block_number", header.Number.Uint64()),
				zap.Error(err),
			)
		} else {
			consensusData.EpochInfo = epochData
			consensusData.IsEpochBoundary = true

			cf.logger.Info("Epoch boundary detected",
				zap.Uint64("block_number", header.Number.Uint64()),
				zap.Uint64("epoch_number", epochData.EpochNumber),
				zap.Int("validator_count", epochData.ValidatorCount),
			)
		}
	}

	return consensusData, nil
}

// ExtractWBFTExtra extracts the raw WBFT extra data structure from a block header
// This implements the ConsensusDataProvider interface method
func (cf *ConsensusFetcher) ExtractWBFTExtra(header *types.Header) (*consensustypes.WBFTExtra, error) {
	if header == nil {
		return nil, fmt.Errorf("header is nil")
	}

	return cf.parser.ParseWBFTExtra(header)
}

// ParseEpochInfo extracts epoch information from a block header
// This implements the ConsensusDataProvider interface method
func (cf *ConsensusFetcher) ParseEpochInfo(header *types.Header) (*consensustypes.EpochData, error) {
	if header == nil {
		return nil, fmt.Errorf("header is nil")
	}

	// First parse the WBFT extra data
	wbftExtra, err := cf.parser.ParseWBFTExtra(header)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WBFT extra: %w", err)
	}

	// Check if epoch info is present
	if wbftExtra.EpochInfo == nil {
		return nil, nil // Not an epoch boundary
	}

	// Parse epoch info
	return cf.parser.ParseEpochInfo(header, wbftExtra.EpochInfo)
}

// GetValidatorsAtBlock returns the active validator set at a specific block
func (cf *ConsensusFetcher) GetValidatorsAtBlock(ctx context.Context, blockNumber uint64) ([]common.Address, error) {
	// Fetch consensus data which includes validators
	consensusData, err := cf.GetConsensusData(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get consensus data: %w", err)
	}

	return consensusData.Validators, nil
}

// GetValidatorParticipation gets detailed validator participation over a block range
func (cf *ConsensusFetcher) GetValidatorParticipation(
	ctx context.Context,
	validatorAddr common.Address,
	startBlock, endBlock uint64,
) (*consensustypes.ValidatorParticipation, error) {
	if startBlock > endBlock {
		return nil, fmt.Errorf("start block (%d) must be <= end block (%d)", startBlock, endBlock)
	}

	participation := &consensustypes.ValidatorParticipation{
		Address:    validatorAddr,
		StartBlock: startBlock,
		EndBlock:   endBlock,
		Blocks:     make([]consensustypes.BlockParticipation, 0, endBlock-startBlock+1),
	}

	var totalBlocks, blocksProposed, blocksCommitted, blocksMissed uint64

	// Iterate through block range
	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		consensusData, err := cf.GetConsensusData(ctx, blockNum)
		if err != nil {
			cf.logger.Error("Failed to get consensus data for participation analysis",
				zap.Uint64("block_number", blockNum),
				zap.String("validator", validatorAddr.Hex()),
				zap.Error(err),
			)
			continue
		}

		totalBlocks++

		// Check if this validator was involved
		wasProposer := consensusData.Proposer == validatorAddr
		signedPrepare := false
		signedCommit := false

		for _, signer := range consensusData.PrepareSigners {
			if signer == validatorAddr {
				signedPrepare = true
				break
			}
		}

		for _, signer := range consensusData.CommitSigners {
			if signer == validatorAddr {
				signedCommit = true
				blocksCommitted++
				break
			}
		}

		if wasProposer {
			blocksProposed++
		}

		if !signedCommit {
			blocksMissed++
		}

		// Record block participation
		participation.Blocks = append(participation.Blocks, consensustypes.BlockParticipation{
			BlockNumber:   blockNum,
			WasProposer:   wasProposer,
			SignedPrepare: signedPrepare,
			SignedCommit:  signedCommit,
			Round:         consensusData.Round,
		})
	}

	// Calculate aggregated statistics
	participation.TotalBlocks = totalBlocks
	participation.BlocksProposed = blocksProposed
	participation.BlocksCommitted = blocksCommitted
	participation.BlocksMissed = blocksMissed

	if totalBlocks > 0 {
		participation.ParticipationRate = float64(blocksCommitted) / float64(totalBlocks) * 100.0
	}

	return participation, nil
}

// AnalyzeRoundChanges analyzes round changes over a block range
func (cf *ConsensusFetcher) AnalyzeRoundChanges(
	ctx context.Context,
	startBlock, endBlock uint64,
) (*consensustypes.RoundAnalysis, error) {
	if startBlock > endBlock {
		return nil, fmt.Errorf("start block (%d) must be <= end block (%d)", startBlock, endBlock)
	}

	analysis := &consensustypes.RoundAnalysis{
		StartBlock:        startBlock,
		EndBlock:          endBlock,
		RoundDistribution: make([]consensustypes.RoundDistribution, 0),
	}

	roundCounts := make(map[uint32]uint64)
	var totalRound uint64
	var maxRound uint32
	var blocksWithRoundChange uint64

	// Iterate through block range
	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		consensusData, err := cf.GetConsensusData(ctx, blockNum)
		if err != nil {
			cf.logger.Error("Failed to get consensus data for round analysis",
				zap.Uint64("block_number", blockNum),
				zap.Error(err),
			)
			continue
		}

		analysis.TotalBlocks++
		roundCounts[consensusData.Round]++
		totalRound += uint64(consensusData.Round)

		if consensusData.Round > maxRound {
			maxRound = consensusData.Round
		}

		if consensusData.Round > 0 {
			blocksWithRoundChange++
		}
	}

	analysis.BlocksWithRoundChange = blocksWithRoundChange
	analysis.MaxRound = maxRound

	if analysis.TotalBlocks > 0 {
		analysis.RoundChangeRate = float64(blocksWithRoundChange) / float64(analysis.TotalBlocks) * 100.0
		analysis.AverageRound = float64(totalRound) / float64(analysis.TotalBlocks)
	}

	// Build round distribution
	for round, count := range roundCounts {
		percentage := float64(count) / float64(analysis.TotalBlocks) * 100.0
		analysis.RoundDistribution = append(analysis.RoundDistribution, consensustypes.RoundDistribution{
			Round:      round,
			Count:      count,
			Percentage: percentage,
		})
	}

	return analysis, nil
}

// publishConsensusBlockEvent publishes a consensus block event to the EventBus
func (cf *ConsensusFetcher) publishConsensusBlockEvent(consensusData *consensustypes.ConsensusData) {
	// Calculate missed validator rate
	missedValidatorRate := 0.0
	if len(consensusData.Validators) > 0 {
		missedValidatorRate = float64(len(consensusData.MissedCommit)) / float64(len(consensusData.Validators)) * 100.0
	}

	// Prepare epoch data if at boundary
	var epochNumber *uint64
	var epochValidators []common.Address
	if consensusData.IsEpochBoundary && consensusData.EpochInfo != nil {
		epochNum := consensusData.EpochInfo.EpochNumber
		epochNumber = &epochNum
		// Extract addresses from ValidatorInfo slice
		epochValidators = make([]common.Address, len(consensusData.EpochInfo.Validators))
		for i, v := range consensusData.EpochInfo.Validators {
			epochValidators[i] = v.Address
		}
	}

	// Create and publish event
	event := events.NewConsensusBlockEvent(
		consensusData.BlockNumber,
		consensusData.BlockHash,
		consensusData.Timestamp,
		consensusData.Round,
		consensusData.PrevRound,
		consensusData.Proposer,
		len(consensusData.Validators),
		consensusData.PrepareCount,
		consensusData.CommitCount,
		consensusData.ParticipationRate(),
		missedValidatorRate,
		consensusData.IsEpochBoundary,
		epochNumber,
		epochValidators,
	)

	if !cf.eventBus.Publish(event) {
		cf.logger.Warn("Failed to publish consensus block event - event bus full",
			zap.Uint64("block_number", consensusData.BlockNumber),
		)
	}
}

// publishValidatorChangeEvent publishes a validator set change event at epoch boundaries
func (cf *ConsensusFetcher) publishValidatorChangeEvent(consensusData *consensustypes.ConsensusData) {
	if !consensusData.IsEpochBoundary || consensusData.EpochInfo == nil {
		return
	}

	// Extract validator addresses from ValidatorInfo
	validatorAddresses := make([]common.Address, len(consensusData.EpochInfo.Validators))
	for i, v := range consensusData.EpochInfo.Validators {
		validatorAddresses[i] = v.Address
	}

	// Determine change type (simplified - would need previous set for accurate determination)
	changeType := "epoch_change"
	var addedValidators []common.Address
	var removedValidators []common.Address
	previousCount := len(consensusData.Validators) // Use current validator count as approximation
	newCount := len(validatorAddresses)

	// Create additional info
	additionalInfo := map[string]interface{}{
		"epochNumber":    consensusData.EpochInfo.EpochNumber,
		"blockNumber":    consensusData.BlockNumber,
		"previousEpoch":  consensusData.EpochInfo.EpochNumber - 1,
		"validatorCount": newCount,
		"candidateCount": consensusData.EpochInfo.CandidateCount,
	}

	// Create and publish event
	event := events.NewConsensusValidatorChangeEvent(
		consensusData.BlockNumber,
		consensusData.BlockHash,
		consensusData.Timestamp,
		consensusData.EpochInfo.EpochNumber,
		true,
		changeType,
		addedValidators,
		removedValidators,
		previousCount,
		newCount,
		validatorAddresses,
		additionalInfo,
	)

	if !cf.eventBus.Publish(event) {
		cf.logger.Warn("Failed to publish validator change event - event bus full",
			zap.Uint64("block_number", consensusData.BlockNumber),
			zap.Uint64("epoch_number", consensusData.EpochInfo.EpochNumber),
		)
	}
}

// checkAndPublishConsensusErrors checks for and publishes consensus error events
func (cf *ConsensusFetcher) checkAndPublishConsensusErrors(consensusData *consensustypes.ConsensusData) {
	// Check for round changes (indication of consensus issues)
	if consensusData.Round > 0 {
		cf.publishConsensusErrorEvent(
			consensusData,
			"round_change",
			"medium",
			fmt.Sprintf("Round change occurred: round %d (previous: %d)", consensusData.Round, consensusData.PrevRound),
			map[string]interface{}{
				"round":     consensusData.Round,
				"prevRound": consensusData.PrevRound,
			},
			false, // Round changes are normal, not consensus-impacting
		)
	}

	// Check for low participation
	participationRate := consensusData.ParticipationRate()
	if participationRate < constants.DefaultMinParticipationRate { // Less than 2/3 participation
		severity := "high"
		if participationRate < 50.0 {
			severity = "critical"
		}

		cf.publishConsensusErrorEvent(
			consensusData,
			"low_participation",
			severity,
			fmt.Sprintf("Low validator participation: %.2f%%", participationRate),
			map[string]interface{}{
				"participationRate":  participationRate,
				"expectedValidators": len(consensusData.Validators),
				"actualSigners":      consensusData.CommitCount,
				"missedCount":        len(consensusData.MissedCommit),
			},
			severity == "critical",
		)
	}

	// Check for missed validators
	if len(consensusData.MissedCommit) > 0 {
		missedRate := float64(len(consensusData.MissedCommit)) / float64(len(consensusData.Validators)) * 100.0
		severity := "low"
		if missedRate > 33.0 {
			severity = "medium"
		}
		if missedRate > 50.0 {
			severity = "high"
		}

		cf.publishConsensusErrorEvent(
			consensusData,
			"missed_validators",
			severity,
			fmt.Sprintf("%d validators missed commit (%d%%)", len(consensusData.MissedCommit), int(missedRate)),
			map[string]interface{}{
				"missedValidators": consensusData.MissedCommit,
				"missedCount":      len(consensusData.MissedCommit),
				"missedRate":       missedRate,
			},
			false,
		)
	}
}

// publishConsensusErrorEvent publishes a consensus error event
func (cf *ConsensusFetcher) publishConsensusErrorEvent(
	consensusData *consensustypes.ConsensusData,
	errorType string,
	severity string,
	errorMessage string,
	errorDetails map[string]interface{},
	consensusImpacted bool,
) {
	event := events.NewConsensusErrorEvent(
		consensusData.BlockNumber,
		consensusData.BlockHash,
		consensusData.Timestamp,
		errorType,
		severity,
		errorMessage,
		consensusData.Round,
		len(consensusData.Validators),
		consensusData.CommitCount,
		consensusData.MissedCommit,
		consensusData.ParticipationRate(),
		consensusImpacted,
		errorDetails,
	)

	if !cf.eventBus.Publish(event) {
		cf.logger.Warn("Failed to publish consensus error event - event bus full",
			zap.Uint64("block_number", consensusData.BlockNumber),
			zap.String("error_type", errorType),
		)
	}
}
