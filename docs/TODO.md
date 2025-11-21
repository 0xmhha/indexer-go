# indexer-go TODO List

> 프로젝트 진행 상황 및 작업 계획

**Last Updated**: 2025-11-21
**Current Work**: Stable-One 체인 특화 기능 Phase 4 (System Contracts & WBFT 완료)

---

## 프로젝트 현황

### 전체 진행률: ~99%

```
[███████████████████████████▌] 99%
```

**핵심 인프라 완료:**
- 블록체인 데이터 인덱싱 (Fetcher)
- PebbleDB 스토리지
- API 서버 (GraphQL, JSON-RPC, WebSocket)
- 실시간 이벤트 구독 시스템
- Historical Data API
- Stable-One 체인 특화 기능 Phase 3
  - EIP-1559/4844 필드
  - Fee Delegation 지원 (type 0x16)
  - go-stablenet 통합
  - GraphQL Subscription
- **System Contracts & Governance API (Phase 4)**
  - NativeCoinAdapter 이벤트 추적 (Mint/Burn)
  - Gov 컨트랙트 이벤트 추적 (5개 컨트랙트)
  - Governance Proposal API (17 GraphQL queries, 10 JSON-RPC methods)
  - 총 공급량, Minter/Validator 관리 API
- **WBFT Consensus Metadata API**
  - WBFT 블록 메타데이터 파싱 (RLP 디코딩)
  - 검증자 서명 통계 및 활동 추적
  - 에폭(Epoch) 정보 관리
  - 8개 GraphQL API 구현

**진행 중:**
- Phase 4 나머지 작업 (주소 인덱싱, 이벤트 필터)

**예정:**
- 주소 인덱싱 확장 (컨트랙트 생성, 내부 트랜잭션)
- 이벤트 필터 시스템
- 고급 기능 개발 (Analytics & Notifications)
- 수평 확장 지원 (Horizontal Scaling)

---

## 현재 작업

### Stable-One 체인 특화 기능 Phase 4 (진행 중)

> Stable-One은 go-ethereum 기반 WBFT 합의 엔진(Anzeon)을 사용하는 체인으로, Gno(Tendermint2)와 다른 구조를 가짐

#### Phase 2: Fetcher 최적화 (예정)
**우선순위**: Medium

- [ ] 워커 풀 튜닝
  - [ ] RPC rate limit 고려한 워커 수 조정
  - [ ] 동적 워커 풀 크기 조정
- [ ] Gap 감지 개선
  - [ ] 효율적인 gap detection 알고리즘
  - [ ] 자동 gap recovery 정책
- [ ] 배치 요청 고도화
  - [ ] adaptive batch sizing
  - [ ] RPC 대역폭 최적화
- [ ] Receipt 병렬화
  - [ ] `eth_getBlockReceipts` 활용
  - [ ] 대용량 블록 (105M gas) 처리 최적화

#### Phase 4: 고급 인덱싱 (진행 중)
**우선순위**: High
**참고**: STABLE_ONE_TECHNICAL_ANALYSIS.md 섹션 3.2, 3.3
**상태**: System Contracts & WBFT Metadata API 완료 (2025-11-21)

- [x] NativeCoinAdapter 이벤트 추적
  - [x] Mint/Burn 이벤트 파싱 및 저장
  - [x] 베이스 코인 총발행량 추적 (totalSupply API)
  - [x] Minter 정보 API (activeMinters, minterAllowance)
  - [x] Mint/Burn 이벤트 조회 API (GraphQL, JSON-RPC)
  - [ ] 주소별 베이스 코인 잔액 추적
- [x] Gov 컨트랙트 이벤트 추적
  - [x] GovValidator (0x1001) 이벤트 파싱 및 API
  - [x] GovMasterMinter (0x1002) 이벤트 파싱
  - [x] GovMinter (0x1003) 이벤트 파싱
  - [x] GovCouncil (0x1004) 이벤트 파싱 및 API
  - [x] Validator/Minter 권한 변경 히스토리 (GraphQL, JSON-RPC)
  - [x] Governance Proposal 조회 API (proposals, proposalVotes)
  - [x] Blacklist 관리 API
- [x] WBFT 메타데이터 파싱
  - [x] Block header Extra 필드 파서
  - [x] BLS signature 추출
  - [x] Round/committed seal 정보
  - [x] Validator 서명 통계
  - [x] GraphQL API 구현 (8개 쿼리)
- [ ] 주소 인덱싱 확장
  - [ ] 컨트랙트 생성 트랜잭션 인덱싱
  - [ ] 내부 트랜잭션 (internal tx) 추적
  - [ ] ERC20/ERC721 토큰 전송 인덱싱
- [ ] 이벤트 필터 시스템
  - [ ] Topic 기반 필터링
  - [ ] ABI 디코딩
  - [ ] 로그 인덱싱 파이프라인

#### Phase 5: 성능 및 운영 (예정)
**우선순위**: Low

- [ ] Rate Limiting/Caching 고도화
  - [ ] 엔드포인트별 rate limit
  - [ ] Redis 캐싱 통합
  - [ ] Query result 캐싱
- [ ] WBFT 모니터링 메트릭
  - [ ] Validator 서명 실패/지연 감지
  - [ ] Priority fee 변경 감지
  - [ ] Prometheus 메트릭 노출
- [ ] 대용량 블록 최적화
  - [ ] Pebble compaction 튜닝
  - [ ] 디스크 IOPS 최적화
  - [ ] 백업 전략 개선

#### 예상 구현 순서
1. **Phase 4** (고급 인덱싱) - 체인 특화 기능
2. **Phase 2** (Fetcher 최적화) - 성능 개선
3. **Phase 5** (성능 및 운영) - 프로덕션 안정화

---

## 예정된 작업

### 고급 기능 개발

#### 분석 기능 (예정)
- [ ] Gas 사용량 통계
  - [ ] 블록별 gas 사용량
  - [ ] 주소별 gas 소비
  - [ ] 시간대별 gas 트렌드
- [ ] 네트워크 활동 메트릭
  - [ ] TPS (Transactions Per Second)
  - [ ] 블록 생성 시간
  - [ ] 네트워크 활동 추세
- [ ] Top Addresses
  - [ ] 가장 활동적인 주소
  - [ ] 가장 많은 gas 소비
  - [ ] 최근 활동 주소

#### 알림 기능 (예정)
- [ ] Webhook 통합
  - [ ] Webhook 설정 API
  - [ ] 이벤트 전달 시스템
  - [ ] 재시도 로직
- [ ] Email 알림
  - [ ] SMTP 설정
  - [ ] 이메일 템플릿
  - [ ] 구독 관리
- [ ] Slack 통합
  - [ ] Slack webhook
  - [ ] 알림 포맷팅
  - [ ] 채널 관리

### 수평 확장 (예정)
- [ ] Redis Pub/Sub 통합
- [ ] Kafka 이벤트 스트리밍
- [ ] Load balancer 설정
- [ ] Multi-node deployment

---

## 우선순위별 분류

### P0 (Critical) - 즉시 구현 필요
_현재 없음 - 핵심 기능 완료_

### P1 (High) - 다음 분기 목표
1. 주소 인덱싱 확장
2. 이벤트 필터 시스템
3. 기본 분석 기능

### P2 (Medium) - 성능 개선
1. Fetcher 최적화
2. Rate Limiting/Caching 고도화
3. 분석 기능 (Gas 통계, 네트워크 메트릭)

### P3 (Low) - 향후 개선
1. 수평 확장 (Redis/Kafka)
2. 알림 기능 (Webhook, Email, Slack)
3. 대용량 블록 최적화

---

## 향후 계획

### 단기 목표 (1-2개월)
- [x] NativeCoinAdapter 이벤트 추적 구현 ✅ 완료 (2025-11-21)
- [x] Gov 컨트랙트 이벤트 추적 ✅ 완료 (2025-11-21)
- [x] WBFT 메타데이터 파서 구현 ✅ 완료 (2025-11-21)
- [ ] 주소 인덱싱 확장
- [ ] 기본 분석 기능 (Gas 통계, TPS)

### 중기 목표 (3-6개월)
- [ ] 고급 분석 대시보드
- [ ] 알림 시스템 (Webhook, Email)
- [ ] Fetcher 최적화
- [ ] Redis 캐싱 통합

### 장기 목표 (6개월+)
- [ ] 수평 확장 지원
- [ ] Kafka 이벤트 스트리밍
- [ ] Multi-region deployment
- [ ] 고급 모니터링 및 알림

---

## 월간 목표

**November 2025** (진행 중)
- [x] Phase 4: 고급 인덱싱 시작 ✅
  - [x] NativeCoinAdapter 이벤트 추적 ✅
  - [x] Gov 컨트랙트 이벤트 추적 ✅
  - [x] System Contracts & Governance API 구현 ✅
  - [x] WBFT 메타데이터 파싱 ✅

**December 2025** (예정)
- [ ] 주소 인덱싱 확장 (컨트랙트 생성, 내부 트랜잭션)
- [ ] 이벤트 필터 시스템
- [ ] 기본 분석 기능 구현
- [ ] WBFT JSON-RPC API 추가 (현재 GraphQL만 지원)

**Q1 2026** (예정)
- [ ] 알림 시스템 구현
- [ ] 성능 최적화
- [ ] 수평 확장 준비

---

## 알려진 이슈

### Critical
- 없음

### High
- 없음

### Medium
- WebSocket 재연결 로직 미구현

### Low
- Storage 테스트 커버리지 86.8% (목표 90%, 나머지는 DB mock 필요)
- Client SDK 없음 (별도 프로젝트로 분리)

---

## 참고 문서

### 핵심 문서
- [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) - 전체 구현 계획
- [STABLE_ONE_TECHNICAL_ANALYSIS.md](./STABLE_ONE_TECHNICAL_ANALYSIS.md) - Stable-One 체인 분석
- [README.md](../README.md) - 프로젝트 개요 및 사용법

### API 문서
- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - Event Subscription API 레퍼런스
- [METRICS_MONITORING.md](./METRICS_MONITORING.md) - Prometheus 모니터링 가이드
- [HISTORICAL_API_DESIGN.md](./HISTORICAL_API_DESIGN.md) - Historical Data API 설계
- [ToFrontend.md](./ToFrontend.md) - Frontend 통합 가이드
- **[SYSTEM_CONTRACTS_EVENTS_DESIGN.md](./SYSTEM_CONTRACTS_EVENTS_DESIGN.md)** - System Contracts 이벤트 설계 ✨ 신규

### 운영 문서
- [OPERATIONS_GUIDE.md](./OPERATIONS_GUIDE.md) - 프로덕션 배포 및 운영 가이드

---

## 기여 가이드

### 작업 진행 시
1. TODO 항목 선택
2. 브랜치 생성 (`feature/native-coin-adapter` 등)
3. 구현 및 테스트
4. PR 생성 (TODO 항목 체크)
5. 코드 리뷰 후 머지

### 커밋 메시지 규칙
```
<type>(<scope>): <subject>

feat(indexing): add NativeCoinAdapter event tracking
fix(fetch): optimize worker pool allocation
test(gov): add Gov contract event tests
docs(wbft): add WBFT metadata parsing guide
```

---

**Status**: 프로덕션 준비 완료 (Production Ready)
**Completion**: 99% - 핵심 기능 완료, System Contracts & WBFT Metadata API 완료 ✅
**Last Achievement**: WBFT Consensus Metadata API 구현 완료 (2025-11-21)
**Next Milestone**: 주소 인덱싱 확장 및 이벤트 필터 시스템
