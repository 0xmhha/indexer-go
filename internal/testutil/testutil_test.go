package testutil

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestNewTestLogger tests creating a test logger
func TestNewTestLogger(t *testing.T) {
	logger := NewTestLogger(t)
	if logger == nil {
		t.Fatal("NewTestLogger() returned nil")
	}
}

// TestNewTestBlock tests creating a test block
func TestNewTestBlock(t *testing.T) {
	block := NewTestBlock(1)
	if block == nil {
		t.Fatal("NewTestBlock() returned nil")
	}
	if block.Number().Uint64() != 1 {
		t.Errorf("Block number = %d, want 1", block.Number().Uint64())
	}
}

// TestNewTestBlockWithTransactions tests creating a block with transaction metadata
func TestNewTestBlockWithTransactions(t *testing.T) {
	txCount := 5
	block := NewTestBlockWithTransactions(2, txCount)
	if block == nil {
		t.Fatal("NewTestBlockWithTransactions() returned nil")
	}
	if block.Number().Uint64() != 2 {
		t.Errorf("Block number = %d, want 2", block.Number().Uint64())
	}
	// Note: The function creates a simple block for testing
	// Actual transaction data would require more complex setup
	expectedGasUsed := uint64(21000 * txCount)
	if block.GasUsed() != expectedGasUsed {
		t.Errorf("Gas used = %d, want %d", block.GasUsed(), expectedGasUsed)
	}
}

// TestNewTestReceipt tests creating a test receipt
func TestNewTestReceipt(t *testing.T) {
	txHash := common.HexToHash("0x1234")
	receipt := NewTestReceipt(txHash, 100, 1)
	if receipt == nil {
		t.Fatal("NewTestReceipt() returned nil")
	}
	if receipt.TxHash != txHash {
		t.Errorf("Receipt TxHash = %s, want %s", receipt.TxHash, txHash)
	}
	if receipt.BlockNumber.Uint64() != 100 {
		t.Errorf("Receipt BlockNumber = %d, want 100", receipt.BlockNumber.Uint64())
	}
	if receipt.Status != 1 {
		t.Errorf("Receipt Status = %d, want 1", receipt.Status)
	}
}

// TestAssertNoError tests the AssertNoError helper
func TestAssertNoError(t *testing.T) {
	// Should not panic with nil error
	AssertNoError(t, nil)
}

// TestAssertEqual tests the AssertEqual helper
func TestAssertEqual(t *testing.T) {
	// Should not fail with equal values
	AssertEqual(t, 1, 1)
	AssertEqual(t, "test", "test")
}

// TestAssertNotEqual tests the AssertNotEqual helper
func TestAssertNotEqual(t *testing.T) {
	// Should not fail with different values
	AssertNotEqual(t, 1, 2)
	AssertNotEqual(t, "test", "other")
}

// TestAssertTrue tests the AssertTrue helper
func TestAssertTrue(t *testing.T) {
	// Should not fail with true condition
	AssertTrue(t, true)
	a, b := 1, 1
	AssertTrue(t, a == b)
}

// TestAssertFalse tests the AssertFalse helper
func TestAssertFalse(t *testing.T) {
	// Should not fail with false condition
	AssertFalse(t, false)
	AssertFalse(t, 1 == 2)
}

// TestAssertNil tests the AssertNil helper
func TestAssertNil(t *testing.T) {
	// Should not fail with nil value
	var nilValue *int
	AssertNil(t, nil)
	AssertNil(t, nilValue)
}

// TestAssertNotNil tests the AssertNotNil helper
func TestAssertNotNil(t *testing.T) {
	// Should not fail with non-nil value
	value := 1
	AssertNotNil(t, &value)
	AssertNotNil(t, "test")
}
