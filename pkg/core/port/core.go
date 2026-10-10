package port

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
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

	// ErrInvalidReceipt is returned when a receipt fails validation
	ErrInvalidReceipt = errors.New("invalid receipt")
)

// Reader provides read-only access to blockchain data
// Following Interface Segregation Principle - clients depend only on read methods
type Reader interface {
	BlockReader

	// GetLatestHeight returns the latest indexed block height
	GetLatestHeight(ctx context.Context) (uint64, error)

	// GetTransactions returns multiple transactions and their locations by
	// hash (batch operation). A hash that cannot be read leaves nil entries;
	// the first such error is returned with the partial result.
	GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*TxLocation, error)

	// GetTransactionsByAddress returns one page of the transactions indexed
	// for an address (AddTransactionToAddressIndex), in the order they were
	// indexed, and the cursor of the next page (see Page).
	GetTransactionsByAddress(ctx context.Context, addr common.Address, page Page) ([]common.Hash, string, error)

	// GetReceipts returns multiple receipts by transaction hashes (batch
	// operation), with the same partial-result rule as GetTransactions.
	GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error)

	// GetReceiptsByBlockHash returns all receipts for a block by block hash
	GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error)

	// GetReceiptsByBlockNumber returns the stored receipts of a block, in
	// transaction order; missing receipts are skipped.
	GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error)

	// HasBlock checks if a block exists at given height
	HasBlock(ctx context.Context, height uint64) (bool, error)

	// HasTransaction checks if a transaction exists
	HasTransaction(ctx context.Context, hash common.Hash) (bool, error)

	// HasReceipt checks if a receipt exists for a transaction
	HasReceipt(ctx context.Context, hash common.Hash) (bool, error)

	// GetMissingReceipts returns transaction hashes that have no stored receipts for a block
	GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error)
}

// AddressTransactionsNewestFirst reads the address index in reverse.
type AddressTransactionsNewestFirst interface {
	// GetTransactionsByAddressNewestFirst is GetTransactionsByAddress in
	// reverse: the most recently indexed transaction first.
	GetTransactionsByAddressNewestFirst(ctx context.Context, addr common.Address, page Page) ([]common.Hash, string, error)
}

// Writer provides write access to blockchain data
// Following Interface Segregation Principle - separate write interface
type Writer interface {
	BlockWriter

	// SetLatestHeight updates the latest indexed block height
	SetLatestHeight(ctx context.Context, height uint64) error

	// AddTransactionToAddressIndex adds a transaction to an address index
	AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error

	// DeleteBlock removes a block (for reorganization handling)
	DeleteBlock(ctx context.Context, height uint64) error
}

// KVStore provides raw key-value storage operations
// Used by modules that need direct storage access (e.g., watchlist, resilience)
type KVStore interface {
	// Put stores a value with the given key
	Put(ctx context.Context, key, value []byte) error

	// Get retrieves a value by key
	// Returns ErrNotFound if key doesn't exist
	Get(ctx context.Context, key []byte) ([]byte, error)

	// Delete removes a key-value pair
	Delete(ctx context.Context, key []byte) error

	// Iterate iterates over keys with the given prefix
	// The callback receives key and value for each matching entry
	// Return false from the callback to stop iteration
	Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error

	// Has checks if a key exists
	Has(ctx context.Context, key []byte) (bool, error)
}

// KV is the storage port of code outside this package that keeps its own
// data, such as chain-specific stores under pkg/chains/<chain>/. Reads and
// writes use the block transaction bound to ctx, so they commit, and roll
// back on a reorganization, together with the block. Owners register
// their key prefixes with RegisterKeyspace.
type KV interface {
	KVStore
	// Scan visits the keys in [lower, upper) in key order, or in reverse
	// order when reverse is set, until fn returns false. A nil upper means
	// no upper bound. Keys and values passed to fn are copies.
	Scan(ctx context.Context, lower, upper []byte, reverse bool, fn func(key, value []byte) bool) error
	// NewCursor returns a cursor over the keys in [lower, upper) (a nil
	// upper means no upper bound), bound to the block transaction in ctx
	// like the other methods. The caller must close it.
	NewCursor(ctx context.Context, lower, upper []byte) (Cursor, error)
}

// Cursor is a cursor over stored keys. Key and Value are valid until the
// cursor moves.
type Cursor interface {
	First() bool
	Last() bool
	Next() bool
	Prev() bool
	Valid() bool
	Key() []byte
	Value() []byte
	Error() error
	Close() error
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

// Matches reports whether log satisfies the address and topic criteria
// (the block range is not checked).
func (f *LogFilter) Matches(log *model.Log) bool {
	if len(f.Addresses) > 0 {
		found := false
		for _, a := range f.Addresses {
			if a == log.Address {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for i, options := range f.Topics {
		if len(options) == 0 {
			continue
		}
		if i >= len(log.Topics) {
			return false
		}
		found := false
		for _, o := range options {
			if log.Topics[i] == o {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// LogReader provides read access to event logs
type LogReader interface {
	// GetLogs returns logs matching the given filter
	GetLogs(ctx context.Context, filter *LogFilter) ([]*model.Log, error)

	// GetLogsByBlock returns all logs in a specific block
	GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*model.Log, error)

	// GetLogsByAddress returns logs emitted by a specific contract
	GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*model.Log, error)

	// GetLogsByTopic returns logs with a specific topic at a specific position
	GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*model.Log, error)
}

// LogWriter provides write access to event logs
type LogWriter interface {
	// IndexLogs indexes logs from a receipt
	IndexLogs(ctx context.Context, logs []*model.Log) error

	// IndexLog indexes a single log
	IndexLog(ctx context.Context, log *model.Log) error
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
