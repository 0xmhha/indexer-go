# Multi-Chain Manager API Reference

Multi-Chain Manager를 사용한 여러 블록체인 동시 인덱싱 가이드입니다.

**Last Updated**: 2026-01-31

---

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [GraphQL API](#graphql-api)
- [Chain Lifecycle](#chain-lifecycle)
- [Health Monitoring](#health-monitoring)
- [Error Handling](#error-handling)
- [Best Practices](#best-practices)

---

## Overview

Multi-Chain Manager는 단일 인덱서 인스턴스에서 여러 블록체인을 동시에 관리합니다:

- **동적 체인 등록/해제** - 런타임에 체인 추가/제거
- **독립적 라이프사이클** - 각 체인 개별 시작/중지
- **자동 복구** - 실패한 체인 자동 재시작
- **Health Check** - 체인별 상태 모니터링
- **Sync Progress** - 실시간 동기화 진행률

### Chain Status Flow

```
REGISTERED → STARTING → SYNCING → ACTIVE
                ↓          ↓        ↓
              ERROR ←────────────────┘
                ↓
            STOPPING → STOPPED
```

---

## Quick Start

### 1. Configuration 설정

```yaml
# config.yaml
multichain:
  enabled: true
  health_check_interval: 30s
  max_unhealthy_duration: 5m
  auto_restart: true
  auto_restart_delay: 30s
  chains:
    - id: "ethereum-mainnet"
      name: "Ethereum Mainnet"
      rpc_endpoint: "https://eth.llamarpc.com"
      chain_id: 1
      adapter_type: "evm"
      start_height: 0
      enabled: true
    - id: "stableone-mainnet"
      name: "StableOne Mainnet"
      rpc_endpoint: "http://localhost:8545"
      ws_endpoint: "ws://localhost:8546"
      chain_id: 1234
      adapter_type: "stableone"
      start_height: 0
      enabled: true
```

### 2. GraphQL로 체인 관리

```graphql
# 체인 목록 조회
query {
  chains {
    id
    name
    status
    syncProgress {
      percentage
      isSynced
    }
  }
}

# 새 체인 등록
mutation {
  registerChain(input: {
    name: "Polygon Mainnet"
    rpcEndpoint: "https://polygon-rpc.com"
    chainId: "137"
    adapterType: "evm"
    startHeight: "0"
  }) {
    id
    status
  }
}

# 체인 시작
mutation {
  startChain(id: "polygon-mainnet") {
    id
    status
  }
}
```

---

## Configuration

### MultiChainConfig

| 필드 | 타입 | 기본값 | 설명 |
|------|------|--------|------|
| `enabled` | bool | false | Multi-chain 모드 활성화 |
| `health_check_interval` | duration | 30s | Health check 주기 |
| `max_unhealthy_duration` | duration | 5m | Unhealthy 허용 시간 |
| `auto_restart` | bool | false | 실패 체인 자동 재시작 |
| `auto_restart_delay` | duration | 30s | 재시작 대기 시간 |
| `chains` | []ChainConfig | - | 체인 설정 목록 |

### ChainConfig

| 필드 | 타입 | 필수 | 설명 |
|------|------|------|------|
| `id` | string | ✓ | 고유 체인 식별자 |
| `name` | string | ✓ | 표시용 이름 |
| `rpc_endpoint` | string | ✓ | JSON-RPC 엔드포인트 |
| `ws_endpoint` | string | - | WebSocket 엔드포인트 |
| `chain_id` | uint64 | ✓ | EVM Chain ID |
| `adapter_type` | string | - | 어댑터 타입 (auto/evm/stableone/anvil) |
| `start_height` | uint64 | - | 인덱싱 시작 블록 |
| `enabled` | bool | - | 체인 활성화 여부 |

### Adapter Types

| 타입 | 설명 |
|------|------|
| `auto` | 자동 감지 (기본값) |
| `evm` | 표준 EVM 체인 |
| `stableone` | StableOne (WBFT consensus) |
| `anvil` | Anvil 로컬 개발 환경 |

---

## GraphQL API

### Queries

#### chains

모든 체인 목록을 조회합니다.

```graphql
query {
  chains {
    id
    name
    chainId
    status
    enabled
    latestHeight
    health {
      healthy
      latency
      lastCheck
    }
    syncProgress {
      currentBlock
      targetBlock
      percentage
      blocksPerSecond
      isSynced
    }
  }
}
```

#### chain

특정 체인 정보를 조회합니다.

```graphql
query {
  chain(id: "ethereum-mainnet") {
    id
    name
    status
    syncProgress {
      percentage
      estimatedTimeRemaining
    }
  }
}
```

### Mutations

#### registerChain

새 체인을 등록합니다.

```graphql
mutation {
  registerChain(input: {
    name: "Arbitrum One"
    rpcEndpoint: "https://arb1.arbitrum.io/rpc"
    chainId: "42161"
    adapterType: "evm"
    startHeight: "0"
  }) {
    id
    status
  }
}
```

**Response**:
```json
{
  "data": {
    "registerChain": {
      "id": "arbitrum-one",
      "status": "REGISTERED"
    }
  }
}
```

#### startChain

등록된 체인을 시작합니다.

```graphql
mutation {
  startChain(id: "arbitrum-one") {
    id
    status
  }
}
```

#### stopChain

실행 중인 체인을 중지합니다.

```graphql
mutation {
  stopChain(id: "arbitrum-one") {
    id
    status
  }
}
```

#### unregisterChain

체인을 등록 해제합니다 (중지 후 제거).

```graphql
mutation {
  unregisterChain(id: "arbitrum-one")
}
```

### Subscriptions

#### chainStatus

체인 상태 변경을 실시간으로 구독합니다.

```graphql
subscription {
  chainStatus(chainId: "ethereum-mainnet") {
    id
    status
    syncProgress {
      percentage
      isSynced
    }
  }
}
```

---

## Chain Lifecycle

### Status 정의

| Status | 설명 |
|--------|------|
| `REGISTERED` | 등록됨, 시작 대기 |
| `STARTING` | 시작 중 (어댑터/Fetcher 초기화) |
| `SYNCING` | 동기화 진행 중 |
| `ACTIVE` | 정상 운영 중 (실시간 인덱싱) |
| `STOPPING` | 중지 처리 중 |
| `STOPPED` | 중지됨 |
| `ERROR` | 오류 발생 |

### Lifecycle Methods

```go
// Manager 생성
manager, err := multichain.NewManager(config, storage, eventBus, logger)

// 전체 시작 (설정된 모든 체인)
err := manager.Start(ctx)

// 개별 체인 등록
chainID, err := manager.RegisterChain(ctx, &ChainConfig{...})

// 개별 체인 시작
err := manager.StartChain(ctx, "chain-id")

// 개별 체인 중지
err := manager.StopChain(ctx, "chain-id")

// 체인 등록 해제
err := manager.UnregisterChain(ctx, "chain-id")

// 전체 중지
err := manager.Stop(ctx)
```

---

## Health Monitoring

### HealthStatus

```go
type HealthStatus struct {
    ChainID       string        // 체인 ID
    Healthy       bool          // 건강 상태
    Latency       time.Duration // RPC 응답 시간
    LastCheck     time.Time     // 마지막 체크 시간
    LatestHeight  uint64        // 체인 최신 블록
    IndexedHeight uint64        // 인덱싱된 블록
    SyncLag       uint64        // 동기화 지연 블록 수
    ErrorMessage  string        // 오류 메시지 (있는 경우)
}
```

### Health Check GraphQL

```graphql
query {
  chains {
    id
    health {
      healthy
      latency
      lastCheck
      latestHeight
      indexedHeight
      syncLag
      errorMessage
    }
  }
}
```

### Auto-Restart 동작

1. `health_check_interval` 주기로 상태 확인
2. `max_unhealthy_duration` 초과 시 체인 중지
3. `auto_restart: true`인 경우 `auto_restart_delay` 후 재시작

---

## Error Handling

### Error Types

| 에러 | 설명 |
|------|------|
| `ErrChainNotFound` | 체인을 찾을 수 없음 |
| `ErrChainAlreadyExists` | 동일 ID 체인이 이미 존재 |
| `ErrChainNotRunning` | 체인이 실행 중이 아님 |
| `ErrChainAlreadyRunning` | 체인이 이미 실행 중 |
| `ErrStorageRequired` | 스토리지가 필요함 |
| `ErrInvalidConfig` | 잘못된 설정 |

### GraphQL Error Response

```json
{
  "errors": [
    {
      "message": "chain not found: invalid-chain",
      "path": ["chain"],
      "extensions": {
        "code": "CHAIN_NOT_FOUND"
      }
    }
  ]
}
```

---

## Best Practices

### 1. 체인 ID 명명 규칙

```yaml
# 권장: 네트워크-환경
chains:
  - id: "ethereum-mainnet"
  - id: "ethereum-sepolia"
  - id: "polygon-mainnet"
  - id: "stableone-testnet"
```

### 2. Resource 관리

```yaml
# 체인 수에 따른 리소스 조정
multichain:
  enabled: true
  # 체인이 많을수록 health check 간격 늘리기
  health_check_interval: 60s  # 5+ chains

storage:
  # 체인별 prefix로 데이터 격리됨
  # chain:<chainID>:block:<height>
```

### 3. 점진적 체인 추가

```graphql
# 1. 먼저 등록만
mutation {
  registerChain(input: {...}) { id status }
}

# 2. 설정 확인 후 시작
mutation {
  startChain(id: "new-chain") { id status }
}

# 3. 동기화 진행 모니터링
subscription {
  chainStatus(chainId: "new-chain") {
    syncProgress { percentage isSynced }
  }
}
```

### 4. Error Recovery

```yaml
multichain:
  auto_restart: true
  auto_restart_delay: 30s      # 즉시 재시작 방지
  max_unhealthy_duration: 5m   # 일시적 장애 허용
```

---

## 참고 문서

- [ARCHITECTURE.md](./ARCHITECTURE.md) - 시스템 아키텍처
- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - 이벤트 구독
- [WATCHLIST_API.md](./WATCHLIST_API.md) - 주소 모니터링
- [WEBSOCKET_RESILIENCE.md](./WEBSOCKET_RESILIENCE.md) - WebSocket 복구
