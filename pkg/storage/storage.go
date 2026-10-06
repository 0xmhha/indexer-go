package storage

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Storage combines Reader and Writer interfaces
// Follows Dependency Inversion Principle - depend on abstraction
type Storage interface {
	Reader
	Writer
	LogReader
	LogWriter
	ABIReader
	ABIWriter
	SearchReader
	ContractVerificationReader
	ContractVerificationWriter
	FeeDelegationReader
	FeeDelegationWriter
	HistoricalReader
	HistoricalWriter
	TokenMetadataReader
	TokenMetadataWriter
	SetCodeIndexReader
	SetCodeIndexWriter
	UserOpIndexReader
	UserOpIndexWriter

	// SetTokenMetadataFetcher sets the fetcher for on-demand token metadata lookups
	SetTokenMetadataFetcher(fetcher TokenMetadataFetcher)

	// Close closes the storage and releases resources
	Close() error

	// NewBatch creates a new batch for atomic writes
	NewBatch() Batch

	// Compact triggers manual compaction (optional optimization)
	Compact(ctx context.Context, start, end []byte) error
}

// Batch provides atomic batch write operations
type Batch interface {
	Writer

	// Commit writes all batched operations atomically
	Commit() error

	// Reset clears all operations in the batch
	Reset()

	// Count returns the number of operations in the batch
	Count() int

	// Close releases batch resources without committing
	Close() error
}

// Config holds storage configuration
type Config struct {
	// Path to the database directory
	Path string

	// Cache size in MB (default: 128)
	Cache int

	// MaxOpenFiles is the maximum number of open files (default: 1000)
	MaxOpenFiles int

	// WriteBuffer size in MB (default: 64)
	WriteBuffer int

	// DisableWAL disables write-ahead log (not recommended)
	DisableWAL bool

	// ReadOnly opens the database in read-only mode
	ReadOnly bool

	// CompactionConcurrency for background compaction (default: 1)
	CompactionConcurrency int
}

// DefaultConfig returns a default configuration
func DefaultConfig(path string) *Config {
	return &Config{
		Path:                  path,
		Cache:                 128, // 128 MB
		MaxOpenFiles:          1000,
		WriteBuffer:           64, // 64 MB
		DisableWAL:            false,
		ReadOnly:              false,
		CompactionConcurrency: 1,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Path == "" {
		return errors.New("path cannot be empty")
	}
	if c.Cache < 0 {
		return errors.New("cache size cannot be negative")
	}
	if c.MaxOpenFiles < 0 {
		return errors.New("max open files cannot be negative")
	}
	if c.WriteBuffer < 0 {
		return errors.New("write buffer size cannot be negative")
	}
	if c.CompactionConcurrency < 1 {
		return errors.New("compaction concurrency must be at least 1")
	}
	return nil
}

// Stats holds storage statistics
type Stats struct {
	// LatestHeight is the latest indexed block height
	LatestHeight uint64

	// BlockCount is the number of blocks stored
	BlockCount uint64

	// TransactionCount is the number of transactions stored
	TransactionCount uint64

	// DiskUsage in bytes
	DiskUsage uint64

	// CompactionCount is the number of compactions performed
	CompactionCount uint64
}

// Aliases of the ports moved to pkg/core/port (refactoring plan R1-1);
// removed once every consumer uses the port package.
type (
	Reader    = port.Reader
	Writer    = port.Writer
	KVStore   = port.KVStore
	KV        = port.KV
	Cursor    = port.Cursor
	LogFilter = port.LogFilter
	LogReader = port.LogReader
	LogWriter = port.LogWriter
	ABIReader = port.ABIReader
	ABIWriter = port.ABIWriter
)

var (
	ErrNotFound       = port.ErrNotFound
	ErrInvalidKey     = port.ErrInvalidKey
	ErrInvalidData    = port.ErrInvalidData
	ErrClosed         = port.ErrClosed
	ErrBatchTooLarge  = port.ErrBatchTooLarge
	ErrReadOnly       = port.ErrReadOnly
	ErrInvalidReceipt = port.ErrInvalidReceipt
)
