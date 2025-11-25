# indexer-go TODO List

> 미구현 기능 및 향후 개발 계획

**Last Updated**: 2025-11-25 (newPendingTransactions 구독, WebSocket 재연결 가이드 완료)
**Status**: 핵심 기능 100% 완료, Medium Priority 작업 완료, 추가 고급 기능 개발 예정

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
**상태**: ✅ 완료 (2025-11-24)

- [x] Topic 기반 필터링
  - [x] eth_getLogs 구현 (decode 파라미터 지원)
  - [x] eth_newFilter / eth_getFilterChanges (decode 파라미터 지원)
  - [x] eth_newBlockFilter / eth_newPendingTransactionFilter
  - [x] 다중 topic 조합 필터링
- [x] ABI 디코딩
  - [x] ABI 파싱 및 저장 (PebbleDB)
  - [x] 이벤트 로그 자동 디코딩
  - [x] 함수 호출 데이터 디코딩
  - [x] JSON-RPC API (setContractABI, getContractABI, removeContractABI, listContractABIs)
  - [x] GraphQL 통합 (decode 파라미터, DecodedLog 타입)
- [x] 로그 인덱싱 파이프라인
  - [x] Topic별 인덱스 생성
  - [x] 주소별 로그 인덱싱
  - [x] 블록 범위 쿼리 최적화
  - [x] 필터별 decode 설정 지속성

### 2. Fetcher 최적화
**우선순위**: Medium
**상태**: ✅ 완료 (2025-11-24)

- [x] 워커 풀 튜닝
  - [x] RPC rate limit 고려한 워커 수 동적 조정 (AdaptiveOptimizer)
  - [x] 에러율 기반 백오프 전략 (RPCMetrics)
  - [x] 워커 수 자동 스케일링 (30초 간격 자동 조정)
- [x] 배치 요청 고도화
  - [x] Adaptive batch sizing (RPC 응답 시간 기반)
  - [x] RPC 대역폭 최적화 (슬라이딩 윈도우 메트릭)
  - [x] 배치 크기 자동 튜닝 (5-50 범위 동적 조정)
- [x] 대용량 블록 처리 최적화
  - [x] 105M gas 블록 처리 성능 개선 (LargeBlockProcessor)
  - [x] Receipt 병렬 처리 최적화 (최대 10 워커, 100개 배치)
  - [x] 메모리 사용량 최적화 (배치 단위 처리, 메모리 추정)

### 3. Analytics API
**우선순위**: Medium
**상태**: ✅ 완료 (2025-11-24)

- [x] Gas 사용량 통계
  - [x] 블록별 gas 사용량 집계 (GetGasStatsByBlockRange)
  - [x] 주소별 gas 소비 통계 (GetGasStatsByAddress)
  - [x] Gas price 추이 분석 (AverageGasPrice 포함)
- [x] 네트워크 활동 메트릭
  - [x] TPS (Transactions Per Second) 계산
  - [x] 블록 생성 시간 통계
  - [x] 네트워크 활동 추세 (GetNetworkMetrics)
- [x] Top Addresses
  - [x] 가장 활동적인 주소 (GetTopAddressesByTxCount)
  - [x] 가장 많은 gas 소비 주소 (GetTopAddressesByGasUsed)

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
- [x] 주소별 베이스 코인 잔액 추적 (Balance Indexing 구현 완료 - 2025-11-25)
- [x] WebSocket 재연결 로직 (서버 ping/pong 구현 완료, 클라이언트 가이드 문서화 - 2025-11-25)
- [ ] Storage 테스트 커버리지 90% 달성
- [ ] Client SDK 개발 (별도 프로젝트)

---

## 우선순위 요약

### High Priority (1-2개월)
1. ~~**이벤트 필터 시스템**~~ - ✅ 완료 (2025-11-24) - eth_getLogs, Topic 필터링, ABI 디코딩
2. ~~**WBFT JSON-RPC API**~~ - ✅ 완료 (2025-11-21)

### Medium Priority (3-6개월)
1. ~~**Analytics API**~~ - ✅ 완료 (2025-11-24) - Gas 통계, TPS, Top Addresses
2. ~~**Fetcher 최적화**~~ - ✅ 완료 (2025-11-24) - 동적 워커 풀, Adaptive batch sizing, 대용량 블록 처리

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

### Low
- Storage 테스트 커버리지 86.8% (목표 90%)
- Client SDK 미제공 (별도 프로젝트로 분리 예정)
- ChainConfig 변경 이벤트 자동 감지 미구현 (수동 이벤트 발행 필요)

## 최근 완료 작업

### 2025-11-25
- ✅ **Balance Indexing 구현** - 네이티브 ETH 잔액 자동 추적
  - ✅ processBalanceTracking 함수 구현 - 트랜잭션 처리 시 자동으로 잔액 변화 추적
  - ✅ 송신자 잔액 차감 - (value + gas cost) 계산 및 적용
  - ✅ 수신자 잔액 증가 - value 전송량 추적
  - ✅ 컨트랙트 생성 처리 - 컨트랙트 주소로의 값 이전 지원
  - ✅ 통합 테스트 작성 - TestProcessBalanceTracking with signed transactions
  - ✅ HistoricalWriter 인터페이스 구현 - UpdateBalance, SetBalance, SetBlockTimestamp
- ✅ **GraphQL 스키마 업데이트** - API 문서와 실제 구현 동기화
  - ✅ MinerStats 타입 필드 추가 - lastBlockTime, percentage, totalRewards
  - ✅ TokenBalance 타입 필드 추가 - name, symbol, decimals, metadata
  - ✅ topMiners 쿼리 파라미터 추가 - fromBlock, toBlock
  - ✅ tokenBalances 쿼리 파라미터 추가 - tokenType 필터링
- ✅ **Frontend API 가이드 문서화** - Token Balance & Address Balance API
  - ✅ Token Balance API 섹션 추가 - 8개 필드 전체 문서화 (326줄)
  - ✅ Address Balance API 섹션 추가 - 네이티브 잔액 추적 가이드 (284줄)
  - ✅ React 통합 예제 작성 - TypeScript, Apollo Client, ethers.js
  - ✅ UI 디자인 권장사항 - NFT 메타데이터, 차트, 타임라인
  - ✅ 재인덱싱 안내 추가 - --clear-data 플래그 사용 가이드
- ✅ **프론트엔드 요청사항 처리** - 모든 API 이미 구현됨 확인
  - ✅ Issue #1 확인 - addressBalance BigInt 처리 정상 (balance.String() 사용)
  - ✅ Issue #2 해결 - contractAddress 필드 이미 존재 (프론트엔드에서 사용 가능)
  - ✅ Search API 확인 - 이미 완전히 구현됨
  - ✅ Top Miners API 확인 - 모든 필드 구현 완료
  - ✅ Token Balance API 확인 - 메타데이터 포함 완전 구현
- ✅ **WebSocket 구독 기능 강화** - 실시간 이벤트 구독 개선 및 System Events 지원
  - ✅ miner 필드 추가 - newBlock 구독에 Block.Coinbase() 정보 포함
  - ✅ Transaction Filter 구현 - from/to 주소 기반 트랜잭션 필터링 지원
  - ✅ System Events 구독 구현 - chainConfig, validatorSet 이벤트 실시간 구독
  - ✅ ChainConfigEvent 추가 - 체인 설정 변경 이벤트 (gasLimit, chainId 등)
  - ✅ ValidatorSetEvent 추가 - Validator 추가/제거/업데이트 이벤트
  - ✅ detectSystemEvents() 구현 - GovValidator 컨트랙트(0x1001) 이벤트 감지
  - ✅ WEBSOCKET_GUIDE.md 업데이트 - miner, chainConfig, validatorSet 문서화
  - ✅ 통합 테스트 통과 - 모든 WebSocket 테스트 (7/7) 성공
  - ✅ GraphQL-WS 프로토콜 준수 - payload.data 래퍼 정확히 구현
- ✅ **newPendingTransactions 구독 구현** - Mempool의 대기 중인 트랜잭션 실시간 구독
  - ✅ SubscribePendingTransactions() 추가 - Client에 RPC subscription 메서드 구현
  - ✅ StartPendingTxSubscription() 구현 - Fetcher에 pending tx 감지 및 발행 로직 추가
  - ✅ RPC EthSubscribe 활용 - newPendingTransactions WebSocket subscription
  - ✅ 실시간 트랜잭션 감지 - Transaction 해시 수신 시 전체 트랜잭션 정보 조회
  - ✅ EventBus 통합 - Pending tx를 TransactionEvent로 변환하여 실시간 발행
  - ✅ Graceful error handling - 별도 goroutine에서 실행, 에러 채널로 관리
  - ✅ WEBSOCKET_GUIDE.md 업데이트 - Section 2-7 추가, 사용법 및 주의사항 문서화
  - ✅ 제약사항 업데이트 - RPC 서버 subscription 지원 필요성 명시
  - ✅ RPC 서버 호환성 검증 - go-stablenet 코드베이스 분석, 완전 지원 확인
- ✅ **WebSocket 재연결 로직 구현** - 클라이언트 자동 재연결 및 서버 keep-alive
  - ✅ 서버 측 ping/pong - 54초마다 ping, 60초 timeout, 연결 상태 추적
  - ✅ Application-level ping - GraphQL-WS 프로토콜의 ping/pong 메시지 지원
  - ✅ ToFrontend.md 업데이트 - Apollo Client, vanilla JS, React Hook 재연결 예제
  - ✅ Exponential backoff - 1s → 2s → 4s → 8s → max 30s 재연결 간격
  - ✅ Subscription 복원 - 재연결 후 자동 재구독 로직 가이드
  - ✅ Best practices 문서화 - Keep-alive, 상태 표시, graceful degradation
- ✅ **Frontend 통합 문서 강화** - newPendingTransactions 상세 가이드
  - ✅ React 통합 예제 - PendingTransactionMonitor 컴포넌트 (170줄)
  - ✅ 주소별 필터링 - AddressPendingTxMonitor 예제 (30줄)
  - ✅ 실시간 TPS 계산 - RealtimeTPS 컴포넌트 (30줄)
  - ✅ UI 권장사항 - 실시간 피드, 상태 인디케이터, 트랜잭션 타입 뱃지, 필터링 옵션
  - ✅ 사용 사례 - 네트워크 활동 모니터링, 지갑 트랜잭션 감지, TPS 차트, 가스 가격 분석
  - ✅ 주의사항 - Stable-One 빠른 블록 생성 시간 (1-2초) 고려

### 2025-11-24
- ✅ **Fetcher 최적화 시스템 구현** - 지능형 적응형 최적화 및 대용량 블록 처리
  - ✅ RPC Metrics 수집 - 응답 시간, 에러율, 처리량 실시간 추적 (슬라이딩 윈도우)
  - ✅ Adaptive Optimizer - RPC 성능 기반 워커 수/배치 크기 자동 조정 (30초 간격)
  - ✅ Rate Limit 감지 - 에러 패턴 기반 자동 워커 축소 (50% 감소)
  - ✅ Large Block Processor - 50M gas 이상 블록 병렬 처리 (최대 10 워커)
  - ✅ Receipt 병렬 처리 - 100개 단위 배치 처리로 105M gas 블록 성능 개선
  - ✅ 메모리 최적화 - 메모리 사용량 추정 및 스트리밍 처리 (100MB 임계값)
  - ✅ 성능 모니터링 - 실시간 메트릭 리포팅 (GetMetrics, LogPerformanceMetrics)
- ✅ **Analytics API 구현** - 블록체인 데이터 통계 및 분석 기능
  - ✅ Gas 사용량 통계 - 블록 범위별/주소별 gas 집계, 평균 gas price 계산
  - ✅ 네트워크 메트릭 - TPS, 블록 생성 시간, 평균 블록 크기 계산
  - ✅ Top Addresses - gas 사용량/트랜잭션 수 기준 상위 주소 조회
  - ✅ GraphQL 통합 - gasStats, addressGasStats, topAddressesByGasUsed, topAddressesByTxCount, networkMetrics 쿼리 추가
  - ✅ Storage Layer 구현 - PebbleDB 기반 5개 analytics 메서드 (HistoricalReader 인터페이스)
- ✅ ABI 디코딩 시스템 구현 - 이벤트 로그 및 함수 호출 데이터 자동 디코딩
- ✅ ABI 스토리지 구현 - PebbleDB 기반 ABI 저장/조회/삭제 (ABIReader, ABIWriter 인터페이스)
- ✅ ABI JSON-RPC API 추가 - setContractABI, getContractABI, removeContractABI, listContractABIs
- ✅ eth_getLogs decode 파라미터 지원 - 선택적 ABI 디코딩 기능 추가
- ✅ 필터 메서드 decode 지원 - eth_newFilter, eth_getFilterChanges, eth_getFilterLogs에 decode 파라미터 추가
- ✅ GraphQL ABI 디코딩 통합 - DecodedLog 타입 추가, logs 쿼리에 decode 파라미터 지원
- ✅ 자동 ABI 로딩 - 서버 시작 시 저장된 모든 ABI를 디코더에 자동 로드
- ✅ 타입 직렬화 - 복잡한 Ethereum 타입을 JSON 호환 형식으로 변환 (*big.Int, Address, Hash, 배열, 중첩 구조)

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
**Last Updated**: 2025-11-25
