package multichain

import (
	"errors"
	"fmt"
)

// Sentinel errors for the multichain package.
var (
	// Chain lifecycle errors
	ErrChainNotFound      = errors.New("chain not found")
	ErrChainAlreadyExists = errors.New("chain already exists")
	ErrChainAlreadyRunning = errors.New("chain is already running")
	ErrChainNotRunning    = errors.New("chain is not running")

	// Initialization errors
	ErrClientInitFailed   = errors.New("failed to initialize client")
	ErrAdapterInitFailed  = errors.New("failed to initialize adapter")
	ErrFetcherInitFailed  = errors.New("failed to initialize fetcher")
	ErrStorageInitFailed  = errors.New("failed to initialize storage")

	// Configuration errors
	ErrInvalidConfig     = errors.New("invalid configuration")
	ErrManagerNotEnabled = errors.New("multichain manager is not enabled")

	// Operation errors
	ErrOperationTimeout = errors.New("operation timed out")
	ErrShuttingDown     = errors.New("manager is shutting down")
)

// ChainError wraps an error with chain context.
type ChainError struct {
	ChainID string
	Op      error
	Err     error
}

// NewChainError creates a new chain error.
func NewChainError(chainID string, op error, err error) *ChainError {
	return &ChainError{
		ChainID: chainID,
		Op:      op,
		Err:     err,
	}
}

// Error implements the error interface.
func (e *ChainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("chain %s: %v: %v", e.ChainID, e.Op, e.Err)
	}
	return fmt.Sprintf("chain %s: %v", e.ChainID, e.Op)
}

// Unwrap returns the underlying error.
func (e *ChainError) Unwrap() error {
	return e.Err
}

// Is checks if the target error matches.
func (e *ChainError) Is(target error) bool {
	return errors.Is(e.Op, target) || errors.Is(e.Err, target)
}
