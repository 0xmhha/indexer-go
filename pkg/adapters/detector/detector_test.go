package detector

import (
	"testing"
)

func TestParseClientVersion_Anvil(t *testing.T) {
	detector := &Detector{}

	testCases := []struct {
		name     string
		version  string
		expected NodeType
	}{
		{
			name:     "Anvil standard",
			version:  "anvil/v0.2.0",
			expected: NodeTypeAnvil,
		},
		{
			name:     "Anvil with hash",
			version:  "anvil/v0.1.0/linux-aarch64/abc123",
			expected: NodeTypeAnvil,
		},
		{
			name:     "Foundry anvil",
			version:  "foundry-anvil/0.2.0",
			expected: NodeTypeAnvil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.parseClientVersion(tc.version)
			if result != tc.expected {
				t.Errorf("parseClientVersion(%q) = %s, want %s", tc.version, result, tc.expected)
			}
		})
	}
}

func TestParseClientVersion_Geth(t *testing.T) {
	detector := &Detector{}

	testCases := []struct {
		name     string
		version  string
		expected NodeType
	}{
		{
			name:     "Geth standard",
			version:  "Geth/v1.14.0-stable/linux-amd64/go1.22",
			expected: NodeTypeGeth,
		},
		{
			name:     "go-ethereum",
			version:  "go-ethereum/v1.13.0/linux-amd64",
			expected: NodeTypeGeth,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.parseClientVersion(tc.version)
			if result != tc.expected {
				t.Errorf("parseClientVersion(%q) = %s, want %s", tc.version, result, tc.expected)
			}
		})
	}
}

func TestParseClientVersion_StableOne(t *testing.T) {
	detector := &Detector{}

	testCases := []struct {
		name     string
		version  string
		expected NodeType
	}{
		{
			name:     "StableOne standard",
			version:  "StableOne/v1.0.0",
			expected: NodeTypeStableOne,
		},
		{
			name:     "go-stablenet variant",
			version:  "go-stablenet/v1.0.0",
			expected: NodeTypeStableOne,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.parseClientVersion(tc.version)
			if result != tc.expected {
				t.Errorf("parseClientVersion(%q) = %s, want %s", tc.version, result, tc.expected)
			}
		})
	}
}

func TestParseClientVersion_Hardhat(t *testing.T) {
	detector := &Detector{}

	testCases := []struct {
		name     string
		version  string
		expected NodeType
	}{
		{
			name:     "Hardhat Network",
			version:  "HardhatNetwork/2.22.0",
			expected: NodeTypeHardhat,
		},
		{
			name:     "hardhat lowercase",
			version:  "hardhat/1.0.0",
			expected: NodeTypeHardhat,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.parseClientVersion(tc.version)
			if result != tc.expected {
				t.Errorf("parseClientVersion(%q) = %s, want %s", tc.version, result, tc.expected)
			}
		})
	}
}

func TestParseClientVersion_Ganache(t *testing.T) {
	detector := &Detector{}

	testCases := []struct {
		name     string
		version  string
		expected NodeType
	}{
		{
			name:     "Ganache standard",
			version:  "Ganache/v7.9.0",
			expected: NodeTypeGanache,
		},
		{
			name:     "EthereumJS TestRPC",
			version:  "EthereumJS TestRPC/v2.0.0",
			expected: NodeTypeGanache,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.parseClientVersion(tc.version)
			if result != tc.expected {
				t.Errorf("parseClientVersion(%q) = %s, want %s", tc.version, result, tc.expected)
			}
		})
	}
}

func TestParseClientVersion_Unknown(t *testing.T) {
	detector := &Detector{}

	testCases := []struct {
		name     string
		version  string
		expected NodeType
	}{
		{
			name:     "Empty string",
			version:  "",
			expected: NodeTypeUnknown,
		},
		{
			name:     "Unknown client",
			version:  "SomeRandomClient/v1.0.0",
			expected: NodeTypeUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.parseClientVersion(tc.version)
			if result != tc.expected {
				t.Errorf("parseClientVersion(%q) = %s, want %s", tc.version, result, tc.expected)
			}
		})
	}
}

func TestNodeInfo_IsLocal(t *testing.T) {
	testCases := []struct {
		name     string
		chainID  uint64
		expected bool
	}{
		{
			name:     "Anvil default chain ID",
			chainID:  31337,
			expected: true,
		},
		{
			name:     "Ganache default chain ID",
			chainID:  1337,
			expected: true,
		},
		{
			name:     "Mainnet chain ID",
			chainID:  1,
			expected: false,
		},
		{
			name:     "Arbitrum chain ID",
			chainID:  42161,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isLocalChainID(tc.chainID)
			if result != tc.expected {
				t.Errorf("isLocalChainID(%d) = %v, want %v", tc.chainID, result, tc.expected)
			}
		})
	}
}

func TestNodeInfo_Fields(t *testing.T) {
	info := &NodeInfo{
		Type:                 NodeTypeAnvil,
		ClientVersion:        "anvil/v0.2.0",
		ChainID:              31337,
		IsLocal:              true,
		SupportsPendingTx:    true,
		SupportsDebug:        true,
		SupportsAnvilMethods: true,
	}

	if info.Type != NodeTypeAnvil {
		t.Errorf("Expected type Anvil, got %s", info.Type)
	}

	if info.ClientVersion != "anvil/v0.2.0" {
		t.Errorf("Expected client version 'anvil/v0.2.0', got %s", info.ClientVersion)
	}

	if info.ChainID != 31337 {
		t.Errorf("Expected chain ID 31337, got %d", info.ChainID)
	}

	if !info.IsLocal {
		t.Error("Expected IsLocal to be true")
	}

	if !info.SupportsAnvilMethods {
		t.Error("Expected SupportsAnvilMethods to be true")
	}
}

func TestNodeType_Constants(t *testing.T) {
	if NodeTypeAnvil != "anvil" {
		t.Errorf("Expected NodeTypeAnvil to be 'anvil', got %s", NodeTypeAnvil)
	}

	if NodeTypeGeth != "geth" {
		t.Errorf("Expected NodeTypeGeth to be 'geth', got %s", NodeTypeGeth)
	}

	if NodeTypeStableOne != "stableone" {
		t.Errorf("Expected NodeTypeStableOne to be 'stableone', got %s", NodeTypeStableOne)
	}

	if NodeTypeHardhat != "hardhat" {
		t.Errorf("Expected NodeTypeHardhat to be 'hardhat', got %s", NodeTypeHardhat)
	}

	if NodeTypeGanache != "ganache" {
		t.Errorf("Expected NodeTypeGanache to be 'ganache', got %s", NodeTypeGanache)
	}

	if NodeTypeUnknown != "unknown" {
		t.Errorf("Expected NodeTypeUnknown to be 'unknown', got %s", NodeTypeUnknown)
	}
}
