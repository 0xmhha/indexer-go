package watchlist

import (
	"errors"
	"fmt"
)

// Sentinel errors
var (
	// Address errors
	ErrAddressNotFound      = errors.New("watched address not found")
	ErrAddressAlreadyExists = errors.New("address already being watched")
	ErrInvalidAddress       = errors.New("invalid address")

	// Subscriber errors
	ErrSubscriberNotFound      = errors.New("subscriber not found")
	ErrSubscriberAlreadyExists = errors.New("subscriber already exists")

	// Filter errors
	ErrInvalidFilter = errors.New("invalid watch filter")
	ErrNoFilterSet   = errors.New("at least one filter option must be enabled")

	// Bloom filter errors
	ErrBloomFilterSizeMismatch = errors.New("bloom filter size mismatch")

	// Service errors
	ErrServiceNotRunning = errors.New("watchlist service is not running")
	ErrServiceStopped    = errors.New("watchlist service has been stopped")

	// Storage errors
	ErrStorageError = errors.New("storage operation failed")
)

// WatchlistError is a domain-specific error with context
type WatchlistError struct {
	Op      string // Operation that failed
	ChainID string // Chain ID (if applicable)
	Address string // Address (if applicable)
	Err     error  // Underlying error
}

func (e *WatchlistError) Error() string {
	if e.Address != "" && e.ChainID != "" {
		return fmt.Sprintf("watchlist: %s [chain=%s, addr=%s]: %v", e.Op, e.ChainID, e.Address, e.Err)
	}
	if e.ChainID != "" {
		return fmt.Sprintf("watchlist: %s [chain=%s]: %v", e.Op, e.ChainID, e.Err)
	}
	if e.Address != "" {
		return fmt.Sprintf("watchlist: %s [addr=%s]: %v", e.Op, e.Address, e.Err)
	}
	return fmt.Sprintf("watchlist: %s: %v", e.Op, e.Err)
}

func (e *WatchlistError) Unwrap() error {
	return e.Err
}

// NewWatchlistError creates a new watchlist error
func NewWatchlistError(op string, err error) *WatchlistError {
	return &WatchlistError{Op: op, Err: err}
}

// WithChainID adds chain ID to the error
func (e *WatchlistError) WithChainID(chainID string) *WatchlistError {
	e.ChainID = chainID
	return e
}

// WithAddress adds address to the error
func (e *WatchlistError) WithAddress(addr string) *WatchlistError {
	e.Address = addr
	return e
}
