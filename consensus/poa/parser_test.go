package poa

import (
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

func TestNewParser(t *testing.T) {
	logger := zap.NewNop()
	parser := NewParser(logger)

	if parser == nil {
		t.Fatal("Expected non-nil parser")
	}
}

func TestParser_ConsensusType(t *testing.T) {
	parser := NewParser(zap.NewNop())

	if parser.ConsensusType() != chain.ConsensusTypePoA {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypePoA, parser.ConsensusType())
	}
}

func TestParser_IsEpochBoundary(t *testing.T) {
	parser := NewParser(zap.NewNop())

	// PoA/Clique doesn't have traditional epochs, so all blocks return false
	testCases := []struct {
		name        string
		blockNumber uint64
		expected    bool
	}{
		{
			name:        "Genesis block",
			blockNumber: 0,
			expected:    false,
		},
		{
			name:        "Block 30000",
			blockNumber: 30000,
			expected:    false,
		},
		{
			name:        "Block 60000",
			blockNumber: 60000,
			expected:    false,
		},
		{
			name:        "Regular block",
			blockNumber: 12345,
			expected:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			header := &types.Header{
				Number: big.NewInt(int64(tc.blockNumber)),
			}
			block := types.NewBlockWithHeader(header)

			result := parser.IsEpochBoundary(block)
			if result != tc.expected {
				t.Errorf("IsEpochBoundary(block %d) = %v, want %v", tc.blockNumber, result, tc.expected)
			}
		})
	}
}

func TestParser_ParseConsensusData_NilBlock(t *testing.T) {
	parser := NewParser(zap.NewNop())

	_, err := parser.ParseConsensusData(nil)
	if err == nil {
		t.Error("Expected error for nil block")
	}
}

func TestParser_ParseConsensusData_EmptyExtra(t *testing.T) {
	parser := NewParser(zap.NewNop())

	header := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(2),
		Extra:      []byte{}, // Empty extra data
	}
	block := types.NewBlockWithHeader(header)

	data, err := parser.ParseConsensusData(block)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should return consensus data with zero signer
	if data == nil {
		t.Fatal("Expected non-nil consensus data")
	}

	if data.ConsensusType != chain.ConsensusTypePoA {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypePoA, data.ConsensusType)
	}
}

func TestParser_ParseConsensusData_ValidExtra(t *testing.T) {
	parser := NewParser(zap.NewNop())

	// Create valid Clique extra data:
	// - 32 bytes vanity
	// - 65 bytes signature (at non-epoch)
	vanity := make([]byte, 32)
	signature := make([]byte, 65)
	// Set some non-zero values in signature to simulate signed block
	signature[0] = 0x1b // Recovery ID

	extraData := append(vanity, signature...)

	header := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(2), // In-turn difficulty
		Extra:      extraData,
		Coinbase:   common.HexToAddress("0x1234567890123456789012345678901234567890"),
	}
	block := types.NewBlockWithHeader(header)

	data, err := parser.ParseConsensusData(block)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if data == nil {
		t.Fatal("Expected non-nil consensus data")
	}

	if data.ConsensusType != chain.ConsensusTypePoA {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypePoA, data.ConsensusType)
	}

	if data.BlockNumber != 100 {
		t.Errorf("Expected block number 100, got %d", data.BlockNumber)
	}

	// ProposerAddress may be zero if signature recovery fails
	t.Logf("ProposerAddress: %s", data.ProposerAddress.Hex())
}

func TestParser_ParseConsensusData_ExtraData(t *testing.T) {
	parser := NewParser(zap.NewNop())

	vanity := make([]byte, 32)
	signature := make([]byte, 65)
	extraData := append(vanity, signature...)

	coinbase := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	header := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(2), // In-turn difficulty
		Extra:      extraData,
		Coinbase:   coinbase,
	}
	block := types.NewBlockWithHeader(header)

	data, err := parser.ParseConsensusData(block)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Check ExtraData is PoAExtraData type
	if data.ExtraData == nil {
		t.Fatal("Expected non-nil extra data")
	}

	poaExtra, ok := data.ExtraData.(*PoAExtraData)
	if !ok {
		t.Fatalf("Expected extra data to be *PoAExtraData, got %T", data.ExtraData)
	}

	// Check Difficulty field
	if poaExtra.Difficulty != 2 {
		t.Errorf("Expected difficulty 2, got %d", poaExtra.Difficulty)
	}

	// Check Coinbase field
	if poaExtra.Coinbase != coinbase {
		t.Errorf("Expected coinbase %s, got %s", coinbase.Hex(), poaExtra.Coinbase.Hex())
	}
}

func TestParser_ParseConsensusData_DifferentDifficulties(t *testing.T) {
	parser := NewParser(zap.NewNop())

	testCases := []struct {
		name       string
		difficulty uint64
	}{
		{
			name:       "In-turn block (difficulty 2)",
			difficulty: 2,
		},
		{
			name:       "Out-of-turn block (difficulty 1)",
			difficulty: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			vanity := make([]byte, 32)
			signature := make([]byte, 65)
			extraData := append(vanity, signature...)

			header := &types.Header{
				Number:     big.NewInt(100),
				Difficulty: big.NewInt(int64(tc.difficulty)),
				Extra:      extraData,
			}
			block := types.NewBlockWithHeader(header)

			data, err := parser.ParseConsensusData(block)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			poaExtra, ok := data.ExtraData.(*PoAExtraData)
			if !ok {
				t.Fatalf("Expected extra data to be *PoAExtraData, got %T", data.ExtraData)
			}

			if poaExtra.Difficulty != tc.difficulty {
				t.Errorf("Expected difficulty %d, got %d", tc.difficulty, poaExtra.Difficulty)
			}
		})
	}
}

func TestParser_ParseConsensusData_ValidatorFields(t *testing.T) {
	parser := NewParser(zap.NewNop())

	vanity := make([]byte, 32)
	signature := make([]byte, 65)
	extraData := append(vanity, signature...)

	header := &types.Header{
		Number:     big.NewInt(100),
		Difficulty: big.NewInt(2),
		Extra:      extraData,
	}
	block := types.NewBlockWithHeader(header)

	data, err := parser.ParseConsensusData(block)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// PoA has single signer per block
	if data.ValidatorCount != 1 {
		t.Errorf("Expected validator count 1, got %d", data.ValidatorCount)
	}

	// SignedValidators should contain the signer
	if len(data.SignedValidators) != 1 {
		t.Errorf("Expected 1 signed validator, got %d", len(data.SignedValidators))
	}

	// Participation rate is 100% for PoA
	if data.ParticipationRate != 100.0 {
		t.Errorf("Expected participation rate 100.0, got %f", data.ParticipationRate)
	}

	// IsEpochBoundary should be false
	if data.IsEpochBoundary {
		t.Error("Expected IsEpochBoundary to be false")
	}
}

func TestFactory(t *testing.T) {
	logger := zap.NewNop()

	parser, err := Factory(nil, logger)
	if err != nil {
		t.Fatalf("Factory returned error: %v", err)
	}

	if parser == nil {
		t.Fatal("Expected non-nil parser from factory")
	}

	if parser.ConsensusType() != chain.ConsensusTypePoA {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypePoA, parser.ConsensusType())
	}
}

func TestParser_GetValidators(t *testing.T) {
	parser := NewParser(zap.NewNop())

	validators, err := parser.GetValidators(nil, 100)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// GetValidators returns empty slice for PoA (validators configured at genesis)
	if validators == nil {
		t.Error("Expected non-nil validators slice")
	}
	if len(validators) != 0 {
		t.Errorf("Expected empty validators slice, got %d validators", len(validators))
	}
}
