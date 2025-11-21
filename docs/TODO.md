# indexer-go TODO List

> 미구현 기능 및 향후 개발 계획

**Last Updated**: 2025-11-21 (GraphQL Subscription 타입 추가 완료)
**Status**: 핵심 기능 완료, 고급 기능 개발 예정

---

## 개요

**핵심 인프라**: ✅ 완료
- 블록체인 데이터 인덱싱 (Fetcher with Gap Recovery)
- PebbleDB 스토리지
- API 서버 (GraphQL, JSON-RPC, WebSocket)
- 실시간 이벤트 구독 시스템
- System Contracts & Governance API
- WBFT Consensus Metadata API (GraphQL)
- Address Indexing API (Contract Creation, Internal Tx, ERC20/ERC721)

**미구현 기능**: 이벤트 필터, Analytics, Notifications, Horizontal Scaling, 성능 최적화

---

## 미구현 기능

### 1. 이벤트 필터 시스템
**우선순위**: High
**예상 소요**: 2-3주

- [ ] Topic 기반 필터링
  - [ ] eth_getLogs 구현
  - [ ] eth_newFilter / eth_getFilterChanges
  - [ ] eth_newBlockFilter / eth_newPendingTransactionFilter
  - [ ] 다중 topic 조합 필터링
- [ ] ABI 디코딩
  - [ ] ABI 파싱 및 저장
  - [ ] 이벤트 로그 자동 디코딩
  - [ ] 함수 호출 데이터 디코딩
- [ ] 로그 인덱싱 파이프라인
  - [ ] Topic별 인덱스 생성
  - [ ] 주소별 로그 인덱싱
  - [ ] 블록 범위 쿼리 최적화

### 2. Fetcher 최적화
**우선순위**: Medium
**예상 소요**: 2-3주

- [ ] 워커 풀 튜닝
  - [ ] RPC rate limit 고려한 워커 수 동적 조정
  - [ ] 에러율 기반 백오프 전략
  - [ ] 워커 수 자동 스케일링
- [ ] 배치 요청 고도화
  - [ ] Adaptive batch sizing (RPC 응답 시간 기반)
  - [ ] RPC 대역폭 최적화
  - [ ] 배치 크기 자동 튜닝
- [ ] 대용량 블록 처리 최적화
  - [ ] 105M gas 블록 처리 성능 개선
  - [ ] Receipt 병렬 처리 최적화
  - [ ] 메모리 사용량 최적화

### 3. Analytics API
**우선순위**: Medium
**예상 소요**: 3-4주

- [ ] Gas 사용량 통계
  - [ ] 블록별 gas 사용량 집계
  - [ ] 주소별 gas 소비 통계
  - [ ] 시간대별 gas 트렌드 분석
  - [ ] Gas price 추이 분석
- [ ] 네트워크 활동 메트릭
  - [ ] TPS (Transactions Per Second) 계산
  - [ ] 블록 생성 시간 통계
  - [ ] 네트워크 활동 추세
  - [ ] 트랜잭션 유형별 분포
- [ ] Top Addresses
  - [ ] 가장 활동적인 주소 (트랜잭션 수)
  - [ ] 가장 많은 gas 소비 주소
  - [ ] 최근 활동 주소
  - [ ] 컨트랙트 호출 빈도 Top N

### 4. Notification System
**우선순위**: Low
**예상 소요**: 2-3주

- [ ] Webhook 통합
  - [ ] Webhook 설정 API (CRUD)
  - [ ] 이벤트 전달 시스템
  - [ ] 재시도 로직 (exponential backoff)
  - [ ] Webhook 상태 모니터링
- [ ] Email 알림
  - [ ] SMTP 설정 및 연동
  - [ ] 이메일 템플릿 시스템
  - [ ] 구독 관리 API
  - [ ] 이메일 큐 및 배치 전송
- [ ] Slack 통합
  - [ ] Slack webhook 연동
  - [ ] 알림 포맷팅 (Rich message)
  - [ ] 채널 관리 및 라우팅

### 5. Horizontal Scaling
**우선순위**: Low
**예상 소요**: 4-6주

- [ ] Redis Pub/Sub 통합
  - [ ] 실시간 이벤트 브로드캐스팅
  - [ ] Multi-node 이벤트 동기화
  - [ ] Redis Cluster 지원
- [ ] Kafka 이벤트 스트리밍
  - [ ] Kafka Producer 구현
  - [ ] 블록/트랜잭션 이벤트 스트림
  - [ ] Consumer 그룹 관리
- [ ] Load Balancer 설정
  - [ ] API 서버 로드 밸런싱
  - [ ] Health check 엔드포인트
  - [ ] Graceful shutdown
- [ ] Multi-node Deployment
  - [ ] 읽기 전용 노드 지원
  - [ ] 쓰기 노드 분리
  - [ ] 데이터 일관성 보장

### 6. Performance & Operations
**우선순위**: Low
**예상 소요**: 2-3주

- [ ] Rate Limiting/Caching 고도화
  - [ ] 엔드포인트별 rate limit
  - [ ] Redis 캐싱 통합
  - [ ] Query result 캐싱 (TTL 관리)
  - [ ] CDN 통합 (정적 데이터)
- [ ] WBFT 모니터링 메트릭
  - [ ] Validator 서명 실패/지연 감지
  - [ ] Priority fee 변경 감지
  - [ ] Prometheus 메트릭 노출
  - [ ] Grafana 대시보드
- [ ] 대용량 블록 최적화
  - [ ] PebbleDB compaction 튜닝
  - [ ] 디스크 IOPS 최적화
  - [ ] Write buffer 크기 조정
  - [ ] 백업 전략 개선 (incremental backup)

### 7. WBFT JSON-RPC API
**우선순위**: Medium
**상태**: ✅ 완료 (2025-11-21)

현재 WBFT API는 GraphQL과 JSON-RPC 모두 지원합니다.

- [x] getWBFTBlockExtra
- [x] getWBFTBlockExtraByHash
- [x] getEpochInfo
- [x] getLatestEpochInfo
- [x] getValidatorSigningStats
- [x] getAllValidatorsSigningStats
- [x] getValidatorSigningActivity
- [x] getBlockSigners

### 8. 기타 개선 사항
**우선순위**: Low

- [x] Magic Number 제거 (constants 패키지로 정리 완료 - 2025-11-21)
- [ ] 주소별 베이스 코인 잔액 추적 (NativeCoinAdapter)
- [ ] WebSocket 재연결 로직 구현
- [ ] Storage 테스트 커버리지 90% 달성
- [ ] Client SDK 개발 (별도 프로젝트)

---

## 우선순위 요약

### High Priority (1-2개월)
1. **이벤트 필터 시스템** - eth_getLogs, Topic 필터링, ABI 디코딩
2. ~~**WBFT JSON-RPC API**~~ - ✅ 완료 (2025-11-21)

### Medium Priority (3-6개월)
1. **Analytics API** - Gas 통계, TPS, Top Addresses
2. **Fetcher 최적화** - 동적 워커 풀, Adaptive batch sizing

### Low Priority (6개월+)
1. **Notification System** - Webhook, Email, Slack
2. **Horizontal Scaling** - Redis Pub/Sub, Kafka
3. **Performance & Operations** - Rate limiting, Caching, Monitoring

---

## 개발 로드맵

### Q4 2025 (12월)
- [ ] 이벤트 필터 시스템 구현
- [ ] WBFT JSON-RPC API 추가

### Q1 2026 (1-3월)
- [ ] Analytics API 구현 (Gas 통계, TPS)
- [ ] Fetcher 최적화 (동적 워커 풀, Adaptive batching)

### Q2 2026 (4-6월)
- [ ] Notification System (Webhook, Email)
- [ ] Rate Limiting/Caching 고도화

### Q3 2026+ (장기)
- [ ] Horizontal Scaling (Redis, Kafka)
- [ ] Multi-region deployment
- [ ] 고급 모니터링 및 알림

---

## 참고 문서

### 구현 완료된 기능 문서
- [ToFrontend.md](./ToFrontend.md) - Frontend API 통합 가이드 (모든 구현된 API)
- [SYSTEM_CONTRACTS_EVENTS_DESIGN.md](./SYSTEM_CONTRACTS_EVENTS_DESIGN.md) - System Contracts 설계
- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - WebSocket Subscription API
- [OPERATIONS_GUIDE.md](./OPERATIONS_GUIDE.md) - 운영 가이드

### 기술 분석 문서
- [STABLE_ONE_TECHNICAL_ANALYSIS.md](./STABLE_ONE_TECHNICAL_ANALYSIS.md) - Stable-One 체인 분석
- [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) - 전체 구현 계획 (참고용)
- [README.md](../README.md) - 프로젝트 개요

---

## 알려진 이슈

### Medium
- WebSocket 재연결 로직 미구현 (클라이언트 측에서 처리 필요)

### Low
- Storage 테스트 커버리지 86.8% (목표 90%)
- Client SDK 미제공 (별도 프로젝트로 분리 예정)

## 최근 완료 작업

### 2025-11-21
- ✅ Magic Number 제거 - 모든 하드코딩된 숫자를 `internal/constants` 패키지로 정리
- ✅ GraphQL Subscription 타입 추가 - schema.go에 Subscription 타입 정의 추가 (newBlock, newTransaction, newPendingTransactions, logs)
- ✅ WBFT JSON-RPC API 구현 - GraphQL로만 제공되던 WBFT 기능을 JSON-RPC로 추가 (8개 메서드)
- ✅ WebSocket 프로토콜 수정 - graphql-transport-ws subprotocol 지원 추가, 연결 로깅 강화
- ✅ WebSocket 엔드포인트 확인 - `/graphql/ws` 경로에서 GraphQL Subscriptions 정상 작동 확인
- ✅ Frontend 통합 가이드 업데이트 - ToFrontend.md에 WBFT JSON-RPC API 및 WebSocket 설정 문서화
- ✅ Address Indexing 테스트 수정 - ERC20/ERC721 값 hex 인코딩, mock storage 개선, 모든 테스트 통과
- ✅ 코드 품질 검증 - 빌드, 테스트(55.2% coverage), go vet, gofmt 모두 통과
- ✅ GasLimit 값 이슈 조사 - go-stablenet genesis 설정 오류 확인, 인덱서는 정상 작동 (docs/GASLIMIT_ISSUE_ANALYSIS.md)

---

**Status**: ✅ 프로덕션 준비 완료
**Core Features**: 100% 완료
**Advanced Features**: 예정
**Last Updated**: 2025-11-21
