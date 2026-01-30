package events

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestConsensusBlockEvent_Interface(t *testing.T) {
	blockNumber := uint64(1000)
	blockHash := common.HexToHash("0xblockhash")
	blockTimestamp := uint64(time.Now().Unix())
	proposer := common.HexToAddress("0x1234567890123456789012345678901234567890")
	epochNum := uint64(5)
	validators := []common.Address{proposer}

	event := NewConsensusBlockEvent(
		blockNumber,
		blockHash,
		blockTimestamp,
		1,     // round
		0,     // prevRound
		proposer,
		10,    // validatorCount
		9,     // prepareCount
		9,     // commitCount
		0.9,   // participationRate
		0.1,   // missedValidatorRate
		true,  // isEpochBoundary
		&epochNum,
		validators,
	)

	// Test Event interface implementation
	if event.Type() != EventTypeConsensusBlock {
		t.Errorf("expected type %s, got %s", EventTypeConsensusBlock, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test fields
	if event.BlockNumber != blockNumber {
		t.Errorf("expected block number %d, got %d", blockNumber, event.BlockNumber)
	}

	if event.BlockHash != blockHash {
		t.Errorf("expected block hash %s, got %s", blockHash.Hex(), event.BlockHash.Hex())
	}

	if event.Proposer != proposer {
		t.Errorf("expected proposer %s, got %s", proposer.Hex(), event.Proposer.Hex())
	}

	if event.ValidatorCount != 10 {
		t.Errorf("expected validator count 10, got %d", event.ValidatorCount)
	}

	if event.ParticipationRate != 0.9 {
		t.Errorf("expected participation rate 0.9, got %f", event.ParticipationRate)
	}

	if !event.IsEpochBoundary {
		t.Error("expected IsEpochBoundary to be true")
	}

	if event.EpochNumber == nil || *event.EpochNumber != epochNum {
		t.Errorf("expected epoch number %d", epochNum)
	}

	// Test RoundChanged logic
	if !event.RoundChanged {
		t.Error("expected RoundChanged to be true when round > 0")
	}
}

func TestConsensusBlockEvent_NoRoundChange(t *testing.T) {
	event := NewConsensusBlockEvent(
		100,
		common.Hash{},
		uint64(time.Now().Unix()),
		0, // round = 0, so RoundChanged should be false
		0,
		common.Address{},
		5,
		5,
		5,
		1.0,
		0.0,
		false,
		nil,
		nil,
	)

	if event.RoundChanged {
		t.Error("expected RoundChanged to be false when round == 0")
	}
}

func TestConsensusForkEvent_Interface(t *testing.T) {
	forkBlockNumber := uint64(500)
	forkBlockHash := common.HexToHash("0xfork")
	chain1Hash := common.HexToHash("0xchain1")
	chain2Hash := common.HexToHash("0xchain2")

	event := NewConsensusForkEvent(
		forkBlockNumber,
		forkBlockHash,
		chain1Hash,
		510,       // chain1Height
		"1000000", // chain1Weight
		chain2Hash,
		508,       // chain2Height
		"999000",  // chain2Weight
		2,         // detectionLag
	)

	// Test Event interface implementation
	if event.Type() != EventTypeConsensusFork {
		t.Errorf("expected type %s, got %s", EventTypeConsensusFork, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test fields
	if event.ForkBlockNumber != forkBlockNumber {
		t.Errorf("expected fork block number %d, got %d", forkBlockNumber, event.ForkBlockNumber)
	}

	if event.ForkBlockHash != forkBlockHash {
		t.Errorf("expected fork block hash %s, got %s", forkBlockHash.Hex(), event.ForkBlockHash.Hex())
	}

	if event.Chain1Hash != chain1Hash {
		t.Errorf("expected chain1 hash %s, got %s", chain1Hash.Hex(), event.Chain1Hash.Hex())
	}

	if event.Chain1Height != 510 {
		t.Errorf("expected chain1 height 510, got %d", event.Chain1Height)
	}

	if event.Chain2Hash != chain2Hash {
		t.Errorf("expected chain2 hash %s, got %s", chain2Hash.Hex(), event.Chain2Hash.Hex())
	}

	if event.Chain2Height != 508 {
		t.Errorf("expected chain2 height 508, got %d", event.Chain2Height)
	}

	if event.DetectionLag != 2 {
		t.Errorf("expected detection lag 2, got %d", event.DetectionLag)
	}

	// Check initial state
	if event.Resolved {
		t.Error("expected Resolved to be false initially")
	}

	if event.WinningChain != 0 {
		t.Errorf("expected WinningChain to be 0 initially, got %d", event.WinningChain)
	}
}

func TestConsensusForkEvent_ResolveFork(t *testing.T) {
	event := NewConsensusForkEvent(
		100,
		common.Hash{},
		common.Hash{},
		110,
		"1000",
		common.Hash{},
		108,
		"900",
		5,
	)

	// Test resolve with chain 1
	event.ResolveFork(1)

	if !event.Resolved {
		t.Error("expected Resolved to be true after ResolveFork")
	}

	if event.WinningChain != 1 {
		t.Errorf("expected WinningChain to be 1, got %d", event.WinningChain)
	}

	// Test resolve with chain 2
	event2 := NewConsensusForkEvent(100, common.Hash{}, common.Hash{}, 110, "1000", common.Hash{}, 108, "900", 5)
	event2.ResolveFork(2)

	if event2.WinningChain != 2 {
		t.Errorf("expected WinningChain to be 2, got %d", event2.WinningChain)
	}
}

func TestConsensusValidatorChangeEvent_Interface(t *testing.T) {
	blockNumber := uint64(1000)
	blockHash := common.HexToHash("0xvalidatorchange")
	blockTimestamp := uint64(time.Now().Unix())
	epochNumber := uint64(10)
	addedValidator := common.HexToAddress("0xadded")
	removedValidator := common.HexToAddress("0xremoved")
	currentValidator := common.HexToAddress("0xcurrent")

	additionalInfo := map[string]any{
		"reason": "scheduled epoch change",
		"source": "governance",
	}

	event := NewConsensusValidatorChangeEvent(
		blockNumber,
		blockHash,
		blockTimestamp,
		epochNumber,
		true, // isEpochBoundary
		"replaced",
		[]common.Address{addedValidator},
		[]common.Address{removedValidator},
		10, // previousCount
		10, // newCount
		[]common.Address{currentValidator, addedValidator},
		additionalInfo,
	)

	// Test Event interface implementation
	if event.Type() != EventTypeConsensusValidatorChange {
		t.Errorf("expected type %s, got %s", EventTypeConsensusValidatorChange, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test fields
	if event.BlockNumber != blockNumber {
		t.Errorf("expected block number %d, got %d", blockNumber, event.BlockNumber)
	}

	if event.EpochNumber != epochNumber {
		t.Errorf("expected epoch number %d, got %d", epochNumber, event.EpochNumber)
	}

	if !event.IsEpochBoundary {
		t.Error("expected IsEpochBoundary to be true")
	}

	if event.ChangeType != "replaced" {
		t.Errorf("expected change type 'replaced', got '%s'", event.ChangeType)
	}

	if len(event.AddedValidators) != 1 || event.AddedValidators[0] != addedValidator {
		t.Errorf("expected added validators to contain %s", addedValidator.Hex())
	}

	if len(event.RemovedValidators) != 1 || event.RemovedValidators[0] != removedValidator {
		t.Errorf("expected removed validators to contain %s", removedValidator.Hex())
	}

	if event.PreviousValidatorCount != 10 {
		t.Errorf("expected previous count 10, got %d", event.PreviousValidatorCount)
	}

	if event.AdditionalInfo == "" {
		t.Error("expected AdditionalInfo to be populated")
	}
}

func TestConsensusValidatorChangeEvent_NilAdditionalInfo(t *testing.T) {
	event := NewConsensusValidatorChangeEvent(
		100,
		common.Hash{},
		uint64(time.Now().Unix()),
		1,
		false,
		"added",
		nil,
		nil,
		5,
		6,
		nil,
		nil, // nil additionalInfo
	)

	if event.AdditionalInfo != "" {
		t.Errorf("expected empty AdditionalInfo when nil is passed, got '%s'", event.AdditionalInfo)
	}
}

func TestConsensusErrorEvent_Interface(t *testing.T) {
	blockNumber := uint64(2000)
	blockHash := common.HexToHash("0xerror")
	blockTimestamp := uint64(time.Now().Unix())
	missedValidator := common.HexToAddress("0xmissed")

	errorDetails := map[string]any{
		"timeout":     30,
		"retry_count": 3,
	}

	event := NewConsensusErrorEvent(
		blockNumber,
		blockHash,
		blockTimestamp,
		"missed_validators",
		"high",
		"Validators missed signing window",
		5, // round
		10, // expectedValidators
		7,  // actualSigners
		[]common.Address{missedValidator},
		0.7,   // participationRate
		false, // consensusImpacted
		errorDetails,
	)

	// Test Event interface implementation
	if event.Type() != EventTypeConsensusError {
		t.Errorf("expected type %s, got %s", EventTypeConsensusError, event.Type())
	}

	if event.Timestamp().IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Test fields
	if event.BlockNumber != blockNumber {
		t.Errorf("expected block number %d, got %d", blockNumber, event.BlockNumber)
	}

	if event.ErrorType != "missed_validators" {
		t.Errorf("expected error type 'missed_validators', got '%s'", event.ErrorType)
	}

	if event.Severity != "high" {
		t.Errorf("expected severity 'high', got '%s'", event.Severity)
	}

	if event.Round != 5 {
		t.Errorf("expected round 5, got %d", event.Round)
	}

	if event.ExpectedValidators != 10 {
		t.Errorf("expected expected validators 10, got %d", event.ExpectedValidators)
	}

	if event.ActualSigners != 7 {
		t.Errorf("expected actual signers 7, got %d", event.ActualSigners)
	}

	if len(event.MissedValidators) != 1 {
		t.Errorf("expected 1 missed validator, got %d", len(event.MissedValidators))
	}

	if event.ParticipationRate != 0.7 {
		t.Errorf("expected participation rate 0.7, got %f", event.ParticipationRate)
	}

	if event.ConsensusImpacted {
		t.Error("expected ConsensusImpacted to be false")
	}

	if event.ErrorDetails == "" {
		t.Error("expected ErrorDetails to be populated")
	}

	if event.RecoveryTime != 0 {
		t.Errorf("expected initial RecoveryTime 0, got %d", event.RecoveryTime)
	}
}

func TestConsensusErrorEvent_SetRecoveryTime(t *testing.T) {
	event := NewConsensusErrorEvent(
		100,
		common.Hash{},
		uint64(time.Now().Unix()),
		"low_participation",
		"medium",
		"Participation below threshold",
		0,
		10,
		6,
		nil,
		0.6,
		false,
		nil,
	)

	event.SetRecoveryTime(5)

	if event.RecoveryTime != 5 {
		t.Errorf("expected RecoveryTime 5, got %d", event.RecoveryTime)
	}
}

func TestConsensusErrorEvent_IsHighSeverity(t *testing.T) {
	tests := []struct {
		severity string
		expected bool
	}{
		{"critical", true},
		{"high", true},
		{"medium", false},
		{"low", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			event := NewConsensusErrorEvent(
				100,
				common.Hash{},
				uint64(time.Now().Unix()),
				"test_error",
				tt.severity,
				"test message",
				0,
				10,
				10,
				nil,
				1.0,
				false,
				nil,
			)

			if got := event.IsHighSeverity(); got != tt.expected {
				t.Errorf("IsHighSeverity() = %v, want %v for severity '%s'", got, tt.expected, tt.severity)
			}
		})
	}
}

func TestConsensusErrorEvent_NilErrorDetails(t *testing.T) {
	event := NewConsensusErrorEvent(
		100,
		common.Hash{},
		uint64(time.Now().Unix()),
		"test",
		"low",
		"test message",
		0,
		5,
		5,
		nil,
		1.0,
		false,
		nil, // nil errorDetails
	)

	if event.ErrorDetails != "" {
		t.Errorf("expected empty ErrorDetails when nil is passed, got '%s'", event.ErrorDetails)
	}
}

func TestAllConsensusEventTypes_Interface(t *testing.T) {
	// Create one of each consensus event type
	events := []Event{
		NewConsensusBlockEvent(
			100, common.Hash{}, uint64(time.Now().Unix()),
			0, 0, common.Address{}, 5, 5, 5, 1.0, 0.0, false, nil, nil,
		),
		NewConsensusForkEvent(
			100, common.Hash{}, common.Hash{}, 110, "1000", common.Hash{}, 108, "900", 5,
		),
		NewConsensusValidatorChangeEvent(
			100, common.Hash{}, uint64(time.Now().Unix()), 1, false, "added", nil, nil, 5, 6, nil, nil,
		),
		NewConsensusErrorEvent(
			100, common.Hash{}, uint64(time.Now().Unix()), "test", "low", "msg", 0, 5, 5, nil, 1.0, false, nil,
		),
	}

	expectedTypes := []EventType{
		EventTypeConsensusBlock,
		EventTypeConsensusFork,
		EventTypeConsensusValidatorChange,
		EventTypeConsensusError,
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
