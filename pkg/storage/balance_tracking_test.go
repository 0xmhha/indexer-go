package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestBalanceTrackingFullFlow tests the complete balance tracking workflow
func TestBalanceTrackingFullFlow(t *testing.T) {
	storageInterface, cleanup := setupTestStorage(t)
	defer cleanup()

	// Cast to *PebbleStorage to access HistoricalWriter methods
	storage, ok := storageInterface.(*PebbleStorage)
	if !ok {
		t.Fatal("setupTestStorage did not return *PebbleStorage")
	}

	ctx := context.Background()
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// 1. Verify interface implementation
	_, ok = interface{}(storage).(HistoricalWriter)
	if !ok {
		t.Fatal("PebbleStorage does not implement HistoricalWriter interface")
	}
	t.Log("✅ PebbleStorage implements HistoricalWriter")

	// 2. Get initial balance (should be 0)
	initialBalance, err := storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() failed: %v", err)
	}
	if initialBalance.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("Initial balance should be 0, got %s", initialBalance.String())
	}
	t.Logf("✅ Initial balance is 0: %s", initialBalance.String())

	// 3. Update balance with +100 ETH
	delta1 := new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18))
	txHash1 := common.HexToHash("0xabc123")
	err = storage.UpdateBalance(ctx, addr, 1, delta1, txHash1)
	if err != nil {
		t.Fatalf("UpdateBalance() failed: %v", err)
	}
	t.Logf("✅ Updated balance with +%s wei", delta1.String())

	// 4. Verify balance is now 100 ETH
	balance1, err := storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() after update failed: %v", err)
	}
	expected1 := delta1
	if balance1.Cmp(expected1) != 0 {
		t.Errorf("Balance after first update should be %s, got %s", expected1.String(), balance1.String())
	}
	t.Logf("✅ Balance after first update: %s wei (100 ETH)", balance1.String())

	// 5. Update balance with -30 ETH
	delta2 := new(big.Int).Mul(big.NewInt(-30), big.NewInt(1e18))
	txHash2 := common.HexToHash("0xdef456")
	err = storage.UpdateBalance(ctx, addr, 2, delta2, txHash2)
	if err != nil {
		t.Fatalf("UpdateBalance() with negative delta failed: %v", err)
	}
	t.Logf("✅ Updated balance with %s wei", delta2.String())

	// 6. Verify balance is now 70 ETH
	balance2, err := storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() after second update failed: %v", err)
	}
	expected2 := new(big.Int).Mul(big.NewInt(70), big.NewInt(1e18))
	if balance2.Cmp(expected2) != 0 {
		t.Errorf("Balance after second update should be %s, got %s", expected2.String(), balance2.String())
	}
	t.Logf("✅ Balance after second update: %s wei (70 ETH)", balance2.String())

	// 7. Test GetBalanceHistory
	history, err := storage.GetBalanceHistory(ctx, addr, 0, 100, 10, 0)
	if err != nil {
		t.Fatalf("GetBalanceHistory() failed: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("Expected 2 balance snapshots, got %d", len(history))
	}
	t.Logf("✅ Balance history has %d snapshots", len(history))

	// 8. Verify history details
	if len(history) >= 1 {
		snapshot1 := history[0]
		t.Logf("   Snapshot 1: Block=%d, Balance=%s, Delta=%s, TxHash=%s",
			snapshot1.BlockNumber, snapshot1.Balance.String(), snapshot1.Delta.String(), snapshot1.TxHash.Hex())
	}
	if len(history) >= 2 {
		snapshot2 := history[1]
		t.Logf("   Snapshot 2: Block=%d, Balance=%s, Delta=%s, TxHash=%s",
			snapshot2.BlockNumber, snapshot2.Balance.String(), snapshot2.Delta.String(), snapshot2.TxHash.Hex())
	}

	// 9. Test SetBalance (direct set instead of delta)
	directBalance := new(big.Int).Mul(big.NewInt(200), big.NewInt(1e18))
	err = storage.SetBalance(ctx, addr, 3, directBalance)
	if err != nil {
		t.Fatalf("SetBalance() failed: %v", err)
	}
	t.Logf("✅ Set balance directly to %s wei", directBalance.String())

	// 10. Verify direct set worked
	balance3, err := storage.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		t.Fatalf("GetAddressBalance() after SetBalance failed: %v", err)
	}
	if balance3.Cmp(directBalance) != 0 {
		t.Errorf("Balance after SetBalance should be %s, got %s", directBalance.String(), balance3.String())
	}
	t.Logf("✅ Balance after SetBalance: %s wei (200 ETH)", balance3.String())

	t.Log("\n✅ All balance tracking tests passed!")
}

// TestInterfaceAssertion verifies that PebbleStorage implements required interfaces
func TestInterfaceAssertion(t *testing.T) {
	storageInterface, cleanup := setupTestStorage(t)
	defer cleanup()

	// Cast to *PebbleStorage
	storage, ok := storageInterface.(*PebbleStorage)
	if !ok {
		t.Fatal("setupTestStorage did not return *PebbleStorage")
	}

	// Test HistoricalWriter
	_, ok = interface{}(storage).(HistoricalWriter)
	if !ok {
		t.Error("PebbleStorage should implement HistoricalWriter")
	} else {
		t.Log("✅ PebbleStorage implements HistoricalWriter")
	}

	// Test HistoricalReader
	_, ok = interface{}(storage).(HistoricalReader)
	if !ok {
		t.Error("PebbleStorage should implement HistoricalReader")
	} else {
		t.Log("✅ PebbleStorage implements HistoricalReader")
	}

	// Test HistoricalStorage (composite interface)
	_, ok = interface{}(storage).(HistoricalStorage)
	if !ok {
		t.Error("PebbleStorage should implement HistoricalStorage")
	} else {
		t.Log("✅ PebbleStorage implements HistoricalStorage")
	}
}
