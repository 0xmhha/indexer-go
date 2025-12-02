package events

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// EventType represents the type of blockchain event
type EventType string

const (
	// EventTypeBlock represents a new block event
	EventTypeBlock EventType = "block"

	// EventTypeTransaction represents a new transaction event
	EventTypeTransaction EventType = "transaction"

	// EventTypeLog represents a log event emitted from receipts
	EventTypeLog EventType = "log"

	// EventTypeChainConfig represents a chain configuration change event
	EventTypeChainConfig EventType = "chainConfig"

	// EventTypeValidatorSet represents a validator set change event
	EventTypeValidatorSet EventType = "validatorSet"

	// EventTypeSystemContract represents a system contract event
	EventTypeSystemContract EventType = "systemContract"
)

// Event is the base interface for all blockchain events
type Event interface {
	// Type returns the event type
	Type() EventType

	// Timestamp returns when the event was created
	Timestamp() time.Time
}

// BlockEvent represents a new block event
type BlockEvent struct {
	// Block data
	Block *types.Block

	// Block number
	Number uint64

	// Block hash
	Hash common.Hash

	// Timestamp when this event was created
	CreatedAt time.Time

	// Number of transactions in the block
	TxCount int
}

// Type implements Event interface
func (e *BlockEvent) Type() EventType {
	return EventTypeBlock
}

// Timestamp implements Event interface
func (e *BlockEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// TransactionEvent represents a new transaction event
type TransactionEvent struct {
	// Transaction data
	Tx *types.Transaction

	// Transaction hash
	Hash common.Hash

	// Block number containing this transaction
	BlockNumber uint64

	// Block hash containing this transaction
	BlockHash common.Hash

	// Transaction index in the block
	Index uint

	// From address (sender)
	From common.Address

	// To address (receiver, nil for contract creation)
	To *common.Address

	// Value transferred
	Value string // big.Int as string to avoid serialization issues

	// Receipt data (optional, may be nil)
	Receipt *types.Receipt

	// Timestamp when this event was created
	CreatedAt time.Time
}

// Type implements Event interface
func (e *TransactionEvent) Type() EventType {
	return EventTypeTransaction
}

// Timestamp implements Event interface
func (e *TransactionEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewBlockEvent creates a new block event from a block
func NewBlockEvent(block *types.Block) *BlockEvent {
	return &BlockEvent{
		Block:     block,
		Number:    block.NumberU64(),
		Hash:      block.Hash(),
		CreatedAt: time.Now(),
		TxCount:   len(block.Transactions()),
	}
}

// NewTransactionEvent creates a new transaction event
func NewTransactionEvent(
	tx *types.Transaction,
	blockNumber uint64,
	blockHash common.Hash,
	index uint,
	from common.Address,
	receipt *types.Receipt,
) *TransactionEvent {
	var to *common.Address
	if tx.To() != nil {
		toAddr := *tx.To()
		to = &toAddr
	}

	return &TransactionEvent{
		Tx:          tx,
		Hash:        tx.Hash(),
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Index:       index,
		From:        from,
		To:          to,
		Value:       tx.Value().String(),
		Receipt:     receipt,
		CreatedAt:   time.Now(),
	}
}

// LogEvent represents a log emitted as part of a transaction receipt
type LogEvent struct {
	// Log is the raw Ethereum log data
	Log *types.Log

	// CreatedAt is when this event was generated
	CreatedAt time.Time
}

// Type implements Event interface
func (e *LogEvent) Type() EventType {
	return EventTypeLog
}

// Timestamp implements Event interface
func (e *LogEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewLogEvent wraps a types.Log into an Event
func NewLogEvent(log *types.Log) *LogEvent {
	return &LogEvent{
		Log:       log,
		CreatedAt: time.Now(),
	}
}

// ChainConfigEvent represents a chain configuration change event
type ChainConfigEvent struct {
	// Block number where the config change occurred
	BlockNumber uint64

	// Block hash
	BlockHash common.Hash

	// Config parameter that changed (e.g., "gasLimit", "difficulty", "chainId")
	Parameter string

	// Old value (JSON encoded)
	OldValue string

	// New value (JSON encoded)
	NewValue string

	// Timestamp when this event was created
	CreatedAt time.Time
}

// Type implements Event interface
func (e *ChainConfigEvent) Type() EventType {
	return EventTypeChainConfig
}

// Timestamp implements Event interface
func (e *ChainConfigEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewChainConfigEvent creates a new chain config change event
func NewChainConfigEvent(blockNumber uint64, blockHash common.Hash, parameter, oldValue, newValue string) *ChainConfigEvent {
	return &ChainConfigEvent{
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Parameter:   parameter,
		OldValue:    oldValue,
		NewValue:    newValue,
		CreatedAt:   time.Now(),
	}
}

// ValidatorSetEvent represents a validator set change event
type ValidatorSetEvent struct {
	// Block number where the validator set change occurred
	BlockNumber uint64

	// Block hash
	BlockHash common.Hash

	// Change type: "added", "removed", "updated"
	ChangeType string

	// Validator address that was added/removed/updated
	Validator common.Address

	// Additional validator info (optional, JSON encoded)
	// May include: voting power, commission rate, etc.
	ValidatorInfo string

	// Current validator set size after this change
	ValidatorSetSize int

	// Timestamp when this event was created
	CreatedAt time.Time
}

// Type implements Event interface
func (e *ValidatorSetEvent) Type() EventType {
	return EventTypeValidatorSet
}

// Timestamp implements Event interface
func (e *ValidatorSetEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewValidatorSetEvent creates a new validator set change event
func NewValidatorSetEvent(
	blockNumber uint64,
	blockHash common.Hash,
	changeType string,
	validator common.Address,
	validatorInfo string,
	validatorSetSize int,
) *ValidatorSetEvent {
	return &ValidatorSetEvent{
		BlockNumber:      blockNumber,
		BlockHash:        blockHash,
		ChangeType:       changeType,
		Validator:        validator,
		ValidatorInfo:    validatorInfo,
		ValidatorSetSize: validatorSetSize,
		CreatedAt:        time.Now(),
	}
}

// SystemContractEventType represents the specific type of system contract event
type SystemContractEventType string

const (
	// Governance events
	SystemContractEventProposalCreated           SystemContractEventType = "ProposalCreated"
	SystemContractEventProposalVoted             SystemContractEventType = "ProposalVoted"
	SystemContractEventProposalApproved          SystemContractEventType = "ProposalApproved"
	SystemContractEventProposalRejected          SystemContractEventType = "ProposalRejected"
	SystemContractEventProposalExecuted          SystemContractEventType = "ProposalExecuted"
	SystemContractEventProposalFailed            SystemContractEventType = "ProposalFailed"
	SystemContractEventProposalExpired           SystemContractEventType = "ProposalExpired"
	SystemContractEventProposalCancelled         SystemContractEventType = "ProposalCancelled"
	SystemContractEventProposalExecutionSkipped  SystemContractEventType = "ProposalExecutionSkipped"
	SystemContractEventMaxProposalsUpdated       SystemContractEventType = "MaxProposalsPerMemberUpdated"

	// Member events
	SystemContractEventMemberAdded   SystemContractEventType = "MemberAdded"
	SystemContractEventMemberRemoved SystemContractEventType = "MemberRemoved"
	SystemContractEventMemberChanged SystemContractEventType = "MemberChanged"
	SystemContractEventQuorumUpdated SystemContractEventType = "QuorumUpdated"

	// Token events (NativeCoinAdapter)
	SystemContractEventMint                 SystemContractEventType = "Mint"
	SystemContractEventBurn                 SystemContractEventType = "Burn"
	SystemContractEventMinterConfigured     SystemContractEventType = "MinterConfigured"
	SystemContractEventMinterRemoved        SystemContractEventType = "MinterRemoved"
	SystemContractEventMasterMinterChanged  SystemContractEventType = "MasterMinterChanged"

	// GovValidator events
	SystemContractEventGasTipUpdated       SystemContractEventType = "GasTipUpdated"
	SystemContractEventValidatorAdded      SystemContractEventType = "ValidatorAdded"
	SystemContractEventValidatorRemoved    SystemContractEventType = "ValidatorRemoved"

	// GovMasterMinter events
	SystemContractEventMaxMinterAllowanceUpdated SystemContractEventType = "MaxMinterAllowanceUpdated"
	SystemContractEventEmergencyPaused           SystemContractEventType = "EmergencyPaused"
	SystemContractEventEmergencyUnpaused         SystemContractEventType = "EmergencyUnpaused"

	// GovMinter events
	SystemContractEventDepositMintProposed SystemContractEventType = "DepositMintProposed"
	SystemContractEventBurnPrepaid         SystemContractEventType = "BurnPrepaid"
	SystemContractEventBurnExecuted        SystemContractEventType = "BurnExecuted"

	// GovCouncil events
	SystemContractEventAddressBlacklisted       SystemContractEventType = "AddressBlacklisted"
	SystemContractEventAddressUnblacklisted     SystemContractEventType = "AddressUnblacklisted"
	SystemContractEventAuthorizedAccountAdded   SystemContractEventType = "AuthorizedAccountAdded"
	SystemContractEventAuthorizedAccountRemoved SystemContractEventType = "AuthorizedAccountRemoved"
)

// SystemContractEvent represents an event emitted by a system contract
type SystemContractEvent struct {
	// Contract address that emitted the event
	Contract common.Address

	// Specific event type
	EventName SystemContractEventType

	// Block number
	BlockNumber uint64

	// Transaction hash
	TxHash common.Hash

	// Log index in the transaction
	LogIndex uint

	// Event data as key-value pairs (JSON serializable)
	Data map[string]interface{}

	// Timestamp when this event was created
	CreatedAt time.Time
}

// Type implements Event interface
func (e *SystemContractEvent) Type() EventType {
	return EventTypeSystemContract
}

// Timestamp implements Event interface
func (e *SystemContractEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewSystemContractEvent creates a new system contract event
func NewSystemContractEvent(
	contract common.Address,
	eventName SystemContractEventType,
	blockNumber uint64,
	txHash common.Hash,
	logIndex uint,
	data map[string]interface{},
) *SystemContractEvent {
	return &SystemContractEvent{
		Contract:    contract,
		EventName:   eventName,
		BlockNumber: blockNumber,
		TxHash:      txHash,
		LogIndex:    logIndex,
		Data:        data,
		CreatedAt:   time.Now(),
	}
}
