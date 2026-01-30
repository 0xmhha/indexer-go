package multichain

import (
	"errors"
	"testing"
)

func TestNewChainError(t *testing.T) {
	underlyingErr := errors.New("connection refused")
	chainErr := NewChainError("eth-mainnet", ErrClientInitFailed, underlyingErr)

	if chainErr == nil {
		t.Fatal("expected non-nil ChainError")
	}

	if chainErr.ChainID != "eth-mainnet" {
		t.Errorf("expected ChainID 'eth-mainnet', got '%s'", chainErr.ChainID)
	}

	if chainErr.Op != ErrClientInitFailed {
		t.Errorf("expected Op ErrClientInitFailed, got %v", chainErr.Op)
	}

	if chainErr.Err != underlyingErr {
		t.Errorf("expected underlying error, got %v", chainErr.Err)
	}
}

func TestChainError_Error(t *testing.T) {
	tests := []struct {
		name     string
		chainErr *ChainError
		expected string
	}{
		{
			name: "with underlying error",
			chainErr: &ChainError{
				ChainID: "eth",
				Op:      ErrClientInitFailed,
				Err:     errors.New("connection refused"),
			},
			expected: "chain eth: failed to initialize client: connection refused",
		},
		{
			name: "without underlying error",
			chainErr: &ChainError{
				ChainID: "polygon",
				Op:      ErrChainNotRunning,
				Err:     nil,
			},
			expected: "chain polygon: chain is not running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chainErr.Error()
			if result != tt.expected {
				t.Errorf("Error() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestChainError_Unwrap(t *testing.T) {
	underlyingErr := errors.New("timeout")
	chainErr := &ChainError{
		ChainID: "eth",
		Op:      ErrAdapterInitFailed,
		Err:     underlyingErr,
	}

	unwrapped := chainErr.Unwrap()
	if unwrapped != underlyingErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlyingErr)
	}
}

func TestChainError_Is(t *testing.T) {
	underlyingErr := errors.New("specific error")
	chainErr := &ChainError{
		ChainID: "eth",
		Op:      ErrClientInitFailed,
		Err:     underlyingErr,
	}

	// Should match the operation error
	if !chainErr.Is(ErrClientInitFailed) {
		t.Error("Is(ErrClientInitFailed) should return true")
	}

	// Should match the underlying error
	if !chainErr.Is(underlyingErr) {
		t.Error("Is(underlyingErr) should return true")
	}

	// Should not match unrelated error
	if chainErr.Is(ErrAdapterInitFailed) {
		t.Error("Is(ErrAdapterInitFailed) should return false")
	}
}

func TestChainError_ErrorsIs(t *testing.T) {
	underlyingErr := errors.New("connection timeout")
	chainErr := NewChainError("eth", ErrClientInitFailed, underlyingErr)

	// Test using errors.Is
	if !errors.Is(chainErr, ErrClientInitFailed) {
		t.Error("errors.Is should match ErrClientInitFailed")
	}

	if !errors.Is(chainErr, underlyingErr) {
		t.Error("errors.Is should match underlying error")
	}

	if errors.Is(chainErr, ErrChainNotFound) {
		t.Error("errors.Is should not match ErrChainNotFound")
	}
}
