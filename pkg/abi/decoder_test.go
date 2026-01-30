package abi

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Sample ABI for testing (ERC20 Transfer event and approve function)
const testABI = `[
	{
		"anonymous": false,
		"inputs": [
			{"indexed": true, "name": "from", "type": "address"},
			{"indexed": true, "name": "to", "type": "address"},
			{"indexed": false, "name": "value", "type": "uint256"}
		],
		"name": "Transfer",
		"type": "event"
	},
	{
		"inputs": [
			{"name": "spender", "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"name": "approve",
		"outputs": [{"name": "", "type": "bool"}],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

func TestNewDecoder(t *testing.T) {
	d := NewDecoder()
	if d == nil {
		t.Fatal("NewDecoder returned nil")
	}
	if d.contracts == nil {
		t.Error("contracts map should be initialized")
	}
}

func TestDecoder_LoadABI(t *testing.T) {
	d := NewDecoder()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	err := d.LoadABI(addr, "TestToken", testABI)
	if err != nil {
		t.Fatalf("LoadABI failed: %v", err)
	}

	// Verify the ABI is loaded
	if !d.HasABI(addr) {
		t.Error("HasABI should return true after LoadABI")
	}
}

func TestDecoder_LoadABI_InvalidABI(t *testing.T) {
	d := NewDecoder()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	err := d.LoadABI(addr, "TestToken", "invalid json")
	if err == nil {
		t.Error("LoadABI should fail with invalid ABI JSON")
	}
}

func TestDecoder_UnloadABI(t *testing.T) {
	d := NewDecoder()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	_ = d.LoadABI(addr, "TestToken", testABI)
	if !d.HasABI(addr) {
		t.Error("ABI should be loaded")
	}

	d.UnloadABI(addr)
	if d.HasABI(addr) {
		t.Error("HasABI should return false after UnloadABI")
	}
}

func TestDecoder_HasABI(t *testing.T) {
	d := NewDecoder()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	if d.HasABI(addr) {
		t.Error("HasABI should return false for unknown address")
	}

	_ = d.LoadABI(addr, "TestToken", testABI)
	if !d.HasABI(addr) {
		t.Error("HasABI should return true for loaded address")
	}
}

func TestDecoder_GetABI(t *testing.T) {
	d := NewDecoder()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Test not found
	_, err := d.GetABI(addr)
	if err == nil {
		t.Error("GetABI should return error for unknown address")
	}

	// Test found
	_ = d.LoadABI(addr, "TestToken", testABI)
	contractABI, err := d.GetABI(addr)
	if err != nil {
		t.Fatalf("GetABI failed: %v", err)
	}
	if contractABI.Name != "TestToken" {
		t.Errorf("expected name TestToken, got %s", contractABI.Name)
	}
	if contractABI.Address != addr {
		t.Errorf("expected address %s, got %s", addr.Hex(), contractABI.Address.Hex())
	}
}

func TestValidateABI(t *testing.T) {
	tests := []struct {
		name    string
		abiJSON string
		wantErr bool
	}{
		{
			name:    "valid ABI",
			abiJSON: testABI,
			wantErr: false,
		},
		{
			name:    "empty array is valid",
			abiJSON: "[]",
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			abiJSON: "not json",
			wantErr: true,
		},
		{
			name:    "invalid ABI structure",
			abiJSON: `{"invalid": "structure"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateABI(tt.abiJSON)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateABI() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetEventSignature(t *testing.T) {
	// Test success case
	sig, err := GetEventSignature(testABI, "Transfer")
	if err != nil {
		t.Fatalf("GetEventSignature failed: %v", err)
	}
	if sig == (common.Hash{}) {
		t.Error("signature should not be empty")
	}

	// Test invalid ABI
	_, err = GetEventSignature("invalid", "Transfer")
	if err == nil {
		t.Error("GetEventSignature should fail with invalid ABI")
	}

	// Test event not found
	_, err = GetEventSignature(testABI, "NonExistent")
	if err == nil {
		t.Error("GetEventSignature should fail for unknown event")
	}
}

func TestGetMethodSelector(t *testing.T) {
	// Test success case
	selector, err := GetMethodSelector(testABI, "approve")
	if err != nil {
		t.Fatalf("GetMethodSelector failed: %v", err)
	}
	if len(selector) != 4 {
		t.Errorf("selector should be 4 bytes, got %d", len(selector))
	}

	// Test invalid ABI
	_, err = GetMethodSelector("invalid", "approve")
	if err == nil {
		t.Error("GetMethodSelector should fail with invalid ABI")
	}

	// Test method not found
	_, err = GetMethodSelector(testABI, "nonExistent")
	if err == nil {
		t.Error("GetMethodSelector should fail for unknown method")
	}
}

func TestExtractEventsFromABI(t *testing.T) {
	events, err := ExtractEventsFromABI(testABI)
	if err != nil {
		t.Fatalf("ExtractEventsFromABI failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}

	if _, ok := events["Transfer"]; !ok {
		t.Error("Transfer event not found")
	}

	// Test invalid ABI
	_, err = ExtractEventsFromABI("invalid")
	if err == nil {
		t.Error("ExtractEventsFromABI should fail with invalid ABI")
	}
}

func TestExtractMethodsFromABI(t *testing.T) {
	methods, err := ExtractMethodsFromABI(testABI)
	if err != nil {
		t.Fatalf("ExtractMethodsFromABI failed: %v", err)
	}

	if len(methods) != 1 {
		t.Errorf("expected 1 method, got %d", len(methods))
	}

	if selector, ok := methods["approve"]; !ok {
		t.Error("approve method not found")
	} else if len(selector) != 4 {
		t.Errorf("selector should be 4 bytes, got %d", len(selector))
	}

	// Test invalid ABI
	_, err = ExtractMethodsFromABI("invalid")
	if err == nil {
		t.Error("ExtractMethodsFromABI should fail with invalid ABI")
	}
}

func TestMarshalUnmarshalContractABI(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	original := &ContractABI{
		Address: addr,
		Name:    "TestToken",
		ABI:     testABI,
	}

	// Marshal
	data, err := MarshalContractABI(original)
	if err != nil {
		t.Fatalf("MarshalContractABI failed: %v", err)
	}

	// Unmarshal
	restored, err := UnmarshalContractABI(data)
	if err != nil {
		t.Fatalf("UnmarshalContractABI failed: %v", err)
	}

	if restored.Name != original.Name {
		t.Errorf("Name mismatch: expected %s, got %s", original.Name, restored.Name)
	}
	if restored.Address != original.Address {
		t.Errorf("Address mismatch: expected %s, got %s", original.Address.Hex(), restored.Address.Hex())
	}
	if restored.parsed == nil {
		t.Error("parsed ABI should not be nil after unmarshal")
	}
}

func TestUnmarshalContractABI_InvalidJSON(t *testing.T) {
	_, err := UnmarshalContractABI([]byte("invalid json"))
	if err == nil {
		t.Error("UnmarshalContractABI should fail with invalid JSON")
	}
}

func TestUnmarshalContractABI_InvalidABI(t *testing.T) {
	// Valid JSON but invalid ABI
	data := []byte(`{"address":"0x1234567890123456789012345678901234567890","name":"Test","abi":"invalid abi"}`)
	_, err := UnmarshalContractABI(data)
	if err == nil {
		t.Error("UnmarshalContractABI should fail with invalid ABI in JSON")
	}
}
