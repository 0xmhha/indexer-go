# Address Watchlist API Reference

특정 주소의 트랜잭션 및 이벤트를 실시간으로 모니터링하는 Watchlist API 가이드입니다.

**Last Updated**: 2026-01-31

---

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [GraphQL API](#graphql-api)
- [Event Types](#event-types)
- [Filtering](#filtering)
- [Real-time Subscriptions](#real-time-subscriptions)
- [Best Practices](#best-practices)

---

## Overview

Address Watchlist는 특정 주소의 활동을 실시간으로 추적합니다:

- **다양한 이벤트 타입** - TX 송신/수신, ERC20/ERC721 Transfer, Contract Logs
- **Bloom Filter 최적화** - 100,000+ 주소 모니터링 지원
- **실시간 알림** - GraphQL Subscription으로 즉시 알림
- **이벤트 히스토리** - 과거 이벤트 조회 (기본 30일 보관)
- **Multi-Chain 지원** - 체인별 독립적 모니터링

### Event Flow

```
Block Indexed → Bloom Filter Check → Match Found → Event Created → Subscribers Notified
```

---

## Quick Start

### 1. Configuration 설정

```yaml
# config.yaml
watchlist:
  enabled: true
  bloom_filter:
    expected_items: 100000      # 예상 모니터링 주소 수
    false_positive_rate: 0.0001 # 허용 오탐률 (0.01%)
  history:
    retention: 720h             # 이벤트 보관 기간 (30일)
```

### 2. 주소 등록 및 구독

```graphql
# 1. 주소 등록
mutation {
  watchAddress(input: {
    address: "0x742d35Cc6634C0532925a3b844Bc9e7595f8fEb6"
    chainId: "ethereum-mainnet"
    label: "My Wallet"
    filter: {
      txFrom: true
      txTo: true
      erc20: true
      erc721: false
      logs: false
    }
  }) {
    id
    address
    filter {
      txFrom
      txTo
      erc20
    }
  }
}

# 2. 실시간 이벤트 구독
subscription {
  watchedAddressEvents(addressId: "wl_abc123") {
    id
    eventType
    txHash
    blockNumber
    data
    timestamp
  }
}
```

---

## Configuration

### WatchlistConfig

| 필드 | 타입 | 기본값 | 설명 |
|------|------|--------|------|
| `enabled` | bool | false | Watchlist 서비스 활성화 |
| `bloom_filter.expected_items` | int | 100000 | 예상 모니터링 주소 수 |
| `bloom_filter.false_positive_rate` | float | 0.0001 | Bloom Filter 오탐률 |
| `history.retention` | duration | 720h | 이벤트 보관 기간 |

### Bloom Filter 최적화

Bloom Filter는 대량 주소 모니터링 시 성능을 최적화합니다:

| 주소 수 | 메모리 사용량 | 검색 시간 |
|---------|--------------|----------|
| 10,000 | ~120KB | O(k) |
| 100,000 | ~1.2MB | O(k) |
| 1,000,000 | ~12MB | O(k) |

k = hash 함수 수 (일반적으로 7-10)

---

## GraphQL API

### Queries

#### watchedAddresses

등록된 모든 모니터링 주소를 조회합니다.

```graphql
query {
  watchedAddresses(
    chainId: "ethereum-mainnet"  # optional
    limit: 10
    offset: 0
  ) {
    id
    address
    chainId
    label
    filter {
      txFrom
      txTo
      erc20
      erc721
      logs
      minValue
    }
    stats {
      totalEvents
      eventsLast24h
      lastEventAt
    }
    createdAt
  }
}
```

#### watchedAddress

특정 모니터링 주소 정보를 조회합니다.

```graphql
query {
  watchedAddress(id: "wl_abc123") {
    id
    address
    label
    recentEvents(limit: 10) {
      eventType
      txHash
      blockNumber
      timestamp
    }
  }
}
```

#### watchEvents

조건에 맞는 이벤트를 조회합니다.

```graphql
query {
  watchEvents(
    addressId: "wl_abc123"
    eventType: TX_FROM
    limit: 20
    offset: 0
  ) {
    id
    eventType
    txHash
    blockNumber
    logIndex
    data
    timestamp
  }
}
```

### Mutations

#### watchAddress

새 주소를 모니터링 목록에 추가합니다.

```graphql
mutation {
  watchAddress(input: {
    address: "0x742d35Cc6634C0532925a3b844Bc9e7595f8fEb6"
    chainId: "ethereum-mainnet"
    label: "Exchange Hot Wallet"
    filter: {
      txFrom: true
      txTo: true
      erc20: true
      erc721: true
      logs: true
      minValue: "1000000000000000000"  # 1 ETH minimum
    }
  }) {
    id
    address
    filter {
      txFrom
      erc20
      minValue
    }
  }
}
```

**Response**:
```json
{
  "data": {
    "watchAddress": {
      "id": "wl_abc123",
      "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f8fEb6",
      "filter": {
        "txFrom": true,
        "erc20": true,
        "minValue": "1000000000000000000"
      }
    }
  }
}
```

#### updateWatchFilter

모니터링 필터를 업데이트합니다.

```graphql
mutation {
  updateWatchFilter(
    id: "wl_abc123"
    filter: {
      txFrom: true
      txTo: true
      erc20: true
      erc721: false    # ERC721 모니터링 해제
      logs: false
      minValue: null   # 최소 금액 제한 해제
    }
  ) {
    id
    filter {
      txFrom
      txTo
      erc20
      erc721
    }
  }
}
```

#### unwatchAddress

주소 모니터링을 해제합니다.

```graphql
mutation {
  unwatchAddress(id: "wl_abc123")
}
```

### Subscriptions

#### watchedAddressEvents

특정 주소의 이벤트를 실시간으로 구독합니다.

```graphql
subscription {
  watchedAddressEvents(addressId: "wl_abc123") {
    id
    eventType
    txHash
    blockNumber
    logIndex
    data
    timestamp
  }
}
```

#### watchedAddressEventsByChain

특정 체인의 모든 watchlist 이벤트를 구독합니다.

```graphql
subscription {
  watchedAddressEventsByChain(chainId: "ethereum-mainnet") {
    id
    addressId
    eventType
    txHash
    data
  }
}
```

---

## Event Types

### WatchEventType

| 타입 | 설명 | 필터 필드 |
|------|------|----------|
| `TX_FROM` | 주소가 송신자인 트랜잭션 | `txFrom: true` |
| `TX_TO` | 주소가 수신자인 트랜잭션 | `txTo: true` |
| `ERC20_TRANSFER` | ERC20 토큰 전송 (from/to) | `erc20: true` |
| `ERC721_TRANSFER` | ERC721 NFT 전송 (from/to) | `erc721: true` |
| `LOG` | 주소가 발생시킨 모든 로그 | `logs: true` |

### Event Data Structures

#### TX_FROM / TX_TO

```json
{
  "from": "0x742d35Cc6634C0532925a3b844Bc9e7595f8fEb6",
  "to": "0x1234567890abcdef1234567890abcdef12345678",
  "value": "1000000000000000000",
  "gasUsed": "21000",
  "gasPrice": "50000000000",
  "nonce": 42,
  "input": "0x"
}
```

#### ERC20_TRANSFER

```json
{
  "token": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
  "tokenName": "USD Coin",
  "tokenSymbol": "USDC",
  "tokenDecimals": 6,
  "from": "0x742d35Cc6634C0532925a3b844Bc9e7595f8fEb6",
  "to": "0x1234567890abcdef1234567890abcdef12345678",
  "value": "1000000000"
}
```

#### ERC721_TRANSFER

```json
{
  "token": "0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D",
  "tokenName": "BoredApeYachtClub",
  "tokenSymbol": "BAYC",
  "from": "0x742d35Cc6634C0532925a3b844Bc9e7595f8fEb6",
  "to": "0x1234567890abcdef1234567890abcdef12345678",
  "tokenId": "1234"
}
```

#### LOG

```json
{
  "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f8fEb6",
  "topics": [
    "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
    "0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f8feb6"
  ],
  "data": "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000"
}
```

---

## Filtering

### WatchFilter Options

| 필드 | 타입 | 설명 |
|------|------|------|
| `txFrom` | bool | 주소가 트랜잭션 송신자인 경우 |
| `txTo` | bool | 주소가 트랜잭션 수신자인 경우 |
| `erc20` | bool | ERC20 Transfer 이벤트 (from 또는 to) |
| `erc721` | bool | ERC721 Transfer 이벤트 (from 또는 to) |
| `logs` | bool | 주소 컨트랙트가 발생시킨 모든 로그 |
| `minValue` | BigInt | 최소 트랜잭션 금액 (wei) |

### Filter Examples

#### 대형 트랜잭션만 모니터링

```graphql
mutation {
  watchAddress(input: {
    address: "0x..."
    chainId: "ethereum-mainnet"
    filter: {
      txFrom: true
      txTo: true
      erc20: false
      erc721: false
      logs: false
      minValue: "10000000000000000000"  # 10 ETH 이상
    }
  }) { id }
}
```

#### 토큰 전송만 모니터링

```graphql
mutation {
  watchAddress(input: {
    address: "0x..."
    chainId: "ethereum-mainnet"
    filter: {
      txFrom: false
      txTo: false
      erc20: true
      erc721: true
      logs: false
    }
  }) { id }
}
```

#### 컨트랙트 활동 전체 모니터링

```graphql
mutation {
  watchAddress(input: {
    address: "0x..."  # Contract address
    chainId: "ethereum-mainnet"
    filter: {
      txFrom: true
      txTo: true
      erc20: true
      erc721: true
      logs: true  # 모든 이벤트 로그
    }
  }) { id }
}
```

---

## Real-time Subscriptions

### WebSocket 연결

```javascript
import { createClient } from 'graphql-ws';

const client = createClient({
  url: 'ws://localhost:8080/graphql',
});

// 이벤트 구독
const unsubscribe = client.subscribe(
  {
    query: `
      subscription WatchEvents($addressId: ID!) {
        watchedAddressEvents(addressId: $addressId) {
          id
          eventType
          txHash
          data
          timestamp
        }
      }
    `,
    variables: { addressId: 'wl_abc123' }
  },
  {
    next: (result) => {
      console.log('Event received:', result.data.watchedAddressEvents);
    },
    error: (err) => console.error('Error:', err),
    complete: () => console.log('Subscription completed'),
  }
);

// 구독 해제
unsubscribe();
```

### React Hook 예제

```typescript
import { useSubscription, gql } from '@apollo/client';

const WATCH_EVENTS_SUBSCRIPTION = gql`
  subscription WatchEvents($addressId: ID!) {
    watchedAddressEvents(addressId: $addressId) {
      id
      eventType
      txHash
      blockNumber
      data
      timestamp
    }
  }
`;

function WatchEventsFeed({ addressId }: { addressId: string }) {
  const { data, loading, error } = useSubscription(WATCH_EVENTS_SUBSCRIPTION, {
    variables: { addressId },
  });

  if (loading) return <p>Listening for events...</p>;
  if (error) return <p>Error: {error.message}</p>;

  return (
    <div>
      <h3>Latest Event</h3>
      <pre>{JSON.stringify(data.watchedAddressEvents, null, 2)}</pre>
    </div>
  );
}
```

---

## Best Practices

### 1. 필터 최적화

```graphql
# 좋음: 필요한 이벤트만 필터링
filter: {
  txFrom: true
  txTo: false
  erc20: true
  erc721: false
  logs: false
}

# 나쁨: 모든 이벤트 수신 (높은 노이즈)
filter: {
  txFrom: true
  txTo: true
  erc20: true
  erc721: true
  logs: true
}
```

### 2. Label 활용

```graphql
# 관리 편의를 위해 명확한 label 사용
watchAddress(input: {
  address: "0x..."
  chainId: "ethereum-mainnet"
  label: "Binance Hot Wallet #1"  # 명확한 식별
  filter: {...}
})
```

### 3. 대량 주소 관리

```yaml
# 많은 주소 모니터링 시 bloom filter 조정
watchlist:
  bloom_filter:
    expected_items: 500000       # 예상 주소 수 증가
    false_positive_rate: 0.0001  # 오탐률 유지
```

### 4. 이벤트 히스토리 관리

```yaml
# 필요에 따라 보관 기간 조정
watchlist:
  history:
    retention: 168h  # 7일 (저장 공간 절약)
    # retention: 2160h  # 90일 (장기 분석용)
```

### 5. 에러 처리

```typescript
// GraphQL 에러 처리
const { error } = useSubscription(WATCH_EVENTS_SUBSCRIPTION, {
  variables: { addressId },
  onError: (error) => {
    if (error.message.includes('ADDRESS_NOT_FOUND')) {
      // 주소가 watchlist에 없음
      console.error('Address not in watchlist');
    }
  },
});
```

---

## 참고 문서

- [MULTICHAIN.md](./MULTICHAIN.md) - Multi-Chain 관리
- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - 이벤트 구독 시스템
- [WEBSOCKET_RESILIENCE.md](./WEBSOCKET_RESILIENCE.md) - WebSocket 복구
- [FRONTEND_SUBSCRIPTION_GUIDE.md](./FRONTEND_SUBSCRIPTION_GUIDE.md) - 프론트엔드 통합
