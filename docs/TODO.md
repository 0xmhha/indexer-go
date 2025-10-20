# indexer-go TODO List

> 프로젝트 진행 상황 및 작업 계획

**Last Updated**: 2025-10-20
**Current Phase**: Phase 4 완료, Phase 5 (실시간 이벤트 구독) 설계 완료

---

## 📊 프로젝트 현황

### 전체 진행률: ~70%

```
[████████████████████░░░░░░░░] 70%
```

**완료된 기능:**
- ✅ 블록체인 데이터 인덱싱 (Fetcher)
- ✅ PebbleDB 스토리지
- ✅ API 서버 (GraphQL, JSON-RPC, WebSocket)
- ✅ CLI 인터페이스
- ✅ 설정 관리 (YAML, ENV, CLI)
- ✅ Docker 지원
- ✅ 테스트 커버리지 90%+

**진행 중:**
- 🔄 실시간 이벤트 구독 시스템 설계

**예정:**
- 📋 실시간 이벤트 구독 시스템 구현
- 📋 성능 벤치마크
- 📋 프로덕션 배포 준비

---

## ✅ 완료된 작업

### Phase 1: 코어 인프라 구축 (완료)

#### Storage Layer
- [x] PebbleDB 통합
- [x] Block 저장/조회
- [x] Transaction 저장/조회
- [x] Receipt 저장/조회
- [x] Address sequence 관리
- [x] Latest height 추적
- [x] Gap detection
- [x] 테스트 커버리지 (85%+)

#### Client Layer
- [x] Ethereum RPC 클라이언트
- [x] 연결 관리 및 timeout 처리
- [x] Batch request 지원
- [x] Block 조회 (by number, by hash)
- [x] Transaction 조회
- [x] Receipt 조회
- [x] 테스트 커버리지 (16.7% - unit only)

#### Fetcher Layer
- [x] Worker pool 기반 병렬 처리
- [x] Batch fetching (chunk 단위)
- [x] Gap recovery 모드
- [x] Context cancellation 지원
- [x] Retry 메커니즘
- [x] Progress tracking
- [x] 테스트 커버리지 (87.3%)
- [x] Context cancellation 버그 수정

### Phase 2: API 서버 구축 (완료)

#### GraphQL API
- [x] gqlgen 통합
- [x] Schema 정의
- [x] Resolver 구현
- [x] Playground UI
- [x] 테스트 커버리지 (92.0%)

#### JSON-RPC API
- [x] JSON-RPC 2.0 서버
- [x] 표준 메서드 구현
  - [x] getBlock
  - [x] getTxResult
  - [x] getTxReceipt
  - [x] getLatestHeight
- [x] 에러 처리
- [x] 테스트 커버리지 (92.2%)

#### WebSocket API
- [x] Hub/Client 아키텍처
- [x] Pub/Sub 패턴
- [x] Subscribe/Unsubscribe
- [x] Ping/Pong 헬스체크
- [x] Graceful shutdown
- [x] 테스트 커버리지 (86.5%)

#### API Server
- [x] Chi router 통합
- [x] Middleware 스택
  - [x] Recovery
  - [x] Logger
  - [x] CORS
  - [x] Compression
- [x] Health check endpoint
- [x] Version endpoint
- [x] Graceful shutdown
- [x] 테스트 커버리지 (91.8%, Middleware 100%)

### Phase 3: 메인 프로그램 구현 (완료)

#### CLI Interface
- [x] Command-line flags
  - [x] 필수 플래그 (--rpc, --db)
  - [x] 인덱서 플래그 (--workers, --batch-size, --start-height)
  - [x] API 서버 플래그 (--api, --graphql, --jsonrpc, --websocket)
  - [x] 로깅 플래그 (--log-level, --log-format)
- [x] Configuration 관리
  - [x] YAML 파일 지원
  - [x] 환경변수 지원
  - [x] 우선순위 처리 (CLI > ENV > YAML > Default)
- [x] 컴포넌트 초기화
  - [x] Ethereum 클라이언트
  - [x] PebbleDB 스토리지
  - [x] Fetcher
  - [x] API 서버 (선택적)
- [x] Graceful shutdown
  - [x] Signal 처리 (SIGINT, SIGTERM)
  - [x] Context cancellation
  - [x] 리소스 정리
- [x] Version 정보 주입 (ldflags)

#### Build System
- [x] Makefile 업데이트
  - [x] Version injection
  - [x] Build targets
- [x] 컴파일 검증

### Phase 4: 설정 파일 및 문서 (완료)

#### Configuration Files
- [x] config.example.yaml
  - [x] 모든 설정 옵션
  - [x] 상세한 주석
- [x] .env.example
  - [x] 환경변수 예제
- [x] 설정 테스트 및 검증

#### Docker Support
- [x] Dockerfile
  - [x] Multi-stage build
  - [x] Alpine Linux base
  - [x] Version injection
  - [x] Health check
  - [x] Non-root user
- [x] docker-compose.yml
  - [x] 서비스 설정
  - [x] 환경변수 지원
  - [x] Volume 마운트
  - [x] Network 설정
- [x] .dockerignore

#### Documentation
- [x] README.md 업데이트
  - [x] 빌드 가이드
  - [x] Quick Start
  - [x] 설정 가이드
  - [x] API 문서

---

## 🔄 현재 작업

### Phase 5: 실시간 이벤트 구독 시스템 (설계 완료, 구현 대기)

#### 설계 문서
- [x] EVENT_SUBSCRIPTION_DESIGN.md 작성
  - [x] 요구사항 분석
  - [x] 현재 시스템 분석
  - [x] 상세 설계
  - [x] 성능 최적화 전략
  - [x] 구현 계획
  - [x] 테스트 전략
  - [x] 확장성 고려사항

---

## 📋 예정된 작업

### Phase 5: 실시간 이벤트 구독 시스템 구현 (7-10일)

#### 5.1. Event Bus 구현 (1-2일)
**파일**: `events/bus.go`, `events/types.go`

- [ ] EventBus 구조체 정의
  - [ ] Event channels (block, transaction)
  - [ ] Subscriber registry
  - [ ] Worker pool
  - [ ] Metrics
- [ ] 기본 Pub/Sub 구현
  - [ ] Publish() 메서드
  - [ ] Subscribe() 메서드
  - [ ] Unsubscribe() 메서드
- [ ] Event 타입 정의
  - [ ] BlockEvent
  - [ ] TransactionEvent
  - [ ] EventMetadata
- [ ] 테스트 작성
  - [ ] 단위 테스트
  - [ ] 통합 테스트

**성공 기준:**
- 단일 구독자에게 이벤트 전달 성공
- 테스트 커버리지 80%+

#### 5.2. Fetcher 연동 (1일)
**파일**: `fetch/fetcher.go`, `cmd/indexer/main.go`

- [ ] Fetcher에 EventBus 추가
  - [ ] EventBus 필드 추가
  - [ ] 생성자 수정
- [ ] 블록 처리 후 이벤트 발행
  - [ ] ProcessBlock() 수정
  - [ ] BlockEvent 생성 및 발행
  - [ ] TransactionEvent 생성 및 발행
- [ ] Main에서 EventBus 초기화
  - [ ] EventBus 생성
  - [ ] Fetcher와 WebSocket 연결
- [ ] 테스트
  - [ ] End-to-end 테스트
  - [ ] 이벤트 전달 검증

**성공 기준:**
- 새 블록 생성 시 WebSocket으로 이벤트 전달 확인
- 지연시간 < 100ms

#### 5.3. 필터 시스템 구현 (2-3일)
**파일**: `events/filter.go`, `events/matcher.go`

- [ ] Filter 구조체 정의
  - [ ] Address 필터 (From, To, Contract)
  - [ ] Event type 필터
  - [ ] Value 범위 필터
  - [ ] Block 범위 필터
  - [ ] Topics 필터 (EVM logs)
- [ ] Filter validation
  - [ ] 필터 유효성 검증
  - [ ] 제약 조건 체크
- [ ] FilterMatcher 구현
  - [ ] Match() 메서드
  - [ ] 각 필터 타입별 매칭 로직
- [ ] WebSocket subscribe 확장
  - [ ] Subscribe 메시지에 filter 추가
  - [ ] Client에 filter 저장
- [ ] 테스트
  - [ ] 필터 매칭 테스트
  - [ ] 다양한 필터 조합 테스트

**성공 기준:**
- 주소 필터링 동작 확인
- 복합 필터 조건 동작 확인
- 테스트 커버리지 85%+

#### 5.4. 성능 최적화 (2-3일)
**파일**: `events/index.go`, `events/worker.go`, `events/batch.go`

- [ ] Filter Index 구현
  - [ ] Address → Subscribers 맵
  - [ ] EventType → Subscribers 맵
  - [ ] Index 업데이트 로직
- [ ] Bloom Filter 적용
  - [ ] Bloom filter 라이브러리 선택
  - [ ] False positive rate 조정
  - [ ] Quick negative matching
- [ ] Worker Pool 구현
  - [ ] EventWorker 구조체
  - [ ] Worker 수 설정 (CPU 코어 수)
  - [ ] Work distribution
- [ ] Event Batching
  - [ ] Batch 크기/시간 설정
  - [ ] Batch 전송 로직
- [ ] 성능 테스트
  - [ ] 필터 매칭 벤치마크
  - [ ] 구독자 수별 성능 측정

**성능 목표:**
- 1000 구독자 @ < 50ms 지연
- 10000 구독자 @ < 100ms 지연

#### 5.5. 모니터링 & 메트릭 (1-2일)
**파일**: `events/metrics.go`, `api/server.go`

- [ ] Prometheus 메트릭 추가
  - [ ] 구독자 수 게이지
  - [ ] 이벤트 처리 속도 카운터
  - [ ] 이벤트 전달 지연 히스토그램
  - [ ] 필터 매칭 시간 히스토그램
- [ ] Subscriber 통계
  - [ ] EventsSent 카운터
  - [ ] EventsDropped 카운터
  - [ ] LastEventTime
  - [ ] AvgLatency
- [ ] Health check 개선
  - [ ] /health에 EventBus 상태 추가
  - [ ] 구독자 통계 엔드포인트
- [ ] 로깅 강화
  - [ ] Structured logging
  - [ ] Debug mode 추가

**성공 기준:**
- Prometheus 메트릭 수집 확인
- Grafana 대시보드 구성

#### 5.6. 벤치마크 & 문서화 (1-2일)
**파일**: `events/benchmark_test.go`, `docs/BENCHMARK_RESULTS.md`

- [ ] 벤치마크 테스트 작성
  - [ ] 구독자 수별 성능 (10, 100, 1000, 10000)
  - [ ] 지연시간 측정 (p50, p95, p99)
  - [ ] 필터 매칭 성능
  - [ ] 메모리 사용량
- [ ] 부하 테스트
  - [ ] Vegeta/k6 스크립트
  - [ ] Sustained load test
  - [ ] Spike test
- [ ] 성능 리포트 생성
  - [ ] 최대 구독자 수
  - [ ] 응답 시간 분포
  - [ ] 병목 지점 분석
- [ ] 문서 작성
  - [ ] API 문서 (필터 사용법)
  - [ ] 사용 가이드
  - [ ] 성능 튜닝 가이드

**목표 성능:**
```
구독자 수: 10,000+
지연시간(p50): < 10ms
지연시간(p99): < 100ms
처리량: 1000+ events/sec
메모리: < 2GB @ 10K subs
```

---

## 🎯 우선순위별 분류

### P0 (Critical) - 즉시 구현 필요
1. Event Bus 기본 구현
2. Fetcher 연동
3. 주소 기반 필터링
4. 성능 벤치마크 (기본)

### P1 (High) - Phase 5 완료 전 필요
1. Filter Index (성능 최적화)
2. Worker Pool
3. 메트릭 수집
4. End-to-end 테스트

### P2 (Medium) - Phase 5 완료 후
1. Event type 필터링
2. Bloom Filter
3. Event Batching
4. 부하 테스트

### P3 (Low) - 향후 개선
1. Redis/Kafka 통합 (수평 확장)
2. 고급 필터링 (Topics, Value range)
3. Rate limiting per subscriber
4. Event replay 기능

---

## 🚀 향후 계획 (Phase 6+)

### Phase 6: 프로덕션 준비 (예정)
- [ ] Systemd 서비스 파일
- [ ] 로그 로테이션 설정
- [ ] Prometheus 통합
- [ ] Grafana 대시보드
- [ ] 배포 스크립트
- [ ] 운영 문서

### Phase 7: 고급 기능 (예정)
- [ ] Historical data API
  - [ ] 과거 블록 범위 조회
  - [ ] 트랜잭션 히스토리
  - [ ] 주소 잔액 추적
- [ ] 분석 기능
  - [ ] Gas 사용량 통계
  - [ ] 네트워크 활동 메트릭
  - [ ] Top addresses
- [ ] 알림 기능
  - [ ] Webhook 통합
  - [ ] Email 알림
  - [ ] Slack 통합

### Phase 8: 수평 확장 (예정)
- [ ] Redis Pub/Sub 통합
- [ ] Kafka 이벤트 스트리밍
- [ ] Load balancer 설정
- [ ] Multi-node deployment

---

## 📈 진행 상황 추적

### 주간 목표

**Week 1 (현재)**
- [x] Phase 1-4 완료
- [x] Phase 5 설계 완료
- [ ] Phase 5.1 Event Bus 구현

**Week 2**
- [ ] Phase 5.2-5.3 완료 (Fetcher 연동, 필터링)
- [ ] Phase 5.4 시작 (성능 최적화)

**Week 3**
- [ ] Phase 5.4-5.6 완료
- [ ] Phase 5 전체 완료
- [ ] Phase 6 시작

### 월간 목표

**October 2025**
- [x] Core infrastructure (Phase 1-3)
- [x] Documentation (Phase 4)
- [ ] Event subscription system (Phase 5)

**November 2025**
- [ ] Production readiness (Phase 6)
- [ ] Advanced features (Phase 7)

---

## 🐛 알려진 이슈

### Critical
- 없음

### High
- 없음

### Medium
- WebSocket 재연결 로직 미구현
- Rate limiting 미구현

### Low
- GraphQL subscription (WebSocket) 미구현
- Client SDK 없음

---

## 📝 참고 문서

- [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) - 전체 구현 계획
- [EVENT_SUBSCRIPTION_DESIGN.md](./EVENT_SUBSCRIPTION_DESIGN.md) - 이벤트 구독 시스템 설계
- [STABLE_ONE_TECHNICAL_ANALYSIS.md](./STABLE_ONE_TECHNICAL_ANALYSIS.md) - Stable-One 체인 분석
- [README.md](../README.md) - 프로젝트 개요 및 사용법

---

## 🤝 기여 가이드

### 작업 진행 시
1. TODO 항목 선택
2. 브랜치 생성 (`feature/event-bus` 등)
3. 구현 및 테스트
4. PR 생성 (TODO 항목 체크)
5. 코드 리뷰 후 머지

### 커밋 메시지 규칙
```
<type>(<scope>): <subject>

feat(events): add event bus implementation
fix(fetch): fix context cancellation bug
test(events): add filter matching tests
docs(events): add API documentation
```

---

**Status**: 🚧 Active Development
**Phase**: 5.0 (Design) → 5.1 (Implementation Start)
**Next Milestone**: Event Bus 기본 구현 완료
