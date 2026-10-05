package systemcontracts

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestSystemContractEvent_Interface(t *testing.T) {
	contract := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	eventName := SystemContractEventMint
	blockNumber := uint64(3000)
	txHash := common.HexToHash("0xtxhash")
	logIndex := uint(5)
	data := map[string]interface{}{
		"to":     "0x1234",
		"amount": "1000000000000000000",
	}

	event := NewSystemContractEvent(contract, eventName, blockNumber, txHash, logIndex, data)

	// Test Event interface implementation
	if event.Type() != EventTypeSystemContract {
		t.Errorf("expected type %s, got %s", EventTypeSystemContract, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test SystemContractEvent fields
	if event.Contract != contract {
		t.Errorf("expected contract %s, got %s", contract.Hex(), event.Contract.Hex())
	}

	if event.EventName != eventName {
		t.Errorf("expected event name %s, got %s", eventName, event.EventName)
	}

	if event.BlockNumber != blockNumber {
		t.Errorf("expected block number %d, got %d", blockNumber, event.BlockNumber)
	}

	if event.TxHash != txHash {
		t.Errorf("expected tx hash %s, got %s", txHash.Hex(), event.TxHash.Hex())
	}

	if event.LogIndex != logIndex {
		t.Errorf("expected log index %d, got %d", logIndex, event.LogIndex)
	}

	if event.Data["to"] != data["to"] {
		t.Errorf("expected data[to] %v, got %v", data["to"], event.Data["to"])
	}
}

func TestSystemContractEventTypes(t *testing.T) {
	// Test various system contract event types
	eventTypes := []SystemContractEventType{
		SystemContractEventProposalCreated,
		SystemContractEventProposalVoted,
		SystemContractEventProposalApproved,
		SystemContractEventProposalRejected,
		SystemContractEventProposalExecuted,
		SystemContractEventProposalFailed,
		SystemContractEventProposalExpired,
		SystemContractEventProposalCancelled,
		SystemContractEventMemberAdded,
		SystemContractEventMemberRemoved,
		SystemContractEventMint,
		SystemContractEventBurn,
		SystemContractEventValidatorAdded,
		SystemContractEventValidatorRemoved,
	}

	for _, eventType := range eventTypes {
		event := NewSystemContractEvent(
			common.Address{},
			eventType,
			100,
			common.Hash{},
			0,
			nil,
		)

		if event.EventName != eventType {
			t.Errorf("expected event type %s, got %s", eventType, event.EventName)
		}

		if event.Type() != EventTypeSystemContract {
			t.Errorf("expected type %s for %s", EventTypeSystemContract, eventType)
		}
	}
}
