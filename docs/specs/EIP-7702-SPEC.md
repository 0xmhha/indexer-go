# EIP-7702 SetCode Transaction Support Specification

> **Version**: 1.0.0
> **Last Updated**: 2026-02-04
> **Status**: Draft
> **Authors**: indexer-go team

---

## Table of Contents

1. [Overview](#1-overview)
2. [Protocol Specification (go-stablenet)](#2-protocol-specification-go-stablenet)
3. [Indexer Implementation Specification](#3-indexer-implementation-specification)
4. [Storage Schema](#4-storage-schema)
5. [GraphQL API Specification](#5-graphql-api-specification)
6. [Frontend Update Checklist](#6-frontend-update-checklist)
7. [Implementation Checklist](#7-implementation-checklist)
8. [Testing Strategy](#8-testing-strategy)
9. [Migration & Rollback](#9-migration--rollback)
10. [References](#10-references)

---

## 1. Overview

### 1.1 Purpose

EIP-7702는 EOA(Externally Owned Account)가 일시적으로 스마트 컨트랙트 코드를 위임받아 실행할 수 있게 하는 기능입니다. 이 문서는 indexer-go에서 EIP-7702 트랜잭션을 인덱싱하고 조회할 수 있도록 하는 구현 명세를 정의합니다.

### 1.2 Scope

- SetCode 트랜잭션 (type 0x04) 인덱싱
- Authorization 데이터 저장 및 조회
- Address별 SetCode 활동 추적
- Delegation 상태 관리
- go-stablenet 특화 기능 (Authorized/Blacklisted 계정, Gas 정책)

### 1.3 Key Terminology

| 용어 | 설명 |
|------|------|
| **SetCodeTx** | EIP-7702 트랜잭션 타입 (0x04) |
| **Authorization** | EOA가 특정 주소의 코드를 위임받겠다는 서명된 승인 |
| **Authority** | Authorization에 서명한 주소 (위임을 승인하는 EOA) |
| **Target** | Authorization.Address - 코드를 제공하는 주소 |
| **Delegation** | Authority 계정에 저장되는 위임 코드 (0xef0100 + target address) |

### 1.4 Current Implementation Status

| Component | Status | Notes |
|-----------|--------|-------|
| SetCodeTx type recognition | ✅ Done | `types.SetCodeTxType` (0x04) |
| GraphQL SetCodeAuthorization type | ✅ Done | Schema defined |
| API mapper authorization extraction | ✅ Done | JSON-RPC & GraphQL |
| SetCode authorization indexing | ❌ TODO | This spec |
| Address SetCode activity queries | ❌ TODO | This spec |
| Delegation state tracking | ❌ TODO | This spec |
| Anzeon-specific features | ❌ TODO | Authorized/Blacklisted |

---

## 2. Protocol Specification (go-stablenet)

### 2.1 Transaction Type 0x04 (SetCodeTx)

```go
type SetCodeTx struct {
    ChainID    *uint256.Int
    Nonce      uint64
    GasTipCap  *uint256.Int       // maxPriorityFeePerGas
    GasFeeCap  *uint256.Int       // maxFeePerGas
    Gas        uint64
    To         common.Address     // REQUIRED - cannot be nil
    Value      *uint256.Int
    Data       []byte
    AccessList AccessList
    AuthList   []SetCodeAuthorization  // EIP-7702 specific
    V, R, S    *uint256.Int
}
```

**Constraints**:
- `To` field is MANDATORY (contract creation not allowed)
- `AuthList` must NOT be empty

### 2.2 SetCodeAuthorization Structure

```go
type SetCodeAuthorization struct {
    ChainID uint256.Int    // Chain ID (0 = any chain)
    Address common.Address // Target address to delegate code from
    Nonce   uint64         // Authority's current nonce
    V       uint8          // yParity (0 or 1)
    R       uint256.Int
    S       uint256.Int
}
```

**Signature Hash** (prefix 0x05):
```go
func (a *SetCodeAuthorization) SigHash() common.Hash {
    return prefixedRlpHash(0x05, []any{a.ChainID, a.Address, a.Nonce})
}
```

**Authority Recovery**:
```go
func (a *SetCodeAuthorization) Authority() (common.Address, error)
```

### 2.3 Delegation Storage Format

Delegations are stored as 23-byte code:
```
[0xef, 0x01, 0x00] + [20-byte target address]
```

**Parsing**:
```go
var DelegationPrefix = []byte{0xef, 0x01, 0x00}

func ParseDelegation(code []byte) (common.Address, bool) {
    if len(code) != 23 || !bytes.HasPrefix(code, DelegationPrefix) {
        return common.Address{}, false
    }
    return common.BytesToAddress(code[3:]), true
}
```

### 2.4 Gas Calculation

#### Intrinsic Gas

| Component | Cost | Constant |
|-----------|------|----------|
| Base transaction | 21,000 | `params.TxGas` |
| Per authorization | 12,500 | `params.TxAuthTupleGas` |
| Per access list address | 2,400 | `params.TxAccessListAddressGas` |
| Per access list storage key | 1,900 | `params.TxAccessListStorageKeyGas` |

**Formula**:
```
intrinsicGas = TxGas + (len(authList) * TxAuthTupleGas) + accessListGas
```

**Example**: SetCodeTx with 2 authorizations
```
21,000 + (2 × 12,500) = 46,000 gas
```

#### Execution Gas (Delegation Resolution)

| Operation | Cost | Condition |
|-----------|------|-----------|
| Cold delegation access | 2,600 | First access to delegation target |
| Warm delegation access | 100 | Subsequent access |

#### Gas Refund

When applying authorization to an existing account:
```
refund = CallNewAccountGas - TxAuthTupleGas = 25,000 - 12,500 = 12,500 gas
```

### 2.5 Authorization Validation

| Check | Error | Description |
|-------|-------|-------------|
| ChainID | `ErrAuthorizationWrongChainID` | Must be 0 or current chain ID |
| Nonce overflow | `ErrAuthorizationNonceOverflow` | Nonce + 1 < 2^64 |
| Signature | `ErrAuthorizationInvalidSignature` | Valid ECDSA signature |
| Has code | `ErrAuthorizationDestinationHasCode` | Authority must be EOA or have delegation |
| Nonce match | `ErrAuthorizationNonceMismatch` | Authority nonce must match |

### 2.6 go-stablenet Specific Features

#### StateAccount Extra Field

```go
const (
    AccountExtraMaskBlacklisted uint64 = 1 << 63  // Bit 63
    AccountExtraMaskAuthorized  uint64 = 1 << 62  // Bit 62
)
```

| Bit | Mask | Field | Description |
|-----|------|-------|-------------|
| 63 | 0x8000000000000000 | Blacklisted | Account is blacklisted |
| 62 | 0x4000000000000000 | Authorized | Account has reduced gas tip |

#### Authorized Account Gas Policy

- **Authorized accounts**: Gas tip 면제 또는 감소
- **AnzeonTipEnv**: Authorized 상태에 따라 Gas tip 계산

#### Base Fee Distribution

- Base fee는 소각되지 않고 validators에게 diligence에 따라 분배됨
- `consensus/wbft/engine/engine.go`: `distributeBaseFee()`

---

## 3. Indexer Implementation Specification

### 3.1 Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Fetcher Layer                             │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │ processBlock()  │→ │ processSetCode()│→ │ indexAuthority()│  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Storage Layer                             │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │ SetCodeAuth     │  │ Target Index    │  │ Authority Index │  │
│  │ /data/setcode/  │  │ /index/setcode/ │  │ /index/setcode/ │  │
│  │ auth/           │  │ target/         │  │ authority/      │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                         API Layer                                │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │ GraphQL         │  │ JSON-RPC        │  │ REST (future)   │  │
│  │ Resolvers       │  │ Methods         │  │                 │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### 3.2 Data Structures

#### SetCodeAuthorizationRecord

```go
// pkg/storage/setcode.go

type SetCodeAuthorizationRecord struct {
    // Transaction reference
    TxHash      common.Hash `json:"txHash"`
    BlockNumber uint64      `json:"blockNumber"`
    BlockHash   common.Hash `json:"blockHash"`
    TxIndex     uint64      `json:"txIndex"`
    AuthIndex   int         `json:"authIndex"`  // Index within AuthList

    // Authorization data
    TargetAddress    common.Address `json:"targetAddress"`    // Address field
    AuthorityAddress common.Address `json:"authorityAddress"` // Recovered signer
    ChainID          *big.Int       `json:"chainId"`
    Nonce            uint64         `json:"nonce"`

    // Signature
    YParity uint8    `json:"yParity"`
    R       *big.Int `json:"r"`
    S       *big.Int `json:"s"`

    // Validation result
    Applied bool   `json:"applied"` // Was authorization successfully applied?
    Error   string `json:"error"`   // Validation error if not applied

    // Timestamp
    Timestamp time.Time `json:"timestamp"`
}
```

#### AddressDelegationState

```go
// pkg/storage/setcode.go

type AddressDelegationState struct {
    Address         common.Address  `json:"address"`
    HasDelegation   bool            `json:"hasDelegation"`
    DelegationTarget *common.Address `json:"delegationTarget,omitempty"`
    LastUpdatedBlock uint64         `json:"lastUpdatedBlock"`
    LastUpdatedTxHash common.Hash   `json:"lastUpdatedTxHash"`
}
```

#### AddressSetCodeStats

```go
// pkg/storage/setcode.go

type AddressSetCodeStats struct {
    Address            common.Address `json:"address"`
    AsTargetCount      int            `json:"asTargetCount"`      // Received delegations
    AsAuthorityCount   int            `json:"asAuthorityCount"`   // Signed authorizations
    CurrentDelegation  *common.Address `json:"currentDelegation,omitempty"`
    LastActivityBlock  uint64         `json:"lastActivityBlock"`
}
```

### 3.3 Interface Definitions

#### SetCodeIndexReader

```go
// pkg/storage/address_index.go

type SetCodeIndexReader interface {
    // Get authorization by transaction hash and index
    GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*SetCodeAuthorizationRecord, error)

    // Get all authorizations in a transaction
    GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*SetCodeAuthorizationRecord, error)

    // Get authorizations where address is the target (received delegations)
    GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*SetCodeAuthorizationRecord, error)

    // Get authorizations where address is the authority (signed delegations)
    GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*SetCodeAuthorizationRecord, error)

    // Get SetCode statistics for an address
    GetAddressSetCodeStats(ctx context.Context, address common.Address) (*AddressSetCodeStats, error)

    // Get current delegation state for an address
    GetAddressDelegationState(ctx context.Context, address common.Address) (*AddressDelegationState, error)

    // Get count of SetCode authorizations by target
    GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error)

    // Get count of SetCode authorizations by authority
    GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error)
}
```

#### SetCodeIndexWriter

```go
// pkg/storage/address_index.go

type SetCodeIndexWriter interface {
    // Save a SetCode authorization record
    SaveSetCodeAuthorization(ctx context.Context, record *SetCodeAuthorizationRecord) error

    // Save multiple SetCode authorization records (batch)
    SaveSetCodeAuthorizations(ctx context.Context, records []*SetCodeAuthorizationRecord) error

    // Update delegation state for an address
    UpdateAddressDelegationState(ctx context.Context, state *AddressDelegationState) error

    // Increment SetCode stats counters
    IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool) error
}
```

---

## 4. Storage Schema

### 4.1 Key Prefixes

```go
// pkg/storage/schema.go

const (
    // === SetCode Data Storage ===
    // Primary storage for SetCode authorization records
    // Key: /data/setcode/auth/{txHash}/{authIndex}
    // Value: SetCodeAuthorizationRecord (JSON encoded)
    prefixSetCodeAuth = "/data/setcode/auth/"

    // Delegation state per address
    // Key: /data/setcode/delegation/{address}
    // Value: AddressDelegationState (JSON encoded)
    prefixSetCodeDelegation = "/data/setcode/delegation/"

    // SetCode stats per address
    // Key: /data/setcode/stats/{address}
    // Value: AddressSetCodeStats (JSON encoded)
    prefixSetCodeStats = "/data/setcode/stats/"

    // === SetCode Index Prefixes ===
    // Index by target address (who received delegation)
    // Key: /index/setcode/target/{targetAddress}/{blockNumber}/{txIndex}/{authIndex}
    // Value: txHash
    prefixIdxSetCodeTarget = "/index/setcode/target/"

    // Index by authority address (who signed delegation)
    // Key: /index/setcode/authority/{authorityAddress}/{blockNumber}/{txIndex}/{authIndex}
    // Value: txHash
    prefixIdxSetCodeAuthority = "/index/setcode/authority/"

    // Index by block number (for block-level queries)
    // Key: /index/setcode/block/{blockNumber}/{txIndex}/{authIndex}
    // Value: txHash
    prefixIdxSetCodeBlock = "/index/setcode/block/"
)
```

### 4.2 Key Generation Functions

```go
// pkg/storage/schema.go

// SetCodeAuthorizationKey returns the key for storing a SetCode authorization record
// Format: /data/setcode/auth/{txHash}/{authIndex}
func SetCodeAuthorizationKey(txHash common.Hash, authIndex int) []byte {
    return []byte(fmt.Sprintf("%s%s/%d", prefixSetCodeAuth, txHash.Hex(), authIndex))
}

// SetCodeTargetIndexKey returns the index key for querying by target address
// Format: /index/setcode/target/{address}/{blockNumber:016x}/{txIndex:08x}/{authIndex:04x}
func SetCodeTargetIndexKey(target common.Address, blockNumber uint64, txIndex uint64, authIndex int) []byte {
    return []byte(fmt.Sprintf("%s%s/%016x/%08x/%04x",
        prefixIdxSetCodeTarget, target.Hex(), blockNumber, txIndex, authIndex))
}

// SetCodeAuthorityIndexKey returns the index key for querying by authority address
// Format: /index/setcode/authority/{address}/{blockNumber:016x}/{txIndex:08x}/{authIndex:04x}
func SetCodeAuthorityIndexKey(authority common.Address, blockNumber uint64, txIndex uint64, authIndex int) []byte {
    return []byte(fmt.Sprintf("%s%s/%016x/%08x/%04x",
        prefixIdxSetCodeAuthority, authority.Hex(), blockNumber, txIndex, authIndex))
}

// SetCodeDelegationStateKey returns the key for storing delegation state
// Format: /data/setcode/delegation/{address}
func SetCodeDelegationStateKey(address common.Address) []byte {
    return []byte(fmt.Sprintf("%s%s", prefixSetCodeDelegation, address.Hex()))
}

// SetCodeStatsKey returns the key for storing SetCode stats
// Format: /data/setcode/stats/{address}
func SetCodeStatsKey(address common.Address) []byte {
    return []byte(fmt.Sprintf("%s%s", prefixSetCodeStats, address.Hex()))
}
```

### 4.3 Encoding/Decoding

```go
// pkg/storage/encoder.go

func EncodeSetCodeAuthorization(record *SetCodeAuthorizationRecord) ([]byte, error) {
    return json.Marshal(record)
}

func DecodeSetCodeAuthorization(data []byte) (*SetCodeAuthorizationRecord, error) {
    var record SetCodeAuthorizationRecord
    if err := json.Unmarshal(data, &record); err != nil {
        return nil, err
    }
    return &record, nil
}
```

---

## 5. GraphQL API Specification

### 5.1 Type Definitions

#### SetCodeAuthorization (Extended)

```graphql
# Extended SetCodeAuthorization with transaction reference
type SetCodeAuthorizationWithTx {
  # Transaction reference
  txHash: Hash!
  blockNumber: BigInt!
  blockHash: Hash!
  transactionIndex: Int!
  authorizationIndex: Int!

  # Authorization data
  chainId: BigInt!
  address: Address!           # Target address (delegation source)
  nonce: BigInt!
  yParity: Int!
  r: Bytes!
  s: Bytes!
  authority: Address          # Recovered signer address

  # Validation result
  applied: Boolean!           # Was this authorization successfully applied?
  error: String               # Error message if not applied

  # Timestamp
  timestamp: DateTime!

  # Related transaction
  transaction: Transaction
}

# Connection type for pagination
type SetCodeAuthorizationConnection {
  edges: [SetCodeAuthorizationEdge!]!
  pageInfo: PageInfo!
  totalCount: Int!
}

type SetCodeAuthorizationEdge {
  node: SetCodeAuthorizationWithTx!
  cursor: String!
}
```

#### AddressSetCodeInfo

```graphql
# SetCode-specific information for an address
type AddressSetCodeInfo {
  # Address being queried
  address: Address!

  # Delegation state
  hasDelegation: Boolean!
  delegationTarget: Address       # Current delegation target (if any)

  # Activity counts
  asTargetCount: Int!             # Times received delegation
  asAuthorityCount: Int!          # Times signed authorization

  # Last activity
  lastActivityBlock: BigInt
  lastActivityTimestamp: DateTime
}
```

#### AddressOverview (Extended)

```graphql
# Extended AddressOverview with SetCode information
type AddressOverview {
  # ... existing fields ...

  # === EIP-7702 SetCode Information ===

  # Whether this address currently has a delegation
  hasDelegation: Boolean!

  # Current delegation target (if hasDelegation is true)
  delegationTarget: Address

  # Number of times this address received delegation (as target)
  setCodeTargetCount: Int!

  # Number of times this address signed authorization (as authority)
  setCodeAuthorityCount: Int!

  # Most recent SetCode transaction involving this address
  lastSetCodeTransaction: Transaction
}
```

### 5.2 Query Definitions

```graphql
type Query {
  # === EIP-7702 SetCode Queries ===

  # Get a specific SetCode authorization by transaction hash and index
  setCodeAuthorization(
    txHash: Hash!
    authIndex: Int!
  ): SetCodeAuthorizationWithTx

  # Get all SetCode authorizations in a transaction
  setCodeAuthorizationsByTx(
    txHash: Hash!
  ): [SetCodeAuthorizationWithTx!]!

  # Get SetCode authorizations where address is the target (received delegations)
  setCodeAuthorizationsByTarget(
    target: Address!
    pagination: PaginationInput
  ): SetCodeAuthorizationConnection!

  # Get SetCode authorizations where address is the authority (signed delegations)
  setCodeAuthorizationsByAuthority(
    authority: Address!
    pagination: PaginationInput
  ): SetCodeAuthorizationConnection!

  # Get SetCode information for an address
  addressSetCodeInfo(
    address: Address!
  ): AddressSetCodeInfo!

  # Get SetCode transactions in a block
  setCodeTransactionsInBlock(
    blockNumber: BigInt!
  ): [Transaction!]!

  # Get recent SetCode transactions (global)
  recentSetCodeTransactions(
    limit: Int
  ): [Transaction!]!

  # Get SetCode transaction count
  setCodeTransactionCount: Int!
}
```

### 5.3 Subscription Definitions

```graphql
type Subscription {
  # === EIP-7702 SetCode Subscriptions ===

  # Subscribe to new SetCode transactions
  newSetCodeTransaction: Transaction!

  # Subscribe to SetCode authorizations for a specific address (as target or authority)
  setCodeAuthorizationForAddress(
    address: Address!
  ): SetCodeAuthorizationWithTx!
}
```

### 5.4 Filter Extensions

```graphql
# Extended TransactionFilter for SetCode
input TransactionFilter {
  # ... existing fields ...

  # Filter by SetCode target address
  setCodeTarget: Address

  # Filter by SetCode authority address
  setCodeAuthority: Address

  # Include only SetCode transactions
  setCodeOnly: Boolean
}
```

---

## 6. Frontend Update Checklist

### 6.1 New Pages/Views

| Page | Route | Priority | Description |
|------|-------|----------|-------------|
| SetCode Transactions List | `/setcode` | HIGH | List of all SetCode transactions |
| SetCode Transaction Detail | `/tx/{hash}` (extended) | HIGH | Extended tx detail for SetCode |
| Address SetCode Activity | `/address/{addr}/setcode` | HIGH | Address SetCode history |
| Delegation Status | `/address/{addr}` (extended) | HIGH | Show delegation in address overview |

### 6.2 Component Updates

#### Transaction Detail Page

- [ ] **SetCode Authorization List**
  - Display all authorizations in the transaction
  - Show target address, authority, chainId, nonce
  - Indicate if each authorization was applied successfully
  - Link to target and authority address pages

- [ ] **SetCode Gas Breakdown**
  - Show intrinsic gas calculation
  - Per-authorization gas cost (12,500 each)
  - Delegation resolution gas if applicable

#### Address Overview Page

- [ ] **Delegation Status Badge**
  - Show "Has Delegation" badge if address has delegation
  - Show delegation target with link

- [ ] **SetCode Statistics Card**
  - As Target Count: X times
  - As Authority Count: X times
  - Link to detailed SetCode activity

- [ ] **SetCode Activity Tab**
  - List of SetCode authorizations involving this address
  - Filter by "As Target" / "As Authority"
  - Pagination support

#### Block Detail Page

- [ ] **SetCode Transactions Section**
  - List SetCode transactions in the block
  - Show authorization count per transaction

#### Transaction List Page

- [ ] **SetCode Filter Option**
  - Filter to show only SetCode transactions
  - Filter by target or authority address

### 6.3 API Integration

#### New GraphQL Queries to Implement

```typescript
// queries/setcode.ts

// Get SetCode authorizations by target
export const GET_SETCODE_BY_TARGET = gql`
  query GetSetCodeByTarget($target: Address!, $pagination: PaginationInput) {
    setCodeAuthorizationsByTarget(target: $target, pagination: $pagination) {
      edges {
        node {
          txHash
          blockNumber
          authorizationIndex
          chainId
          address
          authority
          applied
          error
          timestamp
        }
      }
      pageInfo {
        hasNextPage
        endCursor
      }
      totalCount
    }
  }
`;

// Get SetCode authorizations by authority
export const GET_SETCODE_BY_AUTHORITY = gql`
  query GetSetCodeByAuthority($authority: Address!, $pagination: PaginationInput) {
    setCodeAuthorizationsByAuthority(authority: $authority, pagination: $pagination) {
      edges {
        node {
          txHash
          blockNumber
          authorizationIndex
          chainId
          address
          authority
          applied
          error
          timestamp
        }
      }
      pageInfo {
        hasNextPage
        endCursor
      }
      totalCount
    }
  }
`;

// Get address SetCode info
export const GET_ADDRESS_SETCODE_INFO = gql`
  query GetAddressSetCodeInfo($address: Address!) {
    addressSetCodeInfo(address: $address) {
      address
      hasDelegation
      delegationTarget
      asTargetCount
      asAuthorityCount
      lastActivityBlock
      lastActivityTimestamp
    }
  }
`;

// Subscribe to new SetCode transactions
export const SUBSCRIBE_NEW_SETCODE = gql`
  subscription OnNewSetCodeTransaction {
    newSetCodeTransaction {
      hash
      blockNumber
      from
      to
      authorizationList {
        chainId
        address
        authority
        applied
      }
    }
  }
`;
```

### 6.4 UI Components to Create

| Component | File | Description |
|-----------|------|-------------|
| `SetCodeAuthorizationList` | `components/SetCode/AuthorizationList.tsx` | List of authorizations |
| `SetCodeAuthorizationCard` | `components/SetCode/AuthorizationCard.tsx` | Single authorization display |
| `DelegationBadge` | `components/Address/DelegationBadge.tsx` | Delegation status indicator |
| `SetCodeStatsCard` | `components/Address/SetCodeStatsCard.tsx` | SetCode statistics |
| `SetCodeActivityTable` | `components/SetCode/ActivityTable.tsx` | Paginated activity list |
| `SetCodeGasBreakdown` | `components/Transaction/SetCodeGasBreakdown.tsx` | Gas calculation display |

### 6.5 State Management Updates

```typescript
// store/setcode.ts

interface SetCodeState {
  // Recent SetCode transactions
  recentSetCodeTxs: Transaction[];

  // Address-specific SetCode info cache
  addressSetCodeInfo: Map<string, AddressSetCodeInfo>;

  // Loading states
  loading: {
    recentTxs: boolean;
    addressInfo: boolean;
    authorizations: boolean;
  };
}
```

### 6.6 Localization Keys

```json
{
  "setcode": {
    "title": "SetCode Transactions",
    "authorization": "Authorization",
    "target": "Target Address",
    "authority": "Authority",
    "applied": "Applied",
    "notApplied": "Not Applied",
    "delegationActive": "Delegation Active",
    "noDelegation": "No Delegation",
    "asTarget": "As Target",
    "asAuthority": "As Authority",
    "gasPerAuth": "Gas per Authorization",
    "totalAuthGas": "Total Authorization Gas"
  }
}
```

---

## 7. Implementation Checklist

### Phase 1: Storage Layer ✅ COMPLETED (2026-02-04)

- [x] **1.1 Schema Updates**
  - [x] Add SetCode prefixes to `pkg/storage/schema.go`
  - [x] Add key generation functions
  - [x] Add encoding/decoding functions

- [x] **1.2 Data Structures**
  - [x] Create `pkg/storage/setcode.go`
  - [x] Implement `SetCodeAuthorizationRecord`
  - [x] Implement `AddressDelegationState`
  - [x] Implement `AddressSetCodeStats`

- [x] **1.3 Interface Updates**
  - [x] Update `pkg/storage/address_index.go` with SetCode interfaces
  - [x] Add `SetCodeIndexReader` interface
  - [x] Add `SetCodeIndexWriter` interface

- [x] **1.4 Pebble Implementation**
  - [x] Implement `SaveSetCodeAuthorization` in `pebble_setcode.go`
  - [x] Implement `GetSetCodeAuthorizationsByTarget`
  - [x] Implement `GetSetCodeAuthorizationsByAuthority`
  - [x] Implement `GetAddressSetCodeStats`
  - [x] Implement `UpdateAddressDelegationState`

- [x] **1.5 Unit Tests**
  - [x] Create `pkg/storage/pebble_setcode_test.go`
  - [x] All 12 tests passing

### Phase 2: Fetcher Layer

- [ ] **2.1 SetCode Processing**
  - [ ] Create `pkg/fetch/setcode.go`
  - [ ] Implement `processSetCodeTransaction()`
  - [ ] Implement authority recovery from signatures
  - [ ] Handle authorization validation errors

- [ ] **2.2 Integration**
  - [ ] Add SetCode processing to `processBlock()` in `fetcher.go`
  - [ ] Update `processAddressIndexing()` for SetCode
  - [ ] Add SetCode processing to `large_block.go`

- [ ] **2.3 Validation**
  - [ ] Implement authorization validation logic
  - [ ] Record validation errors in authorization records

### Phase 3: GraphQL API

- [ ] **3.1 Schema Updates**
  - [ ] Add `SetCodeAuthorizationWithTx` type
  - [ ] Add `SetCodeAuthorizationConnection` type
  - [ ] Add `AddressSetCodeInfo` type
  - [ ] Extend `AddressOverview` type
  - [ ] Add SetCode queries
  - [ ] Add SetCode subscriptions
  - [ ] Extend `TransactionFilter`

- [ ] **3.2 Resolver Implementation**
  - [ ] Create `pkg/api/graphql/resolvers_setcode.go`
  - [ ] Implement `setCodeAuthorization` resolver
  - [ ] Implement `setCodeAuthorizationsByTarget` resolver
  - [ ] Implement `setCodeAuthorizationsByAuthority` resolver
  - [ ] Implement `addressSetCodeInfo` resolver
  - [ ] Update `addressOverview` resolver

- [ ] **3.3 Subscription Implementation**
  - [ ] Implement `newSetCodeTransaction` subscription
  - [ ] Implement `setCodeAuthorizationForAddress` subscription

### Phase 4: JSON-RPC API

- [ ] **4.1 Method Extensions**
  - [ ] Extend `eth_getTransactionByHash` response
  - [ ] Add `eth_getSetCodeAuthorizations` method (optional)

### Phase 5: Anzeon-Specific Features

- [ ] **5.1 Account Extra Field**
  - [ ] Add `AccountExtra` to address index storage
  - [ ] Implement Blacklisted/Authorized tracking
  - [ ] Update AddressOverview with extra field info

- [ ] **5.2 Gas Policy Tracking**
  - [ ] Track Authorized account gas benefits
  - [ ] Add gas analysis fields

### Phase 6: Testing

- [ ] **6.1 Unit Tests**
  - [ ] Storage layer tests
  - [ ] Fetcher tests
  - [ ] Resolver tests

- [ ] **6.2 Integration Tests**
  - [ ] End-to-end SetCode indexing test
  - [ ] GraphQL query tests

- [ ] **6.3 Performance Tests**
  - [ ] Large authorization list handling
  - [ ] Query performance benchmarks

### Phase 7: Frontend Updates

- [ ] **7.1 API Integration**
  - [ ] Add GraphQL queries
  - [ ] Add subscriptions
  - [ ] Update state management

- [ ] **7.2 Components**
  - [ ] Implement SetCode components
  - [ ] Update existing components

- [ ] **7.3 Pages**
  - [ ] SetCode transaction list page
  - [ ] Address SetCode activity page
  - [ ] Update transaction detail page
  - [ ] Update address overview page

- [ ] **7.4 Testing**
  - [ ] Component tests
  - [ ] Integration tests

---

## 8. Testing Strategy

### 8.1 Unit Tests

```go
// pkg/storage/pebble_setcode_test.go

func TestSaveSetCodeAuthorization(t *testing.T)
func TestGetSetCodeAuthorizationsByTarget(t *testing.T)
func TestGetSetCodeAuthorizationsByAuthority(t *testing.T)
func TestUpdateAddressDelegationState(t *testing.T)
func TestGetAddressSetCodeStats(t *testing.T)
```

```go
// pkg/fetch/setcode_test.go

func TestProcessSetCodeTransaction(t *testing.T)
func TestRecoverAuthority(t *testing.T)
func TestValidateAuthorization(t *testing.T)
```

### 8.2 Integration Tests

```go
// pkg/api/graphql/setcode_test.go

func TestSetCodeAuthorizationQuery(t *testing.T)
func TestSetCodeAuthorizationsByTargetQuery(t *testing.T)
func TestSetCodeAuthorizationsByAuthorityQuery(t *testing.T)
func TestAddressSetCodeInfoQuery(t *testing.T)
```

### 8.3 Test Data

Create test fixtures with:
- Valid SetCode transactions with multiple authorizations
- Invalid authorizations (wrong chain ID, nonce mismatch, etc.)
- Delegation clear (target = zero address)
- Edge cases (empty auth list, max authorizations)

---

## 9. Migration & Rollback

### 9.1 Data Migration

For existing blocks with SetCode transactions:

```go
// cmd/indexer/migrate_setcode.go

func MigrateSetCodeData(ctx context.Context, storage Storage, startBlock, endBlock uint64) error {
    for block := startBlock; block <= endBlock; block++ {
        txs, err := storage.GetBlockTransactions(ctx, block)
        if err != nil {
            return err
        }

        for _, tx := range txs {
            if tx.Type() == types.SetCodeTxType {
                if err := processSetCodeTransaction(ctx, storage, tx, block); err != nil {
                    log.Warn("Failed to migrate SetCode tx", "hash", tx.Hash(), "err", err)
                }
            }
        }
    }
    return nil
}
```

### 9.2 Rollback Procedure

If issues are found:

1. Stop indexer
2. Delete SetCode prefixes from PebbleDB:
   ```
   /data/setcode/*
   /index/setcode/*
   ```
3. Restart indexer with SetCode processing disabled
4. Investigate and fix issues
5. Re-enable and re-migrate

---

## 10. References

### 10.1 EIP References

- [EIP-7702: Set EOA account code](https://eips.ethereum.org/EIPS/eip-7702)
- [EIP-2930: Optional access lists](https://eips.ethereum.org/EIPS/eip-2930)
- [EIP-1559: Fee market change](https://eips.ethereum.org/EIPS/eip-1559)

### 10.2 go-stablenet Commits

| Commit | Description |
|--------|-------------|
| `f32d12e95` | feat: EIP-7702 Set Code Transaction support (#42) |
| `324d08bea` | feat: complete EIP-7702 implementation for go-stablenet |
| `889f25c23` | feat: implement EIP-7702 set code transaction support |
| `b2334d452` | feat: add EIP-7702 SetCode transaction pool support |
| `e7a0d896a` | feat: implement base fee distribution to validators (#45) |
| `4012784fe` | feat: add hysteresis thresholds for base fee adjustment (#48) |

### 10.3 Source Files

**go-stablenet:**
- `core/types/tx_setcode.go` - SetCodeTx definition
- `core/state_transition.go` - Authorization validation & application
- `core/vm/operations_acl.go` - Delegation resolution gas
- `core/types/state_account_extra.go` - Account extra field

**indexer-go:**
- `pkg/api/graphql/schema.graphql` - Current GraphQL schema
- `pkg/api/graphql/mappers.go` - Transaction mapping
- `pkg/storage/address_index.go` - Address indexing interfaces
- `pkg/fetch/fetcher.go` - Block fetching and indexing

---

## Appendix A: Gas Calculation Examples

### Example 1: Simple SetCode Transaction

```
Transaction:
- 1 authorization
- No access list
- No data

Intrinsic Gas:
  Base:         21,000
  Authorization: 12,500 × 1 = 12,500
  ─────────────────────────────────
  Total:        33,500 gas
```

### Example 2: Complex SetCode Transaction

```
Transaction:
- 3 authorizations
- Access list: 2 addresses, 5 storage keys
- Data: 100 bytes (20 non-zero)

Intrinsic Gas:
  Base:              21,000
  Authorization:     12,500 × 3 = 37,500
  Access list addr:   2,400 × 2 =  4,800
  Access list keys:   1,900 × 5 =  9,500
  Non-zero data:         16 × 20 =    320
  Zero data:              4 × 80 =    320
  ─────────────────────────────────────────
  Total:             73,440 gas
```

---

## Appendix B: JSON-RPC Response Examples

### eth_getTransactionByHash (SetCode)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "blockHash": "0x...",
    "blockNumber": "0x123",
    "from": "0xsender...",
    "gas": "0x7a120",
    "gasPrice": null,
    "maxFeePerGas": "0x12a05f200",
    "maxPriorityFeePerGas": "0x2",
    "hash": "0xtxhash...",
    "input": "0x...",
    "nonce": "0x0",
    "to": "0xrecipient...",
    "transactionIndex": "0x0",
    "value": "0x0",
    "type": "0x4",
    "accessList": [],
    "authorizationList": [
      {
        "chainId": "0x1",
        "address": "0xtarget...",
        "nonce": "0x5",
        "yParity": "0x1",
        "r": "0x...",
        "s": "0x...",
        "authority": "0xauthority..."
      }
    ],
    "v": "0x1",
    "r": "0x...",
    "s": "0x..."
  }
}
```

---

**Document End**
