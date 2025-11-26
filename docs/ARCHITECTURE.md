# Indexer-Go Architecture

## Overview

**indexer-go**는 이더리움 호환 블록체인(Stable One)을 위한 블록체인 인덱서입니다.

### 핵심 기능
- 블록체인 데이터(블록, 트랜잭션, 영수증, 로그) 수집 및 인덱싱
- GraphQL, JSON-RPC, WebSocket API 제공
- WBFT 컨센서스 데이터 처리
- 시스템 컨트랙트 이벤트 추적

---

## Project Structure

```
indexer-go/
├── cmd/indexer/main.go     # 진입점
├── api/                    # API 레이어
│   ├── graphql/           # GraphQL 구현
│   ├── jsonrpc/           # JSON-RPC 구현
│   └── websocket/         # WebSocket 구현
├── fetch/                  # 데이터 수집 엔진
│   ├── fetcher.go         # 메인 인덱싱 로직
│   ├── optimizer.go       # 성능 자동 최적화
│   └── consensus.go       # 컨센서스 데이터 수집
├── storage/               # 저장소 레이어 (Pebble DB)
├── types/                 # 데이터 타입 정의
├── client/                # 이더리움 RPC 클라이언트
├── events/                # 이벤트 버스 (실시간 알림)
└── internal/config/       # 설정 관리
```

---

## Core Components

### 1. Fetcher (`fetch/fetcher.go`)

블록체인 데이터를 지속적으로 가져오는 **핵심 엔진**

**역할:**
- RPC를 통해 블록/트랜잭션/영수증 수집
- 병렬 워커 풀로 효율적 처리
- 재시도 로직 및 에러 복구
- 실시간 이벤트 발행

**주요 메서드:**
- `FetchBlock()` - 단일 블록 수집 (재시도 포함)
- `Run()` - 연속 인덱싱 루프
- `RunWithGapRecovery()` - 갭 탐지 및 복구

### 2. Storage (`storage/pebble.go`)

**Pebble DB** 기반 데이터 저장소

**저장 데이터:**
- 블록 메타데이터 및 해시
- 트랜잭션 (해시 → 위치 인덱스)
- 영수증 및 로그
- 주소별 트랜잭션 인덱스

**인터페이스:**
```go
type Reader interface {
    GetLatestHeight(ctx) (uint64, error)
    GetBlock(ctx, height) (*Block, error)
    GetTransaction(ctx, hash) (*Tx, *Location, error)
    GetReceipt(ctx, hash) (*Receipt, error)
    // ...
}

type Writer interface {
    SetLatestHeight(ctx, height) error
    SetBlock(ctx, block) error
    SetTransaction(ctx, tx, location) error
    SetReceipt(ctx, receipt) error
    // ...
}
```

### 3. API Server (`api/server.go`)

외부 쿼리를 위한 **HTTP 서버**

**엔드포인트:**
| Path | Description |
|------|-------------|
| `/graphql` | GraphQL 쿼리 |
| `/playground` | GraphQL 테스트 UI |
| `/rpc` | JSON-RPC |
| `/ws` | WebSocket 구독 |
| `/health` | 헬스 체크 |
| `/metrics` | Prometheus 메트릭 |

### 4. Event Bus (`events/bus.go`)

실시간 알림을 위한 **Pub/Sub 시스템**

**이벤트 타입:**
- `EVENT_NEW_BLOCK` - 새 블록 인덱싱됨
- `EVENT_NEW_TRANSACTION` - 새 트랜잭션 인덱싱됨
- `EVENT_NEW_LOGS` - 새 로그 발생

---

## Data Flow

### Indexing Pipeline

```
┌─────────────────────────────────────────────────────────────┐
│                    Indexing Cycle                           │
└─────────────────────────────────────────────────────────────┘

1. FETCH (수집)
   ┌────────────────────────────┐
   │ Fetcher.Run()              │
   │ - 마지막 인덱싱 높이 확인      │
   │ - 다음 블록 RPC로 가져오기    │
   └────────────────────────────┘
              ↓
2. PARSE (파싱)
   ┌────────────────────────────┐
   │ 블록 데이터 파싱              │
   │ - 트랜잭션 추출              │
   │ - 영수증 파싱               │
   │ - 로그 추출                 │
   └────────────────────────────┘
              ↓
3. STORE (저장)
   ┌────────────────────────────┐
   │ Storage.SetBlock()         │
   │ Storage.SetTransaction()   │
   │ Storage.SetReceipt()       │
   │ Storage.SetLatestHeight()  │
   └────────────────────────────┘
              ↓
4. PUBLISH (발행)
   ┌────────────────────────────┐
   │ EventBus.Publish()         │
   │ - 구독자에게 알림            │
   │ - WebSocket 클라이언트       │
   │ - GraphQL 구독              │
   └────────────────────────────┘
```

### Query Pipeline

```
User Request
     ↓
┌──────────────────────┐
│ API Layer            │
│ (GraphQL/JSON-RPC)   │
└──────────────────────┘
     ↓
┌──────────────────────┐
│ Storage Query        │
│ - Pebble DB 조회      │
└──────────────────────┘
     ↓
┌──────────────────────┐
│ Response Formatting  │
│ - JSON/GraphQL 직렬화 │
└──────────────────────┘
     ↓
Client Response
```

---

## Startup Sequence

```
 1. CLI 플래그 파싱
 2. 설정 파일 + 환경변수 로드
 3. 설정 유효성 검증
 4. 로거 초기화
 5. 이더리움 RPC 클라이언트 생성
 6. 체인 연결 확인 (chain ID 조회)
 7. Pebble DB 스토리지 초기화
 8. Genesis 밸런스 초기화 래퍼 적용
 9. 마지막 인덱싱 높이 조회 (재개 지점)
10. EventBus 초기화
11. Fetcher 초기화
12. (선택) API 서버 초기화
13. Fetcher 고루틴 시작
14. API 서버 고루틴 시작
15. 종료 시그널 대기
16. Graceful Shutdown
```

---

## Configuration

### 설정 소스 (우선순위)
1. 기본값 (Built-in)
2. YAML 파일 (`config.yaml`)
3. 환경변수 (Override)
4. CLI 플래그 (Override)

### 주요 설정 항목

```yaml
rpc:
  endpoint: "http://localhost:8545"   # RPC 엔드포인트
  timeout: "10s"                      # 요청 타임아웃

database:
  path: "./data"                      # Pebble DB 경로
  readonly: false

log:
  level: "info"                       # debug, info, warn, error
  format: "json"                      # json or console

indexer:
  workers: 100                        # 병렬 워커 수
  chunk_size: 10                      # 배치당 블록 수
  start_height: 0                     # 시작 블록

api:
  enabled: true
  host: "0.0.0.0"
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
  enable_cors: true
```

---

## WBFT Consensus

Stable One의 **WBFT (Weighted Byzantine Fault Tolerance)** 컨센서스 처리

### 데이터 흐름

```
블록 Extra 데이터 추출
        ↓
┌─────────────────────────┐
│ WBFT 메타데이터 파싱      │
│ - RANDAO reveal         │
│ - 집계된 서명            │
│ - 에폭 정보              │
└─────────────────────────┘
        ↓
검증자 통계 계산
        ↓
┌─────────────────────────┐
│ 검증자별 추적:           │
│ - 서명 횟수 (prepare)    │
│ - 서명 횟수 (commit)     │
│ - 미참여 횟수            │
│ - 참여율                │
└─────────────────────────┘
```

### 관련 타입

```go
// types/consensus/
type WBFTBlockExtra struct {
    RandaoReveal []byte
    AggregatedSig []byte
    EpochInfo *EpochInfo
}

type ValidatorSigningStats struct {
    PrepareSignCount uint64
    CommitSignCount  uint64
    MissCount        uint64
    ParticipationRate float64
}
```

---

## Performance Optimization

### Adaptive Optimizer (`fetch/optimizer.go`)

RPC 성능에 따라 파라미터 자동 조정

```
Performance Metrics Feedback Loop
┌──────────────────────────────────┐
│ Monitor RPC Performance          │
│ - 에러율                          │
│ - 응답 시간                       │
│ - Rate Limit 감지                │
└──────────────────────────────────┘
         ↓
┌──────────────────────────────────┐
│ Calculate Optimal Parameters     │
│ - 워커 수 (1-1000)               │
│ - 배치 크기 (5-50)               │
└──────────────────────────────────┘
         ↓
┌──────────────────────────────────┐
│ Apply Adjustments                │
│ - 성능 좋으면 워커 증가            │
│ - Rate Limit 시 워커 감소         │
│ - 처리량/에러 균형 유지            │
└──────────────────────────────────┘
```

### Large Block Processing (`fetch/large_block.go`)

50MB 이상 블록 효율적 처리:
- 작은 청크로 분할
- 점진적 저장으로 메모리 스파이크 방지
- 스트리밍 영수증 처리

### Batch Operations

그룹화된 스토리지 쓰기:
- I/O 작업 감소
- 원자적 업데이트
- 처리량 향상

---

## Error Handling

### Retry Logic

```
Fetch 실패
    ↓
재시도 (MaxRetries까지)
    ↓
지수 백오프 적용 (delay * 2^attempt)
    ↓
성공 → 저장 & 계속
실패 → 로깅 & 일시 중지
```

### Gap Recovery

- 누락된 블록 주기적 확인
- 시작 시 `RunWithGapRecovery()`로 갭 채우기
- 네트워크 중단 시 자동 복구

### Rate Limiting

- 429 응답 메트릭 추적
- Optimizer가 워커 수 감소
- 배치 크기 축소
- 지수 백오프 적용

---

## GraphQL API

### Core Queries

```graphql
# 기본 쿼리
latestHeight: BigInt!
block(number: BigInt!): Block
transaction(hash: Hash!): Transaction
receipt(transactionHash: Hash!): Receipt
logs(filter: LogFilter!): LogConnection!
```

### Address Indexing

```graphql
transactionsByAddress(address: Address!): TransactionConnection!
contractCreation(address: Address!): ContractCreation
contractVerification(address: Address!): ContractVerification
erc20TransfersByAddress(address: Address!): ERC20TransferConnection!
```

### Consensus Queries

```graphql
wbftBlockExtra(blockNumber: BigInt!): WBFTBlockExtra
epochInfo(epochNumber: BigInt!): EpochInfo
validatorSigningStats(validatorAddress: Address!): ValidatorSigningStats
blockSigners(blockNumber: BigInt!): BlockSigners
```

### Real-time Subscriptions

```graphql
subscription {
  newBlock { hash number timestamp ... }
  newTransaction { hash blockNumber ... }
  logs(filter: LogFilter!) { address topics data ... }
}
```

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `cmd/indexer/main.go` | 진입점, 초기화 |
| `fetch/fetcher.go` | 인덱싱 엔진 |
| `fetch/optimizer.go` | 성능 자동 최적화 |
| `fetch/consensus.go` | 컨센서스 데이터 수집 |
| `storage/storage.go` | 스토리지 인터페이스 |
| `storage/pebble.go` | Pebble DB 구현 |
| `storage/schema.go` | 키 인코딩 스키마 |
| `api/server.go` | HTTP 서버 |
| `api/graphql/schema.graphql` | GraphQL 스키마 |
| `api/graphql/resolvers_*.go` | GraphQL 리졸버 |
| `events/bus.go` | 이벤트 시스템 |
| `client/client.go` | RPC 클라이언트 |
| `types/types.go` | 핵심 타입 정의 |
| `types/consensus/` | 컨센서스 타입 |

---

## Design Principles

1. **Modular Design** - 관심사 분리된 패키지 구조
2. **Interface-Based** - 구현이 아닌 인터페이스에 의존
3. **Composable** - 의존성 주입을 통한 컴포넌트 조합
4. **Resilient** - 재시도, 에러 처리, 그레이스풀 디그레이데이션
5. **Observable** - 로깅, 메트릭, 헬스체크
6. **Scalable** - 워커 풀, 배치 연산, 적응형 최적화
