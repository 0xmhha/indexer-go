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

### T-009: BLS 서명 검증 구현
- **파일**: `pkg/fetch/parser.go:252`
- **현재**: Placeholder 구현
- **작업**:
  - BLS 라이브러리 통합
  - 검증 로직 구현
  - 실패 시 처리 로직
- **예상 시간**: 8시간
- **담당**: -

### T-010: Pending Transaction 추적 구현
- **파일**: `pkg/api/jsonrpc/filter_manager.go:276`
- **현재**: 미구현
- **작업**:
  - Pending TX 풀 모니터링
  - 필터 매칭 로직
  - 구독 알림
- **예상 시간**: 6시간
- **담당**: -

---

## P3: Low (1-3개월)

### T-011: Kafka EventBus 구현
- **파일**: `pkg/eventbus/factory.go:100`
- **현재**: TODO로 표시, Local로 폴백
- **작업**:
  - Kafka 클라이언트 통합
  - Producer/Consumer 구현
  - 설정 관리
- **예상 시간**: 16시간
- **담당**: -

### T-012: Fee Delegation 지원 (go-stablenet)
- **파일**:
  - `pkg/fetch/fetcher.go:1782`
  - `pkg/fetch/large_block.go:240`
  - `pkg/api/jsonrpc/methods.go:542`
  - `pkg/api/graphql/mappers.go:240`
- **현재**: go-stablenet 클라이언트 필요
- **작업**:
  - go-stablenet 의존성 추가
  - Fee Delegation 메타데이터 추출
  - API 응답 포함
- **예상 시간**: 16시간
- **담당**: -

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

## Summary

| Priority | Tasks | Est. Hours |
|----------|-------|------------|
| P0 | 1 | 0.25h |
| P1 | 3 | 10h |
| P2 | 6 | 38h |
| P3 | 5 | 42h |
| **Total** | **15** | **90.25h** |

---

## Task Dependencies

```
T-001 (Debug 제거)
    └── 독립 작업

T-002 (Token Type) ─────┐
T-003 (Token Metadata) ─┴── T-005 (pebble.go 분리) 이후 권장

T-005 (pebble.go 분리) ─┬── T-007 (함수 리팩토링)
T-006 (fetcher.go 분리) ┘

T-011 (Kafka) ──── T-013 (Redis TLS) 참고 가능

T-012 (Fee Delegation) ──── go-stablenet 의존성 선행 필요
```

---

## Checklist

- [x] T-001: Debug Print 문 제거 (2026-02-06 완료)
- [x] T-002: Token Type 감지 구현 (2026-02-06 완료)
- [x] T-003: Token Metadata 지원 추가 (2026-02-06 완료)
- [x] T-004: Notification 상세 필터 매칭 구현 (2026-02-06 완료)
- [x] T-005: pebble.go 파일 분리 (2026-02-06 완료)
- [x] T-006: fetcher.go 파일 분리 (2026-02-06 완료)
- [x] T-007: 긴 함수 리팩토링 (2026-02-06 완료)
- [x] T-008: 매직 넘버 상수화 (2026-02-06 완료)
- [ ] T-009: BLS 서명 검증 구현
- [ ] T-010: Pending Transaction 추적 구현
- [ ] T-011: Kafka EventBus 구현
- [ ] T-012: Fee Delegation 지원
- [x] T-013: Redis TLS 인증서 로드 구현 (2026-02-06 완료)
- [x] T-014: Multichain 등록 시간 저장 (2026-02-06 완료)
- [x] T-015: Panic을 Error Return으로 변경 (2026-02-06 완료)
