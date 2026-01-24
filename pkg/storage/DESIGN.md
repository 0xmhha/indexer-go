# Storage Layer Design

## Overview

Storage layer provides persistent storage for blockchain data using PebbleDB with RLP encoding.

## SOLID Principles Application

### Single Responsibility Principle (SRP)
Each interface has one reason to change:
- `Reader`: Read operations only
- `Writer`: Write operations only
- `Schema`: Key generation logic
- `Encoder`: RLP encoding/decoding logic

### Open/Closed Principle (OCP)
- Storage interface is open for extension (can add new storage backends)
- Closed for modification (existing code doesn't change when adding new backends)

### Liskov Substitution Principle (LSP)
- Any implementation of Storage can replace another
- PebbleStorage, MemoryStorage (for testing), etc.

### Interface Segregation Principle (ISP)
- Clients depend only on methods they use
- Reader interface for read-only clients
- Writer interface for write-only clients
- Storage interface combines both

### Dependency Inversion Principle (DIP)
- Depend on Storage interface, not concrete PebbleStorage
- Fetcher depends on Storage interface
- Easy to swap implementations for testing

## Architecture

```
┌─────────────────────────────────────┐
│         Storage Interface           │
│  (Reader + Writer + Metadata)       │
└─────────────────┬───────────────────┘
                  │
    ┌─────────────┼─────────────┐
    │             │             │
┌───▼────┐  ┌────▼─────┐  ┌───▼────┐
│ Pebble │  │  Memory  │  │ Future │
│Storage │  │ Storage  │  │Backends│
└────────┘  └──────────┘  └────────┘
```

## Interfaces

### Reader Interface
```go
type Reader interface {
    // Block operations
    GetLatestHeight(ctx context.Context) (uint64, error)
    GetBlock(ctx context.Context, height uint64) (*types.Block, error)
    GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)

    // Transaction operations
    GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *TxLocation, error)
    GetTransactionsByAddress(ctx context.Context, addr common.Address, opts *PaginationOptions) ([]*IndexedTransaction, error)

    // Receipt operations
    GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)

    // Batch operations
    GetBlocks(ctx context.Context, heights []uint64) ([]*types.Block, error)
}
```

### Writer Interface
```go
type Writer interface {
    // Metadata operations
    SetLatestHeight(ctx context.Context, height uint64) error

    // Block operations
    SetBlock(ctx context.Context, block *types.Block) error
    SetBlocks(ctx context.Context, blocks []*types.Block) error

    // Transaction operations
    SetTransaction(ctx context.Context, tx *types.Transaction, location *TxLocation) error

    // Receipt operations
    SetReceipt(ctx context.Context, receipt *types.Receipt) error

    // Index operations
    AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error
}
```

### Storage Interface
```go
type Storage interface {
    Reader
    Writer

    // Lifecycle
    Close() error

    // Batch operations
    NewBatch() Batch
}
```

### Batch Interface
```go
type Batch interface {
    Writer

    // Commit writes all batched operations atomically
    Commit() error

    // Reset clears the batch
    Reset()
}
```

## Key Schema

Keys follow a hierarchical structure:

```
/meta/lh                     → Latest height (uint64)
/data/blocks/{height}        → RLP-encoded block
/data/txs/{height}/{index}   → RLP-encoded transaction
/data/receipts/{txhash}      → RLP-encoded receipt
/index/txh/{txhash}          → Transaction location (height + index)
/index/addr/{address}/{seq}  → Transaction hash for address
```

### Schema Package
```go
package schema

// Key generators
func LatestHeightKey() []byte
func BlockKey(height uint64) []byte
func TransactionKey(height uint64, index uint64) []byte
func ReceiptKey(txHash common.Hash) []byte
func TransactionHashIndexKey(txHash common.Hash) []byte
func AddressTransactionKey(addr common.Address, seq uint64) []byte

// Key parsers
func ParseBlockKey(key []byte) (uint64, error)
func ParseTransactionKey(key []byte) (uint64, uint64, error)
```

## RLP Encoding

Use go-ethereum's RLP encoding for all data:

```go
package encoder

import (
    "github.com/ethereum/go-ethereum/rlp"
)

func EncodeBlock(block *types.Block) ([]byte, error)
func DecodeBlock(data []byte) (*types.Block, error)

func EncodeTransaction(tx *types.Transaction) ([]byte, error)
func DecodeTransaction(data []byte) (*types.Transaction, error)

func EncodeReceipt(receipt *types.Receipt) ([]byte, error)
func DecodeReceipt(data []byte) (*types.Receipt, error)

func EncodeUint64(n uint64) []byte
func DecodeUint64(data []byte) (uint64, error)

func EncodeTxLocation(loc *TxLocation) ([]byte, error)
func DecodeTxLocation(data []byte) (*TxLocation, error)
```

## Data Types

### TxLocation
```go
type TxLocation struct {
    BlockHeight uint64
    TxIndex     uint64
    BlockHash   common.Hash
}
```

### PaginationOptions
```go
type PaginationOptions struct {
    Limit  int
    Offset int
}
```

## PebbleDB Implementation

### Configuration
```go
type Config struct {
    Path string

    // PebbleDB options
    Cache           int // MB
    WriteBuffer     int // MB
    MaxOpenFiles    int
    CompactionStyle int
}
```

### PebbleStorage
```go
type PebbleStorage struct {
    db     *pebble.DB
    logger *zap.Logger
}

func NewPebbleStorage(cfg *Config) (*PebbleStorage, error)
func (s *PebbleStorage) Close() error
```

## Error Handling

### Custom Errors
```go
var (
    ErrNotFound      = errors.New("not found")
    ErrInvalidKey    = errors.New("invalid key")
    ErrInvalidData   = errors.New("invalid data")
    ErrClosed        = errors.New("storage closed")
    ErrBatchTooLarge = errors.New("batch too large")
)
```

### Error Wrapping
```go
if err != nil {
    return fmt.Errorf("failed to get block %d: %w", height, err)
}
```

## Testing Strategy

### Unit Tests
- Test each method independently
- Use table-driven tests for multiple scenarios
- Test error cases

### Integration Tests
- Test with real PebbleDB instance
- Test batch operations
- Test concurrent access
- Test recovery from failures

### Mock Storage
```go
type MockStorage struct {
    blocks      map[uint64]*types.Block
    transactions map[common.Hash]*types.Transaction
    receipts    map[common.Hash]*types.Receipt
}
```

## Performance Considerations

### Batch Operations
- Use batch writes for multiple operations
- Reduces write amplification
- Improves throughput

### Address Index
- Use sequence numbers for efficient pagination
- Store only transaction hashes (not full transactions)
- Paginate with limit/offset

### Memory Usage
- Set appropriate cache size
- Limit batch size
- Use iterators for large result sets

### Compression
- PebbleDB uses Snappy compression by default
- RLP encoding is space-efficient

## Extension Points

### Future Enhancements
1. **Caching Layer**: Add LRU cache in front of storage
2. **Sharding**: Support multiple database shards
3. **Archival**: Separate hot and cold data
4. **Pruning**: Remove old data to save space
5. **Replication**: Support read replicas

### Alternative Backends
- MemoryStorage (testing)
- LevelDB (compatibility)
- BadgerDB (alternative)
- PostgreSQL (SQL backend)

## Migration Path

When adding new storage backends:

1. Implement Storage interface
2. Add tests using same test suite
3. Add benchmark comparisons
4. Document performance characteristics
5. Provide migration tools

## Compatibility

### go-ethereum Compatibility
- Use native types.Block, types.Transaction, types.Receipt
- Use RLP encoding (compatible with geth)
- Can import/export data with geth tools

### Upgradability
- Version metadata in database
- Support schema migrations
- Backward compatibility for reads
