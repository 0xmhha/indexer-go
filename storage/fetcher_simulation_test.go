package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestFetcherInterfaceCheck simulates the exact interface check that fetcher.go does
func TestFetcherInterfaceCheck(t *testing.T) {
	storageInterface, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()

	// This is exactly what fetch/fetcher.go does at line 1202
	histWriter, ok := storageInterface.(HistoricalWriter)
	if !ok {
		t.Fatal("❌ Interface check failed: storage does not implement HistoricalWriter")
	}
	t.Log("✅ Interface check passed: storage implements HistoricalWriter")

	// Now try to use it
	addr := common.HexToAddress("0xABCDEF")
	delta := big.NewInt(1000000)
	txHash := common.HexToHash("0x123")

	err := histWriter.UpdateBalance(ctx, addr, 100, delta, txHash)
	if err != nil {
		t.Fatalf("❌ UpdateBalance failed: %v", err)
	}
	t.Log("✅ UpdateBalance succeeded via HistoricalWriter interface")

	// Verify the balance was actually updated
	// We need to cast back to check
	storage, ok := storageInterface.(*PebbleStorage)
	if !ok {
		t.Fatal("Cannot cast back to *PebbleStorage")
	}

	balance, err := storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance failed: %v", err)
	}

	if balance.Cmp(delta) != 0 {
		t.Errorf("❌ Balance mismatch: expected %s, got %s", delta.String(), balance.String())
	} else {
		t.Logf("✅ Balance correctly updated to %s", balance.String())
	}

	t.Log("\n✅ Fetcher simulation test passed - interface check and balance update work correctly!")
}

// TestStorageInterfaceReturnsHistoricalWriter verifies the actual storage interface used by fetcher
func TestStorageInterfaceReturnsHistoricalWriter(t *testing.T) {
	// This tests the scenario where Storage interface is used (like in fetcher)
	storageInterface, cleanup := setupTestStorage(t)
	defer cleanup()

	// Test 1: Check if it's a Storage interface
	_, isStorage := storageInterface.(Storage)
	if !isStorage {
		t.Error("❌ Not a Storage interface")
	} else {
		t.Log("✅ Is a Storage interface")
	}

	// Test 2: Check if Storage interface can be cast to HistoricalWriter
	_, isHistWriter := storageInterface.(HistoricalWriter)
	if !isHistWriter {
		t.Error("❌ Storage cannot be cast to HistoricalWriter")
	} else {
		t.Log("✅ Storage CAN be cast to HistoricalWriter")
	}

	// Test 3: Check if Storage interface can be cast to HistoricalStorage
	_, isHistStorage := storageInterface.(HistoricalStorage)
	if !isHistStorage {
		t.Error("❌ Storage cannot be cast to HistoricalStorage")
	} else {
		t.Log("✅ Storage CAN be cast to HistoricalStorage")
	}

	t.Log("\n✅ Storage interface correctly supports HistoricalWriter!")
}
