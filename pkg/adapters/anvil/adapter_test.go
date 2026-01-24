package anvil

import (
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"go.uber.org/zap"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	if config.ChainID.Cmp(big.NewInt(DefaultChainID)) != 0 {
		t.Errorf("Expected chain ID %d, got %s", DefaultChainID, config.ChainID)
	}

	if config.NativeCurrency != DefaultNativeCurrency {
		t.Errorf("Expected native currency %s, got %s", DefaultNativeCurrency, config.NativeCurrency)
	}

	if !config.EnableAnvilFeatures {
		t.Error("Expected Anvil features to be enabled by default")
	}
}

func TestConfig_CustomValues(t *testing.T) {
	config := &Config{
		ChainID:             big.NewInt(1337),
		RPCEndpoint:         "http://localhost:8545",
		NativeCurrency:      "MATIC",
		EnableAnvilFeatures: false,
	}

	if config.ChainID.Cmp(big.NewInt(1337)) != 0 {
		t.Errorf("Expected chain ID 1337, got %s", config.ChainID)
	}

	if config.NativeCurrency != "MATIC" {
		t.Errorf("Expected native currency MATIC, got %s", config.NativeCurrency)
	}

	if config.EnableAnvilFeatures {
		t.Error("Expected Anvil features to be disabled")
	}
}

func TestConstants(t *testing.T) {
	if DefaultChainID != 31337 {
		t.Errorf("Expected default chain ID 31337, got %d", DefaultChainID)
	}

	if DefaultNativeCurrency != "ETH" {
		t.Errorf("Expected default native currency ETH, got %s", DefaultNativeCurrency)
	}

	if DefaultNativeDecimals != 18 {
		t.Errorf("Expected default native decimals 18, got %d", DefaultNativeDecimals)
	}
}

// MockEVMClient is a mock implementation for testing
type MockEVMClient struct{}

func (m *MockEVMClient) GetLatestBlockNumber(ctx interface{}) (uint64, error) {
	return 100, nil
}

func TestAdapter_Info(t *testing.T) {
	// We can't easily test NewAdapter without a real client,
	// but we can test the Info structure
	expectedInfo := &chain.ChainInfo{
		ChainID:        big.NewInt(31337),
		ChainType:      chain.ChainTypeEVM,
		ConsensusType:  chain.ConsensusTypePoA,
		Name:           "Anvil",
		NativeCurrency: "ETH",
		Decimals:       18,
	}

	if expectedInfo.ChainID.Cmp(big.NewInt(31337)) != 0 {
		t.Error("Expected chain ID 31337")
	}

	if expectedInfo.ChainType != chain.ChainTypeEVM {
		t.Error("Expected chain type EVM")
	}

	if expectedInfo.ConsensusType != chain.ConsensusTypePoA {
		t.Error("Expected consensus type PoA")
	}

	if expectedInfo.Name != "Anvil" {
		t.Error("Expected name Anvil")
	}
}

func TestAnvilClient_Nil(t *testing.T) {
	// Test that GetAnvilClient returns nil when rpcClient is nil
	adapter := &Adapter{
		config:    DefaultConfig(),
		logger:    zap.NewNop(),
		rpcClient: nil,
	}

	client := adapter.GetAnvilClient()
	if client != nil {
		t.Error("Expected nil AnvilClient when rpcClient is nil")
	}
}

func TestAdapter_SystemContracts(t *testing.T) {
	adapter := &Adapter{
		config: DefaultConfig(),
		logger: zap.NewNop(),
	}

	// Anvil doesn't have system contracts
	if adapter.SystemContracts() != nil {
		t.Error("Expected nil SystemContracts for Anvil adapter")
	}
}

func TestAdapter_ConsensusParser_Nil(t *testing.T) {
	adapter := &Adapter{
		config:          DefaultConfig(),
		logger:          zap.NewNop(),
		consensusParser: nil,
	}

	if adapter.ConsensusParser() != nil {
		t.Error("Expected nil consensus parser when not set")
	}
}
