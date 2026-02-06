# Additional Features Implementation Plan

## Overview

EIP-3091 Block Explorer 지원을 위한 3가지 추가 기능 구현 계획입니다.

| # | Feature | Status | Priority |
|---|---------|--------|----------|
| 1 | eth_getNonce Proxy | ✅ Complete | High |
| 2 | Unified Address Overview API | ✅ Complete | High |
| 3 | Token Holder Indexing | ✅ Complete | Medium |

---

## Feature 1: eth_getNonce Proxy

### 목적
RPC 프록시를 통해 주소의 현재 nonce(transaction count)를 조회하는 기능 추가.

### 수정 파일

| File | Action | Description |
|------|--------|-------------|
| `pkg/rpcproxy/types.go` | Modify | NonceRequest, NonceResponse 타입 추가 |
| `pkg/rpcproxy/proxy.go` | Modify | GetNonce() 메서드 추가 |
| `pkg/rpcproxy/cache.go` | Modify | Nonce 캐시 키 빌더 추가 |

### 구현 상세

#### 1.1 Types (`pkg/rpcproxy/types.go`)

```go
// NonceRequest is a request for getting an address's nonce
type NonceRequest struct {
    Address     common.Address
    BlockNumber *big.Int // nil = latest
}

// NonceResponse is the response containing an address's nonce
type NonceResponse struct {
    Address     common.Address
    Nonce       uint64
    BlockNumber uint64
}
```

#### 1.2 GetNonce Method (`pkg/rpcproxy/proxy.go`)

- Rate limiting 적용
- Circuit breaker 적용
- 캐시 (BalanceTTL과 동일한 TTL 사용)
- `ethClient.NonceAt()` 호출

#### 1.3 Cache Key (`pkg/rpcproxy/cache.go`)

```go
func (b *CacheKeyBuilder) Nonce(address, block string) string {
    return fmt.Sprintf("%s:nonce:%s:%s", b.prefix, address, block)
}
```

### Checklist

- [ ] NonceRequest, NonceResponse 타입 추가
- [ ] CacheKeyBuilder.Nonce() 메서드 추가
- [ ] Proxy.GetNonce() 메서드 구현
- [ ] 빌드 및 테스트 통과

---

## Feature 2: Unified Address Overview API

### 목적
단일 API 호출로 주소에 대한 모든 정보를 조회할 수 있는 통합 엔드포인트 개선.

### 수정 파일

| File | Action | Description |
|------|--------|-------------|
| `pkg/api/graphql/types.go` | Modify | AddressOverview 타입에 새 필드 추가 |
| `pkg/api/graphql/schema.go` | Modify | addressOverview 쿼리 반환 타입 확장 |
| `pkg/api/graphql/resolvers_address.go` | Modify | resolveAddressOverview 로직 확장 |
| `pkg/api/jsonrpc/methods.go` | Modify | getAddressOverview 응답 확장 |

### 추가할 필드

| Field | Type | Source | Description |
|-------|------|--------|-------------|
| `currentBalance` | BigInt | RPC Proxy | 실시간 잔액 (wei) |
| `nonce` | BigInt | RPC Proxy | 현재 nonce |
| `isToken` | Boolean | Storage | TokenMetadata 존재 여부 |
| `tokenMetadata` | TokenMetadata | Storage | 토큰 메타데이터 (토큰인 경우) |
| `hasDelegation` | Boolean | Storage | EIP-7702 delegation 활성화 여부 |
| `delegationTarget` | Address | Storage | delegation 대상 주소 |
| `asAuthorityCount` | Int | Storage | authority로 사용된 횟수 |
| `asTargetCount` | Int | Storage | target으로 사용된 횟수 |

### 구현 상세

#### 2.1 GraphQL Type Extension

```graphql
type AddressOverview {
    # Existing fields
    address: Address!
    isContract: Boolean!
    balance: BigInt!
    transactionCount: Int!
    sentCount: Int!
    receivedCount: Int!
    internalTxCount: Int!
    erc20TokenCount: Int!
    erc721TokenCount: Int!
    contractInfo: ContractInfo
    verificationInfo: VerificationInfo
    firstSeen: BigInt
    lastSeen: BigInt

    # New fields (Feature 2)
    currentBalance: BigInt
    nonce: BigInt
    isToken: Boolean!
    tokenMetadata: TokenMetadata
    hasDelegation: Boolean!
    delegationTarget: Address
    asAuthorityCount: Int!
    asTargetCount: Int!
}
```

#### 2.2 Resolver Extension

1. RPC Proxy를 통한 currentBalance 조회
2. RPC Proxy를 통한 nonce 조회 (Feature 1 의존)
3. TokenMetadataReader를 통한 토큰 정보 조회
4. SetCodeIndexReader를 통한 EIP-7702 정보 조회

### Checklist

- [ ] types.go: AddressOverviewType에 새 필드 추가
- [ ] schema.go: addressOverview 쿼리 반환 타입 확인
- [ ] resolvers_address.go: currentBalance 조회 로직 추가
- [ ] resolvers_address.go: nonce 조회 로직 추가
- [ ] resolvers_address.go: isToken, tokenMetadata 조회 로직 추가
- [ ] resolvers_address.go: SetCode 관련 필드 조회 로직 추가
- [ ] methods.go: JSON-RPC 응답에 새 필드 포함
- [ ] 빌드 및 테스트 통과

---

## Feature 3: Token Holder Indexing

### 목적
ERC20 토큰의 홀더 목록과 잔액을 인덱싱하여 토큰 페이지에서 홀더 정보 제공.

### 새로 생성할 파일

| File | Description |
|------|-------------|
| `pkg/storage/token_holder.go` | 인터페이스 및 타입 정의 |
| `pkg/storage/pebble_token_holder.go` | PebbleDB 구현 |

### 수정할 파일

| File | Action | Description |
|------|--------|-------------|
| `pkg/storage/encoder.go` | Modify | 토큰 홀더 키 스키마 추가 |
| `pkg/storage/pebble.go` | Modify | 인터페이스 구현 등록 |
| `pkg/storage/pebble_address_index.go` | Modify | SaveERC20Transfer 시 홀더 잔액 업데이트 |
| `pkg/api/graphql/schema.go` | Modify | 토큰 홀더 쿼리 추가 |
| `pkg/api/graphql/resolvers_token.go` | Modify | 토큰 홀더 리졸버 추가 |
| `pkg/api/jsonrpc/methods.go` | Modify | JSON-RPC 메서드 추가 |

### 데이터 구조

#### 3.1 Types (`pkg/storage/token_holder.go`)

```go
// TokenHolder represents a token holder's balance
type TokenHolder struct {
    TokenAddress  common.Address `json:"tokenAddress"`
    HolderAddress common.Address `json:"holderAddress"`
    Balance       *big.Int       `json:"balance"`
    LastUpdatedAt uint64         `json:"lastUpdatedAt"` // Block number
}

// TokenHolderStats represents aggregate stats for a token
type TokenHolderStats struct {
    TokenAddress   common.Address `json:"tokenAddress"`
    HolderCount    int            `json:"holderCount"`
    TransferCount  int            `json:"transferCount"`
    LastActivityAt uint64         `json:"lastActivityAt"`
}
```

#### 3.2 Interface (`pkg/storage/token_holder.go`)

```go
// TokenHolderIndexReader defines read operations
type TokenHolderIndexReader interface {
    GetTokenHolders(ctx context.Context, token common.Address, limit, offset int) ([]*TokenHolder, error)
    GetTokenHolderCount(ctx context.Context, token common.Address) (int, error)
    GetTokenBalance(ctx context.Context, token, holder common.Address) (*big.Int, error)
    GetTokenHolderStats(ctx context.Context, token common.Address) (*TokenHolderStats, error)
}

// TokenHolderIndexWriter defines write operations
type TokenHolderIndexWriter interface {
    UpdateTokenBalance(ctx context.Context, token, holder common.Address, newBalance *big.Int, blockNumber uint64) error
    IncrementTokenHolderCount(ctx context.Context, token common.Address) error
    DecrementTokenHolderCount(ctx context.Context, token common.Address) error
}
```

#### 3.3 Key Schema (`pkg/storage/encoder.go`)

```go
// Token holder balance key (for balance-sorted iteration)
// Format: "tokenholder/{token}/{inverted_balance_hex}/{holder}"
func TokenHolderByBalanceKey(token, holder common.Address, balance *big.Int) []byte

// Holder-token key (for address lookup)
// Format: "holdertoken/{holder}/{token}"
func HolderTokenKey(holder, token common.Address) []byte

// Token stats key
// Format: "tokenstats/{token}"
func TokenStatsKey(token common.Address) []byte
```

#### 3.4 GraphQL Queries

```graphql
type TokenHolder {
    tokenAddress: Address!
    holderAddress: Address!
    balance: BigInt!
    lastUpdatedBlock: BigInt!
}

type TokenHolderConnection {
    nodes: [TokenHolder!]!
    totalCount: Int!
    pageInfo: PageInfo!
}

type TokenHolderStats {
    tokenAddress: Address!
    holderCount: Int!
    transferCount: Int!
    lastActivityBlock: BigInt
}

extend type Query {
    tokenHolders(token: Address!, pagination: PaginationInput): TokenHolderConnection!
    tokenHolderCount(token: Address!): Int!
    tokenBalance(token: Address!, holder: Address!): BigInt!
    tokenHolderStats(token: Address!): TokenHolderStats
}
```

### 구현 순서

1. Storage 인터페이스 정의 (`token_holder.go`)
2. 키 스키마 추가 (`encoder.go`)
3. PebbleDB 구현 (`pebble_token_holder.go`)
4. ERC20Transfer 저장 시 홀더 잔액 업데이트 (`pebble_address_index.go`)
5. GraphQL 쿼리 및 리졸버 추가
6. JSON-RPC 메서드 추가

### Checklist

- [ ] token_holder.go: 타입 및 인터페이스 정의
- [ ] encoder.go: 키 스키마 함수 추가
- [ ] pebble_token_holder.go: Reader/Writer 구현
- [ ] pebble_address_index.go: SaveERC20Transfer에서 홀더 잔액 업데이트 호출
- [ ] schema.go: TokenHolder 타입 및 쿼리 추가
- [ ] resolvers_token.go: 홀더 관련 리졸버 추가
- [ ] methods.go: JSON-RPC 메서드 추가
- [ ] 빌드 및 테스트 통과

---

## Implementation Order

```
Feature 1: eth_getNonce Proxy
    ↓
Feature 2: Unified Address Overview API (depends on Feature 1)
    ↓
Feature 3: Token Holder Indexing (independent)
```

---

## Verification

구현 완료 후 각 체크리스트 항목을 확인하고, 다음 테스트를 수행합니다:

### Feature 1 Verification
```bash
# Build test
go build ./...

# Unit test (if exists)
go test ./pkg/rpcproxy/... -v
```

### Feature 2 Verification
```graphql
# GraphQL query test
query {
  addressOverview(address: "0x...") {
    address
    currentBalance
    nonce
    isToken
    hasDelegation
    delegationTarget
    asAuthorityCount
    asTargetCount
  }
}
```

### Feature 3 Verification
```graphql
# GraphQL query test
query {
  tokenHolders(token: "0x...", pagination: {limit: 10}) {
    nodes {
      holderAddress
      balance
    }
    totalCount
  }
  tokenHolderCount(token: "0x...")
}
```

---

## Notes

- Feature 1, 2는 RPC Proxy 기반으로 실시간 데이터 제공
- Feature 3은 인덱싱 기반으로 히스토리컬 데이터 제공
- Token Holder Indexing은 기존 데이터 마이그레이션이 필요할 수 있음 (기존 Transfer 이벤트 재처리)
