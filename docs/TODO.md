# indexer-go TODO List

> 미구현 기능 및 향후 개발 계획

**Last Updated**: 2026-01-30
**Status**: 핵심 기능 100% 완료, Notification System 완료, Low Priority 작업 예정

---

## 개요

**핵심 인프라**: ✅ 완료
- 블록체인 데이터 인덱싱 (Fetcher with Gap Recovery, Adaptive Optimization)
- PebbleDB 스토리지
- API 서버 (GraphQL, JSON-RPC, WebSocket)
- 실시간 이벤트 구독 시스템
- System Contracts & Governance API
- WBFT Consensus Metadata API (GraphQL & JSON-RPC)
- Address Indexing API (Contract Creation, Internal Tx, ERC20/ERC721)
- 이벤트 필터 시스템 (Topic 필터링, ABI 디코딩)
- Analytics API (Gas 통계, TPS, Top Addresses)
- Notification System (Webhook, Email, Slack)

**미구현 기능**: Horizontal Scaling, Performance & Operations, 테스트 커버리지 개선

---

## 완료된 기능

### 1. ~~Notification System~~ ✅ 완료
**우선순위**: Low
**예상 소요**: 1주
**상태**: ✅ 완료 (100%)

**완료된 항목:**
- [x] Webhook 통합
  - [x] Webhook 핸들러 구현 (`pkg/notifications/webhook.go`)
  - [x] 이벤트 전달 시스템
  - [x] 재시도 로직 (exponential backoff)
  - [x] HMAC 서명 지원
- [x] Email 알림
  - [x] SMTP 설정 및 연동 (`pkg/notifications/email.go`)
  - [x] 이메일 템플릿 시스템
  - [x] Rate limiting
  - [x] TLS 지원
- [x] Slack 통합
  - [x] Slack webhook 연동 (`pkg/notifications/slack.go`)
  - [x] 알림 포맷팅 (Rich message with attachments)
  - [x] Rate limiting
- [x] 스토리지 레이어
  - [x] PebbleDB 스토리지 구현 (`pkg/notifications/storage.go`)
  - [x] 알림 설정 CRUD
  - [x] 배달 이력 관리
  - [x] 통계 추적
- [x] Main App 통합
  - [x] 설정 파일 통합 (`internal/config/config.go`)
  - [x] 서비스 라이프사이클 통합 (`cmd/indexer/main.go`)
- [x] API 구현
  - [x] GraphQL mutations/queries (`pkg/api/graphql/resolvers_notification.go`, `types_notification.go`)
  - [x] JSON-RPC methods (`pkg/api/jsonrpc/methods_notification.go`)

---

## 미구현 기능

### 2. Horizontal Scaling
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

### 3. Performance & Operations
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

### 4. 기타 개선 사항
**우선순위**: Low

- [ ] Storage 테스트 커버리지 90% 달성
- [ ] Client SDK 개발 (별도 프로젝트)

---

## 우선순위 요약

### ✅ 완료
1. ~~**Notification System**~~ - Webhook, Email, Slack ✅

### Low Priority (6개월+)
1. **Horizontal Scaling** - Redis Pub/Sub, Kafka
2. **Performance & Operations** - Rate limiting, Caching, Monitoring
3. **테스트 커버리지 개선** - Storage 90% 목표
4. **Client SDK** - 별도 프로젝트로 분리 예정

---

## 개발 로드맵

### ✅ 완료 (2026-01)
- [x] Notification System (Webhook, Email, Slack)

### Q1 2026 (1-3월)
- [ ] Storage 테스트 커버리지 90% 달성
- [ ] 성능 모니터링 기반 구축 (Prometheus, Grafana)

### Q2 2026 (4-6월)
- [ ] Rate Limiting/Caching 고도화

### Q3 2026+ (장기)
- [ ] Horizontal Scaling (Redis, Kafka)
- [ ] Multi-region deployment
- [ ] Client SDK 개발 (별도 프로젝트)

---

## 참고 문서

### 설계 및 구조
- [ARCHITECTURE.md](./ARCHITECTURE.md) - 시스템 아키텍처 (Chain Adapters, Consensus Plugins, Storage Backend)
- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - Event Subscription API (Replay 기능 포함)
- [FRONTEND_SUBSCRIPTION_GUIDE.md](./FRONTEND_SUBSCRIPTION_GUIDE.md) - Frontend GraphQL Subscription 가이드

### 운영 및 모니터링
- [OPERATIONS_GUIDE.md](./OPERATIONS_GUIDE.md) - 운영 가이드
- [DOCKER_SETUP.md](./DOCKER_SETUP.md) - Docker 설정 가이드
- [METRICS_MONITORING.md](./METRICS_MONITORING.md) - Prometheus 메트릭 가이드

### 테스트 및 참고
- [TESTING.md](./TESTING.md) - 테스트 가이드
- [CONTRACT_VERIFICATION_GUIDE.md](./CONTRACT_VERIFICATION_GUIDE.md) - 컨트랙트 검증 가이드
- [README.md](../README.md) - 프로젝트 개요

---

## 알려진 이슈

### Low
- Storage 테스트 커버리지 86.8% (목표 90%)
- Client SDK 미제공 (별도 프로젝트로 분리 예정)
- ChainConfig 변경 이벤트 자동 감지 미구현 (수동 이벤트 발행 필요)

---

**Status**: ✅ 프로덕션 준비 완료, Notification System 완료
**Core Features**: 100% 완료
**Notification System**: ✅ 완료
**Remaining**: Scaling, Operations, Test Coverage
**Last Updated**: 2026-01-30
