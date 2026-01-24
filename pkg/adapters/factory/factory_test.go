package factory

import (
	"math/big"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/adapters/detector"
	"go.uber.org/zap"
)

func TestDefaultConfig(t *testing.T) {
	rpcEndpoint := "http://localhost:8545"
	config := DefaultConfig(rpcEndpoint)

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	if config.RPCEndpoint != rpcEndpoint {
		t.Errorf("Expected RPC endpoint %s, got %s", rpcEndpoint, config.RPCEndpoint)
	}

	if config.DetectionTimeout != 10*time.Second {
		t.Errorf("Expected detection timeout 10s, got %s", config.DetectionTimeout)
	}
}

func TestNewFactory(t *testing.T) {
	config := DefaultConfig("http://localhost:8545")
	logger := zap.NewNop()

	factory := NewFactory(config, logger)

	if factory == nil {
		t.Fatal("Expected non-nil factory")
	}
}

func TestNewFactory_DefaultTimeout(t *testing.T) {
	config := &Config{
		RPCEndpoint:      "http://localhost:8545",
		DetectionTimeout: 0, // Zero timeout should be defaulted
	}
	logger := zap.NewNop()

	factory := NewFactory(config, logger)

	// Factory should set default timeout
	if factory.config.DetectionTimeout != 10*time.Second {
		t.Errorf("Expected default timeout 10s, got %s", factory.config.DetectionTimeout)
	}
}

func TestConfig_WithForceAdapterType(t *testing.T) {
	config := &Config{
		RPCEndpoint:      "http://localhost:8545",
		ForceAdapterType: "anvil",
	}

	if config.ForceAdapterType != "anvil" {
		t.Errorf("Expected force adapter type 'anvil', got %s", config.ForceAdapterType)
	}
}

func TestConfig_WithChainID(t *testing.T) {
	chainID := big.NewInt(31337)
	config := &Config{
		RPCEndpoint: "http://localhost:8545",
		ChainID:     chainID,
	}

	if config.ChainID.Cmp(chainID) != 0 {
		t.Errorf("Expected chain ID %s, got %s", chainID, config.ChainID)
	}
}

func TestCreateResult_Fields(t *testing.T) {
	result := &CreateResult{
		Adapter:     nil,
		Client:      nil,
		NodeInfo:    &detector.NodeInfo{Type: detector.NodeTypeAnvil},
		AdapterType: "anvil",
	}

	if result.AdapterType != "anvil" {
		t.Errorf("Expected adapter type 'anvil', got %s", result.AdapterType)
	}

	if result.NodeInfo.Type != detector.NodeTypeAnvil {
		t.Errorf("Expected node type Anvil, got %s", result.NodeInfo.Type)
	}
}

func TestNodeTypeToAdapterType(t *testing.T) {
	testCases := []struct {
		name        string
		nodeType    detector.NodeType
		forceType   string
		expectedStr string
	}{
		{
			name:        "Anvil forced",
			nodeType:    detector.NodeTypeUnknown,
			forceType:   "anvil",
			expectedStr: "anvil",
		},
		{
			name:        "StableOne forced",
			nodeType:    detector.NodeTypeUnknown,
			forceType:   "stableone",
			expectedStr: "stableone",
		},
		{
			name:        "Geth forced",
			nodeType:    detector.NodeTypeUnknown,
			forceType:   "geth",
			expectedStr: "geth",
		},
		{
			name:        "Default EVM",
			nodeType:    detector.NodeTypeUnknown,
			forceType:   "",
			expectedStr: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				RPCEndpoint:      "http://localhost:8545",
				ForceAdapterType: tc.forceType,
			}

			if config.ForceAdapterType != tc.expectedStr {
				t.Errorf("Expected force adapter type %s, got %s", tc.expectedStr, config.ForceAdapterType)
			}
		})
	}
}
