# Historical Data API Design

Design reference for the historical data APIs, focusing on efficient indexing, query semantics, and rollout considerations.

**Last Updated**: 2025-11-20

---

## Table of Contents

- [Overview](#overview)
- [Current State](#current-state)
- [Requirements](#requirements)
- [API Design](#api-design)
- [Storage Schema](#storage-schema)
- [Implementation Plan](#implementation-plan)
- [Performance Considerations](#performance-considerations)
- [Testing Strategy](#testing-strategy)

---

## Overview

### Goals

The Historical Data API provides efficient access to historical blockchain data with the following objectives:

1. **Historical Block Queries**: Query blocks by range, time period, and filters
2. **Transaction History**: Retrieve transaction history for addresses with flexible filtering
3. **Balance Tracking**: Track address balances over time with snapshots
4. **Performance**: Sub-second response times for common queries
5. **Scalability**: Support for millions of blocks and transactions

### Use Cases

- **Block Explorers**: Display historical blocks and transactions
- **Analytics Platforms**: Analyze historical trends and patterns
- **Auditing Tools**: Track address activity and balance changes
- **Data Export**: Export historical data for external analysis
- **Compliance**: Generate reports for regulatory requirements

---

## Current State

### Existing Storage Layer

Our storage layer (`storage/pebble.go`) already implements core historical data access:

#### Implemented Features

1. **Block Range Queries**
   ```go
   GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error)
   ```
   - Supports querying blocks by height range
   - Returns all blocks in the specified range
   - Skips missing blocks gracefully

2. **Transaction History by Address**
   ```go
   GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error)
   ```
   - Indexed by address with sequence numbers
   - Supports pagination (limit, offset)
   - Efficient prefix scanning

3. **Individual Lookups**
   ```go
   GetBlock(ctx context.Context, height uint64) (*types.Block, error)
   GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
   GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *TxLocation, error)
   GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
   GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error)
   ```

#### Missing Features

1. **Time-Based Queries**: No timestamp index for blocks
2. **Balance Tracking**: No address balance history
3. **Transaction Filtering**: Limited filter options (no value range, gas filter, etc.)
4. **Aggregation**: No built-in statistics or analytics
5. **Reverse Iteration**: No support for newest-first queries

### Storage Schema

```
/meta/lh                                    → Latest height (uint64)
/data/blocks/{height}                       → Block data (RLP encoded)
/data/txs/{height}/{index}                  → Transaction data (RLP encoded)
/data/receipts/{txhash}                     → Receipt data (RLP encoded)
/index/txh/{txhash}                         → TxLocation (height, index)
/index/blockh/{blockhash}                   → Block height (uint64)
/index/addr/{address}/{seq}                 → Transaction hash for address
```

---

## Requirements

### Functional Requirements

#### FR1: Historical Block Range API
- Query blocks by height range with pagination
- Support both GraphQL and JSON-RPC endpoints
- Return block metadata (hash, number, timestamp, transactions count)
- Configurable page size (default: 100, max: 1000)

#### FR2: Enhanced Transaction History
- Query transactions by address with filters:
  - Time range (block number range)
  - Transaction type (to/from/internal)
  - Value range (min/max)
  - Status (success/failed)
- Support pagination and sorting (asc/desc by block number)
- Return full transaction details with receipts

#### FR3: Address Balance Tracking
- Track balance changes for addresses
- Support balance snapshots at specific block heights
- Provide balance history with pagination
- Calculate balance at any historical block

#### FR4: Block Timestamp Index
- Index blocks by timestamp for time-based queries
- Support querying blocks by time range
- Efficient binary search for timestamp lookup

### Non-Functional Requirements

#### NFR1: Performance
- Block range queries: < 500ms for 100 blocks
- Transaction history: < 200ms for 100 transactions
- Balance lookup: < 100ms for single query
- Timestamp lookup: < 50ms for binary search

#### NFR2: Storage Efficiency
- Use existing storage format (no breaking changes)
- Incremental indexing during block processing
- Minimal overhead per block (<1% increase)
- Compress historical data when appropriate

#### NFR3: Scalability
- Support 10M+ blocks
- Support 100M+ transactions
- Handle 1000+ concurrent queries
- Graceful degradation under load

---

## API Design

### GraphQL API

#### Query: Block Range

```graphql
type Query {
  """
  Get blocks within a height range
  """
  blockRange(
    fromBlock: BigInt!
    toBlock: BigInt!
    limit: Int = 100
    offset: Int = 0
  ): BlockConnection!

  """
  Get blocks within a time range
  """
  blocksByTime(
    fromTime: BigInt!
    toTime: BigInt!
    limit: Int = 100
    offset: Int = 0
  ): BlockConnection!
}

type BlockConnection {
  edges: [BlockEdge!]!
  pageInfo: PageInfo!
  totalCount: Int!
}

type BlockEdge {
  node: BlockSummary!
  cursor: String!
}

type BlockSummary {
  number: BigInt!
  hash: Bytes32!
  parentHash: Bytes32!
  timestamp: BigInt!
  transactionCount: Int!
  gasUsed: BigInt!
  gasLimit: BigInt!
  miner: Address!
  difficulty: BigInt!
  size: BigInt!
}

type PageInfo {
  hasNextPage: Boolean!
  hasPreviousPage: Boolean!
  startCursor: String
  endCursor: String
}
```

#### Query: Transaction History

```graphql
type Query {
  """
  Get transaction history for an address
  """
  transactionHistory(
    address: Address!
    filter: TransactionFilter
    limit: Int = 100
    offset: Int = 0
    orderBy: TransactionOrder = BLOCK_DESC
  ): TransactionConnection!
}

input TransactionFilter {
  fromBlock: BigInt
  toBlock: BigInt
  minValue: BigInt
  maxValue: BigInt
  type: TransactionType
  status: TransactionStatus
}

enum TransactionType {
  ALL
  SENT
  RECEIVED
}

enum TransactionStatus {
  ALL
  SUCCESS
  FAILED
}

enum TransactionOrder {
  BLOCK_ASC
  BLOCK_DESC
  VALUE_ASC
  VALUE_DESC
}

type TransactionConnection {
  edges: [TransactionEdge!]!
  pageInfo: PageInfo!
  totalCount: Int!
}

type TransactionEdge {
  node: TransactionWithReceipt!
  cursor: String!
}

type TransactionWithReceipt {
  transaction: Transaction!
  receipt: Receipt!
  block: BlockSummary!
}
```

#### Query: Address Balance

```graphql
type Query {
  """
  Get current balance for an address
  """
  addressBalance(
    address: Address!
    blockNumber: BigInt
  ): Balance!

  """
  Get balance history for an address
  """
  balanceHistory(
    address: Address!
    fromBlock: BigInt
    toBlock: BigInt
    limit: Int = 100
    offset: Int = 0
  ): BalanceConnection!
}

type Balance {
  address: Address!
  balance: BigInt!
  blockNumber: BigInt!
  blockHash: Bytes32!
  timestamp: BigInt!
}

type BalanceConnection {
  edges: [BalanceEdge!]!
  pageInfo: PageInfo!
  totalCount: Int!
}

type BalanceEdge {
  node: BalanceSnapshot!
  cursor: String!
}

type BalanceSnapshot {
  blockNumber: BigInt!
  balance: BigInt!
  delta: BigInt!
  transaction: Bytes32
  timestamp: BigInt!
}
```

### JSON-RPC API

#### Method: `getBlockRange`

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "getBlockRange",
  "params": {
    "fromBlock": 1000,
    "toBlock": 1100,
    "limit": 100,
    "offset": 0
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "blocks": [
      {
        "number": "0x3e8",
        "hash": "0x...",
        "timestamp": "0x...",
        "transactionCount": 5,
        "gasUsed": "0x...",
        // ... other fields
      }
    ],
    "pageInfo": {
      "hasNextPage": true,
      "totalCount": 101
    }
  }
}
```

#### Method: `getTransactionHistory`

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "getTransactionHistory",
  "params": {
    "address": "0x...",
    "filter": {
      "fromBlock": 1000,
      "toBlock": 2000,
      "type": "SENT",
      "status": "SUCCESS"
    },
    "limit": 50,
    "offset": 0,
    "orderBy": "BLOCK_DESC"
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "transactions": [
      {
        "hash": "0x...",
        "from": "0x...",
        "to": "0x...",
        "value": "0x...",
        "blockNumber": "0x...",
        "status": 1,
        // ... other fields
      }
    ],
    "pageInfo": {
      "hasNextPage": false,
      "totalCount": 25
    }
  }
}
```

#### Method: `getAddressBalance`

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "getAddressBalance",
  "params": {
    "address": "0x...",
    "blockNumber": 1000  // optional, defaults to latest
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "address": "0x...",
    "balance": "0x...",
    "blockNumber": "0x3e8",
    "blockHash": "0x...",
    "timestamp": "0x..."
  }
}
```

#### Method: `getBalanceHistory`

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "getBalanceHistory",
  "params": {
    "address": "0x...",
    "fromBlock": 1000,
    "toBlock": 2000,
    "limit": 100,
    "offset": 0
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "history": [
      {
        "blockNumber": "0x7d0",
        "balance": "0x...",
        "delta": "0x...",
        "transaction": "0x...",
        "timestamp": "0x..."
      }
    ],
    "pageInfo": {
      "hasNextPage": true,
      "totalCount": 250
    }
  }
}
```

---

## Storage Schema

### New Indexes

#### 1. Block Timestamp Index

```
/index/time/{timestamp}/{height}  → Block height (for timestamp-based queries)
```

- **Purpose**: Enable time-range queries
- **Format**: Timestamp (Unix seconds) + Height for uniqueness
- **Storage**: 8 bytes (timestamp) + 8 bytes (height) = 16 bytes per block
- **Overhead**: ~16 bytes per block (~1.6 MB per 100K blocks)

#### 2. Address Balance Index

```
/index/balance/{address}/{blockNumber}  → Balance (big.Int)
```

- **Purpose**: Track balance at specific blocks
- **Format**: Address + Block Number → Balance
- **Strategy**: Store only when balance changes
- **Overhead**: Variable (depends on transaction frequency)

Alternative (more efficient):

```
/index/balance/{address}/latest         → Current balance
/index/balance/{address}/history/{seq}  → (blockNumber, delta, txHash)
```

- **Benefits**: Reduces storage, faster lookups
- **Trade-off**: Requires calculation for historical balance

#### 3. Enhanced Address Transaction Index (Optional)

```
/index/addr/{address}/sent/{seq}      → Transaction hash (sent)
/index/addr/{address}/received/{seq}  → Transaction hash (received)
```

- **Purpose**: Separate sent vs received transactions
- **Overhead**: 2x current index size
- **Alternative**: Use existing index with transaction type lookup

---

## Implementation Plan

### Core Storage Methods (Week 1)

**Files**: `storage/historical.go`, `storage/historical_test.go`

#### Tasks

1. **Block Timestamp Indexing**
   ```go
   // Add to Writer interface
   SetBlockWithTimestamp(ctx context.Context, block *types.Block) error

   // Add to Reader interface
   GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error)
   GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error)
   ```

2. **Address Balance Tracking**
   ```go
   // Balance tracking methods
   GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error)
   GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]BalanceSnapshot, error)
   UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error
   ```

3. **Enhanced Transaction Queries**
   ```go
   // Transaction filtering
   type TxFilter struct {
       FromBlock  uint64
       ToBlock    uint64
       MinValue   *big.Int
       MaxValue   *big.Int
       TxType     TxType  // Sent/Received/All
       Status     bool    // Success/Failed
   }

   GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *TxFilter, limit, offset int) ([]*TransactionWithReceipt, error)
   ```

#### Testing

- Unit tests for each method (90%+ coverage)
- Integration tests with real block data
- Performance benchmarks

### GraphQL API (Week 1-2)

**Files**: `api/graphql/schema.graphqls`, `api/graphql/resolver.go`

#### Tasks

1. Update GraphQL schema with new queries
2. Implement resolvers for:
   - `blockRange`
   - `blocksByTime`
   - `transactionHistory`
   - `addressBalance`
   - `balanceHistory`
3. Add pagination helpers
4. Add response caching

#### Testing

- GraphQL query tests
- Integration tests with storage layer
- Performance tests for large result sets

### JSON-RPC API (Week 2)

**Files**: `api/jsonrpc/handler.go`, `api/jsonrpc/handler_test.go`

#### Tasks

1. Implement new JSON-RPC methods:
   - `getBlockRange`
   - `getTransactionHistory`
   - `getAddressBalance`
   - `getBalanceHistory`
2. Add input validation
3. Add rate limiting
4. Add response pagination

#### Testing

- JSON-RPC method tests
- Error handling tests
- Rate limiting tests

### Fetcher Integration (Week 2)

**Files**: `fetch/fetcher.go`

#### Tasks

1. Update fetcher to build indexes during sync:
   - Timestamp index
   - Balance updates
2. Add balance calculation logic
3. Ensure atomic updates with block storage
4. Add metrics for indexing performance

#### Testing

- Integration tests with fetcher
- Balance accuracy verification
- Performance benchmarks

### Documentation & Examples (Week 2)

**Files**: `docs/HISTORICAL_API.md`, `README.md`

#### Tasks

1. Complete API documentation
2. Add usage examples for each endpoint
3. Performance tuning guide
4. Migration guide (if schema changes needed)

---

## Performance Considerations

### Optimization Strategies

#### 1. Indexing Strategy

- **Lazy Indexing**: Build indexes on-demand for rarely accessed data
- **Incremental Updates**: Update indexes during block processing
- **Batch Operations**: Group multiple index updates in single transaction

#### 2. Caching

- **Block Metadata Cache**: Cache recent block summaries (LRU, 10K blocks)
- **Balance Cache**: Cache latest balances for active addresses (LRU, 100K addresses)
- **Query Result Cache**: Cache common queries (TTL: 60s)

#### 3. Query Optimization

- **Limit Result Sets**: Enforce max page size (1000 items)
- **Cursor-Based Pagination**: More efficient than offset-based
- **Parallel Fetching**: Fetch blocks and transactions in parallel
- **Early Termination**: Stop scanning when limit reached

#### 4. Storage Optimization

- **Sparse Balance History**: Store only balance changes, not every block
- **Compression**: Use PebbleDB compression for historical data
- **Compaction**: Periodic compaction to reclaim space

### Performance Targets

| Operation | Target | Explanation |
|-----------|--------|-------------|
| Block range (100 blocks) | <500ms | Sequential block reads |
| Tx history (100 txs) | <200ms | Index scan + lookup |
| Balance lookup | <100ms | Single index read + calculation |
| Balance history (100) | <300ms | Index scan + calculation |
| Timestamp lookup | <50ms | Binary search + index read |

### Benchmarking Plan

```go
BenchmarkGetBlockRange100         // 100 blocks
BenchmarkGetBlockRange1000        // 1000 blocks
BenchmarkGetTxHistory100          // 100 transactions
BenchmarkGetTxHistoryFiltered100  // 100 with filters
BenchmarkGetBalance               // Single balance lookup
BenchmarkGetBalanceHistory100     // 100 balance snapshots
BenchmarkGetBlockByTimestamp      // Timestamp-based lookup
```

---

## Testing Strategy

### Unit Tests

1. **Storage Layer** (`storage/historical_test.go`)
   - Test each storage method individually
   - Edge cases (empty results, invalid inputs)
   - Error handling
   - **Target Coverage**: 90%+

2. **API Layer** (`api/graphql/*_test.go`, `api/jsonrpc/*_test.go`)
   - Test each resolver/handler
   - Input validation
   - Pagination logic
   - **Target Coverage**: 85%+

### Integration Tests

1. **End-to-End Queries** (`test/integration/historical_test.go`)
   - Full query flow (GraphQL → Storage)
   - Multi-block scenarios
   - Address with many transactions
   - Balance changes over time

2. **Fetcher Integration** (`fetch/fetcher_test.go`)
   - Verify indexes built during sync
   - Verify balance accuracy
   - Handle edge cases (reorganization)

### Performance Tests

1. **Benchmarks** (`storage/historical_bench_test.go`)
   - Measure query performance
   - Identify bottlenecks
   - Validate against targets

2. **Load Tests**
   - Concurrent queries
   - Large result sets
   - Stress testing

### Test Data

- Use real Stable-One testnet data
- Generate synthetic data for edge cases
- Test with varying data sizes (1K, 10K, 100K, 1M blocks)

---

## Migration & Rollout

### Schema Migration

**No breaking changes** - all new features are additive.

1. **Index Deployment**: Deploy new indexes
   - Add timestamp index during sync
   - Build indexes for existing blocks (background job)

2. **API Deployment**: Enable APIs
   - Deploy GraphQL endpoints
   - Deploy JSON-RPC endpoints

3. **Optimization**: Performance tuning
   - Monitor performance
   - Add caching as needed
   - Tune index parameters

### Backward Compatibility

- All existing APIs continue to work
- New APIs are opt-in
- No changes to existing storage format

### Rollback Plan

If issues arise:
1. Disable new API endpoints
2. Continue using existing APIs
3. New indexes can be deleted (no impact on core functionality)

---

## Security Considerations

1. **Rate Limiting**: Prevent abuse of expensive queries
2. **Input Validation**: Validate all parameters (ranges, limits)
3. **Access Control**: Optional API key authentication
4. **Resource Limits**: Max page size, query timeout
5. **Audit Logging**: Log all historical data queries

---

## Future Enhancements

1. **Advanced Analytics**
   - Gas usage statistics
   - Transaction volume trends
   - Active addresses count

2. **Aggregation Queries**
   - Total value transferred
   - Average gas price
   - Block time statistics

3. **Export Functionality**
   - CSV export
   - JSON export
   - Streaming exports for large datasets

4. **Real-Time Subscriptions**
   - WebSocket subscriptions for new blocks
   - Balance change notifications
   - Transaction alerts

---

## References

- [Storage Layer](../storage/README.md)
- [GraphQL API](../api/graphql/README.md)
- [JSON-RPC API](../api/jsonrpc/README.md)
- [Event Subscription System](EVENT_SUBSCRIPTION_API.md)

---

**Status**: 🔄 Design Complete, Implementation Starting
**Next**: Implement core storage methods and indexes
