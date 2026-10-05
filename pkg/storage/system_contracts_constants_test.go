package storage

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

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
