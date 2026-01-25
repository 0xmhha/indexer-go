# Multi-Chain, Address Watchlist, WebSocket Resilience 설계 문서

**Version**: 1.0
**Date**: 2026-01-24
**Status**: Approved for Implementation

---

## 1. 개요

### 1.1 목적
블록체인 인덱서에 다음 기능을 추가하여 상용 프로덕트 수준의 서비스 제공:
1. **Multi-Chain Manager**: 단일 인스턴스에서 여러 체인 동적 연결/관리
2. **Address Watchlist**: Contract/EOA 등록 및 실시간 TX/Event 알림
3. **WebSocket Resilience**: 연결 복구, 이벤트 재생, 영구 캐시

### 1.2 아키텍처 결정 사항

| 결정 사항 | 선택 | 근거 |
|----------|------|------|
| Cache Storage | Hybrid (PebbleDB + Redis Interface) | 초기 개발 속도 + 추후 확장성 |
| Multi-Chain | 단일 인스턴스 | 운영 단순성, 리소스 효율 |
| Event Types | All (TX + ERC20/721 + Logs) | 완전한 모니터링 지원 |
| Replay Window | 1 Hour | 일반적 장애 복구에 충분 |

---

## 2. Multi-Chain Manager

### 2.1 디렉토리 구조

```
pkg/multichain/
├── manager.go           # ChainManager 구현
├── instance.go          # ChainInstance (개별 체인 연결)
├── config.go            # 멀티체인 설정
├── health.go            # 헬스체크 로직
└── registry.go          # 체인 레지스트리
```

### 2.2 핵심 인터페이스

#### ChainManager

```go
type ChainManager interface {
    // Chain lifecycle
    RegisterChain(ctx context.Context, config *ChainConfig) (string, error)
    UnregisterChain(ctx context.Context, chainID string) error
    StartChain(ctx context.Context, chainID string) error
    StopChain(ctx context.Context, chainID string) error

    // Query
    GetChain(chainID string) (*ChainInstance, error)
    ListChains() []*ChainStatus

    // Health
    HealthCheck(ctx context.Context) map[string]*HealthStatus
}
```

#### ChainInstance

```go
type ChainInstance struct {
    ID          string
    Config      *ChainConfig
    Adapter     chain.Adapter
    Fetcher     *fetch.Fetcher
    Storage     storage.Storage    // chain-scoped prefix
    EventBus    *events.EventBus   // global, shared
    Status      ChainStatus
}
```

#### ChainConfig

```go
type ChainConfig struct {
    ID            string        `yaml:"id"`
    Name          string        `yaml:"name"`
    RPCEndpoint   string        `yaml:"rpc_endpoint"`
    WSEndpoint    string        `yaml:"ws_endpoint,omitempty"`
    ChainID       uint64        `yaml:"chain_id"`
    AdapterType   string        `yaml:"adapter_type"` // "auto", "evm", "stableone", "anvil"
    StartHeight   uint64        `yaml:"start_height"`
    Enabled       bool          `yaml:"enabled"`
}
```

### 2.3 스토리지 스키마

```
chain:<chainID>:block:<height>      - 블록 데이터
chain:<chainID>:tx:<hash>           - 트랜잭션
chain:<chainID>:receipt:<hash>      - 영수증
chain:<chainID>:log:<block>:<idx>   - 로그
chain:<chainID>:latest              - 마지막 인덱싱 높이
```

### 2.4 체인 상태 다이어그램

```
    REGISTERED
        │
        ▼
    STARTING ──────► ERROR
        │              ▲
        ▼              │
    SYNCING ──────────┤
        │              │
        ▼              │
    ACTIVE ───────────┤
        │              │
        ▼              │
    STOPPING          │
        │              │
        ▼              │
    STOPPED ◄─────────┘
```

---

## 3. Address Watchlist Service

### 3.1 디렉토리 구조

```
pkg/watchlist/
├── service.go           # WatchlistService 구현
├── address.go           # WatchedAddress 관리
├── subscriber.go        # 구독자 관리
├── filter.go            # 이벤트 매칭 로직
├── bloom.go             # Bloom Filter 최적화
├── storage.go           # 스토리지 스키마
└── types.go             # 타입 정의
```

### 3.2 핵심 인터페이스

#### WatchlistService

```go
type WatchlistService interface {
    // Address management
    WatchAddress(ctx context.Context, req *WatchRequest) (*WatchedAddress, error)
    UnwatchAddress(ctx context.Context, addressID string) error
    GetWatchedAddress(ctx context.Context, addressID string) (*WatchedAddress, error)
    ListWatchedAddresses(ctx context.Context, filter *ListFilter) ([]*WatchedAddress, error)

    // Subscriber management
    Subscribe(ctx context.Context, addressID string, subscriber *Subscriber) (string, error)
    Unsubscribe(ctx context.Context, subscriptionID string) error

    // Event processing (internal, called by Fetcher)
    ProcessBlock(ctx context.Context, chainID string, block *types.Block, receipts []*types.Receipt) error
}
```

#### 데이터 타입

```go
type WatchRequest struct {
    Address    common.Address  `json:"address"`
    ChainID    string          `json:"chainId"`
    Label      string          `json:"label,omitempty"`
    Filter     *WatchFilter    `json:"filter"`
}

type WatchFilter struct {
    TxFrom     bool   `json:"txFrom"`      // TX where address is sender
    TxTo       bool   `json:"txTo"`        // TX where address is recipient
    ERC20      bool   `json:"erc20"`       // ERC20 Transfer events
    ERC721     bool   `json:"erc721"`      // ERC721 Transfer events
    Logs       bool   `json:"logs"`        // All logs emitted by address
    MinValue   string `json:"minValue,omitempty"`  // Minimum TX value (wei)
}

type WatchedAddress struct {
    ID         string          `json:"id"`
    Address    common.Address  `json:"address"`
    ChainID    string          `json:"chainId"`
    Label      string          `json:"label"`
    Filter     *WatchFilter    `json:"filter"`
    CreatedAt  time.Time       `json:"createdAt"`
    Stats      *WatchStats     `json:"stats"`
}

type WatchEvent struct {
    ID          string          `json:"id"`
    AddressID   string          `json:"addressId"`
    ChainID     string          `json:"chainId"`
    EventType   WatchEventType  `json:"eventType"`  // "tx_from", "tx_to", "erc20", "erc721", "log"
    BlockNumber uint64          `json:"blockNumber"`
    TxHash      common.Hash     `json:"txHash"`
    LogIndex    *uint           `json:"logIndex,omitempty"`
    Data        interface{}     `json:"data"`
    Timestamp   time.Time       `json:"timestamp"`
}
```

### 3.3 스토리지 스키마

```
wl:addr:<address_id>              - WatchedAddress (JSON)
wl:chain:<chain_id>:addrs         - Set of watched addresses per chain
wl:bloom:<chain_id>               - Bloom filter bytes
wl:sub:<subscriber_id>            - Subscriber info
wl:addr:<address_id>:subs         - Set of subscribers per address
wl:event:<chain>:<block>:<tx>:<log> - WatchEvent (JSON)
wl:eventidx:<addr_id>:<timestamp> - Event index by address
```

### 3.4 Bloom Filter 최적화

- **Expected items**: 100,000 addresses
- **False positive rate**: 0.01%
- 체인별 별도 Bloom Filter 관리
- 블록 처리 시 O(k) 시간에 주소 체크

### 3.5 이벤트 처리 플로우

```
블록 수신
    │
    ▼
Bloom Filter 체크 (O(k))
    │
    ├── Miss → 스킵
    │
    └── Hit → 상세 확인
              │
              ▼
        주소 매칭 확인
              │
              ▼
        필터 조건 확인
              │
              ▼
        WatchEvent 생성
              │
              ▼
        구독자에게 전송
```

---

## 4. WebSocket Resilience System

### 4.1 디렉토리 구조

```
pkg/resilience/
├── session.go           # Session 관리
├── session_store.go     # SessionStore 인터페이스 + PebbleDB 구현
├── event_cache.go       # EventCache (1시간 윈도우)
├── connection.go        # ConnectionManager
├── recovery.go          # 복구 로직
├── redis_store.go       # Redis 구현 (인터페이스만)
└── types.go             # 타입 정의
```

### 4.2 핵심 인터페이스

#### Session

```go
type Session struct {
    ID            string                 `json:"id"`
    ClientID      string                 `json:"clientId"`
    State         SessionState           `json:"state"`  // "active", "disconnected", "expired"
    Subscriptions map[string]*SubState   `json:"subscriptions"`
    LastEventID   string                 `json:"lastEventId"`
    LastSeen      time.Time              `json:"lastSeen"`
    CreatedAt     time.Time              `json:"createdAt"`
    TTL           time.Duration          `json:"ttl"`
}
```

#### SessionStore

```go
type SessionStore interface {
    Save(ctx context.Context, session *Session) error
    Get(ctx context.Context, sessionID string) (*Session, error)
    Delete(ctx context.Context, sessionID string) error
    UpdateLastSeen(ctx context.Context, sessionID string) error
    ListDisconnected(ctx context.Context) ([]*Session, error)
    ExpireOldSessions(ctx context.Context) (int, error)
}
```

#### EventCache

```go
type EventCache interface {
    Store(ctx context.Context, event *CachedEvent) error
    GetAfter(ctx context.Context, sessionID string, afterEventID string, limit int) ([]*CachedEvent, error)
    GetBySession(ctx context.Context, sessionID string, limit int) ([]*CachedEvent, error)
    Cleanup(ctx context.Context, olderThan time.Duration) (int, error)
}

type CachedEvent struct {
    ID          string      `json:"id"`
    SessionID   string      `json:"sessionId"`
    EventType   string      `json:"eventType"`
    Payload     []byte      `json:"payload"`
    Timestamp   time.Time   `json:"timestamp"`
    Delivered   bool        `json:"delivered"`
}
```

### 4.3 스토리지 스키마

```
rs:session:<session_id>           - Session (JSON)
rs:session:idx:client:<client_id> - Session ID by client
rs:session:idx:state:<state>      - Session IDs by state
rs:cache:<session_id>:<event_id>  - CachedEvent (JSON)
rs:cache:idx:<session_id>         - Event IDs by session (sorted by timestamp)
```

### 4.4 복구 프로토콜

```
1. 클라이언트 재연결 (세션 ID 포함)
   {"type": "resume", "payload": {"sessionId": "sess_123", "lastEventId": "evt_456"}}

2. 서버 응답 - 세션 복원 + 놓친 이벤트 수
   {"type": "resumed", "payload": {"sessionId": "sess_123", "missedEvents": 5}}

3. 이벤트 재생 시작
   {"type": "replay_start", "payload": {"count": 5}}

4. 놓친 이벤트들 전송
   {"type": "event", "payload": {...}, "meta": {"replay": true, "eventId": "evt_457"}}

5. 재생 완료
   {"type": "replay_end"}

6. 실시간 이벤트 재개
   {"type": "event", "payload": {...}, "meta": {"eventId": "evt_462"}}
```

### 4.5 세션 상태 다이어그램

```
    ┌─────────────────────────────────────┐
    │                                     │
    ▼                                     │
  NEW ──► ACTIVE ──► DISCONNECTED ──► EXPIRED
              ▲            │
              │            │
              └────────────┘
              (재연결 성공)
```

---

## 5. API Extensions

### 5.1 GraphQL Schema

```graphql
# Types
type Chain {
    id: ID!
    name: String!
    chainId: BigInt!
    status: ChainStatus!
    syncProgress: SyncProgress
    health: HealthStatus
}

enum ChainStatus {
    REGISTERED
    STARTING
    SYNCING
    ACTIVE
    STOPPING
    STOPPED
    ERROR
}

type WatchedAddress {
    id: ID!
    address: Address!
    chainId: String!
    label: String
    filter: WatchFilter!
    createdAt: DateTime!
    recentEvents(limit: Int): [WatchEvent!]!
}

type WatchFilter {
    txFrom: Boolean!
    txTo: Boolean!
    erc20: Boolean!
    erc721: Boolean!
    logs: Boolean!
    minValue: BigInt
}

type WatchEvent {
    id: ID!
    eventType: WatchEventType!
    blockNumber: BigInt!
    txHash: Hash!
    logIndex: Int
    data: JSON!
    timestamp: DateTime!
}

enum WatchEventType {
    TX_FROM
    TX_TO
    ERC20_TRANSFER
    ERC721_TRANSFER
    LOG
}

# Queries
extend type Query {
    # Multi-chain
    chains: [Chain!]!
    chain(id: ID!): Chain

    # Watchlist
    watchedAddresses(chainId: String, limit: Int, offset: Int): [WatchedAddress!]!
    watchedAddress(id: ID!): WatchedAddress
}

# Mutations
extend type Mutation {
    # Multi-chain
    registerChain(input: RegisterChainInput!): Chain!
    startChain(id: ID!): Chain!
    stopChain(id: ID!): Chain!
    unregisterChain(id: ID!): Boolean!

    # Watchlist
    watchAddress(input: WatchAddressInput!): WatchedAddress!
    unwatchAddress(id: ID!): Boolean!
    updateWatchFilter(id: ID!, filter: WatchFilterInput!): WatchedAddress!
}

# Subscriptions
extend type Subscription {
    # Multi-chain status
    chainStatus(chainId: ID!): Chain!

    # Watchlist events
    watchedAddressEvents(addressId: ID!): WatchEvent!
    watchedAddressEventsByChain(chainId: String!): WatchEvent!

    # Multi-chain blocks
    newBlockMultiChain(chainIds: [String!]): BlockWithChain!
}

# Inputs
input RegisterChainInput {
    name: String!
    rpcEndpoint: String!
    wsEndpoint: String
    chainId: BigInt!
    adapterType: String
    startHeight: BigInt
}

input WatchAddressInput {
    address: Address!
    chainId: String!
    label: String
    filter: WatchFilterInput!
}

input WatchFilterInput {
    txFrom: Boolean!
    txTo: Boolean!
    erc20: Boolean!
    erc721: Boolean!
    logs: Boolean!
    minValue: BigInt
}
```

### 5.2 WebSocket Protocol Extension

```go
// 새로운 메시지 타입
const (
    MsgTypeResume      = "resume"       // 세션 재연결
    MsgTypeResumed     = "resumed"      // 재연결 성공
    MsgTypeReplayStart = "replay_start" // 재생 시작
    MsgTypeReplayEnd   = "replay_end"   // 재생 완료
    MsgTypeAck         = "ack"          // 이벤트 확인
)
```

---

## 6. Configuration

### 6.1 Config 구조

```go
type Config struct {
    // ... existing fields ...

    MultiChain   MultiChainConfig   `yaml:"multichain"`
    Watchlist    WatchlistConfig    `yaml:"watchlist"`
    Resilience   ResilienceConfig   `yaml:"resilience"`
}

type MultiChainConfig struct {
    Enabled bool          `yaml:"enabled"`
    Chains  []ChainConfig `yaml:"chains"`
}

type WatchlistConfig struct {
    Enabled     bool              `yaml:"enabled"`
    BloomFilter BloomFilterConfig `yaml:"bloom_filter"`
    History     HistoryConfig     `yaml:"history"`
}

type ResilienceConfig struct {
    Enabled    bool          `yaml:"enabled"`
    Session    SessionConfig `yaml:"session"`
    EventCache CacheConfig   `yaml:"event_cache"`
}
```

### 6.2 Example Config

```yaml
# Multi-Chain Configuration
multichain:
  enabled: true
  chains:
    - id: "stableone-mainnet"
      name: "StableOne Mainnet"
      rpc_endpoint: "http://localhost:8545"
      ws_endpoint: "ws://localhost:8546"
      chain_id: 1
      adapter_type: "stableone"
      start_height: 0
      enabled: true
    - id: "ethereum-sepolia"
      name: "Ethereum Sepolia"
      rpc_endpoint: "https://sepolia.infura.io/v3/YOUR_KEY"
      chain_id: 11155111
      adapter_type: "evm"
      start_height: 0
      enabled: false

# Address Watchlist Configuration
watchlist:
  enabled: true
  bloom_filter:
    expected_items: 100000
    false_positive_rate: 0.0001
  history:
    retention: 720h  # 30 days

# WebSocket Resilience Configuration
resilience:
  enabled: true
  session:
    ttl: 24h
    cleanup_period: 1h
  event_cache:
    window: 1h
    backend: "pebble"  # or "redis"
```

---

## 7. 구현 일정

| Phase | 기간 | 내용 |
|-------|------|------|
| 1 | Week 1-2 | Multi-Chain Manager 기반 구현 |
| 2 | Week 2-3 | Address Watchlist Service |
| 3 | Week 3-4 | WebSocket Resilience |
| 4 | Week 4-5 | API Integration (GraphQL + WS) |
| 5 | Week 5-6 | Testing & Documentation |

---

## 8. 성공 기준

1. **Multi-Chain**: 3개 이상 체인 동시 인덱싱 가능
2. **Watchlist**: 100,000 주소 등록, <100ms 이벤트 알림
3. **Resilience**: 1시간 내 재연결 시 100% 이벤트 복구
4. **API**: GraphQL/WS 모든 엔드포인트 정상 동작
5. **Tests**: 85% 이상 테스트 커버리지

---

## 9. 관련 문서

- [ARCHITECTURE.md](./ARCHITECTURE.md) - 기존 시스템 아키텍처
- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - 이벤트 구독 API
- [FRONTEND_SUBSCRIPTION_GUIDE.md](./FRONTEND_SUBSCRIPTION_GUIDE.md) - 프론트엔드 가이드
