package consensus

import (
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	registry := NewRegistry()

	// Create a mock factory
	mockFactory := func(config *Config, logger *zap.Logger) (chain.ConsensusParser, error) {
		return &mockParser{}, nil
	}

	// Register the mock parser
	err := registry.Register(chain.ConsensusTypeWBFT, mockFactory, &ParserMetadata{
		Name:        "Mock WBFT",
		Description: "A mock WBFT parser for testing",
		Version:     "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to register parser: %v", err)
	}

	// Verify it's registered
	if !registry.Has(chain.ConsensusTypeWBFT) {
		t.Error("Expected WBFT to be registered")
	}

	// Get the parser
	parser, err := registry.Get(chain.ConsensusTypeWBFT, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to get parser: %v", err)
	}

	if parser == nil {
		t.Error("Expected non-nil parser")
	}

	if parser.ConsensusType() != chain.ConsensusTypeWBFT {
		t.Errorf("Expected consensus type %s, got %s", chain.ConsensusTypeWBFT, parser.ConsensusType())
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	registry := NewRegistry()

	mockFactory := func(config *Config, logger *zap.Logger) (chain.ConsensusParser, error) {
		return &mockParser{}, nil
	}

	// First registration should succeed
	err := registry.Register(chain.ConsensusTypeWBFT, mockFactory, nil)
	if err != nil {
		t.Fatalf("First registration should succeed: %v", err)
	}

	// Second registration should fail
	err = registry.Register(chain.ConsensusTypeWBFT, mockFactory, nil)
	if err == nil {
		t.Error("Expected error for duplicate registration")
	}
}

func TestRegistry_UnknownType(t *testing.T) {
	registry := NewRegistry()

	// Try to get unregistered type
	_, err := registry.Get(chain.ConsensusTypeWBFT, nil, zap.NewNop())
	if err == nil {
		t.Error("Expected error for unknown consensus type")
	}
}

func TestRegistry_SupportedTypes(t *testing.T) {
	registry := NewRegistry()

	mockFactory := func(config *Config, logger *zap.Logger) (chain.ConsensusParser, error) {
		return &mockParser{}, nil
	}

	// Register multiple types
	registry.MustRegister(chain.ConsensusTypeWBFT, mockFactory, nil)
	registry.MustRegister(chain.ConsensusTypePoA, mockFactory, nil)

	types := registry.SupportedTypes()
	if len(types) != 2 {
		t.Errorf("Expected 2 supported types, got %d", len(types))
	}

	// Check both types are present
	hasWBFT := false
	hasPoA := false
	for _, typ := range types {
		if typ == chain.ConsensusTypeWBFT {
			hasWBFT = true
		}
		if typ == chain.ConsensusTypePoA {
			hasPoA = true
		}
	}

	if !hasWBFT || !hasPoA {
		t.Error("Expected both WBFT and PoA to be in supported types")
	}
}

func TestRegistry_Metadata(t *testing.T) {
	registry := NewRegistry()

	mockFactory := func(config *Config, logger *zap.Logger) (chain.ConsensusParser, error) {
		return &mockParser{}, nil
	}

	metadata := &ParserMetadata{
		Name:        "Test Parser",
		Description: "A test parser",
		Version:     "2.0.0",
		SupportedChainTypes: []chain.ChainType{
			chain.ChainTypeEVM,
		},
	}

	registry.MustRegister(chain.ConsensusTypeWBFT, mockFactory, metadata)

	// Get metadata
	gotMeta, exists := registry.GetMetadata(chain.ConsensusTypeWBFT)
	if !exists {
		t.Fatal("Expected metadata to exist")
	}

	if gotMeta.Name != "Test Parser" {
		t.Errorf("Expected name 'Test Parser', got '%s'", gotMeta.Name)
	}

	if gotMeta.Version != "2.0.0" {
		t.Errorf("Expected version '2.0.0', got '%s'", gotMeta.Version)
	}
}

func TestRegistry_Unregister(t *testing.T) {
	registry := NewRegistry()

	mockFactory := func(config *Config, logger *zap.Logger) (chain.ConsensusParser, error) {
		return &mockParser{}, nil
	}

	registry.MustRegister(chain.ConsensusTypeWBFT, mockFactory, nil)

	if !registry.Has(chain.ConsensusTypeWBFT) {
		t.Error("Expected WBFT to be registered")
	}

	registry.Unregister(chain.ConsensusTypeWBFT)

	if registry.Has(chain.ConsensusTypeWBFT) {
		t.Error("Expected WBFT to be unregistered")
	}
}

// mockParser is a mock implementation of chain.ConsensusParser for testing
type mockParser struct{}

func (m *mockParser) ConsensusType() chain.ConsensusType {
	return chain.ConsensusTypeWBFT
}

func (m *mockParser) ParseConsensusData(block *types.Block) (*chain.ConsensusData, error) {
	return nil, nil
}

func (m *mockParser) GetValidators(ctx context.Context, blockNumber uint64) ([]common.Address, error) {
	return nil, nil
}

func (m *mockParser) IsEpochBoundary(block *types.Block) bool {
	return false
}
