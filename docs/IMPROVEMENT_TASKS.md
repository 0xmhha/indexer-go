# Improvement Tasks

**Project**: indexer-go
**Created**: 2026-02-06
**Based on**: Code Review Report

---

## Task Priority Legend

| Priority | Description | Timeline |
|----------|-------------|----------|
| P0 | Critical - 즉시 수정 | 1일 이내 |
| P1 | High - 빠른 수정 필요 | 1주 이내 |
| P2 | Medium - 계획된 개선 | 2-4주 |
| P3 | Low - 장기 개선 | 1-3개월 |

---

## P0: Critical (즉시 수정)

### T-001: Debug Print 문 제거
- **파일**: `pkg/verifier/verifier.go`
- **라인**: 260, 312, 313, 322
- **작업**: `fmt.Printf("[DEBUG]...)` 문 제거 또는 로거로 교체
- **예상 시간**: 15분
- **담당**: -

```go
// 제거 대상
fmt.Printf("[DEBUG] Immutable-masked similarity: %.4f (threshold: %.4f)\n", similarity, MinBytecodeSimilarityThreshold)
fmt.Printf("[DEBUG] Deployed length: %d, without meta: %d\n", len(deployed), len(deployedWithoutMeta))
fmt.Printf("[DEBUG] Compiled length: %d, without meta: %d\n", len(compiled), len(compiledWithoutMeta))
fmt.Printf("[DEBUG] Similarity: %.4f (threshold: %.4f)\n", similarity, MinBytecodeSimilarityThreshold)
```

---

## P1: High (1주 이내)

### T-002: Token Type 감지 구현
- **파일**: `pkg/storage/pebble.go:2001`
- **현재**: 모든 토큰이 ERC20으로 하드코딩
- **작업**:
  - Transfer 이벤트 시그니처 분석
  - ERC721, ERC1155 감지 로직 추가
- **예상 시간**: 4시간
- **담당**: -

### T-003: Token Metadata 지원 추가
- **파일**: `pkg/storage/pebble.go:2007`
- **현재**: 빈 문자열
- **작업**:
  - TokenMetadata 구조체 확장
  - 로고 URL, 소수점, 추가 정보 저장
- **예상 시간**: 2시간
- **담당**: -

### T-004: Notification 상세 필터 매칭 구현
- **파일**: `pkg/notifications/service.go:359`
- **현재**: 단순 필터만 동작
- **작업**:
  - 금액 범위 필터
  - 이벤트 타입 필터
  - 컨트랙트 주소 필터
- **예상 시간**: 4시간
- **담당**: -

---

## P2: Medium (2-4주)

### T-005: pebble.go 파일 분리 (SRP) ✅
- **파일**: `pkg/storage/pebble.go` (4,302 라인 → 386 라인)
- **작업**: 도메인별 파일 분리
  - [x] `pebble.go` - 코어 (386줄)
  - [x] `pebble_blocks.go` - 블록 연산 (351줄)
  - [x] `pebble_transactions.go` - 트랜잭션 연산 (185줄)
  - [x] `pebble_receipts.go` - 영수증 연산 (250줄)
  - [x] `pebble_logs.go` - 로그 연산 (448줄)
  - [x] `pebble_historical.go` - 히스토리 쿼리 (545줄)
  - [x] `pebble_token.go` - 토큰 연산 (80줄)
  - [x] `pebble_analytics.go` - 분석 쿼리 (340줄)
  - [x] `pebble_batch.go` - 배치 연산 (233줄)
  - [x] `pebble_search.go` - 검색 연산 (214줄)
  - [x] `pebble_fee_delegation.go` - Fee Delegation (435줄)
  - [x] `pebble_helpers.go` - 헬퍼 함수 (294줄)
- **예상 시간**: 8시간
- **완료**: 2026-02-06
- **담당**: -

### T-006: fetcher.go 파일 분리 (SRP) ✅
- **파일**: `pkg/fetch/fetcher.go` (2,579 라인 → 767 라인)
- **작업**: 프로세서별 파일 분리
  - [x] `fetcher.go` - 코어 로직 (767줄)
  - [x] `fetcher_processing.go` - 블록 처리 (350줄)
  - [x] `fetcher_indexing.go` - 주소 인덱싱 (540줄)
  - [x] `fetcher_events.go` - 이벤트 처리 (317줄)
  - [x] `fetcher_gaps.go` - 갭 복구 (365줄)
  - [x] `fetcher_consensus.go` - 합의 처리 (287줄)
  - [x] `fetcher_metrics.go` - 메트릭스 (63줄)
- **예상 시간**: 6시간
- **완료**: 2026-02-06
- **담당**: -

### T-007: 긴 함수 리팩토링 ✅
- **대상 함수**:
  | 파일 | 함수 | 이전 | 현재 | 목표 | 상태 |
  |------|------|------|------|------|------|
  | `resolvers.go` | `resolveBlocks()` | 233줄 | ~50줄 | <50줄 | ✅ 완료 |
  | `resolvers.go` | `resolveTransactions()` | 222줄 | ~38줄 | <50줄 | ✅ 완료 |
  | `resolvers.go` | `resolveLogs()` | 205줄 | ~35줄 | <50줄 | ✅ 완료 |
  | `pebble_token.go` | `GetTokenBalances()` | 143줄 | ~25줄 | <50줄 | ✅ 완료 |
  | `pebble_historical.go` | `GetTopMiners()` | 112줄 | ~25줄 | <50줄 | ✅ 완료 |
- **리팩토링 내용**:
  - `resolver_helpers.go` 파일 추가 (393줄): 재사용 가능한 헬퍼 함수들
  - `pebble_helpers.go` 파일 추가: Storage 헬퍼 함수들
  - 파싱, 필터링, 응답 구성 로직을 별도 함수로 분리
  - `extractContext`, `parsePaginationParams`, `parseBlockFilter` 등 공통 로직 추출
  - `scanTransferEvents`, `applyTokenMetadata`, `aggregateMinerStats` 등 Storage 헬퍼 추출
- **예상 시간**: 8시간
- **완료**: 2026-02-06
- **담당**: -

### T-008: 매직 넘버 상수화
- **대상 파일**: 다수
- **작업**:
  - [ ] `internal/constants/defaults.go` 파일 생성
  - [ ] 버퍼 크기 상수화 (1000, 100)
  - [ ] 타임아웃 상수화 (60s, 100ms)
  - [ ] 임계값 상수화
- **예상 시간**: 2시간
- **담당**: -

### T-009: BLS 서명 검증 구현 ✅
- **파일**: `pkg/fetch/parser.go:252`
- **현재**: Placeholder 구현
- **작업**:
  - [x] BLS 라이브러리 통합 (supranational/blst)
  - [x] 검증 로직 구현
  - [x] 실패 시 처리 로직
- **예상 시간**: 8시간
- **담당**: -
- **완료**: 2026-02-06
- **변경 사항**:
  - `pkg/crypto/bls/` 패키지 신규 생성
    - `bls.go`: BLS 공개키/서명 파싱, 집계, 검증 (184줄)
    - `seal.go`: Seal Hash 계산 로직 (143줄)
    - `verifier.go`: WBFT seal 검증기 (276줄)
    - `bls_test.go`: 단위 테스트 (195줄)
    - `verifier_test.go`: 검증기 테스트 (261줄)
  - `pkg/fetch/parser.go`: VerifySeal 함수 업데이트
    - 기본 구조 검증 (서명 길이, sealers 비트맵)
    - 쿼럼 검사 (2/3 이상 참여)
    - BLSVerifier 인터페이스를 통한 전체 암호화 검증 지원
    - VerifySealWithBLS() 메서드 추가

### T-010: Pending Transaction 추적 구현 ✅
- **파일**: `pkg/api/jsonrpc/filter_manager.go:276`
- **현재**: 미구현
- **작업**:
  - Pending TX 풀 모니터링
  - 필터 매칭 로직
  - 구독 알림
- **예상 시간**: 6시간
- **담당**: -
- **완료**: 2026-02-06
- **변경 사항**:
  - `pkg/api/jsonrpc/pending_pool.go` 신규 생성 (331줄)
    - PendingPool 구조체: pending tx 추적 및 TTL 기반 자동 정리
    - EventBus 구독을 통한 실시간 pending tx 수신
    - AddTransaction, RemoveTransaction, GetTransactionsSince 메서드
    - 최대 크기 초과 시 oldest 트랜잭션 자동 evict
  - `pkg/api/jsonrpc/filter_manager.go` 수정
    - Filter에 LastPendingTxIndex 필드 추가
    - FilterManager에 pendingPool 필드 추가
    - NewFilterManagerWithPendingPool 생성자 추가
    - GetPendingTransactionsSinceLastPoll 구현
  - `pkg/api/jsonrpc/pending_pool_test.go` 신규 생성 (282줄)
    - PendingPool 단위 테스트
    - FilterManager + PendingPool 통합 테스트

---

## P3: Low (1-3개월)

### T-011: Kafka EventBus 구현 ✅
- **파일**: `pkg/eventbus/kafka_eventbus.go`, `pkg/eventbus/kafka_helpers.go`
- **완료**: KafkaEventBus 구현 (RedisEventBus와 동일한 adapter 패턴)
- **작업**:
  - SASL/TLS 헬퍼를 kafka_helpers.go로 추출하여 공유
  - KafkaEventBus: producer + consumer group 기반 분산 이벤트 브로드캐스팅
  - Echo prevention via node_id header
  - Factory 통합 (kafka/hybrid 모드 지원)
  - 단위 테스트 10개 (broker 불필요)
- **완료일**: 2026-02-08

### T-012: Fee Delegation 지원 (go-stablenet) ✅
- **파일**:
  - `pkg/fetch/fetcher_indexing.go` - getFeePayer 스토리지 조회
  - `pkg/fetch/large_block.go` - getFeePayer 스토리지 조회
  - `pkg/api/jsonrpc/methods.go` - FeeDelegationReader 스토리지 조회
  - `pkg/api/graphql/mappers.go` - FeeDelegationReader 스토리지 조회
- **작업**:
  - go-stablenet 의존성 없이 스토리지 기반 접근으로 해결
  - FeeDelegationReader 인터페이스를 통해 저장된 메타데이터 조회
  - feePayer, feePayerSignatures (v, r, s) 필드 반환
- **예상 시간**: 16시간
- **담당**: -
- **완료**: 2026-02-08
- **변경 사항**:
  - Fetcher가 블록 처리 시 FeeDelegationClient로 메타데이터를 스토리지에 저장
  - GraphQL mappers, JSON-RPC methods, fetcher_indexing, large_block 모두 FeeDelegationReader로 조회
  - go-stablenet 직접 의존성 불필요

### T-013: Redis TLS 인증서 로드 구현 ✅
- **파일**: `pkg/eventbus/redis_adapter.go:115`
- **현재**: 인증서 설정 시 스킵
- **작업**:
  - 인증서 파일 로드
  - TLS 설정 구성
  - 연결 검증
- **예상 시간**: 4시간
- **담당**: -
- **완료**: 2026-02-06
- **변경 사항**:
  - `buildTLSConfig()` 메서드 추가
  - CA 인증서 파일 로드 (`ca_file` 설정)
  - 클라이언트 인증서/키 쌍 로드 (`cert_file`, `key_file` 설정)
  - 로드 성공/실패 로깅 추가

### T-014: Multichain 등록 시간 저장 ✅
- **파일**: `pkg/api/graphql/resolvers_multichain.go:256`
- **현재**: `time.Now()` 사용
- **작업**:
  - 등록 시간 DB 저장
  - 조회 시 저장된 시간 반환
- **예상 시간**: 2시간
- **담당**: -
- **완료**: 2026-02-06
- **변경 사항**:
  - `ChainInstance`에 `registeredAt` 필드 추가
  - `NewChainInstance()`에서 등록 시간 자동 설정
  - `Info()`에서 `CreatedAt` 필드 반환
  - `chainInstanceToMap()`에서 실제 등록 시간 사용

### T-015: Panic을 Error Return으로 변경 ✅
- **파일**:
  - `pkg/consensus/registry.go:102`
  - `pkg/eventbus/factory.go:122`
  - `pkg/storage/backend.go:202`
  - `pkg/adapters/factory/factory.go:304`
- **현재**: 초기화 실패 시 panic
- **작업**:
  - error 반환으로 변경
  - 호출자에서 에러 처리
- **예상 시간**: 4시간
- **담당**: -
- **완료**: 2026-02-06
- **변경 사항**:
  - `MustXxx` 함수에 Deprecated 주석 추가 (error-returning 버전 사용 권장)
  - `init()` 함수들을 `Register()` + `log.Fatal()` 패턴으로 변경
  - 수정 파일: `poa/register.go`, `wbft/register.go`, `pebble_backend.go`

---

## Phase 1 Summary (T-001 ~ T-015): COMPLETE

| Priority | Tasks | Est. Hours |
|----------|-------|------------|
| P0 | 1 | 0.25h |
| P1 | 3 | 10h |
| P2 | 6 | 38h |
| P3 | 5 | 42h |
| **Total** | **15/15 완료** | **90.25h** |

---

## Checklist (Phase 1)

- [x] T-001: Debug Print 문 제거 (2026-02-06 완료)
- [x] T-002: Token Type 감지 구현 (2026-02-06 완료)
- [x] T-003: Token Metadata 지원 추가 (2026-02-06 완료)
- [x] T-004: Notification 상세 필터 매칭 구현 (2026-02-06 완료)
- [x] T-005: pebble.go 파일 분리 (2026-02-06 완료)
- [x] T-006: fetcher.go 파일 분리 (2026-02-06 완료)
- [x] T-007: 긴 함수 리팩토링 (2026-02-06 완료)
- [x] T-008: 매직 넘버 상수화 (2026-02-06 완료)
- [x] T-009: BLS 서명 검증 구현 (2026-02-06 완료)
- [x] T-010: Pending Transaction 추적 구현 (2026-02-06 완료)
- [x] T-011: Kafka EventBus 구현 (2026-02-08 완료)
- [x] T-012: Fee Delegation 지원 (2026-02-08 완료, 스토리지 기반)
- [x] T-013: Redis TLS 인증서 로드 구현 (2026-02-06 완료)
- [x] T-014: Multichain 등록 시간 저장 (2026-02-06 완료)
- [x] T-015: Panic을 Error Return으로 변경 (2026-02-06 완료)

---
---

# Phase 2: Quality & Coverage (T-016 ~ T-030)

**Created**: 2026-02-08
**Based on**: Codebase Analysis (테스트 커버리지 70% → 목표 85%+)
**현황**: 11개 패키지 테스트 없음, rpcproxy 안전성 문제, 에러 처리 미흡

---

## P0: Critical (즉시 수정)

### T-016: rpcproxy Unsafe Type Assertion 수정 ✅ (2026-02-08)
- **파일**: `pkg/rpcproxy/proxy.go`, `pkg/rpcproxy/queue.go`, `pkg/rpcproxy/cache.go`
- **현재**: 20+ 인스턴스에서 `value.(Type)` 사용 → panic 가능
- **작업**:
  - `proxy.go:131,261,377,437,497,552,655,659,663,799` 안전한 assertion으로 변경
  - `queue.go:69,84,126,168,193` 안전한 assertion으로 변경
  - `cache.go:184` 안전한 assertion으로 변경
  - 에러 반환 또는 로깅 추가
- **완료**: 16개 unsafe type assertion을 `value, ok := x.(*Type)` 패턴으로 변경
  - proxy.go: 6 cache assertions + 3 handleRequest payload assertions + 1 sync.Map assertion
  - queue.go: 4 heap.Pop assertions + 1 Push assertion
  - cache.go: 1 evictOldest assertion
- **담당**: Claude

### T-017: Critical 에러 무시 수정 ✅ (2026-02-08)
- **파일**: 다수
- **현재**: 비즈니스 로직에서 에러 무시 (`_ =`)
- **작업**:
  - `pebble_token_holder.go:48` — `big.Int.SetString` 실패 시 데이터 무결성 문제
  - `pebble_address_index.go:352,436,602,685` — logIndex 파싱 실패 시 잘못된 데이터
  - `rpcproxy/proxy.go:608,613` — gas 값 파싱 실패 시 잘못된 gas 추정
  - `cmd/indexer/main.go:853` — shutdown 시 에러 로깅 추가
- **완료**: 8개 에러 무시 수정
  - pebble_token_holder.go: `SetString` 반환값 체크 (`ok` 확인 후 할당)
  - pebble_address_index.go: 4곳 `fmt.Sscanf` 반환값 체크 + warn 로깅 + continue
  - rpcproxy/proxy.go: `SetString`(value) + `Sscanf`(gas, gasUsed) 3곳 반환값 체크 + warn 로깅
  - cmd/indexer/main.go: `rpcProxy.Stop()` 에러 로깅 추가
- **담당**: Claude

---

## P1: High (1주 이내)

### T-018: rpcproxy 테스트 추가 ✅ (2026-02-08)
- **파일**: `pkg/rpcproxy/` (2,038줄, 테스트 0)
- **현재**: 가장 큰 미테스트 패키지, 동시성 복잡
- **작업**:
  - Cache: key 빌드, TTL, eviction, LRU
  - Rate limiter: per-IP, global, burst
  - Circuit breaker: failure threshold, recovery, half-open
  - Worker pool: task 분배, priority queue, backpressure
  - Priority queue: Push, Pop, timeout
  - Proxy: request 처리, metrics
- **완료**: 67개 테스트 추가 (3파일)
  - `cache_test.go`: 23개 (Get/Set, TTL, LRU eviction, Stats, HitRate, GetOrSet, CacheKeyBuilder)
  - `queue_test.go`: 16개 (PriorityQueue, MultiPriorityQueue, blocking, timeout, drain, concurrent)
  - `worker_test.go`: 28개 (WorkerPool, CircuitBreaker, ProxyError, Config, RateLimiter)
- **담당**: Claude

### T-019: compiler 테스트 추가 ✅ (2026-02-08)
- **파일**: `pkg/compiler/` (614줄, 테스트 0)
- **현재**: solc 바이너리 실행, 보안 민감
- **작업**:
  - Standard JSON 입력 컴파일
  - 컴파일 출력 파싱 (bytecode, ABI, metadata)
  - 에러 처리 (실패, 타임아웃, 잘못된 버전)
  - `exec.CommandContext` 입력 검증 테스트
- **완료**: 31개 테스트 추가
  - Config: Validate, DefaultConfig
  - Version validation: 16 cases (semver, path traversal, shell injection)
  - Path validation, compilation options validation
  - parseCompilationOutput/parseStandardJsonOutput (JSON/StandardJSON)
  - IsVersionAvailable, ListVersions, getCompilerPath, isStandardJsonInput
  - ImmutableReferences, Close, error sentinels
- **담당**: Claude

### T-020: compiler solcPath 입력 검증 ✅ (2026-02-08)
- **파일**: `pkg/compiler/solc.go:112,312`
- **현재**: `exec.CommandContext(ctx, solcPath, ...)` — 외부 입력으로 경로 조작 가능
- **작업**:
  - 컴파일러 버전 화이트리스트 검증
  - 경로/인수 sanitization
  - 테스트 추가
- **완료**:
  - `validateVersion()`: semver 정규식으로 버전 포맷 검증 (경로 탈출/쉘 메타문자 차단)
  - `validateSolcPath()`: 해석된 경로가 BinDir 내에 있는지 검증
  - `Compile()`, `DownloadVersion()`에 검증 적용
- **담당**: Claude

### T-021: verifier 테스트 추가
- **파일**: `pkg/verifier/` (테스트 0)
- **현재**: 컨트랙트 소스 검증 — 핵심 기능
- **작업**:
  - Bytecode 비교 (metadata 제외)
  - Constructor argument 처리
  - Verification 워크플로우 (compile → fetch → compare)
  - 에러 케이스 (mismatch, no deployed code, invalid args)
- **예상 시간**: 8시간
- **담당**: -

### T-022: consensus/wbft 테스트 추가
- **파일**: `pkg/consensus/wbft/` (478줄, 테스트 0)
- **현재**: WBFT 합의 파싱 — 복잡한 RLP 디코딩
- **작업**:
  - Block extra data 파싱 (RLP)
  - Validator set 추출 및 캐싱
  - Epoch 경계 감지
  - Edge cases: malformed data, empty validators
- **예상 시간**: 10시간
- **담당**: -

### T-023: adapters/evm 테스트 추가
- **파일**: `pkg/adapters/evm/` (294줄, 테스트 0)
- **현재**: EVM 체인 어댑터 — 기본 블록체인 상호작용
- **작업**:
  - Block fetching (GetLatestBlockNumber, GetBlockByNumber)
  - Transaction parsing (ParseTransaction, ParseLogs)
  - Contract creation 감지
  - Adapter lifecycle
- **예상 시간**: 6시간
- **담당**: -

### T-024: adapters/stableone 테스트 추가
- **파일**: `pkg/adapters/stableone/` (487줄, 테스트 0)
- **현재**: StableOne 어댑터 — WBFT + 시스템 컨트랙트
- **작업**:
  - 초기화 및 설정
  - WBFT consensus parser 통합
  - System contracts handler
  - ChainInfo metadata 검증
- **예상 시간**: 8시간
- **담당**: -

---

## P2: Medium (2-4주)

### T-025: token 서비스 테스트 추가
- **파일**: `pkg/token/` (1,274줄, 테스트 0)
- **현재**: 토큰 감지/메타데이터 서비스
- **작업**:
  - ERC165 인터페이스 감지
  - ERC20/721/1155 메타데이터 fetch
  - Storage CRUD
  - Block processor 통합
- **예상 시간**: 15시간
- **담당**: -

### T-026: etherscan API 테스트 추가
- **파일**: `pkg/api/etherscan/` (432줄, 테스트 0)
- **현재**: Etherscan 호환 REST API
- **작업**:
  - HTTP 요청/응답 핸들링
  - 쿼리 파라미터 파싱 및 검증
  - Contract verification 엔드포인트
  - Etherscan API 응답 포맷 호환성
- **예상 시간**: 10시간
- **담당**: -

### T-027: GraphQL 파싱 에러 처리 개선 ✅ (2026-02-08)
- **파일**: `pkg/api/graphql/resolvers_dynamic_contract.go:80,83,233`
- **현재**: `strconv.ParseUint` 에러 무시, 0으로 기본값
- **작업**:
  - 유효하지 않은 입력에 대해 클라이언트에 에러 반환
  - 입력 검증 메시지 추가
- **완료**: 3곳 `strconv.ParseUint` 에러를 `fmt.Errorf`로 반환
  - `fromBlock`, `toBlock`, `blockNumber` 파싱 실패 시 명확한 에러 메시지
- **담당**: Claude

### T-028: Slice 용량 힌트 추가 ✅ (2026-02-08)
- **파일**: `pkg/storage/pebble_address_index.go`, `pebble_wbft.go`
- **현재**: `make([]Type, 0)` 후 루프에서 append → 반복 할당
- **완료**: 4곳 용량 힌트 적용
  - `pebble_address_index.go`: `make([]*InternalTransaction, 0, 16)`
  - `pebble_wbft.go`: `make([]*ValidatorSigningActivity, 0, 32)`, `make([]common.Address, 0, 32)` x2
- **담당**: Claude

### T-029: context.Background() 감사 및 수정 ✅ (2026-02-08)
- **파일**: 다수
- **현재**: 부모 context 무시 → 리소스 누수/지연 shutdown 가능
- **완료**: 감사 완료 — 3곳 모두 적절한 사용으로 판단
  - `contract_oracle.go:78`: 생성자에서 초기화용 → Background() 적절
  - `worker.go:42`: WorkerPool 생성자에서 lifetime context → Background() 적절
  - `client.go:73`: Client 생성자에서 초기화 → Background() 적절
  - 모두 constructor/initialization context이므로 변경 불필요
- **담당**: Claude

---

## P3: Low (1-3개월)

### T-030: 나머지 패키지 테스트 추가
- **파일**: `pkg/types/`, `pkg/types/chain/`, `pkg/price/`
- **작업**:
  - `pkg/types` (89줄) — 타입 직렬화/역직렬화 테스트
  - `pkg/types/chain` (287줄) — 인터페이스 계약 검증, mock 구현
  - `pkg/price` (258줄) — Oracle 가용성, 가격 조회, NoOpOracle
- **예상 시간**: 15시간
- **담당**: -

---

## Phase 2 Summary

| Priority | Tasks | Est. Hours |
|----------|-------|------------|
| P0 | 2 | 7h |
| P1 | 7 | 67h |
| P2 | 5 | 32h |
| P3 | 1 | 15h |
| **Total** | **15** | **121h** |

---

## Task Dependencies (Phase 2)

```
T-016 (Type Assertion) ──┐
T-017 (에러 수정)       ──┼── T-018 (rpcproxy 테스트) 선행 권장
                          │
T-020 (solcPath 검증) ────┤── T-019 (compiler 테스트) 와 병행
                          │
T-022 (wbft 테스트) ──────┤── T-024 (stableone 테스트) 선행
T-023 (evm 테스트) ───────┘

T-025 (token 테스트) ─── 독립
T-026 (etherscan 테스트) ─── 독립
T-027 (GraphQL 에러) ─── 독립
T-028 (Slice 힌트) ─── 독립
T-029 (context 감사) ─── 독립
T-030 (나머지 테스트) ─── T-023~T-026 이후 권장
```

---

## Checklist (Phase 2)

- [x] T-016: rpcproxy Unsafe Type Assertion 수정 (2026-02-08 완료)
- [x] T-017: Critical 에러 무시 수정 (2026-02-08 완료)
- [x] T-018: rpcproxy 테스트 추가 (2026-02-08 완료)
- [x] T-019: compiler 테스트 추가 (2026-02-08 완료)
- [x] T-020: compiler solcPath 입력 검증 (2026-02-08 완료)
- [x] T-021: verifier 테스트 추가 (2026-02-08 완료, 49 tests)
- [x] T-022: consensus/wbft 테스트 추가 (2026-02-08 완료, 37 tests)
- [x] T-023: adapters/evm 테스트 추가 (2026-02-08 완료, 29 tests)
- [x] T-024: adapters/stableone 테스트 추가 (2026-02-08 완료, 35 tests)
- [x] T-025: token 서비스 테스트 추가 (2026-02-08 완료, 42 tests)
- [x] T-026: etherscan API 테스트 추가 (2026-02-08 완료, 35 tests)
- [x] T-027: GraphQL 파싱 에러 처리 개선 (2026-02-08 완료)
- [x] T-028: Slice 용량 힌트 추가 (2026-02-08 완료)
- [x] T-029: context.Background() 감사 및 수정 (2026-02-08 완료)
- [x] T-030: 나머지 패키지 테스트 추가 (2026-02-08 완료, types 12 + chain 16 + price 17 = 45 tests)

---
---

# Phase 3: Security, Performance & Coverage (T-031 ~ T-045)

**Created**: 2026-02-08
**Based on**: Phase 1-2 완료 후 코드베이스 심층 분석
**현황**: 전체 커버리지 47.4%, 보안/성능 개선 필요

---

## P0: Critical (즉시 수정)

### T-031: JSON-RPC Request Body 크기 제한
- **파일**: `pkg/api/jsonrpc/server.go:37`
- **현재**: `io.ReadAll(r.Body)` — 크기 제한 없음 → 메모리 고갈 DoS
- **작업**:
  - `http.MaxBytesReader(w, r.Body, maxSize)` 적용
  - 최대 크기 설정 가능하게 (기본 1MB)
  - 초과 시 413 응답 반환
- **예상 시간**: 1시간
- **담당**: -

### T-032: Rate Limiter 자동 정리
- **파일**: `pkg/api/middleware/ratelimit.go` (109줄)
- **현재**: IP별 limiter가 `map[string]*rate.Limiter`에 무한 누적 → 메모리 누수
- **작업**:
  - TTL 기반 자동 정리 (기본 10분)
  - 주기적 CleanupLimiters 고루틴 추가
  - 또는 LRU + maxSize 적용
- **예상 시간**: 2시간
- **담당**: -

### T-033: Address Overview N+1 쿼리 최적화 ✅
- **파일**: `pkg/api/graphql/resolvers_address.go`, `pkg/api/graphql/resolvers.go`, `pkg/storage/storage.go`, `pkg/storage/pebble_transactions.go`
- **현재**: ~~txHash 루프에서 `GetTransaction()` 개별 호출 (10,000tx → 10,000+ DB 쿼리)~~
- **완료**:
  - Batch `GetTransactions(ctx, []Hash)` 메서드를 Reader 인터페이스 및 PebbleStorage에 추가
  - `resolvers_address.go`: addressOverview의 sent/received 카운팅을 단일 batch 호출로 변경
  - `resolvers.go`: GetAddressTransactions 리졸버의 tx 조회를 단일 batch 호출로 변경
  - 7개 mock storage 구현체에 GetTransactions 추가
- **예상 시간**: 6시간
- **담당**: Claude

---

## P1: High (1주 이내)

### T-034: X-Forwarded-For 헤더 검증
- **파일**: `pkg/api/middleware/ratelimit.go:69-73`
- **현재**: `X-Forwarded-For`, `X-Real-IP` 무조건 신뢰 → rate limit 우회 가능
- **작업**:
  - Trusted proxy 목록 설정
  - IP 형식 검증
  - 신뢰할 수 없는 경우 `r.RemoteAddr` 사용
- **예상 시간**: 2시간
- **담당**: -

### T-035: HTTP Client 타임아웃 추가
- **파일**: `pkg/compiler/solc.go:489`
- **현재**: `http.DefaultClient.Do(req)` — 타임아웃 없음 → 커넥션 행(hang)
- **작업**:
  - `&http.Client{Timeout: 60*time.Second}` 사용
  - 다운로드 progress 모니터링 추가
- **예상 시간**: 1시간
- **담당**: -

### T-036: Log 쿼리 블록 범위 제한
- **파일**: `pkg/storage/pebble_logs.go:43-66`
- **현재**: fromBlock~toBlock 무제한 범위 쿼리 → 메모리 고갈
- **작업**:
  - 최대 블록 범위 제한 (기본 10,000 블록)
  - 초과 시 에러 반환 (ErrBlockRangeTooLarge)
  - 결과 개수 제한 (기본 10,000 로그)
- **예상 시간**: 2시간
- **담당**: -

### T-037: adapters/factory 테스트 추가
- **파일**: `pkg/adapters/factory/` (1.5% → 목표 60%+)
- **현재**: client.go 545줄, 43개 함수 — 거의 미테스트
- **작업**:
  - EVMClient: JSON-RPC 호출 mock 테스트
  - rpcTransaction: UnmarshalJSON, FeeDelegation 파싱
  - parseRawBlock/parseRawBlockWithMetas
  - Factory: createForced, createByNodeType 분기
  - flexibleUint64 UnmarshalJSON 엣지케이스
- **예상 시간**: 10시간
- **담당**: -

### T-038: events 패키지 테스트 보강
- **파일**: `pkg/events/` (26.0% → 목표 60%+)
- **현재**: bus.go 551줄 — 이벤트 라우팅 핵심 로직
- **작업**:
  - EventBus: 구독/발행/필터링 단위 테스트
  - EventParser: 로그 디코딩, 이벤트 매칭
  - EventRegistry: 핸들러 등록/조회
  - 동시성 시나리오 (concurrent publish/subscribe)
- **예상 시간**: 8시간
- **담당**: -

### T-039: fetch 패키지 테스트 보강
- **파일**: `pkg/fetch/` (31.5% → 목표 50%+)
- **현재**: fetcher.go 767줄 — 블록 처리 파이프라인
- **작업**:
  - fetcher_events.go: publishBlockEvents, detectSystemEvents
  - fetcher_indexing.go: 주소 인덱싱, fee delegation
  - fetcher_gaps.go: 갭 감지, 복구 로직
  - fetcher_processing.go: 블록 처리 워크플로우
- **예상 시간**: 12시간
- **담당**: -

---

## P2: Medium (2-4주)

### T-040: eventbus 테스트 보강
- **파일**: `pkg/eventbus/` (31.8% → 목표 55%+)
- **현재**: Kafka/Redis 어댑터 연결 없이 테스트 가능한 로직 미테스트
- **작업**:
  - KafkaEventBus: echo prevention, message handling (broker 불필요)
  - RedisEventBus: 직렬화, 로컬 전달, 상태 관리 (Redis 불필요)
  - Factory: 생성 분기, degraded mode, hybrid 로직
- **예상 시간**: 6시간
- **담당**: -

### T-041: nolint 정리 및 미사용 필드 제거 ✅
- **파일**: 다수 (17개 `//nolint` 인스턴스)
- **현재**: 미사용 필드에 nolint 억제 → 코드 건강도 저하
- **대상**:
  - `pkg/eventbus/redis_adapter.go:64` — `mu sync.RWMutex //nolint:unused`
  - `pkg/api/health.go:99` — `lastCheck time.Time //nolint:unused`
  - `pkg/api/graphql/types*.go` — 미사용 GraphQL 타입 5개
- **작업**:
  - 실제 필요한 필드인지 검증
  - 불필요하면 삭제, 필요하면 사용처 추가 또는 주석 설명
- **완료**: 17개 nolint 인스턴스 모두 검증 후 삭제 (미사용 확인)
  - 구조체 필드 2개 제거 (redis_adapter.go mu, health.go lastCheck)
  - GraphQL 타입 선언+초기화 코드 9개 삭제 (types.go, types_multichain.go, types_watchlist.go)
  - 미사용 함수 4개 삭제 (methods_notification.go, methods_logs.go, resolvers_*.go)
  - 미사용 fmt import 정리 (methods_notification.go)
- **예상 시간**: 2시간
- **담당**: -

### T-042: Deprecated Must*() 함수 마이그레이션 ✅
- **파일**: 4곳
  - `pkg/consensus/registry.go:103` — MustRegister()
  - `pkg/eventbus/factory.go:147` — MustCreate()
  - `pkg/storage/backend.go:204` — MustRegister()
  - `pkg/adapters/factory/factory.go:304` — MustCreateAdapter()
- **현재**: Deprecated 주석 있으나 아직 export됨 → panic 위험
- **작업**:
  - 모든 호출부를 error-returning 버전으로 마이그레이션
  - Must*() 함수를 internal로 이동하거나 제거
- **완료**: 모든 Must*() 함수 삭제 완료
  - 테스트 코드의 MustRegister/MustCreate 호출을 Register/Create + require.NoError로 마이그레이션
  - panic 테스트를 error-returning 테스트로 변환
  - 4개 Must* 함수 + 2개 글로벌 래퍼 함수 삭제
- **예상 시간**: 3시간
- **담당**: -

### T-043: Critical 경로 에러 로깅 보강 ✅
- **파일**: 다수
- **현재**: 중요한 경로에서 에러 무시 (`_ =`)
- **대상**:
  - `pkg/resilience/connection.go:295` — 이벤트 캐시 실패 무시
  - `pkg/notifications/service.go:594-625` — 알림 스토리지 에러 무시
  - `pkg/api/etherscan/handler.go:132` — Sscanf 파싱 에러 무시
- **작업**:
  - 에러 발생 시 최소 warn 레벨 로깅 추가
  - 비즈니스 임팩트에 따라 metric 카운터 추가
- **완료**: 모든 `_ =` 에러 무시를 warn 레벨 로깅으로 변경
  - connection.go: eventCache.Store 실패 시 warn 로깅
  - service.go: UpdateNotificationStatus, SaveDeliveryHistory, IncrementStats 실패 시 warn 로깅 (7곳)
  - handler.go: Sscanf 파싱 실패 시 warn 로깅 + default 값 유지
- **예상 시간**: 2시간
- **담당**: -

### T-044: Slice/Map 용량 힌트 추가 (2차)
- **파일**: 다수
- **현재**: 루프 내 append 시 capacity 미지정 → 반복 재할당
- **대상**:
  - `pkg/storage/pebble_logs.go:48,57,66` — 로그 쿼리 결과
  - `pkg/watchlist/service.go:308,342,370,472` — 워치리스트 매칭
  - `pkg/api/middleware/ratelimit.go:23` — limiter map
- **작업**:
  - 예상 크기 기반 capacity 힌트 추가
  - 또는 최대 크기 제한 적용
- **예상 시간**: 2시간
- **담당**: -

---

## P3: Low (1-3개월)

### T-045: API 인증/인가 레이어 추가 ✅
- **파일**: `pkg/api/middleware/auth.go`, `pkg/api/config.go`, `pkg/api/server.go`
- **현재**: ~~GraphQL, JSON-RPC, WebSocket, Etherscan API 모두 인증 없음~~
- **완료**:
  - API Key 미들웨어 구현 (`X-API-Key` 헤더, `api_key` 쿼리 파라미터, `Authorization: Bearer` 지원)
  - 상수 시간 비교로 타이밍 공격 방지
  - health/metrics/version 엔드포인트 인증 바이패스
  - Config에 `EnableAPIKeyAuth`, `APIKeys` 필드 추가
  - 인증 활성화 시 API 키 미설정 검증
  - 인증된 키 레이블을 request context에 저장
  - 13개 테스트 작성 (유효/무효 키, 바이패스 경로, 컨텍스트, 우선순위 등)
- **예상 시간**: 16시간
- **담당**: Claude

---

## Phase 3 Summary

| Priority | Tasks | Est. Hours |
|----------|-------|------------|
| P0 | 3 | 9h |
| P1 | 6 | 35h |
| P2 | 5 | 15h |
| P3 | 1 | 16h |
| **Total** | **15** | **75h** |

---

## Coverage Targets (Phase 3)

| Package | Current | Target | Delta |
|---------|---------|--------|-------|
| `adapters/factory` | 1.5% | 60% | +58.5% |
| `events` | 26.0% | 60% | +34.0% |
| `fetch` | 31.5% | 50% | +18.5% |
| `eventbus` | 31.8% | 55% | +23.2% |
| **전체** | **47.4%** | **55%+** | **+7.6%+** |

---

## Task Dependencies (Phase 3)

```
T-031 (Body 제한) ─── 독립
T-032 (Rate Limiter) ─── 독립
T-033 (N+1 쿼리) ─── 독립

T-034 (XFF 검증) ─── T-032 이후 권장
T-035 (HTTP 타임아웃) ─── 독립
T-036 (Log 범위 제한) ─── 독립

T-037 (factory 테스트) ─── 독립
T-038 (events 테스트) ─── 독립
T-039 (fetch 테스트) ─── T-038 이후 권장 (events 의존)

T-040 (eventbus 테스트) ─── 독립
T-041 (nolint 정리) ─── 독립
T-042 (Must* 마이그레이션) ─── 독립
T-043 (에러 로깅) ─── 독립
T-044 (Slice 힌트) ─── 독립

T-045 (API 인증) ─── T-034 이후 권장
```

---

## 권장 실행 순서

```
1차 (보안): T-031 → T-032 → T-034 → T-035
2차 (성능): T-033 → T-036 → T-044
3차 (테스트): T-037 → T-038 → T-039 → T-040
4차 (정리): T-041 → T-042 → T-043
5차 (장기): T-045
```

---

## Checklist (Phase 3)

- [x] T-031: JSON-RPC Request Body 크기 제한 ✅
- [x] T-032: Rate Limiter 자동 정리 ✅
- [x] T-033: Address Overview N+1 쿼리 최적화 ✅
- [x] T-034: X-Forwarded-For 헤더 검증 ✅
- [x] T-035: HTTP Client 타임아웃 추가 ✅
- [x] T-036: Log 쿼리 블록 범위 제한 ✅
- [x] T-037: adapters/factory 테스트 추가 (1.5%→43.5%) ✅
- [x] T-038: events 패키지 테스트 보강 (26%→78.2%) ✅
- [x] T-039: fetch 패키지 테스트 보강 (31.5%→50.1%) ✅
- [x] T-040: eventbus 테스트 보강 (31.8%→61.1%) ✅
- [x] T-041: nolint 정리 및 미사용 필드 제거 ✅
- [x] T-042: Deprecated Must*() 함수 마이그레이션 ✅
- [x] T-043: Critical 경로 에러 로깅 보강 ✅
- [x] T-044: Slice/Map 용량 힌트 추가 (2차) ✅
- [x] T-045: API 인증/인가 레이어 추가 ✅

---
---

# Phase 4: Stability, Coverage & Hardening (T-046 ~ T-060)

**Created**: 2026-02-08
**Based on**: Phase 1-3 완료 후 코드베이스 재분석
**현황**: 전체 커버리지 53.1%, WebSocket/JSON-RPC 안정성 및 저커버리지 패키지 개선 필요

---

## P0: Critical (즉시 수정)

### T-046: WebSocket Hub goroutine leak 수정 ✅
- **파일**: `pkg/api/websocket/hub.go`
- **완료**:
  - `Run()`에 `done` 채널 기반 shutdown 시그널 추가 (`select case <-h.done`)
  - `Stop()` 호출 시 `close(h.done)`으로 Run goroutine 정상 종료
  - `DefaultMaxClients` (10,000) 제한 추가 → 초과 시 연결 거부
  - 모든 기존 테스트 통과
- **예상 시간**: 2시간
- **우선순위**: P0
- **담당**: Claude

### T-047: JSON-RPC batch 요청 크기 제한 ✅
- **파일**: `pkg/api/jsonrpc/server.go`
- **완료**:
  - `maxBatchSize = 100` 상수 추가
  - batch 크기 초과 시 `InvalidRequest` 에러 응답 반환
  - warn 로깅 추가 (batch_size, max_batch_size)
- **예상 시간**: 1시간
- **우선순위**: P0
- **담당**: Claude

### T-048: context.Background() → parent context 전파 ✅
- **파일**: `pkg/api/jsonrpc/filter_manager.go`, `pkg/api/jsonrpc/pending_pool.go`
- **완료**:
  - `NewFilterManager(parentCtx, timeout)` — parent context 수용으로 변경
  - `NewFilterManagerWithPendingPool(parentCtx, timeout, pool)` — 동일
  - `NewPendingPool(parentCtx, maxSize, ttl)` — parent context 수용으로 변경
  - 모든 테스트 호출부 수정
  - `subscription.go:75`: WebSocket은 HTTP 요청보다 오래 유지되므로 `context.Background()` 유지 (정당)
- **예상 시간**: 3시간
- **우선순위**: P0
- **담당**: Claude

---

## P1: High (1주 이내)

### T-049: GraphQL 패키지 테스트 보강 (26.5% → 50%+)
- **파일**: `pkg/api/graphql/` (27개 소스 파일, 6개 테스트 파일)
- **현재**: 26.5% 커버리지, 핵심 resolver 테스트 부족
- **작업**:
  - `resolvers_notification.go` (688 lines) 테스트 추가
  - `resolvers_watchlist.go` (351 lines) 테스트 추가
  - `resolvers_multichain.go` (311 lines) 테스트 추가
  - `resolvers_dynamic_contract.go` (297 lines) 테스트 추가
- **예상 시간**: 8시간
- **우선순위**: P1

### T-050: JSON-RPC 패키지 테스트 보강 (36.9% → 55%+) ✅ DONE (67.8%)
- **파일**: `pkg/api/jsonrpc/` (14개 소스 파일, 5개 테스트 파일)
- **현재**: 36.9% 커버리지, 주요 methods 테스트 부재
- **작업**:
  - `methods_filter.go` (369 lines) 테스트 추가
  - `methods_systemcontracts.go` (486 lines) 테스트 추가
  - `methods_logs.go` (237 lines) 테스트 추가
- **예상 시간**: 6시간
- **우선순위**: P1

### T-051: client 패키지 테스트 보강 (12.3% → 50%+) ✅ DONE
- **파일**: `pkg/client/`
- **결과**: 12.3% → **89.3%** 커버리지
- **작업 완료**:
  - Mock JSON-RPC HTTP 서버 인프라 구축 (단일/배치 요청 처리)
  - 18개 exported 메서드 전체 성공/에러 케이스 테스트
  - BatchGetBlocks, BatchGetReceipts 등 배치 작업 테스트
  - (WebSocket 전용 메서드 SubscribeNewHead, SubscribePendingTransactions 제외)
- **우선순위**: P1

### T-052: price 패키지 테스트 보강 (30.3% → 60%+) ✅ DONE
- **파일**: `pkg/price/`
- **결과**: 30.3% → **84.2%** 커버리지
- **작업 완료**:
  - ContractOracle 전체 메서드 테스트 (GetTokenPrice, GetNativePrice, GetTokenValue)
  - Mock ethclient 활용 성공/에러/빈결과 테스트
  - NOTE: checkAvailability ↔ GetNativePrice 무한 재귀 버그 발견 (lines 88,144)
    - `checkAvailability()` → `GetNativePrice()` → `checkAvailability()` 무한 루프
    - checkAvailability 내부 GetNativePrice 호출 경로(lines 88-99) 테스트 불가
- **우선순위**: P1

### T-053: adapters/detector 테스트 보강 (16.9% → 50%+) ✅ DONE
- **파일**: `pkg/adapters/detector/`
- **결과**: 16.9% → **100%** 커버리지
- **작업 완료**:
  - Mock JSON-RPC 서버로 전체 Detect 오케스트레이션 테스트
  - Anvil/Geth/Unknown/에러 감지 시나리오 전체 커버
  - DetectFromRPCURL, Close 등 모든 exported 메서드 테스트
- **우선순위**: P1

### T-054: adapters/anvil 테스트 보강 (9.4% → 40%+) ✅ DONE
- **파일**: `pkg/adapters/anvil/`
- **결과**: 9.4% → **94.3%** 커버리지
- **작업 완료**:
  - mockEVMClient (evm.Client 인터페이스 7개 메서드 구현)
  - NewAdapter, NewAdapterWithRPC 생성자 테스트
  - AnvilClient RPC 메서드 13개 전체 테스트 (Mine, SetBalance, Snapshot, Revert 등)
- **우선순위**: P1

---

## P2: Medium (2주 이내)

### T-055: rpcproxy 패키지 테스트 보강 (47.7% → 65%+)
- **파일**: `pkg/rpcproxy/`
- **현재**: 47.7% 커버리지
- **작업**:
  - proxy.go 통합 테스트 추가
  - 에러 핸들링 및 재시도 경로 테스트
  - 타임아웃/취소 시나리오 테스트
- **예상 시간**: 4시간
- **우선순위**: P2

### T-056: crypto/bls 패키지 테스트 보강 (44.0% → 65%+)
- **파일**: `pkg/crypto/bls/`
- **현재**: 44.0% 커버리지
- **작업**:
  - BLS 서명 검증 엣지 케이스 테스트
  - 잘못된 키/서명 처리 테스트
  - 벤치마크 테스트 추가
- **예상 시간**: 3시간
- **우선순위**: P2

### T-057: internal/config 테스트 보강 (66.8% → 80%+)
- **파일**: `internal/config/`
- **현재**: 66.8% 커버리지
- **작업**:
  - 설정 파일 로딩 엣지 케이스 테스트
  - 환경변수 오버라이드 테스트
  - 유효성 검증 경계값 테스트
- **예상 시간**: 2시간
- **우선순위**: P2

### T-058: API server 통합 테스트 보강 (41.3% → 60%+)
- **파일**: `pkg/api/`
- **현재**: 41.3% 커버리지
- **작업**:
  - Server 생성/시작/종료 라이프사이클 테스트
  - 미들웨어 통합 테스트 (auth + rate limit 조합)
  - Config validation 엣지 케이스 테스트
  - CORS 미들웨어 테스트
- **예상 시간**: 4시간
- **우선순위**: P2

---

## P3: Low (1-3개월)

### T-059: compiler 패키지 테스트 추가 (48.9% → 65%+)
- **파일**: `pkg/compiler/`
- **현재**: 48.9% 커버리지, solc.go 핵심 로직 테스트 부재
- **작업**:
  - Solidity 컴파일러 래퍼 단위 테스트
  - 컴파일 출력 파싱 테스트
  - 에러 핸들링 경로 테스트
- **예상 시간**: 4시간
- **우선순위**: P3

### T-060: interface{} → any 마이그레이션
- **파일**: 프로젝트 전체 (980+ 사용처)
- **현재**: Go 1.18+ 호환이지만 레거시 `interface{}` 사용
- **작업**:
  - `interface{}` → `any` 전역 치환
  - 빌드 및 테스트 검증
- **예상 시간**: 1시간
- **우선순위**: P3

---

## Phase 4 Summary

| Priority | Tasks | Est. Hours |
|----------|-------|------------|
| P0 (Critical) | T-046, T-047, T-048 | 6h |
| P1 (High) | T-049 ~ T-054 | 26h |
| P2 (Medium) | T-055 ~ T-058 | 13h |
| P3 (Low) | T-059 ~ T-060 | 5h |
| **Total** | **15 tasks** | **~50h** |

---

## 현재 커버리지 현황

| 패키지 | 현재 커버리지 | 목표 |
|--------|-------------|------|
| api/graphql | 26.5% | 50%+ |
| api/jsonrpc | 36.9% | 55%+ |
| api/middleware | 95.0% | 유지 |
| api (server) | 41.3% | 60%+ |
| client | 12.3% | 50%+ |
| price | 30.3% | 60%+ |
| adapters/detector | 16.9% | 50%+ |
| adapters/anvil | 9.4% | 40%+ |
| rpcproxy | 47.7% | 65%+ |
| crypto/bls | 44.0% | 65%+ |
| compiler | 48.9% | 65%+ |
| **전체** | **53.1%** | **60%+** |

---

## 의존 관계

```
T-046 (WS Hub) ─── 독립
T-047 (Batch) ─── 독립
T-048 (Context) ─── 독립
T-049 (GraphQL) ─── 독립
T-050 (JSON-RPC) ← T-047 이후 권장
T-051 (Client) ─── 독립
T-052 (Price) ─── 독립
T-053 (Detector) ─── 독립
T-054 (Anvil) ─── 독립
T-055 (RpcProxy) ─── 독립
T-056 (BLS) ─── 독립
T-057 (Config) ─── 독립
T-058 (API Server) ← T-046, T-047 이후 권장
T-059 (Compiler) ─── 독립
T-060 (interface→any) ─── 독립, 마지막에 실행 권장
```

---

## 권장 실행 순서

```
1차 (안정성): T-046 → T-047 → T-048
2차 (저커버리지): T-051 → T-053 → T-054 → T-052
3차 (핵심 API): T-049 → T-050
4차 (보완): T-055 → T-056 → T-057 → T-058
5차 (정리): T-059 → T-060
```

---

## Checklist (Phase 4)

- [x] T-046: WebSocket Hub goroutine leak 수정 ✅
- [x] T-047: JSON-RPC batch 요청 크기 제한 ✅
- [x] T-048: context.Background() → parent context 전파 ✅
- [x] T-049: GraphQL 패키지 테스트 보강 (26.5%→**52.4%**)
- [x] T-050: JSON-RPC 패키지 테스트 보강 (36.9%→**67.8%**)
- [x] T-051: client 패키지 테스트 보강 (12.3%→**89.3%**)
- [x] T-052: price 패키지 테스트 보강 (30.3%→**84.2%**)
- [x] T-053: adapters/detector 테스트 보강 (16.9%→**100%**)
- [x] T-054: adapters/anvil 테스트 보강 (9.4%→**94.3%**)
- [ ] T-055: rpcproxy 패키지 테스트 보강 (47.7%→65%+)
- [ ] T-056: crypto/bls 패키지 테스트 보강 (44.0%→65%+)
- [ ] T-057: internal/config 테스트 보강 (66.8%→80%+)
- [ ] T-058: API server 통합 테스트 보강 (41.3%→60%+)
- [ ] T-059: compiler 패키지 테스트 추가 (48.9%→65%+)
- [ ] T-060: interface{} → any 마이그레이션
