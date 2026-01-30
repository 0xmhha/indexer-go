package storage

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestIsSystemContract(t *testing.T) {
	tests := []struct {
		name     string
		addr     common.Address
		expected bool
	}{
		{
			name:     "NativeCoinAdapter is system contract",
			addr:     NativeCoinAdapterAddress,
			expected: true,
		},
		{
			name:     "GovValidator is system contract",
			addr:     GovValidatorAddress,
			expected: true,
		},
		{
			name:     "GovMasterMinter is system contract",
			addr:     GovMasterMinterAddress,
			expected: true,
		},
		{
			name:     "GovMinter is system contract",
			addr:     GovMinterAddress,
			expected: true,
		},
		{
			name:     "GovCouncil is system contract",
			addr:     GovCouncilAddress,
			expected: true,
		},
		{
			name:     "random address is not system contract",
			addr:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			expected: false,
		},
		{
			name:     "zero address is not system contract",
			addr:     common.Address{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSystemContract(tt.addr)
			if result != tt.expected {
				t.Errorf("IsSystemContract(%s) = %v, want %v", tt.addr.Hex(), result, tt.expected)
			}
		})
	}
}

func TestGetSystemContractTokenMetadata(t *testing.T) {
	// Test NativeCoinAdapter should have metadata
	metadata := GetSystemContractTokenMetadata(NativeCoinAdapterAddress)
	if metadata == nil {
		t.Error("NativeCoinAdapter should have token metadata")
	} else {
		if metadata.Name == "" {
			t.Error("metadata Name should not be empty")
		}
		if metadata.Symbol == "" {
			t.Error("metadata Symbol should not be empty")
		}
	}

	// Test random address should return nil
	randomAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	metadata = GetSystemContractTokenMetadata(randomAddr)
	if metadata != nil {
		t.Error("random address should not have token metadata")
	}
}

func TestGetEventName(t *testing.T) {
	tests := []struct {
		name        string
		sig         common.Hash
		shouldExist bool
	}{
		{
			name:        "Transfer event",
			sig:         EventSigTransfer,
			shouldExist: true,
		},
		{
			name:        "Approval event",
			sig:         EventSigApproval,
			shouldExist: true,
		},
		{
			name:        "Mint event",
			sig:         EventSigMint,
			shouldExist: true,
		},
		{
			name:        "Burn event",
			sig:         EventSigBurn,
			shouldExist: true,
		},
		{
			name:        "ProposalCreated event",
			sig:         EventSigProposalCreated,
			shouldExist: true,
		},
		{
			name:        "unknown event returns Unknown",
			sig:         common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
			shouldExist: true, // Returns "Unknown" for unknown events
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEventName(tt.sig)
			if tt.shouldExist && result == "" {
				t.Errorf("GetEventName(%s) should return non-empty name", tt.sig.Hex())
			}
			if !tt.shouldExist && result != "" {
				t.Errorf("GetEventName(%s) should return empty for unknown event, got %q", tt.sig.Hex(), result)
			}
		})
	}
}

func TestSystemContractAddresses(t *testing.T) {
	// Verify the map contains expected addresses
	if len(SystemContractAddresses) == 0 {
		t.Error("SystemContractAddresses map should not be empty")
	}

	// Check that NativeCoinAdapter is in the map
	if !SystemContractAddresses[NativeCoinAdapterAddress] {
		t.Error("NativeCoinAdapterAddress should be in SystemContractAddresses map")
	}

	// Check GovValidator is in the map
	if !SystemContractAddresses[GovValidatorAddress] {
		t.Error("GovValidatorAddress should be in SystemContractAddresses map")
	}
}

func TestEventSignatureToName(t *testing.T) {
	// Verify the map is populated
	if len(EventSignatureToName) == 0 {
		t.Error("EventSignatureToName map should not be empty")
	}

	// Check specific mappings
	if name, ok := EventSignatureToName[EventSigTransfer]; !ok {
		t.Error("EventSigTransfer should be in EventSignatureToName")
	} else if name == "" {
		t.Error("EventSigTransfer name should not be empty")
	}
}
