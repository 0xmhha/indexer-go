package watchlist

import (
	"errors"
	"testing"
)

func TestWatchlistError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *WatchlistError
		expected string
	}{
		{
			name: "error with all fields",
			err: &WatchlistError{
				Op:      "WatchAddress",
				ChainID: "chain-1",
				Address: "0x1234",
				Err:     ErrAddressAlreadyExists,
			},
			expected: "watchlist: WatchAddress [chain=chain-1, addr=0x1234]: address already being watched",
		},
		{
			name: "error with chain ID only",
			err: &WatchlistError{
				Op:      "ProcessBlock",
				ChainID: "chain-1",
				Err:     ErrServiceNotRunning,
			},
			expected: "watchlist: ProcessBlock [chain=chain-1]: watchlist service is not running",
		},
		{
			name: "error with address only",
			err: &WatchlistError{
				Op:      "GetWatchedAddress",
				Address: "0x5678",
				Err:     ErrAddressNotFound,
			},
			expected: "watchlist: GetWatchedAddress [addr=0x5678]: watched address not found",
		},
		{
			name: "error with no context",
			err: &WatchlistError{
				Op:  "Subscribe",
				Err: ErrInvalidFilter,
			},
			expected: "watchlist: Subscribe: invalid watch filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWatchlistError_Unwrap(t *testing.T) {
	underlyingErr := ErrAddressNotFound
	err := &WatchlistError{
		Op:  "GetWatchedAddress",
		Err: underlyingErr,
	}

	if unwrapped := err.Unwrap(); unwrapped != underlyingErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlyingErr)
	}

	// Test that errors.Is works
	if !errors.Is(err, ErrAddressNotFound) {
		t.Error("errors.Is should return true for the underlying error")
	}
}

func TestNewWatchlistError(t *testing.T) {
	err := NewWatchlistError("TestOp", ErrStorageError)

	if err.Op != "TestOp" {
		t.Errorf("Op = %q, want %q", err.Op, "TestOp")
	}

	if err.Err != ErrStorageError {
		t.Errorf("Err = %v, want %v", err.Err, ErrStorageError)
	}

	if err.ChainID != "" {
		t.Error("ChainID should be empty")
	}

	if err.Address != "" {
		t.Error("Address should be empty")
	}
}

func TestWatchlistError_WithChainID(t *testing.T) {
	err := NewWatchlistError("TestOp", ErrServiceNotRunning).WithChainID("chain-42")

	if err.ChainID != "chain-42" {
		t.Errorf("ChainID = %q, want %q", err.ChainID, "chain-42")
	}

	// Test fluent interface returns same pointer
	err2 := err.WithChainID("chain-99")
	if err != err2 {
		t.Error("WithChainID should return the same pointer")
	}
}

func TestWatchlistError_WithAddress(t *testing.T) {
	err := NewWatchlistError("TestOp", ErrAddressNotFound).WithAddress("0xabcd")

	if err.Address != "0xabcd" {
		t.Errorf("Address = %q, want %q", err.Address, "0xabcd")
	}

	// Test fluent interface returns same pointer
	err2 := err.WithAddress("0xefgh")
	if err != err2 {
		t.Error("WithAddress should return the same pointer")
	}
}

func TestWatchlistError_Chaining(t *testing.T) {
	err := NewWatchlistError("UnwatchAddress", ErrAddressNotFound).
		WithChainID("mainnet").
		WithAddress("0x1234567890")

	expected := "watchlist: UnwatchAddress [chain=mainnet, addr=0x1234567890]: watched address not found"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
}

func TestSentinelErrors(t *testing.T) {
	// Verify all sentinel errors are defined and unique
	sentinelErrors := []error{
		ErrAddressNotFound,
		ErrAddressAlreadyExists,
		ErrInvalidAddress,
		ErrSubscriberNotFound,
		ErrSubscriberAlreadyExists,
		ErrInvalidFilter,
		ErrNoFilterSet,
		ErrBloomFilterSizeMismatch,
		ErrServiceNotRunning,
		ErrServiceStopped,
		ErrStorageError,
	}

	// Check that none are nil
	for i, err := range sentinelErrors {
		if err == nil {
			t.Errorf("Sentinel error at index %d is nil", i)
		}
	}

	// Check that all are unique
	seen := make(map[string]bool)
	for _, err := range sentinelErrors {
		msg := err.Error()
		if seen[msg] {
			t.Errorf("Duplicate sentinel error message: %q", msg)
		}
		seen[msg] = true
	}
}
