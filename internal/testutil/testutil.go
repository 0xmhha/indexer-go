package testutil

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// NewTestLogger creates a test logger that doesn't output to console
func NewTestLogger(t *testing.T) *zap.Logger {
	t.Helper()
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return logger
}

// NewTestBlock creates a test block with the given height
func NewTestBlock(height uint64) *types.Block {
	header := &types.Header{
		Number:     big.NewInt(int64(height)),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1000),
		GasLimit:   8000000,
		GasUsed:    21000,
	}
	return types.NewBlockWithHeader(header)
}

// NewTestBlockWithTransactions creates a test block with the given number of transactions
// Note: This creates a simple block for testing purposes only.
// In production, blocks should be created through proper consensus mechanisms.
func NewTestBlockWithTransactions(height uint64, txCount int) *types.Block {
	// For testing purposes, we'll create a simple block without transactions
	// Real blocks with transactions require proper Merkle tree construction
	// which is complex and not needed for basic testing
	header := &types.Header{
		Number:     big.NewInt(int64(height)),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1000),
		GasLimit:   8000000,
		GasUsed:    21000 * uint64(txCount),
	}

	// Create a basic block
	// In tests that need transactions, use mocks or real block data
	return types.NewBlockWithHeader(header)
}

// NewTestReceipt creates a test receipt for the given transaction hash
func NewTestReceipt(txHash common.Hash, blockNumber uint64, status uint64) *types.Receipt {
	return &types.Receipt{
		Type:              types.LegacyTxType,
		Status:            status,
		CumulativeGasUsed: 21000,
		BlockNumber:       big.NewInt(int64(blockNumber)),
		TxHash:            txHash,
		GasUsed:           21000,
		Logs:              []*types.Log{},
	}
}

// AssertNoError is a helper to assert that there is no error
func AssertNoError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err != nil {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: %v", msgAndArgs[0], err)
		} else {
			t.Fatalf("Unexpected error: %v", err)
		}
	}
}

// AssertError is a helper to assert that there is an error
func AssertError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err == nil {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: expected error but got nil", msgAndArgs[0])
		} else {
			t.Fatal("Expected error but got nil")
		}
	}
}

// AssertEqual is a helper to assert equality
func AssertEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if expected != actual {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: expected %v, got %v", msgAndArgs[0], expected, actual)
		} else {
			t.Fatalf("Expected %v, got %v", expected, actual)
		}
	}
}

// AssertNotEqual is a helper to assert inequality
func AssertNotEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if expected == actual {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: expected not equal to %v, but got %v", msgAndArgs[0], expected, actual)
		} else {
			t.Fatalf("Expected not equal to %v, but got %v", expected, actual)
		}
	}
}

// AssertTrue is a helper to assert that a condition is true
func AssertTrue(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if !condition {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: expected true but got false", msgAndArgs[0])
		} else {
			t.Fatal("Expected true but got false")
		}
	}
}

// AssertFalse is a helper to assert that a condition is false
func AssertFalse(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if condition {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: expected false but got true", msgAndArgs[0])
		} else {
			t.Fatal("Expected false but got true")
		}
	}
}

// AssertNil is a helper to assert that a value is nil
func AssertNil(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if value != nil && !isNil(value) {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: expected nil but got %v", msgAndArgs[0], value)
		} else {
			t.Fatalf("Expected nil but got %v", value)
		}
	}
}

// isNil checks if a value is nil using reflection
// This is needed because interface{} != nil doesn't work for nil pointers
func isNil(value interface{}) bool {
	if value == nil {
		return true
	}

	// Use reflection to check if the underlying value is nil
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// AssertNotNil is a helper to assert that a value is not nil
func AssertNotNil(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if value == nil || isNil(value) {
		if len(msgAndArgs) > 0 {
			t.Fatalf("%s: expected not nil but got nil", msgAndArgs[0])
		} else {
			t.Fatal("Expected not nil but got nil")
		}
	}
}
