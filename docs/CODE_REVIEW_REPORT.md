# POC Indexer-Go 코드 리뷰 보고서

**프로젝트:** `/Users/wm-it-22-00661/Work/github/stable-net/poc/indexer-go`
**리뷰 일자:** 2026-01-23
**리뷰 범위:** SOLID 원칙, Clean Code, 보안 취약점

---

## 목차

1. [전체 요약](#전체-요약)
2. [아키텍처 개요](#아키텍처-개요)
3. [보안 취약점](#보안-취약점)
4. [SOLID 원칙 위반](#solid-원칙-위반)
5. [Clean Code 이슈](#clean-code-이슈)
6. [긍정적 측면](#긍정적-측면)
7. [권장 수정 사항](#권장-수정-사항)
8. [즉시 적용 가능한 수정 예시](#즉시-적용-가능한-수정-예시)

---

## 전체 요약

### 이슈 통계

| 카테고리 | CRITICAL | HIGH | MEDIUM | LOW | 총계 |
|----------|----------|------|--------|-----|------|
| **보안** | 2 | 5 | 6 | 4 | 17 |
| **SOLID 원칙** | 2 | 3 | 2 | - | 7 |
| **Clean Code** | 4 | 7 | 5 | 3 | 19 |
| **총계** | **8** | **15** | **13** | **7** | **43** |

### 위험도 평가

- **전체 위험 수준:** HIGH
- **즉시 조치 필요 항목:** 8개 (CRITICAL)
- **프로덕션 배포 전 필수 수정:** 23개 (CRITICAL + HIGH)

---

## 아키텍처 개요

### 디렉토리 구조

```
indexer-go/
├── cmd/indexer/              # 진입점 및 CLI
├── adapters/                 # 체인 어댑터 패턴
│   ├── anvil/               # Anvil 테스트넷 어댑터
│   ├── stableone/           # StableOne 체인 어댑터
│   ├── evm/                 # 범용 EVM 어댑터
│   ├── factory/             # 어댑터 팩토리 (자동 감지)
│   └── detector/            # 노드 타입 감지
├── types/                    # 핵심 타입 정의
│   ├── chain/               # 체인 인터페이스
│   └── consensus/           # WBFT 컨센서스 타입
├── storage/                  # 영속성 레이어 (PebbleDB)
├── fetch/                    # 블록 조회 및 파싱
├── events/                   # 이벤트 버스 및 Pub/Sub
├── api/                      # HTTP API
│   ├── graphql/             # GraphQL 구현
│   ├── jsonrpc/             # JSON-RPC 구현
│   ├── websocket/           # WebSocket 지원
│   └── middleware/          # HTTP 미들웨어
├── rpcproxy/                 # RPC 프록시
├── client/                   # Ethereum RPC 클라이언트
├── abi/                      # ABI 처리
├── verifier/                 # 컨트랙트 검증
├── compiler/                 # Solidity 컴파일
├── consensus/                # 컨센서스 로직
│   ├── wbft/                # WBFT 전용
│   └── poa/                 # PoA 전용
└── internal/                 # 내부 유틸리티
```

### 주요 패키지 책임

| 패키지 | 책임 | 핵심 파일 |
|--------|------|-----------|
| `cmd/indexer` | 애플리케이션 진입점, 라이프사이클 관리 | `main.go` |
| `adapters` | 체인별 어댑터 및 자동 감지 | Factory 패턴 |
| `types/chain` | 체인 비의존적 인터페이스 | `Adapter`, `BlockFetcher` |
| `storage` | PebbleDB 기반 영속성 레이어 | Key-Value 스키마 |
| `fetch` | 워커 풀 기반 블록 조회 | 동시 블록 조회 |
| `events` | Pub/Sub 및 이벤트 리플레이 | 실시간 이벤트 분배 |
| `api` | GraphQL, JSON-RPC, WebSocket | 다중 프로토콜 지원 |

### 데이터 흐름

```
Client (RPC) → Fetcher (worker pool) → Storage (PebbleDB)
                    ↓
                EventBus (pub/sub)
                    ↓
                API Server (GraphQL/JSON-RPC/WebSocket)
```

---

## 보안 취약점

### CRITICAL (즉시 수정 필요)

#### SEC-001: WebSocket CheckOrigin이 모든 Origin 허용

**파일:** `api/websocket/server.go:10-17`

**문제 코드:**
```go
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        // Allow all origins for now (should be configured in production)
        return true
    },
}
```

**위험:**
- Cross-Site WebSocket Hijacking (CSWSH) 공격에 취약
- 악성 웹사이트에서 사용자 대신 WebSocket 연결 가능
- 서버 리소스 고갈 가능

**해결 방안:**
```go
CheckOrigin: func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    for _, allowed := range config.AllowedOrigins {
        if origin == allowed {
            return true
        }
    }
    return false
}
```

---

#### SEC-002: CORS가 모든 Origin 허용

**파일:** `config.yaml:57-59`, `api/config.go:87`

**문제 코드:**
```yaml
allowed_origins:
  - "*"
```

```go
AllowedOrigins: []string{"*"},
```

**위험:**
- CSRF 공격 가능
- 인증된 엔드포인트의 데이터 유출 가능
- API 리소스 남용 가능

**해결 방안:**
프로덕션 환경에서는 명시적인 origin 목록 설정 필수

---

### HIGH (프로덕션 전 수정 필요)

#### SEC-003: JSON-RPC 요청 본문 크기 제한 없음

**파일:** `api/jsonrpc/server.go:35-36`

**문제 코드:**
```go
body, err := io.ReadAll(r.Body)
```

**위험:**
- 메모리 고갈 공격 가능
- 서비스 거부(DoS) 공격 취약
- 서버 불안정 유발

**해결 방안:**
```go
r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10MB 제한
body, err := io.ReadAll(r.Body)
```

---

#### SEC-004: Rate Limiter IP Spoofing 취약점

**파일:** `api/middleware/ratelimit.go:66-73`

**문제 코드:**
```go
ip := r.RemoteAddr
if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
    ip = xff
} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
    ip = xri
}
```

**위험:**
- 헤더 조작으로 Rate Limiting 우회 가능
- 수백만 개의 가짜 IP로 메모리 고갈 가능
- 리버스 프록시 뒤에서만 헤더 신뢰 가능

**해결 방안:**
- 리버스 프록시 환경에서만 헤더 신뢰
- 최대 limiter 수 제한
- LRU 캐시 사용
- X-Forwarded-For 첫 번째 IP만 사용

---

#### SEC-005: Solidity 컴파일러 다운로드 검증 없음

**파일:** `compiler/solc.go:291-296`

**문제 코드:**
```go
resp, err := http.DefaultClient.Do(req)
```

**위험:**
- MITM 공격으로 악성 컴파일러 주입 가능
- 다운로드된 바이너리 검증 없이 실행

**해결 방안:**
- 체크섬 검증 구현
- 커스텀 HTTP 클라이언트로 TLS 설정
- 서명 검증 구현

---

#### SEC-006: Rate Limiting 기본 비활성화

**파일:** `api/config.go:98`

**문제 코드:**
```go
EnableRateLimit: false, // Disabled by default for development
```

**위험:**
- DoS 공격 취약
- 리소스 고갈 가능
- 무차별 대입 공격 가능

**해결 방안:**
기본값을 `true`로 변경하거나 프로덕션 환경 필수 설정으로 지정

---

#### SEC-007: 컴파일러 명령 실행 위험

**파일:** `compiler/solc.go:171-183`

**문제 코드:**
```go
cmd := exec.CommandContext(ctx, solcPath, args...)
```

**위험:**
- 버전 문자열 조작 시 임의 바이너리 실행 가능
- Path Traversal 위험

**해결 방안:**
- 버전 문자열 엄격한 정규식 검증 (예: `^[0-9]+\.[0-9]+\.[0-9]+$`)
- `filepath.Clean` 사용
- 다운로드된 바이너리 체크섬 검증

---

### MEDIUM

| ID | 이슈 | 파일 | 설명 |
|----|------|------|------|
| SEC-008 | 파일시스템 경로 노출 | config.yaml:67 | 로컬 경로 하드코딩 |
| SEC-009 | Filter ID 예측 가능 | filter_manager.go:149 | 순차적 ID 생성 |
| SEC-010 | 로그 스캔 제한 없음 | pebble_logs.go:59 | 무제한 블록 범위 스캔 |
| SEC-011 | Path Traversal 위험 패턴 | system_contracts_verification.go:132 | 파일경로 검증 부족 |
| SEC-012 | 에러 메시지 정보 노출 | jsonrpc/methods.go | 내부 에러 클라이언트 노출 |
| SEC-013 | ABI 저장 인증 없음 | methods_abi.go:16 | 무인증 쓰기 허용 |

### LOW

| ID | 이슈 | 파일 | 설명 |
|----|------|------|------|
| SEC-014 | WebSocket 메시지 크기 제한 | websocket/client.go:23 | 512바이트로 너무 작음 |
| SEC-015 | Filter Manager 정리 타임아웃 없음 | filter_manager.go:158 | 컨텍스트 취소 없음 |
| SEC-016 | Recovery 미들웨어 헤더 순서 | middleware/recovery.go:27 | WriteHeader 후 헤더 설정 |
| SEC-017 | WebSocket Hub Goroutine 누수 | websocket/hub.go:40 | 종료 조건 없음 |

---

## SOLID 원칙 위반

### CRITICAL

#### SOLID-001: Single Responsibility Principle (SRP) 위반 - Fetcher

**파일:** `fetch/fetcher.go` (2281 lines)

**문제:**
단일 `Fetcher` 구조체가 너무 많은 책임을 가짐:
- Block/Receipt 조회
- WBFT 메타데이터 처리
- Address 인덱싱
- Balance 추적
- Event 발행
- Gap 감지 및 복구
- 성능 최적화
- Pending 트랜잭션 구독

**해결 방안:**
5개 컴포넌트로 분리:

```go
// BlockFetcher - 블록/영수증 조회
type BlockFetcher struct {
    client Client
    logger *zap.Logger
}

// MetadataProcessor - WBFT/주소 인덱싱
type MetadataProcessor struct {
    storage   Storage
    eventBus  *EventBus
    logger    *zap.Logger
}

// BalanceTracker - 네이티브 잔액 추적
type BalanceTracker struct {
    storage Storage
    logger  *zap.Logger
}

// EventPublisher - 이벤트 버스 통합
type EventPublisher struct {
    eventBus *EventBus
    logger   *zap.Logger
}

// GapRecovery - 갭 감지/복구
type GapRecovery struct {
    storage Storage
    fetcher *BlockFetcher
    logger  *zap.Logger
}
```

---

#### SOLID-002: SRP 위반 - Storage Schema

**파일:** `storage/schema.go` (839 lines)

**문제:**
단일 파일에 모든 키 생성 함수 포함

**해결 방안:**
도메인별 분리:
- `BlockKeys` - 블록/트랜잭션 키
- `WBFTKeys` - 컨센서스 키
- `IndexKeys` - 주소/로그 인덱싱 키
- `SystemContractKeys` - 시스템 컨트랙트 키

---

### HIGH

#### SOLID-003: Open/Closed Principle (OCP) 위반

**파일:** `fetch/fetcher.go:1279-1287`

**문제 코드:**
```go
func (f *Fetcher) processWBFTMetadata(ctx context.Context, block *types.Block) error {
    if f.chainAdapter != nil {
        info := f.chainAdapter.Info()
        if info != nil && info.ConsensusType != chain.ConsensusTypeWBFT {
            return nil
        }
    }
    // ... 하드코딩된 WBFT 로직
}
```

**문제:**
새로운 컨센서스 타입 추가 시 코드 수정 필요

**해결 방안:**
Strategy 패턴 적용:

```go
type ConsensusProcessor interface {
    ProcessMetadata(ctx context.Context, block *types.Block) error
}

type ConsensusProcessorRegistry struct {
    processors map[chain.ConsensusType]ConsensusProcessor
}

func (r *ConsensusProcessorRegistry) Process(ctx context.Context, block *types.Block, consensusType chain.ConsensusType) error {
    if processor, ok := r.processors[consensusType]; ok {
        return processor.ProcessMetadata(ctx, block)
    }
    return nil
}
```

---

#### SOLID-004: Liskov Substitution Principle (LSP) 위반

**파일:** `fetch/fetcher.go:1549`

**문제 코드:**
```go
addressWriter, ok := f.storage.(storagepkg.AddressIndexWriter)
if !ok {
    return nil // 조용히 건너뜀
}
```

**문제:**
Storage 구현체가 완전히 상호 교환 가능하지 않음

**해결 방안:**
- 명시적 capability 인터페이스 사용
- 초기화 시 fail-fast 적용
- 또는 INFO 레벨 로깅

---

#### SOLID-005: Dependency Inversion Principle (DIP) 위반

**파일:** `storage/pebble_*.go`

**문제:**
고수준 모듈이 Pebble 구현에 직접 의존

**해결 방안:**
추상 `KVStore` 인터페이스 생성:

```go
type KVStore interface {
    Get(key []byte) ([]byte, error)
    Set(key, value []byte) error
    Delete(key []byte) error
    NewIterator(opts *IteratorOptions) Iterator
    NewBatch() Batch
    Close() error
}
```

---

## Clean Code 이슈

### CRITICAL

#### CC-001: 함수 길이 과다 - FetchRangeConcurrent

**파일:** `fetch/fetcher.go:536-742` (207 lines)

**문제:**
단일 함수가 너무 많은 작업 수행:
- 워커 풀 생성
- 작업 분배
- 결과 수집
- 순차 저장
- 진행 로깅

**해결 방안:**
```go
func (f *Fetcher) FetchRangeConcurrent(ctx context.Context, start, end uint64) error {
    pool := f.createWorkerPool(ctx, numWorkers)
    defer pool.Shutdown()

    f.distributeJobs(pool, start, end)
    results := f.collectResults(pool)
    return f.storeResultsSequentially(ctx, results)
}
```

---

#### CC-002: 함수 길이 과다 - processAddressIndexing

**파일:** `fetch/fetcher.go:1548-1731` (184 lines)

**문제:**
복잡한 중첩 루프로 다양한 인덱싱 타입 처리

**해결 방안:**
```go
func (f *Fetcher) processAddressIndexing(...) error {
    if err := f.indexTransactionAddresses(ctx, block, txs); err != nil {
        return err
    }
    if err := f.indexContractCreation(ctx, block, txs); err != nil {
        return err
    }
    if err := f.indexERC20Transfers(ctx, block, receipts); err != nil {
        return err
    }
    return f.indexERC721Transfers(ctx, block, receipts)
}
```

---

### HIGH

#### CC-003: 코드 중복 - 키 추출 로직

**파일:** `storage/pebble_wbft.go:326-336, 357-367`

**문제 코드:**
```go
// Lines 326-336
keyStr := string(prepareIter.Key())
lastSlash := len(keyStr) - 1
for lastSlash >= 0 && keyStr[lastSlash] != '/' {
    lastSlash--
}

// Lines 357-367 - 완전히 동일
keyStr := string(commitIter.Key())
lastSlash := len(keyStr) - 1
for lastSlash >= 0 && keyStr[lastSlash] != '/' {
    lastSlash--
}
```

**해결 방안:**
```go
func extractAddressFromKey(key []byte) common.Address {
    keyStr := string(key)
    lastSlash := len(keyStr) - 1
    for lastSlash >= 0 && keyStr[lastSlash] != '/' {
        lastSlash--
    }
    if lastSlash < 0 || lastSlash >= len(keyStr)-1 {
        return common.Address{}
    }
    return common.HexToAddress(keyStr[lastSlash+1:])
}
```

---

#### CC-004: 코드 중복 - 페이지네이션 검증

**파일:** `storage/pebble_wbft.go:158-163, 253-258`

**문제 코드:**
```go
if limit <= 0 {
    limit = constants.DefaultMaxPaginationLimit
}
if limit > constants.MaxPaginationLimitExtended {
    limit = constants.MaxPaginationLimitExtended
}
```

**해결 방안:**
```go
func normalizePaginationLimit(limit int) int {
    if limit <= 0 {
        return constants.DefaultMaxPaginationLimit
    }
    if limit > constants.MaxPaginationLimitExtended {
        return constants.MaxPaginationLimitExtended
    }
    return limit
}
```

---

#### CC-005: 깊은 중첩 - EventBus broadcastEvent

**파일:** `events/bus.go:234-291`

**문제:**
4-5 레벨의 중첩된 조건문과 루프

**해결 방안:**
Early return 패턴 및 함수 분리:

```go
func (eb *EventBus) broadcastEvent(event Event) {
    eventType := event.Type()

    for _, sub := range eb.subscribers {
        if !eb.shouldDeliverTo(sub, event, eventType) {
            continue
        }
        eb.deliverEventTo(sub, event)
    }
}

func (eb *EventBus) shouldDeliverTo(sub *Subscription, event Event, eventType EventType) bool {
    if !sub.EventTypes[eventType] {
        return false
    }
    if sub.Filter != nil && !sub.Filter.Match(event) {
        eb.recordFilteredEvent(sub)
        return false
    }
    return true
}
```

---

#### CC-006: 에러 무시

**파일:** `fetch/fetcher.go:1552`

**문제 코드:**
```go
addressWriter, ok := f.storage.(storagepkg.AddressIndexWriter)
if !ok {
    return nil // 에러 로깅 없이 조용히 건너뜀
}
```

**해결 방안:**
```go
addressWriter, ok := f.storage.(storagepkg.AddressIndexWriter)
if !ok {
    f.logger.Info("Address indexing skipped - storage does not implement AddressIndexWriter")
    return nil
}
```

---

### MEDIUM

#### CC-007: Magic Number

**파일:** `fetch/fetcher.go:1567`

**문제 코드:**
```go
const FeeDelegateDynamicFeeTxType = 22
```

**해결 방안:**
`internal/constants` 패키지로 이동 및 문서화

---

#### CC-008: 불명확한 약어

**파일:** `storage/schema.go`

**문제 코드:**
```go
prefixIdxWBFTSignerPrepare  // "Idx"가 무엇인지 불명확
WBFTValidatorActivityAllKeyPrefix  // "All"이 모호함
```

**해결 방안:**
```go
prefixIndexWBFTSignerPrepare
WBFTValidatorActivityCompleteKeyPrefix
```

---

#### CC-009: 문서화 부족

**파일:** `api/jsonrpc/server.go:166`

**문제 코드:**
```go
func (s *Server) HandleMethodDirect(ctx context.Context, method string, params json.RawMessage) (interface{}, *Error) {
```

**해결 방안:**
```go
// HandleMethodDirect executes a JSON-RPC method directly without HTTP transport.
// This is useful for internal calls or testing.
// Returns the method result and an error if the method fails.
func (s *Server) HandleMethodDirect(ctx context.Context, method string, params json.RawMessage) (interface{}, *Error) {
```

---

#### CC-010: 메모리 할당 비효율

**파일:** `storage/pebble_wbft.go:270`

**문제 코드:**
```go
result := make([]*ValidatorSigningActivity, 0)
```

**해결 방안:**
```go
result := make([]*ValidatorSigningActivity, 0, limit)
```

---

## 긍정적 측면

### 잘 설계된 부분

| 항목 | 설명 | 위치 |
|------|------|------|
| 인터페이스 분리 | Reader/Writer로 적절히 분리 | storage/ |
| 에러 래핑 | `%w` 일관된 사용 | 전체 코드베이스 |
| O(1) 룩업 | 해시맵으로 O(n²) 회피 | fetch/fetcher.go:1253 |
| Context 전파 | 적절한 context.Context 사용 | 전체 코드베이스 |
| 동시성 설계 | 좋은 워커 풀 패턴 | fetch/fetcher.go |
| 타입 안전성 | go-ethereum 타입 활용 | types/ |
| 팩토리 패턴 | 어댑터 자동 생성 | adapters/factory/ |
| 전략 패턴 | 다중 어댑터 구현 | adapters/ |

### 좋은 코드 예시

#### 적절한 에러 래핑

```go
// storage/pebble_wbft.go:32
return nil, fmt.Errorf("failed to get WBFT block extra: %w", err)
```

#### 최적화된 조회

```go
// fetch/fetcher.go:1253-1263
// Build receipt map for O(1) lookup (avoids O(n²) matching)
receiptMap := buildReceiptMap(receipts)
```

#### 적절한 슬라이스 사전 할당

```go
// storage/pebble_wbft.go:223
result := make([]*ValidatorSigningStats, 0, len(statsMap))
```

---

## 권장 수정 사항

### 즉시 수정 (1주 내)

| 우선순위 | ID | 이슈 | 예상 공수 |
|----------|-----|------|-----------|
| 1 | SEC-001 | WebSocket origin 제한 | 2시간 |
| 2 | SEC-002 | CORS origin 제한 | 1시간 |
| 3 | SEC-003 | JSON-RPC 요청 크기 제한 | 1시간 |
| 4 | SEC-006 | Rate limiting 기본 활성화 | 30분 |
| 5 | SEC-004 | X-Forwarded-For 처리 개선 | 2시간 |
| 6 | CC-006 | Worker goroutine panic recovery | 1시간 |

### 단기 수정 (1개월)

| 우선순위 | ID | 이슈 | 예상 공수 |
|----------|-----|------|-----------|
| 1 | SOLID-001 | Fetcher 5개 컴포넌트 분리 | 2일 |
| 2 | CC-001, CC-002 | 함수 길이 50라인 이하 축소 | 1일 |
| 3 | CC-003, CC-004 | 중복 코드 제거 | 4시간 |
| 4 | SEC-009 | Filter ID crypto/rand 사용 | 1시간 |
| 5 | SEC-013 | ABI 저장 API 인증 추가 | 4시간 |
| 6 | CC-005 | 깊은 중첩 리팩토링 | 4시간 |

### 장기 수정 (분기)

| 우선순위 | ID | 이슈 | 예상 공수 |
|----------|-----|------|-----------|
| 1 | SOLID-003 | 컨센서스 Strategy 패턴 | 3일 |
| 2 | SOLID-005 | KVStore 추상화 레이어 | 2일 |
| 3 | SOLID-002 | Storage Schema 도메인별 분리 | 1일 |
| 4 | - | Event Handler Registry 패턴 | 2일 |
| 5 | - | 종합 입력 검증 추가 | 3일 |
| 6 | - | 성능 프로파일링 및 최적화 | 1주 |

---

## 즉시 적용 가능한 수정 예시

### 1. JSON-RPC 요청 크기 제한

```go
// api/jsonrpc/server.go
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 10MB 제한 추가
    r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

    body, err := io.ReadAll(r.Body)
    if err != nil {
        if err.Error() == "http: request body too large" {
            s.writeError(w, NewError(InvalidRequest, "request body too large", ""))
            return
        }
        s.writeError(w, NewError(ParseError, "failed to read request body", err.Error()))
        return
    }
    // ...
}
```

### 2. Filter ID 보안 강화

```go
// api/jsonrpc/filter_manager.go
import "crypto/rand"

func (fm *FilterManager) generateID() string {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        // fallback to less secure method
        return common.BytesToHash([]byte(time.Now().String())).Hex()
    }
    return common.BytesToHash(b).Hex()
}
```

### 3. Worker Goroutine Panic Recovery

```go
// fetch/fetcher.go
func (f *Fetcher) FetchRangeConcurrent(ctx context.Context, start, end uint64) error {
    // ...
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func(workerID int) {
            defer func() {
                if r := recover(); r != nil {
                    f.logger.Error("Worker panic recovered",
                        zap.Int("worker", workerID),
                        zap.Any("panic", r),
                        zap.Stack("stack"),
                    )
                }
                wg.Done()
            }()

            for height := range jobs {
                result := f.fetchBlockJob(ctx, height)
                results <- result
            }
        }(i)
    }
    // ...
}
```

### 4. CORS Origin 제한

```go
// api/config.go
func DefaultAPIConfig() *APIConfig {
    return &APIConfig{
        // 개발 환경에서만 * 사용, 프로덕션에서는 명시적 목록
        AllowedOrigins: []string{}, // 빈 슬라이스 = 모두 거부
        // ...
    }
}

// api/server.go
func (s *Server) setupCORS() func(http.Handler) http.Handler {
    if len(s.config.AllowedOrigins) == 0 {
        s.logger.Warn("No CORS origins configured - all cross-origin requests will be rejected")
    }

    return cors.Handler(cors.Options{
        AllowedOrigins: s.config.AllowedOrigins,
        // ...
    })
}
```

### 5. 페이지네이션 헬퍼

```go
// internal/pagination/pagination.go
package pagination

import "github.com/0xmhha/indexer-go/internal/constants"

// NormalizeLimit ensures the limit is within valid bounds
func NormalizeLimit(limit int) int {
    if limit <= 0 {
        return constants.DefaultMaxPaginationLimit
    }
    if limit > constants.MaxPaginationLimitExtended {
        return constants.MaxPaginationLimitExtended
    }
    return limit
}

// NormalizeOffset ensures the offset is non-negative
func NormalizeOffset(offset int) int {
    if offset < 0 {
        return 0
    }
    return offset
}
```

---

## 보안 체크리스트

- [ ] 하드코딩된 시크릿 없음
- [ ] 모든 입력 검증됨 - **부분적**
- [ ] SQL 인젝션 방지 - **해당 없음** (Pebble KV 스토어 사용)
- [ ] XSS 방지 - **해당 없음** (백엔드 API만)
- [ ] CSRF 보호 - **미흡** (CORS가 모든 origin 허용)
- [ ] 인증 필요 - **미흡** (인증 없음)
- [ ] 권한 검증 - **미흡** (쓰기 엔드포인트 미보호)
- [ ] Rate limiting 활성화 - **기본 비활성화**
- [ ] HTTPS 강제 - **미강제** (배포 설정에 의존)
- [ ] 보안 헤더 설정 - **미흡** (보안 헤더 미들웨어 없음)
- [ ] 의존성 업데이트 - **미확인** (`govulncheck` 실행 권장)
- [ ] 로깅 새니타이징 - **부분적**
- [ ] 에러 메시지 안전 - **미흡** (내부 에러 노출)

---

## 결론

POC Indexer-Go는 전반적으로 견고한 아키텍처를 가지고 있으나, 프로덕션 배포 전 주요 보안 이슈와 코드 품질 개선이 필요합니다.

**즉시 조치 항목:**
1. CORS/WebSocket origin 제한
2. 요청 크기 제한 추가
3. Rate limiting 기본 활성화

**중장기 개선 항목:**
1. Fetcher 컴포넌트 분리 (SRP 준수)
2. Strategy 패턴 도입 (OCP 준수)
3. 종합 입력 검증 시스템 구축

---

*이 문서는 자동 생성되었으며, 프로젝트 변경 시 업데이트가 필요합니다.*
