# indexer-go TODO List

> 미구현 기능 및 향후 개발 계획

**Last Updated**: 2025-11-26
**Status**: 핵심 기능 100% 완료, Low Priority 작업 예정

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

**미구현 기능**: Notification System, Horizontal Scaling, Performance & Operations, 테스트 커버리지 개선

---

## 미구현 기능

### 1. Notification System
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

### Low Priority (6개월+)
1. **Notification System** - Webhook, Email, Slack
2. **Horizontal Scaling** - Redis Pub/Sub, Kafka
3. **Performance & Operations** - Rate limiting, Caching, Monitoring
4. **테스트 커버리지 개선** - Storage 90% 목표
5. **Client SDK** - 별도 프로젝트로 분리 예정

---

## 개발 로드맵

### Q1 2026 (1-3월)
- [ ] Storage 테스트 커버리지 90% 달성
- [ ] 성능 모니터링 기반 구축 (Prometheus, Grafana)

### Q2 2026 (4-6월)
- [ ] Notification System (Webhook, Email)
- [ ] Rate Limiting/Caching 고도화

### Q3 2026+ (장기)
- [ ] Horizontal Scaling (Redis, Kafka)
- [ ] Multi-region deployment
- [ ] Client SDK 개발 (별도 프로젝트)

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

### Low
- Storage 테스트 커버리지 86.8% (목표 90%)
- Client SDK 미제공 (별도 프로젝트로 분리 예정)
- ChainConfig 변경 이벤트 자동 감지 미구현 (수동 이벤트 발행 필요)

---

**Status**: ✅ 프로덕션 준비 완료, Low Priority 작업 예정
**Core Features**: 100% 완료
**Advanced Features**: Notification, Scaling, Operations
**Last Updated**: 2025-11-26
