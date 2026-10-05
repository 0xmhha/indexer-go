package systemcontracts

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// EventTypeSystemContract is the event bus type of system contract events.
const EventTypeSystemContract events.EventType = "systemContract"

// SystemContractEventType represents the specific type of system contract event
type SystemContractEventType string

const (
	// Governance events
	SystemContractEventProposalCreated          SystemContractEventType = "ProposalCreated"
	SystemContractEventProposalVoted            SystemContractEventType = "ProposalVoted"
	SystemContractEventProposalApproved         SystemContractEventType = "ProposalApproved"
	SystemContractEventProposalRejected         SystemContractEventType = "ProposalRejected"
	SystemContractEventProposalExecuted         SystemContractEventType = "ProposalExecuted"
	SystemContractEventProposalFailed           SystemContractEventType = "ProposalFailed"
	SystemContractEventProposalExpired          SystemContractEventType = "ProposalExpired"
	SystemContractEventProposalCancelled        SystemContractEventType = "ProposalCancelled"
	SystemContractEventProposalExecutionSkipped SystemContractEventType = "ProposalExecutionSkipped"
	SystemContractEventMaxProposalsUpdated      SystemContractEventType = "MaxProposalsPerMemberUpdated"

	// Member events
	SystemContractEventMemberAdded   SystemContractEventType = "MemberAdded"
	SystemContractEventMemberRemoved SystemContractEventType = "MemberRemoved"
	SystemContractEventMemberChanged SystemContractEventType = "MemberChanged"
	SystemContractEventQuorumUpdated SystemContractEventType = "QuorumUpdated"

	// Token events (NativeCoinAdapter)
	SystemContractEventMint                SystemContractEventType = "Mint"
	SystemContractEventBurn                SystemContractEventType = "Burn"
	SystemContractEventMinterConfigured    SystemContractEventType = "MinterConfigured"
	SystemContractEventMinterRemoved       SystemContractEventType = "MinterRemoved"
	SystemContractEventMasterMinterChanged SystemContractEventType = "MasterMinterChanged"

	// GovValidator events
	SystemContractEventGasTipUpdated    SystemContractEventType = "GasTipUpdated"
	SystemContractEventValidatorAdded   SystemContractEventType = "ValidatorAdded"
	SystemContractEventValidatorRemoved SystemContractEventType = "ValidatorRemoved"

	// GovMasterMinter events
	SystemContractEventMaxMinterAllowanceUpdated SystemContractEventType = "MaxMinterAllowanceUpdated"
	SystemContractEventEmergencyPaused           SystemContractEventType = "EmergencyPaused"
	SystemContractEventEmergencyUnpaused         SystemContractEventType = "EmergencyUnpaused"

	// GovMinter events
	SystemContractEventDepositMintProposed   SystemContractEventType = "DepositMintProposed"
	SystemContractEventBurnPrepaid           SystemContractEventType = "BurnPrepaid"
	SystemContractEventBurnDepositRefunded   SystemContractEventType = "BurnDepositRefunded"
	SystemContractEventBurnRefundClaimed     SystemContractEventType = "BurnRefundClaimed"
	SystemContractEventAuthorizationUsed     SystemContractEventType = "AuthorizationUsed"
	SystemContractEventAuthorizationCanceled SystemContractEventType = "AuthorizationCanceled"
	SystemContractEventBurnExecuted          SystemContractEventType = "BurnExecuted"

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

// Type implements events.Event
func (e *SystemContractEvent) Type() events.EventType {
	return EventTypeSystemContract
}

// Timestamp implements events.Event
func (e *SystemContractEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// systemContractEventData is the JSON form of SystemContractEvent on event
// buses that carry events between processes.
type systemContractEventData struct {
	Contract    common.Address          `json:"contract"`
	EventName   SystemContractEventType `json:"event_name"`
	BlockNumber uint64                  `json:"block_number"`
	TxHash      common.Hash             `json:"tx_hash"`
	LogIndex    uint                    `json:"log_index"`
	Data        map[string]interface{}  `json:"data"`
	CreatedAt   time.Time               `json:"created_at"`
}

func init() {
	events.RegisterCodec(EventTypeSystemContract, events.EventCodec{
		Encode: func(ev events.Event) (interface{}, error) {
			e, ok := ev.(*SystemContractEvent)
			if !ok {
				return nil, fmt.Errorf("unexpected event %T", ev)
			}
			return systemContractEventData{
				Contract: e.Contract, EventName: e.EventName, BlockNumber: e.BlockNumber,
				TxHash: e.TxHash, LogIndex: e.LogIndex, Data: e.Data, CreatedAt: e.CreatedAt,
			}, nil
		},
		Decode: func(data []byte) (events.Event, error) {
			var ed systemContractEventData
			if err := json.Unmarshal(data, &ed); err != nil {
				return nil, err
			}
			return &SystemContractEvent{
				Contract: ed.Contract, EventName: ed.EventName, BlockNumber: ed.BlockNumber,
				TxHash: ed.TxHash, LogIndex: ed.LogIndex, Data: ed.Data, CreatedAt: ed.CreatedAt,
			}, nil
		},
	})
}

// Source implements SourcedEvent.
func (e *SystemContractEvent) Source() (common.Address, string, uint64) {
	return e.Contract, string(e.EventName), e.BlockNumber
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
