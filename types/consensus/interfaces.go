package consensus

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ConsensusDataProvider defines the interface for extracting consensus data from block headers
// This follows the Single Responsibility Principle by focusing solely on data extraction
type ConsensusDataProvider interface {
	// ExtractConsensusData parses the block header and extracts all consensus-related information
	// Returns ConsensusData structure or error if parsing fails
	ExtractConsensusData(header *types.Header) (*ConsensusData, error)

	// ExtractWBFTExtra parses the raw WBFT extra data from block header
	// Returns WBFTExtra structure or error if parsing fails
	ExtractWBFTExtra(header *types.Header) (*WBFTExtra, error)

	// ParseEpochInfo extracts epoch information from block header if present
	// Returns nil if block is not an epoch boundary
	ParseEpochInfo(header *types.Header) (*EpochData, error)
}

// ValidatorTracker defines the interface for tracking validator participation and performance
// This follows the Interface Segregation Principle by separating validator concerns
type ValidatorTracker interface {
	// GetValidatorsAtBlock returns the active validator set at a specific block
	GetValidatorsAtBlock(blockNum uint64) (*ValidatorSet, error)

	// GetValidatorStats returns aggregated statistics for a specific validator over a range
	GetValidatorStats(validatorAddr common.Address, startBlock, endBlock uint64) (*ValidatorStats, error)

	// GetValidatorParticipation returns detailed participation data for a validator
	GetValidatorParticipation(validatorAddr common.Address, startBlock, endBlock uint64) (*ValidatorParticipation, error)

	// GetAllValidatorStats returns statistics for all validators in a range
	GetAllValidatorStats(startBlock, endBlock uint64) (map[common.Address]*ValidatorStats, error)

	// GetValidatorActivity returns current activity status for a validator
	GetValidatorActivity(validatorAddr common.Address) (*ValidatorActivity, error)

	// TrackValidatorChange detects and records validator set changes at epoch boundaries
	TrackValidatorChange(blockNum uint64) (*ValidatorChange, error)
}

// RoundAnalyzer defines the interface for analyzing consensus round changes and health
// This follows the Single Responsibility Principle by focusing on round analysis
type RoundAnalyzer interface {
	// GetRoundInfo returns round information for a specific block
	GetRoundInfo(blockNum uint64) (*RoundInfo, error)

	// AnalyzeRoundChanges performs statistical analysis of round changes over a range
	AnalyzeRoundChanges(startBlock, endBlock uint64) (*RoundAnalysis, error)

	// GetBlocksWithRoundChanges returns blocks that experienced round changes in a range
	GetBlocksWithRoundChanges(startBlock, endBlock uint64) ([]uint64, error)

	// CalculateConsensusHealth returns overall consensus health metrics
	// Health is based on: round change frequency, participation rate, and validator activity
	CalculateConsensusHealth(startBlock, endBlock uint64) (float64, error)
}

// SealVerifier defines the interface for verifying BLS aggregated seals
// This is optional and primarily used for validation purposes
type SealVerifier interface {
	// VerifyPreparedSeal verifies the prepared seal from consensus data
	VerifyPreparedSeal(header *types.Header, seal *WBFTAggregatedSeal, validators []common.Address) error

	// VerifyCommittedSeal verifies the committed seal from consensus data
	VerifyCommittedSeal(header *types.Header, seal *WBFTAggregatedSeal, validators []common.Address) error

	// ExtractSignerAddresses extracts validator addresses from a seal bitmap
	ExtractSignerAddresses(seal *WBFTAggregatedSeal, validators []common.Address) ([]common.Address, error)
}

// ConsensusDataStore defines the interface for persisting and retrieving consensus data
// This follows the Dependency Inversion Principle by abstracting storage operations
type ConsensusDataStore interface {
	// StoreConsensusData persists consensus data for a block
	StoreConsensusData(blockNum uint64, data *ConsensusData) error

	// GetConsensusData retrieves consensus data for a specific block
	GetConsensusData(blockNum uint64) (*ConsensusData, error)

	// GetConsensusDataRange retrieves consensus data for a range of blocks
	GetConsensusDataRange(startBlock, endBlock uint64) ([]*ConsensusData, error)

	// StoreValidatorStats persists validator statistics
	StoreValidatorStats(validatorAddr common.Address, stats *ValidatorStats) error

	// GetValidatorStats retrieves validator statistics
	GetValidatorStats(validatorAddr common.Address) (*ValidatorStats, error)

	// StoreValidatorSet persists the validator set at a specific block
	StoreValidatorSet(blockNum uint64, validatorSet *ValidatorSet) error

	// GetValidatorSet retrieves the validator set at a specific block
	GetValidatorSet(blockNum uint64) (*ValidatorSet, error)

	// StoreValidatorChange persists a validator set change event
	StoreValidatorChange(change *ValidatorChange) error

	// GetValidatorChanges retrieves all validator changes in a range
	GetValidatorChanges(startBlock, endBlock uint64) ([]*ValidatorChange, error)
}

// ConsensusEventEmitter defines the interface for emitting consensus-related events
// This follows the Interface Segregation Principle by separating event concerns
type ConsensusEventEmitter interface {
	// EmitNewBlock emits an event when a new block with consensus data is processed
	EmitNewBlock(data *ConsensusData) error

	// EmitRoundChange emits an event when a round change is detected
	EmitRoundChange(blockNum uint64, round uint32) error

	// EmitValidatorChange emits an event when the validator set changes
	EmitValidatorChange(change *ValidatorChange) error

	// EmitValidatorMissed emits an event when a validator misses participation
	EmitValidatorMissed(blockNum uint64, validatorAddr common.Address, missType string) error

	// EmitConsensusHealthAlert emits an event when consensus health degrades
	EmitConsensusHealthAlert(health float64, reason string) error
}

// ConsensusMetricsCollector defines the interface for collecting consensus metrics
// This follows the Single Responsibility Principle by focusing on metrics collection
type ConsensusMetricsCollector interface {
	// RecordBlockConsensus records consensus metrics for a block
	RecordBlockConsensus(data *ConsensusData) error

	// RecordValidatorParticipation records validator participation metrics
	RecordValidatorParticipation(validatorAddr common.Address, blockNum uint64, participated bool) error

	// RecordRoundChange records a round change event
	RecordRoundChange(blockNum uint64, round uint32) error

	// RecordEpochChange records an epoch boundary event
	RecordEpochChange(epochNum uint64, validatorCount int) error

	// GetMetricsSummary returns aggregated metrics for a time range
	GetMetricsSummary(startBlock, endBlock uint64) (map[string]interface{}, error)
}
