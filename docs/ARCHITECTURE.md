# Architecture

## Overview

**indexer-go**는 Stable-One (Ethereum 호환) 블록체인을 위한 고성능 인덱서입니다. 블록, 트랜잭션, 영수증, 로그를 실시간으로 수집/인덱싱하고, GraphQL, JSON-RPC, WebSocket API를 통해 데이터를 제공합니다.

---

## Project Structure

```
indexer-go/
├── cmd/indexer/
│   └── main.go                    # 진입점, App 초기화, CLI 플래그
│
├── internal/
│   ├── config/                    # 설정 관리 (YAML + ENV + CLI)
│   ├── constants/                 # 체인 ID, 시스템 컨트랙트 주소
│   ├── logger/                    # zap 로거 초기화
│   └── testutil/                  # 테스트 유틸리티
│
├── pkg/
│   ├── abi/                       # ABI 디코딩 (로그 이벤트 파싱)
│   ├── adapters/                  # 체인 어댑터
│   │   ├── anvil/                 #   Anvil (로컬 개발)
│   │   ├── detector/              #   체인 자동 감지
│   │   ├── evm/                   #   범용 EVM
│   │   ├── factory/               #   어댑터 팩토리
│   │   └── stableone/             #   Stable-One (WBFT)
│   │
│   ├── api/                       # API 레이어
│   │   ├── server.go              #   HTTP 서버 (chi 라우터)
│   │   ├── graphql/               #   GraphQL (119 쿼리)
│   │   │   ├── schema.go          #     스키마 빌더
│   │   │   ├── types*.go          #     GraphQL 타입 정의
│   │   │   ├── resolvers*.go      #     리졸버 구현
│   │   │   └── handler.go         #     핸들러/플레이그라운드
│   │   ├── jsonrpc/               #   JSON-RPC (73 메서드)
│   │   │   ├── server.go          #     JSON-RPC 서버
│   │   │   ├── methods.go         #     메서드 라우팅
│   │   │   └── methods_*.go       #     도메인별 메서드 구현
│   │   ├── websocket/             #   WebSocket 서버 (Hub/Client)
│   │   ├── etherscan/             #   Etherscan 호환 API
│   │   └── middleware/            #   HTTP 미들웨어 (CORS, 인증, Rate Limit)
│   │
│   ├── client/                    # Ethereum RPC 클라이언트 래퍼
│   ├── compiler/                  # Solidity 컴파일러 (solc)
│   ├── consensus/                 # 컨센서스 플러그인 시스템
│   │   ├── poa/                   #   PoA 파서
│   │   └── wbft/                  #   WBFT 파서
│   ├── crypto/bls/                # BLS 서명 검증
│   ├── eventbus/                  # EventBus (Local/Redis/Kafka)
│   ├── events/                    # 이벤트 정의, 컨트랙트 등록
│   │
│   ├── fetch/                     # 인덱싱 엔진
│   │   ├── fetcher.go             #   메인 인덱싱 루프
│   │   ├── fetcher_indexing.go    #   블록/트랜잭션 인덱싱
│   │   ├── large_block.go         #   대형 블록 처리
│   │   ├── optimizer.go           #   적응형 성능 최적화
│   │   ├── userop.go              #   EIP-4337 UserOp 프로세서
│   │   └── consensus.go           #   컨센서스 데이터 수집
│   │
│   ├── multichain/                # 멀티체인 매니저
│   ├── notifications/             # 알림 (Webhook/Email/Slack)
│   ├── price/                     # 토큰 가격 데이터
│   ├── resilience/                # WebSocket 복원력
│   ├── rpcproxy/                  # RPC 프록시 (eth_call 등)
│   │
│   ├── storage/                   # 데이터 저장소
│   │   ├── storage.go             #   Storage 인터페이스 (합성)
│   │   ├── schema.go              #   키 스키마 및 생성 함수
│   │   ├── backend.go             #   Backend 인터페이스 (KV)
│   │   ├── pebble_backend.go      #   PebbleDB 백엔드 구현
│   │   ├── pebble.go              #   코어 Reader/Writer 구현
│   │   ├── pebble_address.go      #   주소 인덱싱
│   │   ├── pebble_setcode.go      #   EIP-7702 SetCode
│   │   ├── pebble_userop.go       #   EIP-4337 UserOp
│   │   ├── pebble_consensus.go    #   WBFT 컨센서스
│   │   └── userop.go              #   AA 데이터 모델/인터페이스
│   │
│   ├── token/                     # 토큰 메타데이터 (ERC-20/721)
│   ├── types/                     # 공통 타입 정의
│   │   ├── chain/                 #   체인 어댑터 인터페이스
│   │   └── consensus/             #   컨센서스 타입
│   ├── verifier/                  # 컨트랙트 소스 검증
│   └── watchlist/                 # 주소 감시 서비스
│
├── configs/                       # 환경별 설정 파일
│   ├── config-anvil.yaml
│   └── config-sepolia.yaml
├── deployments/                   # systemd, logrotate, 배포 스크립트
└── e2e/                           # E2E 테스트
```

---

## System Architecture

```
Stable-One Node (RPC)
         ↓
    Client Layer (ethclient)
         ↓
    Fetcher (Worker Pool) ───→ EventBus (Pub/Sub) ───→ WebSocket/Subscription
         │
    ┌────┼─────────────────────────┐
    │    ├─ Address Indexing       │
    │    ├─ Token Transfer Parser  │
    │    ├─ SetCode Processor      │  Processors
    │    ├─ UserOp Processor       │
    │    ├─ Fee Delegation         │
    │    └─ Consensus Parser       │
    └────┬─────────────────────────┘
         ↓
    Storage (PebbleDB)
         ↓
    ┌──────────────────────────────────────┐
    │  API Server (chi router)             │
    │  GraphQL │ JSON-RPC │ WebSocket      │
    │  Etherscan API │ Prometheus Metrics   │
    └──────────────────────────────────────┘
```

---

## Core Components

### 1. Fetcher — 인덱싱 엔진

`pkg/fetch/fetcher.go`

블록체인 데이터를 지속적으로 가져오는 핵심 엔진.

- **Worker Pool**: 병렬 블록 처리 (기본 100 워커)
- **Adaptive Optimizer**: RPC 응답 시간/에러율 기반 워커 수 자동 조정
- **Large Block Processing**: 50MB+ 블록을 청크로 분할 처리
- **Gap Recovery**: 누락 블록 탐지 및 자동 복구
- **Processor Injection**: `SetUserOpProcessor()`, `SetSetCodeProcessor()` 등 setter로 프로세서 주입

**인덱싱 파이프라인:**
```
블록 수집 (RPC) → 트랜잭션/영수증 파싱 → 프로세서 실행 → Storage 저장 → EventBus 발행
```

### 2. Storage — 데이터 저장소

`pkg/storage/`

PebbleDB 기반 Key-Value 저장소. 인터페이스 분리(ISP) 원칙 적용.

**인터페이스 합성:**
```go
type Storage interface {
    Reader              // 블록/트랜잭션/영수증 읽기
    Writer              // 블록/트랜잭션/영수증 쓰기
    LogReader           // 로그 쿼리
    LogWriter           // 로그 저장
    AddressIndexReader  // 주소별 인덱스 읽기
    AddressIndexWriter  // 주소별 인덱스 쓰기
    ConsensusReader     // WBFT 컨센서스 읽기
    ConsensusWriter     // WBFT 컨센서스 쓰기
    SetCodeIndexReader  // EIP-7702 SetCode 읽기
    SetCodeIndexWriter  // EIP-7702 SetCode 쓰기
    UserOpIndexReader   // EIP-4337 UserOp 읽기
    UserOpIndexWriter   // EIP-4337 UserOp 쓰기
    // ... (TokenMetadata, TokenHolder, FeeDelegation 등)
}
```

**키 스키마 (`schema.go`):**
- `/data/` 접두사: 기본 데이터 (블록, 트랜잭션, 영수증)
- `/index/` 접두사: 역색인 (주소→트랜잭션, 센더→UserOp)
- 내림차순 정렬: `^blockNumber` (비트 반전)로 최신 데이터 우선 조회

### 3. API Server — HTTP 서버

`pkg/api/server.go`

**엔드포인트:**

| Path | 프로토콜 | 설명 |
|------|---------|------|
| `/graphql` | POST | GraphQL 쿼리 |
| `/graphql/ws` | WebSocket | GraphQL 서브스크립션 |
| `/playground` | GET | GraphQL Playground UI |
| `/rpc` | POST | JSON-RPC |
| `/ws` | WebSocket | 실시간 구독 |
| `/api` | GET/POST | Etherscan 호환 API |
| `/health` | GET | 헬스체크 |
| `/version` | GET | 버전 정보 |
| `/metrics` | GET | Prometheus 메트릭 |
| `/subscribers` | GET | EventBus 구독자 통계 |

**미들웨어 체인:** Recovery → RequestID → RealIP → Logger → RateLimit → APIKeyAuth → CORS

### 4. EventBus — 실시간 이벤트 시스템

`pkg/eventbus/`

**백엔드:** Local (기본), Redis, Kafka, Hybrid

**이벤트 타입:**
- `EventTypeBlock` — 새 블록
- `EventTypeTransaction` — 새 트랜잭션
- `EventTypeLog` — 새 로그
- `EventTypeConsensusBlock` — 컨센서스 데이터
- `EventTypeSystemContract` — 시스템 컨트랙트 이벤트

**기능:**
- 동기식 구독 등록 (race condition 방지)
- 이벤트 히스토리 버퍼링 (기본 100개)
- Replay: 구독 시 과거 이벤트 즉시 수신

### 5. Chain Adapters — 체인 어댑터

`pkg/adapters/`

플러그 가능한 어댑터 시스템으로 다양한 EVM 체인 지원.

```
chain.Adapter 인터페이스
    ├── evm.Adapter        (범용 EVM — Ethereum, Sepolia 등)
    ├── stableone.Adapter  (Stable-One — WBFT + 시스템 컨트랙트)
    └── anvil.Adapter      (로컬 개발)
```

`detector` 패키지가 체인 ID와 특성 기반으로 어댑터를 자동 선택.

---

## Feature Modules

### EIP-4337 Account Abstraction

**아키텍처:** Event-Only 파싱 (EntryPoint 로그에서 데이터 추출)

```
EntryPoint Contract Logs
    ├── UserOperationEvent      → UserOperationRecord
    ├── AccountDeployed         → AccountDeployedRecord
    ├── UserOperationRevertReason → UserOpRevertRecord
    └── PostOpRevertReason      → UserOpRevertRecord

Bundler = tx.From() of handleOps transaction
```

- `pkg/fetch/userop.go` — UserOpProcessor (이벤트 파싱, 배치 저장)
- `pkg/storage/userop.go` — 데이터 모델, 인터페이스 (UserOpIndexReader 15메서드, Writer 6메서드)
- `pkg/storage/pebble_userop.go` — PebbleDB 구현 (8개 인덱스/UserOp)
- `pkg/api/graphql/resolvers_userop.go` — GraphQL 리졸버 (14쿼리)

### EIP-7702 SetCode Delegation

- SetCode authorization 레코드 인덱싱
- 주소별 delegation 상태 추적
- target, authority, 블록, 트랜잭션별 쿼리

### WBFT Consensus

- 블록 Extra Data에서 WBFT 메타데이터 추출
- 검증자 서명 통계 (prepare/commit/miss)
- 에폭 정보 및 검증자 참여율 추적

---

## Data Flow

### Indexing Pipeline
```
1. FETCH    Fetcher.Run() → RPC로 블록/영수증 가져오기
2. PARSE    트랜잭션, 로그, 컨센서스 데이터 파싱
3. PROCESS  프로세서 실행 (주소 인덱싱, UserOp, SetCode, 토큰)
4. STORE    PebbleDB에 배치 저장 (원자적 쓰기)
5. PUBLISH  EventBus로 실시간 이벤트 발행
```

### Query Pipeline
```
Client Request → API Layer (GraphQL/JSON-RPC) → Storage Query (PebbleDB) → Response
```

### Startup Sequence
```
 1. CLI 플래그 파싱 + config.yaml 로드
 2. 로거 초기화
 3. Ethereum RPC 클라이언트 생성 + 체인 연결 확인
 4. 체인 어댑터 자동 감지/초기화
 5. PebbleDB 스토리지 초기화
 6. 마지막 인덱싱 높이 조회 (재개 지점)
 7. EventBus 초기화
 8. Fetcher + Processors 초기화
 9. API 서버 초기화 (선택)
10. Fetcher 고루틴 시작
11. API 서버 고루틴 시작
12. 종료 시그널(SIGINT/SIGTERM) 대기 → Graceful Shutdown
```

---

## Design Principles

1. **Interface Segregation** — 인터페이스 분리로 의존성 최소화 (Storage, Adapter, Consensus)
2. **Composition** — 인터페이스 합성으로 기능 조합 (Storage = Reader + Writer + ...)
3. **Dependency Injection** — Setter 기반 프로세서 주입 (SetUserOpProcessor 등)
4. **Plugin System** — 컨센서스 파서, 체인 어댑터 플러그인 레지스트리
5. **Batch Operations** — 원자적 배치 쓰기로 I/O 최적화
6. **Adaptive Performance** — RPC 성능 기반 워커/배치 크기 자동 조정
7. **Graceful Degradation** — 재시도, 갭 복구, 에러 격리
