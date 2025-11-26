package storage

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Common errors
var (
	// ErrNotFound is returned when a key is not found
	ErrNotFound = errors.New("not found")

	// ErrInvalidKey is returned when a key format is invalid
	ErrInvalidKey = errors.New("invalid key")

	// ErrInvalidData is returned when data cannot be decoded
	ErrInvalidData = errors.New("invalid data")

	// ErrClosed is returned when operating on a closed storage
	ErrClosed = errors.New("storage closed")

	// ErrBatchTooLarge is returned when a batch exceeds size limits
	ErrBatchTooLarge = errors.New("batch too large")

	// ErrReadOnly is returned when attempting to write to a read-only storage
	ErrReadOnly = errors.New("storage is read-only")
)

// Reader provides read-only access to blockchain data
// Following Interface Segregation Principle - clients depend only on read methods
type Reader interface {
	// GetLatestHeight returns the latest indexed block height
	GetLatestHeight(ctx context.Context) (uint64, error)

	// GetBlock returns a block by height
	GetBlock(ctx context.Context, height uint64) (*types.Block, error)

	// GetBlockByHash returns a block by hash
	GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)

	// GetTransaction returns a transaction and its location by hash
	GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *TxLocation, error)

	// GetTransactionsByAddress returns transactions for an address with pagination
	GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error)

	// GetReceipt returns a transaction receipt by hash
	GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)

	// GetReceipts returns multiple receipts by transaction hashes (batch operation)
	GetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error)

	// GetReceiptsByBlockHash returns all receipts for a block by block hash
	GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error)

	// GetReceiptsByBlockNumber returns all receipts for a block by block number
	GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error)

	// GetBlocks returns multiple blocks by height range (batch operation)
	GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error)

	// HasBlock checks if a block exists at given height
	HasBlock(ctx context.Context, height uint64) (bool, error)

	// HasTransaction checks if a transaction exists
	HasTransaction(ctx context.Context, hash common.Hash) (bool, error)
}

// Writer provides write access to blockchain data
// Following Interface Segregation Principle - separate write interface
type Writer interface {
	// SetLatestHeight updates the latest indexed block height
	SetLatestHeight(ctx context.Context, height uint64) error

	// SetBlock stores a block
	SetBlock(ctx context.Context, block *types.Block) error

	// SetTransaction stores a transaction with its location
	SetTransaction(ctx context.Context, tx *types.Transaction, location *TxLocation) error

	// SetReceipt stores a transaction receipt
	SetReceipt(ctx context.Context, receipt *types.Receipt) error

	// SetReceipts stores multiple receipts atomically (batch operation)
	SetReceipts(ctx context.Context, receipts []*types.Receipt) error

	// AddTransactionToAddressIndex adds a transaction to an address index
	AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error

	// SetBlocks stores multiple blocks atomically (batch operation)
	SetBlocks(ctx context.Context, blocks []*types.Block) error

	// DeleteBlock removes a block (for reorganization handling)
	DeleteBlock(ctx context.Context, height uint64) error
}

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
	SystemContractReader
	ContractVerificationReader
	ContractVerificationWriter

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

// LogFilter represents criteria for filtering event logs
type LogFilter struct {
	// FromBlock is the starting block number (inclusive)
	FromBlock uint64

	// ToBlock is the ending block number (inclusive)
	// Use 0 or latest block for open-ended range
	ToBlock uint64

	// Addresses is a list of contract addresses to filter by
	// Empty list means all addresses
	Addresses []common.Address

	// Topics is a list of topic filters
	// Each position can have multiple options (OR logic)
	// Different positions use AND logic
	// nil in a position means "any value"
	Topics [][]common.Hash
}

// LogReader provides read access to event logs
type LogReader interface {
	// GetLogs returns logs matching the given filter
	GetLogs(ctx context.Context, filter *LogFilter) ([]*types.Log, error)

	// GetLogsByBlock returns all logs in a specific block
	GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error)

	// GetLogsByAddress returns logs emitted by a specific contract
	GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error)

	// GetLogsByTopic returns logs with a specific topic at a specific position
	GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error)
}

// LogWriter provides write access to event logs
type LogWriter interface {
	// IndexLogs indexes logs from a receipt
	IndexLogs(ctx context.Context, logs []*types.Log) error

	// IndexLog indexes a single log
	IndexLog(ctx context.Context, log *types.Log) error
}

// ABIReader provides read access to contract ABIs
type ABIReader interface {
	// GetABI returns the ABI for a contract
	GetABI(ctx context.Context, address common.Address) ([]byte, error)

	// HasABI checks if an ABI exists for a contract
	HasABI(ctx context.Context, address common.Address) (bool, error)

	// ListABIs returns all contract addresses that have ABIs
	ListABIs(ctx context.Context) ([]common.Address, error)
}

// ABIWriter provides write access to contract ABIs
type ABIWriter interface {
	// SetABI stores an ABI for a contract
	SetABI(ctx context.Context, address common.Address, abiJSON []byte) error

	// DeleteABI removes an ABI for a contract
	DeleteABI(ctx context.Context, address common.Address) error
}
