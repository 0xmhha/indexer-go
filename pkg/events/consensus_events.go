package events

import (
	"encoding/json"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Consensus event types
const (
	// EventTypeConsensusBlock represents a consensus finalization event
	EventTypeConsensusBlock EventType = "consensusBlock"

	// EventTypeConsensusFork represents a chain fork detection event
	EventTypeConsensusFork EventType = "consensusFork"

	// EventTypeConsensusValidatorChange represents a validator set change event
	EventTypeConsensusValidatorChange EventType = "consensusValidatorChange"

	// EventTypeConsensusError represents a consensus error or anomaly event
	EventTypeConsensusError EventType = "consensusError"
)

// ConsensusBlockEvent represents a new block finalized with consensus data
type ConsensusBlockEvent struct {
	// Block identification
	BlockNumber    uint64
	BlockHash      common.Hash
	BlockTimestamp uint64

	// Consensus round information
	Round        uint32
	PrevRound    uint32
	RoundChanged bool

	// Block proposer
	Proposer common.Address

	// Validator participation
	ValidatorCount      int
	PrepareCount        int
	CommitCount         int
	ParticipationRate   float64
	MissedValidatorRate float64

	// Epoch information (only present at epoch boundaries)
	IsEpochBoundary bool
	EpochNumber     *uint64
	EpochValidators []common.Address

	// Event metadata
	CreatedAt time.Time
}

// Type implements Event interface
func (e *ConsensusBlockEvent) Type() EventType {
	return EventTypeConsensusBlock
}

// Timestamp implements Event interface
func (e *ConsensusBlockEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewConsensusBlockEvent creates a new consensus block event
func NewConsensusBlockEvent(
	blockNumber uint64,
	blockHash common.Hash,
	blockTimestamp uint64,
	round uint32,
	prevRound uint32,
	proposer common.Address,
	validatorCount int,
	prepareCount int,
	commitCount int,
	participationRate float64,
	missedValidatorRate float64,
	isEpochBoundary bool,
	epochNumber *uint64,
	epochValidators []common.Address,
) *ConsensusBlockEvent {
	return &ConsensusBlockEvent{
		BlockNumber:         blockNumber,
		BlockHash:           blockHash,
		BlockTimestamp:      blockTimestamp,
		Round:               round,
		PrevRound:           prevRound,
		RoundChanged:        round > 0,
		Proposer:            proposer,
		ValidatorCount:      validatorCount,
		PrepareCount:        prepareCount,
		CommitCount:         commitCount,
		ParticipationRate:   participationRate,
		MissedValidatorRate: missedValidatorRate,
		IsEpochBoundary:     isEpochBoundary,
		EpochNumber:         epochNumber,
		EpochValidators:     epochValidators,
		CreatedAt:           time.Now(),
	}
}

// ConsensusForkEvent represents a chain fork detection
type ConsensusForkEvent struct {
	// Fork location
	ForkBlockNumber uint64
	ForkBlockHash   common.Hash

	// Competing chains
	Chain1Hash   common.Hash
	Chain1Height uint64
	Chain1Weight string // Total difficulty as string

	Chain2Hash   common.Hash
	Chain2Height uint64
	Chain2Weight string // Total difficulty as string

	// Fork resolution
	Resolved     bool
	WinningChain int // 1 or 2, 0 if not resolved

	// Detection metadata
	DetectedAt   time.Time
	DetectionLag uint64 // Blocks between fork and detection

	// Event metadata
	CreatedAt time.Time
}

// Type implements Event interface
func (e *ConsensusForkEvent) Type() EventType {
	return EventTypeConsensusFork
}

// Timestamp implements Event interface
func (e *ConsensusForkEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewConsensusForkEvent creates a new fork detection event
func NewConsensusForkEvent(
	forkBlockNumber uint64,
	forkBlockHash common.Hash,
	chain1Hash common.Hash,
	chain1Height uint64,
	chain1Weight string,
	chain2Hash common.Hash,
	chain2Height uint64,
	chain2Weight string,
	detectionLag uint64,
) *ConsensusForkEvent {
	return &ConsensusForkEvent{
		ForkBlockNumber: forkBlockNumber,
		ForkBlockHash:   forkBlockHash,
		Chain1Hash:      chain1Hash,
		Chain1Height:    chain1Height,
		Chain1Weight:    chain1Weight,
		Chain2Hash:      chain2Hash,
		Chain2Height:    chain2Height,
		Chain2Weight:    chain2Weight,
		Resolved:        false,
		WinningChain:    0,
		DetectedAt:      time.Now(),
		DetectionLag:    detectionLag,
		CreatedAt:       time.Now(),
	}
}

// ResolveFork marks the fork as resolved with a winning chain
func (e *ConsensusForkEvent) ResolveFork(winningChain int) {
	e.Resolved = true
	e.WinningChain = winningChain
}

// ConsensusValidatorChangeEvent represents a validator set change
type ConsensusValidatorChangeEvent struct {
	// Block where change occurred
	BlockNumber    uint64
	BlockHash      common.Hash
	BlockTimestamp uint64

	// Epoch information
	EpochNumber     uint64
	IsEpochBoundary bool

	// Validator set changes
	ChangeType        string // "added", "removed", "replaced", "reordered"
	AddedValidators   []common.Address
	RemovedValidators []common.Address

	// Set statistics
	PreviousValidatorCount int
	NewValidatorCount      int
	ValidatorSet           []common.Address

	// Additional info (JSON encoded)
	AdditionalInfo string

	// Event metadata
	CreatedAt time.Time
}

// Type implements Event interface
func (e *ConsensusValidatorChangeEvent) Type() EventType {
	return EventTypeConsensusValidatorChange
}

// Timestamp implements Event interface
func (e *ConsensusValidatorChangeEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewConsensusValidatorChangeEvent creates a new validator change event
func NewConsensusValidatorChangeEvent(
	blockNumber uint64,
	blockHash common.Hash,
	blockTimestamp uint64,
	epochNumber uint64,
	isEpochBoundary bool,
	changeType string,
	addedValidators []common.Address,
	removedValidators []common.Address,
	previousCount int,
	newCount int,
	validatorSet []common.Address,
	additionalInfo map[string]interface{},
) *ConsensusValidatorChangeEvent {
	// Encode additional info as JSON
	var infoJSON string
	if additionalInfo != nil {
		if data, err := json.Marshal(additionalInfo); err == nil {
			infoJSON = string(data)
		}
	}

	return &ConsensusValidatorChangeEvent{
		BlockNumber:            blockNumber,
		BlockHash:              blockHash,
		BlockTimestamp:         blockTimestamp,
		EpochNumber:            epochNumber,
		IsEpochBoundary:        isEpochBoundary,
		ChangeType:             changeType,
		AddedValidators:        addedValidators,
		RemovedValidators:      removedValidators,
		PreviousValidatorCount: previousCount,
		NewValidatorCount:      newCount,
		ValidatorSet:           validatorSet,
		AdditionalInfo:         infoJSON,
		CreatedAt:              time.Now(),
	}
}

// ConsensusErrorEvent represents a consensus error or anomaly
type ConsensusErrorEvent struct {
	// Error location
	BlockNumber    uint64
	BlockHash      common.Hash
	BlockTimestamp uint64

	// Error classification
	ErrorType    string // "round_change", "missed_validators", "low_participation", "proposer_failure", "signature_failure", "other"
	Severity     string // "critical", "high", "medium", "low"
	ErrorMessage string

	// Context data
	Round              uint32
	ExpectedValidators int
	ActualSigners      int
	MissedValidators   []common.Address
	ParticipationRate  float64

	// Impact assessment
	ConsensusImpacted bool
	RecoveryTime      uint64 // Blocks until recovery

	// Additional details (JSON encoded)
	ErrorDetails string

	// Event metadata
	CreatedAt time.Time
}

// Type implements Event interface
func (e *ConsensusErrorEvent) Type() EventType {
	return EventTypeConsensusError
}

// Timestamp implements Event interface
func (e *ConsensusErrorEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewConsensusErrorEvent creates a new consensus error event
func NewConsensusErrorEvent(
	blockNumber uint64,
	blockHash common.Hash,
	blockTimestamp uint64,
	errorType string,
	severity string,
	errorMessage string,
	round uint32,
	expectedValidators int,
	actualSigners int,
	missedValidators []common.Address,
	participationRate float64,
	consensusImpacted bool,
	errorDetails map[string]interface{},
) *ConsensusErrorEvent {
	// Encode error details as JSON
	var detailsJSON string
	if errorDetails != nil {
		if data, err := json.Marshal(errorDetails); err == nil {
			detailsJSON = string(data)
		}
	}

	return &ConsensusErrorEvent{
		BlockNumber:        blockNumber,
		BlockHash:          blockHash,
		BlockTimestamp:     blockTimestamp,
		ErrorType:          errorType,
		Severity:           severity,
		ErrorMessage:       errorMessage,
		Round:              round,
		ExpectedValidators: expectedValidators,
		ActualSigners:      actualSigners,
		MissedValidators:   missedValidators,
		ParticipationRate:  participationRate,
		ConsensusImpacted:  consensusImpacted,
		RecoveryTime:       0,
		ErrorDetails:       detailsJSON,
		CreatedAt:          time.Now(),
	}
}

// SetRecoveryTime sets the recovery time for a consensus error
func (e *ConsensusErrorEvent) SetRecoveryTime(blocks uint64) {
	e.RecoveryTime = blocks
}

// IsHighSeverity returns true if this is a high or critical severity error
func (e *ConsensusErrorEvent) IsHighSeverity() bool {
	return e.Severity == "critical" || e.Severity == "high"
}
