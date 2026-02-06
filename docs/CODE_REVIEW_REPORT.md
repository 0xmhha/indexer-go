# Code Review Report

**Project**: indexer-go
**Review Date**: 2026-02-06
**Reviewer**: Claude Code
**Scope**: SOLID Principles, Clean Code, Extensibility, Readability, Comments, TODO Items

---

## Executive Summary

indexer-go 프로젝트는 전반적으로 **B+ 등급**의 코드 품질을 보여줍니다. 인터페이스 설계와 확장성 측면에서 우수하나, 일부 대형 파일의 분리와 미구현 항목들의 완료가 필요합니다.

| 항목 | 점수 | 비고 |
|------|------|------|
| SOLID 원칙 준수 | B+ | 인터페이스 우수, 구현 파일 분리 필요 |
| Clean Code | B | 대형 파일 및 긴 함수 존재 |
| 확장성 | A- | Adapter 패턴 우수 |
| 가독성 | B+ | 명확한 구조, 일부 복잡한 함수 존재 |
| 주석/문서화 | B | 인터페이스 문서화 양호 |

---

## 1. SOLID 원칙 분석

### 1.1 Single Responsibility Principle (SRP) - Grade: B

**장점:**
- Reader/Writer 인터페이스 분리 (`pkg/storage/storage.go`)
- 도메인별 세분화된 인터페이스 (`LogReader`, `ABIReader`, `AddressIndexReader` 등)
- 체인 추상화 레이어 (`pkg/types/chain/interfaces.go`)

**위반 사항:**

| 파일 | 라인 수 | 문제점 | 권장 조치 |
|------|---------|--------|----------|
| `pkg/storage/pebble.go` | 4,302 | God Object - 너무 많은 책임 | 도메인별 파일 분리 |
| `pkg/fetch/fetcher.go` | 2,579 | 블록 처리, 메타데이터, 이벤트 등 혼재 | 프로세서별 분리 |
| `pkg/api/graphql/resolvers.go` | 2,182 | 모든 리졸버가 단일 파일에 존재 | 도메인별 분리 |

### 1.2 Open/Closed Principle (OCP) - Grade: A-

**장점:**
- Adapter 패턴으로 체인 확장 용이
  ```go
  // 새 체인 추가 시 기존 코드 수정 없이 확장 가능
  type Adapter struct {
      *evm.Adapter  // 기본 EVM 어댑터 임베딩
      // 체인별 추가 필드
  }
  ```
- Factory 패턴으로 어댑터 생성 (`pkg/adapters/factory/`)
- EventBus 백엔드 확장 가능 (Local, Redis, Kafka)

**개선 필요:**
- `processBlockMetadata()` 하드코딩된 처리 로직 → Processor Chain 패턴 권장

### 1.3 Liskov Substitution Principle (LSP) - Grade: A

**장점:**
- 컴파일 타임 인터페이스 검증
  ```go
  var _ chain.Adapter = (*Adapter)(nil)
  ```
- 모든 어댑터가 동일한 인터페이스 구현

**주의 사항:**
- `ConsensusParser()` 반환값 nil 가능성 → 호출자 nil 체크 필요

### 1.4 Interface Segregation Principle (ISP) - Grade: A

**장점:**
- 세분화된 스토리지 인터페이스
  ```go
  type Reader interface { ... }
  type Writer interface { ... }
  type LogReader interface { ... }
  type LogWriter interface { ... }
  type ABIReader interface { ... }
  type ABIWriter interface { ... }
  type AddressIndexReader interface { ... }
  type AddressIndexWriter interface { ... }
  type HistoricalReader interface { ... }
  type HistoricalWriter interface { ... }
  ```
- Optional 인터페이스 패턴
  ```go
  if fdStorage, ok := f.storage.(FeeDelegationStorage); ok {
      // Feature 지원 시에만 실행
  }
  ```

### 1.5 Dependency Inversion Principle (DIP) - Grade: A-

**장점:**
- 모든 주요 컴포넌트가 인터페이스에 의존
  ```go
  type Fetcher struct {
      client       Client        // Interface
      storage      Storage       // Interface
      chainAdapter chain.Adapter // Interface
  }
  ```
- Constructor Injection 패턴 사용

**개선 필요:**
- `NewFetcher()`에서 일부 구체 타입 직접 생성 → 주입으로 변경 권장

---

## 2. Clean Code 분석

### 2.1 Debug 문 (즉시 제거 필요)

```go
// pkg/verifier/verifier.go
Line 260: fmt.Printf("[DEBUG] Immutable-masked similarity: %.4f (threshold: %.4f)\n", ...)
Line 312: fmt.Printf("[DEBUG] Deployed length: %d, without meta: %d\n", ...)
Line 313: fmt.Printf("[DEBUG] Compiled length: %d, without meta: %d\n", ...)
Line 322: fmt.Printf("[DEBUG] Similarity: %.4f (threshold: %.4f)\n", ...)
```

### 2.2 긴 함수 (50줄 초과)

| 파일 | 함수명 | 라인 수 | 권장 조치 |
|------|--------|---------|----------|
| `resolvers.go` | `resolveBlocks()` | 233 | 헬퍼 함수로 분리 |
| `resolvers.go` | `resolveTransactions()` | 222 | 헬퍼 함수로 분리 |
| `resolvers.go` | `resolveLogs()` | 205 | 헬퍼 함수로 분리 |
| `pebble.go` | `GetTokenBalances()` | 131 | 쿼리 로직 분리 |
| `pebble.go` | `GetTopMiners()` | 112 | 집계 로직 분리 |
| `pebble.go` | `SetBlockWithReceipts()` | 104 | 트랜잭션별 처리 분리 |

### 2.3 Panic 사용

| 파일 | 라인 | 컨텍스트 | 평가 |
|------|------|----------|------|
| `pkg/consensus/registry.go` | 102 | 초기화 실패 | 허용 |
| `pkg/eventbus/factory.go` | 122 | 초기화 실패 | 허용 |
| `pkg/storage/backend.go` | 202 | 등록 실패 | 허용 |
| `pkg/adapters/factory/factory.go` | 304 | 어댑터 생성 실패 | 허용 |
| `e2e/anvil/anvil.go` | 320 | 테스트 환경 | 허용 |

> 모두 초기화 단계에서만 사용되어 허용 가능하나, error return 패턴으로 변경 권장

### 2.4 매직 넘버

```go
// 상수로 추출 권장
a.eventBus = events.NewEventBus(1000, 100)  // → constants.DefaultPublishBuffer, constants.DefaultSubscribeBuffer
IdleTimeout: 60 * time.Second               // → constants.DefaultIdleTimeout
time.Sleep(100 * time.Millisecond)          // → constants.DefaultRetryDelay
```

### 2.5 에러 처리

- Swallowed Errors: 발견되지 않음
- 적절한 에러 래핑 사용
- Context 전파 양호

---

## 3. TODO/미구현 항목

### 3.1 Critical (기능 제한)

| 위치 | 내용 | 영향도 |
|------|------|--------|
| `pkg/fetch/fetcher.go:1782` | Fee Delegation 미구현 | StableOne 전용 기능 사용 불가 |
| `pkg/fetch/large_block.go:240` | Fee Delegation 미구현 | 동일 |
| `pkg/eventbus/factory.go:100` | Kafka EventBus 미구현 | 분산 이벤트 처리 불가 |

### 3.2 High (기능 불완전)

| 위치 | 내용 | 영향도 |
|------|------|--------|
| `pkg/fetch/parser.go:252` | BLS 서명 검증 미구현 | 컨센서스 검증 스킵 |
| `pkg/storage/pebble.go:2001` | Token Type 감지 미구현 | 모든 토큰이 ERC20으로 표시 |
| `pkg/storage/pebble.go:2007` | Token Metadata 지원 미구현 | 토큰 메타데이터 저장 불가 |
| `pkg/notifications/service.go:359` | 상세 필터 매칭 미구현 | 단순 필터만 동작 |

### 3.3 Medium (편의성)

| 위치 | 내용 | 영향도 |
|------|------|--------|
| `pkg/api/jsonrpc/filter_manager.go:276` | Pending TX 추적 미구현 | `eth_newPendingTransactionFilter` 미동작 |
| `pkg/api/graphql/resolvers_multichain.go:256` | 등록 시간 저장 미구현 | 멀티체인 등록 시간 비정확 |
| `pkg/eventbus/redis_adapter.go:115` | 인증서 파일 로드 미구현 | Redis TLS 설정 제한 |

### 3.4 Low (개선 사항)

| 위치 | 내용 | 영향도 |
|------|------|--------|
| `pkg/api/jsonrpc/methods.go:542` | Fee Delegation 추출 미구현 | JSON-RPC 응답 불완전 |
| `pkg/api/graphql/mappers.go:240` | Fee Delegation 추출 미구현 | GraphQL 응답 불완전 |

---

## 4. 아키텍처 장점

### 4.1 인터페이스 설계

```
┌─────────────────────────────────────────────────────────────┐
│                     Main Application                         │
└─────────────────────────────────────────────────────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
   ┌────────┐         ┌────────┐        ┌──────────┐
   │ Client │         │Fetcher │        │API Server│
   │(RPC)   │         │        │        │          │
   └────┬───┘         └───┬────┘        └────┬─────┘
        │                 │                  │
        │                 ▼                  │
        │            ┌──────────────────┐   │
        └───────────►│  Storage Layer   │◄──┘
                     │  (Interfaces)    │
                     └──────────────────┘
```

### 4.2 확장성 패턴

- **Adapter Pattern**: 다중 체인 지원
- **Factory Pattern**: 어댑터/이벤트버스 생성
- **Strategy Pattern**: 컨센서스 파서
- **Observer Pattern**: EventBus 구독

### 4.3 테스트 구조

- Unit Tests: `*_test.go`
- E2E Tests: `e2e/`
- Test Utilities: `internal/testutil/`

---

## 5. 권장 파일 구조 개선

### 5.1 pkg/storage/pebble.go 분리

```
pkg/storage/
├── pebble.go              (core: Open, Close, basic operations) ~500 lines
├── pebble_blocks.go       (block operations) ~400 lines
├── pebble_transactions.go (transaction operations) ~400 lines
├── pebble_receipts.go     (receipt operations) ~300 lines
├── pebble_logs.go         (log operations) ~400 lines
├── pebble_historical.go   (historical queries) ~500 lines
├── pebble_address_index.go (existing, ~1100 lines)
├── pebble_token.go        (token operations) ~400 lines
├── pebble_system.go       (system contracts) ~400 lines
└── pebble_analytics.go    (analytics queries) ~400 lines
```

### 5.2 pkg/fetch/fetcher.go 분리

```
pkg/fetch/
├── fetcher.go             (core: Run, FetchBlock) ~800 lines
├── processor.go           (block processing) ~500 lines
├── metadata.go            (WBFT, consensus metadata) ~400 lines
├── indexer.go             (address indexing) ~400 lines
├── balance.go             (balance tracking) ~300 lines
└── events.go              (event publishing) ~200 lines
```

---

## 6. 결론

### 강점
1. 잘 설계된 인터페이스 계층
2. 확장 가능한 어댑터 아키텍처
3. 적절한 에러 처리
4. 명확한 패키지 구조

### 개선 필요
1. 대형 파일 분리 (pebble.go, fetcher.go)
2. Debug 문 제거
3. TODO 항목 완료
4. 긴 함수 리팩토링

### 전체 평가: **B+**

프로덕션 사용에 적합하나, 유지보수성 향상을 위한 리팩토링 권장.
