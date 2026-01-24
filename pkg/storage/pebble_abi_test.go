package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestPebbleStorage_SetABI(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	abiJSON := []byte(`[{"name":"transfer","type":"function"}]`)

	// Set ABI
	err := storage.SetABI(ctx, addr, abiJSON)
	if err != nil {
		t.Fatalf("SetABI() error = %v", err)
	}

	// Verify ABI was set
	hasABI, err := storage.HasABI(ctx, addr)
	if err != nil {
		t.Fatalf("HasABI() error = %v", err)
	}
	if !hasABI {
		t.Error("HasABI() = false, want true")
	}
}

func TestPebbleStorage_GetABI(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	abiJSON := []byte(`[{"name":"transfer","type":"function","inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"}]}]`)

	// Set ABI
	err := storage.SetABI(ctx, addr, abiJSON)
	if err != nil {
		t.Fatalf("SetABI() error = %v", err)
	}

	// Get ABI
	retrievedABI, err := storage.GetABI(ctx, addr)
	if err != nil {
		t.Fatalf("GetABI() error = %v", err)
	}

	// Compare JSON content
	if string(retrievedABI) != string(abiJSON) {
		t.Errorf("GetABI() = %s, want %s", retrievedABI, abiJSON)
	}

	// Verify it's valid JSON
	var abiArray []interface{}
	err = json.Unmarshal(retrievedABI, &abiArray)
	if err != nil {
		t.Errorf("Retrieved ABI is not valid JSON: %v", err)
	}
}

func TestPebbleStorage_GetABI_NotFound(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x9999999999999999999999999999999999999999")

	// Get non-existent ABI
	_, err := storage.GetABI(ctx, addr)
	if err != ErrNotFound {
		t.Errorf("GetABI() error = %v, want ErrNotFound", err)
	}
}

func TestPebbleStorage_HasABI(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	abiJSON := []byte(`[{"name":"test","type":"function"}]`)

	// Initially should not have ABI
	hasABI, err := storage.HasABI(ctx, addr1)
	if err != nil {
		t.Fatalf("HasABI() error = %v", err)
	}
	if hasABI {
		t.Error("HasABI() = true, want false")
	}

	// Set ABI for addr1
	storage.SetABI(ctx, addr1, abiJSON)

	// addr1 should have ABI
	hasABI, err = storage.HasABI(ctx, addr1)
	if err != nil {
		t.Fatalf("HasABI() error = %v", err)
	}
	if !hasABI {
		t.Error("HasABI() = false, want true")
	}

	// addr2 should not have ABI
	hasABI, err = storage.HasABI(ctx, addr2)
	if err != nil {
		t.Fatalf("HasABI() error = %v", err)
	}
	if hasABI {
		t.Error("HasABI() = true, want false")
	}
}

func TestPebbleStorage_DeleteABI(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	abiJSON := []byte(`[{"name":"transfer","type":"function"}]`)

	// Set ABI
	err := storage.SetABI(ctx, addr, abiJSON)
	if err != nil {
		t.Fatalf("SetABI() error = %v", err)
	}

	// Verify ABI exists
	hasABI, err := storage.HasABI(ctx, addr)
	if err != nil {
		t.Fatalf("HasABI() error = %v", err)
	}
	if !hasABI {
		t.Fatal("ABI should exist before deletion")
	}

	// Delete ABI
	err = storage.DeleteABI(ctx, addr)
	if err != nil {
		t.Fatalf("DeleteABI() error = %v", err)
	}

	// Verify ABI was deleted
	hasABI, err = storage.HasABI(ctx, addr)
	if err != nil {
		t.Fatalf("HasABI() error = %v", err)
	}
	if hasABI {
		t.Error("HasABI() = true, want false after deletion")
	}

	// GetABI should return ErrNotFound
	_, err = storage.GetABI(ctx, addr)
	if err != ErrNotFound {
		t.Errorf("GetABI() after deletion error = %v, want ErrNotFound", err)
	}
}

func TestPebbleStorage_ListABIs(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Initially should be empty
	addresses, err := storage.ListABIs(ctx)
	if err != nil {
		t.Fatalf("ListABIs() error = %v", err)
	}
	if len(addresses) != 0 {
		t.Errorf("ListABIs() returned %d addresses, want 0", len(addresses))
	}

	// Add multiple ABIs
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := common.HexToAddress("0x3333333333333333333333333333333333333333")

	abiJSON1 := []byte(`[{"name":"func1","type":"function"}]`)
	abiJSON2 := []byte(`[{"name":"func2","type":"function"}]`)
	abiJSON3 := []byte(`[{"name":"func3","type":"function"}]`)

	storage.SetABI(ctx, addr1, abiJSON1)
	storage.SetABI(ctx, addr2, abiJSON2)
	storage.SetABI(ctx, addr3, abiJSON3)

	// List all ABIs
	addresses, err = storage.ListABIs(ctx)
	if err != nil {
		t.Fatalf("ListABIs() error = %v", err)
	}

	if len(addresses) != 3 {
		t.Fatalf("ListABIs() returned %d addresses, want 3", len(addresses))
	}

	// Verify all addresses are present
	addrMap := make(map[common.Address]bool)
	for _, addr := range addresses {
		addrMap[addr] = true
	}

	if !addrMap[addr1] {
		t.Error("ListABIs() missing addr1")
	}
	if !addrMap[addr2] {
		t.Error("ListABIs() missing addr2")
	}
	if !addrMap[addr3] {
		t.Error("ListABIs() missing addr3")
	}
}

func TestPebbleStorage_UpdateABI(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	abiJSON1 := []byte(`[{"name":"transfer","type":"function"}]`)
	abiJSON2 := []byte(`[{"name":"approve","type":"function"}]`)

	// Set initial ABI
	err := storage.SetABI(ctx, addr, abiJSON1)
	if err != nil {
		t.Fatalf("SetABI() error = %v", err)
	}

	// Verify initial ABI
	retrievedABI, err := storage.GetABI(ctx, addr)
	if err != nil {
		t.Fatalf("GetABI() error = %v", err)
	}
	if string(retrievedABI) != string(abiJSON1) {
		t.Error("Initial ABI mismatch")
	}

	// Update ABI
	err = storage.SetABI(ctx, addr, abiJSON2)
	if err != nil {
		t.Fatalf("SetABI() update error = %v", err)
	}

	// Verify updated ABI
	retrievedABI, err = storage.GetABI(ctx, addr)
	if err != nil {
		t.Fatalf("GetABI() error = %v", err)
	}
	if string(retrievedABI) != string(abiJSON2) {
		t.Error("Updated ABI mismatch")
	}
}

func TestPebbleStorage_SetABI_EmptyJSON(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Try to set empty ABI
	err := storage.SetABI(ctx, addr, []byte{})
	if err == nil {
		t.Error("SetABI() with empty JSON should return error")
	}

	// Try to set nil ABI
	err = storage.SetABI(ctx, addr, nil)
	if err == nil {
		t.Error("SetABI() with nil JSON should return error")
	}
}

func TestPebbleStorage_ABI_ComplexJSON(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Complex ERC20 ABI
	complexABI := []byte(`[
		{
			"name":"transfer",
			"type":"function",
			"inputs":[
				{"name":"to","type":"address"},
				{"name":"value","type":"uint256"}
			],
			"outputs":[{"name":"","type":"bool"}],
			"stateMutability":"nonpayable"
		},
		{
			"name":"balanceOf",
			"type":"function",
			"inputs":[{"name":"account","type":"address"}],
			"outputs":[{"name":"","type":"uint256"}],
			"stateMutability":"view"
		},
		{
			"name":"Transfer",
			"type":"event",
			"inputs":[
				{"name":"from","type":"address","indexed":true},
				{"name":"to","type":"address","indexed":true},
				{"name":"value","type":"uint256","indexed":false}
			]
		}
	]`)

	// Set complex ABI
	err := storage.SetABI(ctx, addr, complexABI)
	if err != nil {
		t.Fatalf("SetABI() error = %v", err)
	}

	// Retrieve and verify
	retrievedABI, err := storage.GetABI(ctx, addr)
	if err != nil {
		t.Fatalf("GetABI() error = %v", err)
	}

	// Verify it's valid JSON
	var abiArray []interface{}
	err = json.Unmarshal(retrievedABI, &abiArray)
	if err != nil {
		t.Errorf("Retrieved ABI is not valid JSON: %v", err)
	}

	if len(abiArray) != 3 {
		t.Errorf("ABI should have 3 elements, got %d", len(abiArray))
	}
}

func TestPebbleStorage_ABI_ReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-test-readonly-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage and add an ABI
	cfg := DefaultConfig(tmpDir)
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	ctx := context.Background()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	abiJSON := []byte(`[{"name":"test","type":"function"}]`)

	storage.SetABI(ctx, addr, abiJSON)
	storage.Close()

	// Reopen as read-only
	cfg.ReadOnly = true
	roStorage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create read-only storage: %v", err)
	}
	defer roStorage.Close()

	// Should be able to read
	retrievedABI, err := roStorage.GetABI(ctx, addr)
	if err != nil {
		t.Fatalf("GetABI() on read-only storage error = %v", err)
	}
	if string(retrievedABI) != string(abiJSON) {
		t.Error("ABI mismatch")
	}

	// Should be able to check existence
	hasABI, err := roStorage.HasABI(ctx, addr)
	if err != nil {
		t.Fatalf("HasABI() on read-only storage error = %v", err)
	}
	if !hasABI {
		t.Error("HasABI() = false, want true")
	}

	// Should be able to list
	addresses, err := roStorage.ListABIs(ctx)
	if err != nil {
		t.Fatalf("ListABIs() on read-only storage error = %v", err)
	}
	if len(addresses) != 1 {
		t.Errorf("ListABIs() returned %d addresses, want 1", len(addresses))
	}

	// Should not be able to write
	newABI := []byte(`[{"name":"new","type":"function"}]`)
	err = roStorage.SetABI(ctx, addr, newABI)
	if err != ErrReadOnly {
		t.Errorf("SetABI() on read-only storage error = %v, want ErrReadOnly", err)
	}

	// Should not be able to delete
	err = roStorage.DeleteABI(ctx, addr)
	if err != ErrReadOnly {
		t.Errorf("DeleteABI() on read-only storage error = %v, want ErrReadOnly", err)
	}
}

func TestPebbleStorage_ABI_ClosedStorage(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	abiJSON := []byte(`[{"name":"test","type":"function"}]`)

	// Close storage
	storage.Close()

	// All operations should return ErrClosed
	err := storage.SetABI(ctx, addr, abiJSON)
	if err != ErrClosed {
		t.Errorf("SetABI() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.GetABI(ctx, addr)
	if err != ErrClosed {
		t.Errorf("GetABI() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.HasABI(ctx, addr)
	if err != ErrClosed {
		t.Errorf("HasABI() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.ListABIs(ctx)
	if err != ErrClosed {
		t.Errorf("ListABIs() on closed storage error = %v, want ErrClosed", err)
	}

	err = storage.DeleteABI(ctx, addr)
	if err != ErrClosed {
		t.Errorf("DeleteABI() on closed storage error = %v, want ErrClosed", err)
	}
}

func TestPebbleStorage_ABI_MultipleContracts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create 10 contracts with different ABIs
	contracts := make(map[common.Address][]byte)
	for i := 0; i < 10; i++ {
		addr := common.HexToAddress(fmt.Sprintf("0x%040d", i))
		abiJSON := []byte(fmt.Sprintf(`[{"name":"func%d","type":"function"}]`, i))
		contracts[addr] = abiJSON

		err := storage.SetABI(ctx, addr, abiJSON)
		if err != nil {
			t.Fatalf("SetABI() for contract %d error = %v", i, err)
		}
	}

	// Verify all ABIs
	for addr, expectedABI := range contracts {
		retrievedABI, err := storage.GetABI(ctx, addr)
		if err != nil {
			t.Fatalf("GetABI() error = %v", err)
		}
		if string(retrievedABI) != string(expectedABI) {
			t.Errorf("ABI mismatch for %s", addr.Hex())
		}
	}

	// Verify list count
	addresses, err := storage.ListABIs(ctx)
	if err != nil {
		t.Fatalf("ListABIs() error = %v", err)
	}
	if len(addresses) != 10 {
		t.Errorf("ListABIs() returned %d addresses, want 10", len(addresses))
	}

	// Delete half
	count := 0
	for addr := range contracts {
		if count >= 5 {
			break
		}
		err := storage.DeleteABI(ctx, addr)
		if err != nil {
			t.Fatalf("DeleteABI() error = %v", err)
		}
		count++
	}

	// Verify list count after deletion
	addresses, err = storage.ListABIs(ctx)
	if err != nil {
		t.Fatalf("ListABIs() error = %v", err)
	}
	if len(addresses) != 5 {
		t.Errorf("ListABIs() after deletion returned %d addresses, want 5", len(addresses))
	}
}
