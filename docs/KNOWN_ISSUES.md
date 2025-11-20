# Known Issues

> indexer-go 프로젝트의 알려진 이슈 및 제한사항

**Last Updated**: 2025-11-20
**Project Status**: Production Ready (98% Complete)

---

## Issue Summary

| Priority | Count | Description |
|----------|-------|-------------|
| Critical | 0 | 즉시 해결 필요 |
| High | 0 | 프로덕션 영향 |
| Medium | 0 | 기능 제한 |
| Low | 1 | 개선 사항 |

---

## Critical Priority

**없음** - 현재 Critical 이슈 없음

---

## High Priority

**없음** - 현재 High 이슈 없음

---

## Medium Priority

**없음** - 현재 Medium 이슈 없음

---

## Low Priority

### L-001: Storage Layer 테스트 커버리지 개선 필요

**Status**: In Progress
**Component**: `storage`
**Affected**: 코드 품질

**Description**:
Storage 패키지의 테스트 커버리지가 86.8%로 목표(90%)에 근접했습니다. 주요 기능과 에러 경로 대부분이 테스트되었으며, 남은 커버리지는 데이터베이스 모킹이 필요한 에러 경로입니다.

**Current Coverage** (Updated 2025-11-20):
```
Overall: 86.8% (72.4% → 86.8%, +14.4% 개선)
```

**Fully Covered Functions** (100%):
- BlockCountKey, TransactionCountKey
- MatchTransaction: 95.0%
- All schema key functions
- Batch operations (SetLatestHeight, SetBlock, SetTransaction, SetReceipt, etc.)

**Improved Functions** (>80%):
- GetBlockCount: 83.3%
- GetTransactionCount: 83.3%
- SetBalance: 77.8%
- GetTransactionsByAddressFiltered: 80.0%
- GetBlocks: 90.9%
- DeleteBlock: 83.3%
- GetTransaction: 71.4%
- SetTransaction: 73.7%

**Target Coverage**: 90%+

**Remaining Low Coverage Functions** (<80%):
- GetTransaction: 71.4% (db 에러 경로 모킹 필요)
- SetTransaction: 73.7% (db 에러 경로 모킹 필요)
- GetBlocksByTimeRange: 79.3%
- UpdateBalance: 75.0%

**Missing Tests**:
- 데이터베이스 에러 경로 (db.Get, db.Set 실패 시 - mock 필요)
- 인코딩 에러 경로 (RLP 인코딩 실패 시)

**Resolution Plan**:
1. ~~주요 0% 커버리지 함수 테스트 추가~~ ✅
2. ~~에러 경로 테스트 추가 (closed storage, nil input)~~ ✅
3. ~~배치 작업 테스트 추가~~ ✅
4. 데이터베이스 mock 인터페이스 구현 (90% 도달을 위해 필요)

**Note**: 86.8%에서 90%까지의 남은 3.2%는 데이터베이스 레벨 에러를 시뮬레이션하는 mock이 필요합니다. 현재 커버리지는 프로덕션 준비에 충분합니다.

---

## Out of Scope

다음 항목들은 이 프로젝트 범위 밖으로 별도 프로젝트에서 진행해야 합니다.

### Client SDK

**Description**:
JavaScript, Python, Go 등의 언어용 공식 Client SDK

**Includes**:
- 타입 안전 API 클라이언트
- WebSocket 자동 재연결 로직
- 구독 상태 복원
- 재시도 메커니즘

**Rationale**:
- 서버와 클라이언트 라이프사이클 분리
- 언어별 독립적인 릴리스 주기
- 별도 저장소에서 관리 권장

**Workaround**:
- GraphQL Codegen으로 타입 생성
- OpenAPI Generator로 REST 클라이언트 생성
- 프론트엔드에서 직접 재연결 로직 구현

---

## Future Considerations

### 수평 확장 관련

- **Redis Pub/Sub 통합**: 다중 인스턴스 간 이벤트 동기화
- **Kafka 스트리밍**: 대규모 이벤트 처리
- **Load Balancer 설정**: WebSocket sticky session 필요

### 성능 최적화 관련

- **Filter Index**: 100+ 구독자 시 O(1) 주소 조회
- **Bloom Filter**: 10,000+ 구독자 시 빠른 부정 매칭
- **Value Range 최적화**: big.Int 캐싱

---

## Reporting New Issues

새로운 이슈 발견 시:

1. 이 문서에 이슈 추가 (적절한 우선순위 섹션에)
2. GitHub Issues에 이슈 생성
3. 재현 가능한 테스트 케이스 포함
4. 영향 범위 및 워크어라운드 기술

### Issue Template

```markdown
### [Priority]-[Number]: Issue Title

**Status**: Open
**Component**: `affected/component`
**Affected**: What functionality is impacted

**Description**:
Clear description of the issue

**Current Behavior**:
- What happens now

**Expected Behavior**:
- What should happen

**Workaround**:
Temporary solution if available

**Resolution Plan**:
1. Step 1
2. Step 2
```

---

## Related Documents

- [TODO.md](./TODO.md) - 전체 작업 목록
- [PROGRESS.md](../PROGRESS.md) - 개발 진행 상황
- [OPERATIONS_GUIDE.md](./OPERATIONS_GUIDE.md) - 운영 가이드

---

