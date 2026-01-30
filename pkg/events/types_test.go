package events

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestBlockEvent_Interface(t *testing.T) {
	// Create a test block
	header := &types.Header{
		Number: big.NewInt(100),
		Time:   uint64(time.Now().Unix()),
	}
	block := types.NewBlockWithHeader(header)

	event := NewBlockEvent(block)

	// Test Event interface implementation
	if event.Type() != EventTypeBlock {
		t.Errorf("expected type %s, got %s", EventTypeBlock, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test BlockEvent fields
	if event.Number != 100 {
		t.Errorf("expected block number 100, got %d", event.Number)
	}

	if event.Hash != block.Hash() {
		t.Errorf("expected hash %s, got %s", block.Hash().Hex(), event.Hash.Hex())
	}

	if event.Block != block {
		t.Error("block reference should match")
	}

	if event.TxCount != 0 {
		t.Errorf("expected 0 transactions, got %d", event.TxCount)
	}
}

func TestBlockEvent_WithTransactions(t *testing.T) {
	// Create a test block with transactions
	header := &types.Header{
		Number: big.NewInt(200),
		Time:   uint64(time.Now().Unix()),
	}

	// Create test transactions
	tx1 := types.NewTransaction(0, common.HexToAddress("0x1"), big.NewInt(100), 21000, big.NewInt(1), nil)
	tx2 := types.NewTransaction(1, common.HexToAddress("0x2"), big.NewInt(200), 21000, big.NewInt(1), nil)

	block := types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: []*types.Transaction{tx1, tx2}})

	event := NewBlockEvent(block)

	if event.TxCount != 2 {
		t.Errorf("expected 2 transactions, got %d", event.TxCount)
	}
}

func TestTransactionEvent_Interface(t *testing.T) {
	// Create a test transaction
	toAddr := common.HexToAddress("0x1234567890abcdef")
	tx := types.NewTransaction(
		0,
		toAddr,
		big.NewInt(1000),
		21000,
		big.NewInt(1),
		nil,
	)

	fromAddr := common.HexToAddress("0xfedcba0987654321")
	blockNumber := uint64(100)
	blockHash := common.HexToHash("0xblock")
	index := uint(5)

	event := NewTransactionEvent(tx, blockNumber, blockHash, index, fromAddr, nil)

	// Test Event interface implementation
	if event.Type() != EventTypeTransaction {
		t.Errorf("expected type %s, got %s", EventTypeTransaction, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test TransactionEvent fields
	if event.Hash != tx.Hash() {
		t.Errorf("expected hash %s, got %s", tx.Hash().Hex(), event.Hash.Hex())
	}

	if event.BlockNumber != blockNumber {
		t.Errorf("expected block number %d, got %d", blockNumber, event.BlockNumber)
	}

	if event.BlockHash != blockHash {
		t.Errorf("expected block hash %s, got %s", blockHash.Hex(), event.BlockHash.Hex())
	}

	if event.Index != index {
		t.Errorf("expected index %d, got %d", index, event.Index)
	}

	if event.From != fromAddr {
		t.Errorf("expected from %s, got %s", fromAddr.Hex(), event.From.Hex())
	}

	if event.To == nil || *event.To != toAddr {
		t.Errorf("expected to %s, got %v", toAddr.Hex(), event.To)
	}

	expectedValue := big.NewInt(1000).String()
	if event.Value != expectedValue {
		t.Errorf("expected value %s, got %s", expectedValue, event.Value)
	}

	if event.Tx != tx {
		t.Error("transaction reference should match")
	}
}

func TestTransactionEvent_ContractCreation(t *testing.T) {
	// Create a contract creation transaction (to = nil)
	tx := types.NewContractCreation(
		0,
		big.NewInt(0),
		21000,
		big.NewInt(1),
		[]byte{0x60, 0x60, 0x60}, // sample bytecode
	)

	fromAddr := common.HexToAddress("0xfedcba0987654321")
	event := NewTransactionEvent(tx, 100, common.Hash{}, 0, fromAddr, nil)

	// For contract creation, To should be nil
	if event.To != nil {
		t.Errorf("expected To to be nil for contract creation, got %v", event.To)
	}
}

func TestTransactionEvent_WithReceipt(t *testing.T) {
	tx := types.NewTransaction(
		0,
		common.HexToAddress("0x1234"),
		big.NewInt(100),
		21000,
		big.NewInt(1),
		nil,
	)

	// Create a test receipt
	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		TxHash:            tx.Hash(),
	}

	fromAddr := common.HexToAddress("0xfrom")
	event := NewTransactionEvent(tx, 100, common.Hash{}, 0, fromAddr, receipt)

	if event.Receipt == nil {
		t.Error("receipt should not be nil")
	}

	if event.Receipt.Status != types.ReceiptStatusSuccessful {
		t.Errorf("expected status %d, got %d", types.ReceiptStatusSuccessful, event.Receipt.Status)
	}

	if event.Receipt.TxHash != tx.Hash() {
		t.Errorf("expected tx hash %s, got %s", tx.Hash().Hex(), event.Receipt.TxHash.Hex())
	}
}

func TestEventType_String(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		expected EventType
	}{
		{
			name: "block event",
			event: &BlockEvent{
				Block:     types.NewBlockWithHeader(&types.Header{}),
				CreatedAt: time.Now(),
			},
			expected: EventTypeBlock,
		},
		{
			name: "transaction event",
			event: &TransactionEvent{
				Tx:        types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), nil),
				CreatedAt: time.Now(),
			},
			expected: EventTypeTransaction,
		},
		{
			name: "log event",
			event: &LogEvent{
				Log:       &types.Log{},
				CreatedAt: time.Now(),
			},
			expected: EventTypeLog,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.event.Type() != tt.expected {
				t.Errorf("expected type %s, got %s", tt.expected, tt.event.Type())
			}
		})
	}
}

func TestEvent_TimestampNotZero(t *testing.T) {
	// Test that all events have non-zero timestamps
	blockEvent := NewBlockEvent(types.NewBlockWithHeader(&types.Header{}))
	if blockEvent.Timestamp().IsZero() {
		t.Error("BlockEvent timestamp should not be zero")
	}

	tx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), nil)
	txEvent := NewTransactionEvent(tx, 0, common.Hash{}, 0, common.Address{}, nil)
	if txEvent.Timestamp().IsZero() {
		t.Error("TransactionEvent timestamp should not be zero")
	}

	logEvent := NewLogEvent(&types.Log{})
	if logEvent.Timestamp().IsZero() {
		t.Error("LogEvent timestamp should not be zero")
	}

	// Ensure timestamps are recent (within last second)
	now := time.Now()
	if now.Sub(blockEvent.Timestamp()) > time.Second {
		t.Error("BlockEvent timestamp is not recent")
	}
	if now.Sub(txEvent.Timestamp()) > time.Second {
		t.Error("TransactionEvent timestamp is not recent")
	}
	if now.Sub(logEvent.Timestamp()) > time.Second {
		t.Error("LogEvent timestamp is not recent")
	}
}

func TestNewLogEvent(t *testing.T) {
	original := &types.Log{Address: common.HexToAddress("0x1")}
	event := NewLogEvent(original)

	if event.Log != original {
		t.Error("expected original log pointer")
	}
	if event.Type() != EventTypeLog {
		t.Errorf("expected type %s", EventTypeLog)
	}
}

func TestChainConfigEvent_Interface(t *testing.T) {
	blockNumber := uint64(1000)
	blockHash := common.HexToHash("0xblock")
	parameter := "gasLimit"
	oldValue := "10000000"
	newValue := "20000000"

	event := NewChainConfigEvent(blockNumber, blockHash, parameter, oldValue, newValue)

	// Test Event interface implementation
	if event.Type() != EventTypeChainConfig {
		t.Errorf("expected type %s, got %s", EventTypeChainConfig, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test ChainConfigEvent fields
	if event.BlockNumber != blockNumber {
		t.Errorf("expected block number %d, got %d", blockNumber, event.BlockNumber)
	}

	if event.BlockHash != blockHash {
		t.Errorf("expected block hash %s, got %s", blockHash.Hex(), event.BlockHash.Hex())
	}

	if event.Parameter != parameter {
		t.Errorf("expected parameter %s, got %s", parameter, event.Parameter)
	}

	if event.OldValue != oldValue {
		t.Errorf("expected old value %s, got %s", oldValue, event.OldValue)
	}

	if event.NewValue != newValue {
		t.Errorf("expected new value %s, got %s", newValue, event.NewValue)
	}
}

func TestValidatorSetEvent_Interface(t *testing.T) {
	blockNumber := uint64(2000)
	blockHash := common.HexToHash("0xvalidator")
	changeType := "added"
	validator := common.HexToAddress("0x1234567890123456789012345678901234567890")
	validatorInfo := `{"name":"validator1","power":100}`
	validatorSetSize := 10

	event := NewValidatorSetEvent(blockNumber, blockHash, changeType, validator, validatorInfo, validatorSetSize)

	// Test Event interface implementation
	if event.Type() != EventTypeValidatorSet {
		t.Errorf("expected type %s, got %s", EventTypeValidatorSet, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test ValidatorSetEvent fields
	if event.BlockNumber != blockNumber {
		t.Errorf("expected block number %d, got %d", blockNumber, event.BlockNumber)
	}

	if event.BlockHash != blockHash {
		t.Errorf("expected block hash %s, got %s", blockHash.Hex(), event.BlockHash.Hex())
	}

	if event.ChangeType != changeType {
		t.Errorf("expected change type %s, got %s", changeType, event.ChangeType)
	}

	if event.Validator != validator {
		t.Errorf("expected validator %s, got %s", validator.Hex(), event.Validator.Hex())
	}

	if event.ValidatorInfo != validatorInfo {
		t.Errorf("expected validator info %s, got %s", validatorInfo, event.ValidatorInfo)
	}

	if event.ValidatorSetSize != validatorSetSize {
		t.Errorf("expected validator set size %d, got %d", validatorSetSize, event.ValidatorSetSize)
	}
}

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

func TestAllEventTypes_Interface(t *testing.T) {
	// Comprehensive test ensuring all event types properly implement the Event interface
	events := []Event{
		NewBlockEvent(types.NewBlockWithHeader(&types.Header{Number: big.NewInt(1)})),
		NewTransactionEvent(
			types.NewTransaction(0, common.Address{}, big.NewInt(0), 21000, big.NewInt(1), nil),
			1, common.Hash{}, 0, common.Address{}, nil,
		),
		NewLogEvent(&types.Log{}),
		NewChainConfigEvent(1, common.Hash{}, "param", "old", "new"),
		NewValidatorSetEvent(1, common.Hash{}, "added", common.Address{}, "{}", 1),
		NewSystemContractEvent(common.Address{}, SystemContractEventMint, 1, common.Hash{}, 0, nil),
	}

	expectedTypes := []EventType{
		EventTypeBlock,
		EventTypeTransaction,
		EventTypeLog,
		EventTypeChainConfig,
		EventTypeValidatorSet,
		EventTypeSystemContract,
	}

	for i, event := range events {
		// Test Type() method
		if event.Type() != expectedTypes[i] {
			t.Errorf("event %d: expected type %s, got %s", i, expectedTypes[i], event.Type())
		}

		// Test Timestamp() method returns non-zero time
		if event.Timestamp().IsZero() {
			t.Errorf("event %d: timestamp should not be zero", i)
		}

		// Ensure timestamp is recent (within last second)
		if time.Since(event.Timestamp()) > time.Second {
			t.Errorf("event %d: timestamp is not recent", i)
		}
	}
}
