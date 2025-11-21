# 추천 우선순위 작업 리스트

> 미구현 기능의 권장 개발 순서 및 상세 계획

**작성일**: 2025-11-21
**기준**: 비즈니스 가치, 사용자 영향도, 기술 의존성, 구현 난이도

---

## 우선순위 1: 이벤트 필터 시스템 ⭐⭐⭐⭐⭐

**예상 소요**: 2-3주
**비즈니스 가치**: 매우 높음
**사용자 영향도**: 매우 높음
**난이도**: 중상

### 구현 이유
- **필수 기능**: 모든 블록체인 탐색기/인덱서의 핵심 기능
- **높은 수요**: DApp 개발자들이 가장 많이 사용하는 API
- **생태계 호환성**: Ethereum 표준 API (eth_getLogs) 구현으로 기존 툴과 호환
- **기존 인프라 활용**: Address Indexing이 완료되어 기반 구조 존재

### 구현 범위
```
Phase 1 (1주): Topic 기반 필터링
- [ ] eth_getLogs 구현
  - 블록 범위 쿼리 (fromBlock, toBlock)
  - 주소 필터 (address, addresses)
  - Topic 필터 (topics[0-3])
- [ ] PebbleDB 인덱스 구조 설계
  - /index/log/address/{address}/{blockNumber}/{txIndex}/{logIndex}
  - /index/log/topic/{topic0}/{blockNumber}/{txIndex}/{logIndex}
- [ ] GraphQL API 추가
  - logs(filter: LogFilter, pagination: PaginationInput)

Phase 2 (1주): Filter 관리
- [ ] eth_newFilter / eth_getFilterChanges
- [ ] eth_newBlockFilter
- [ ] eth_newPendingTransactionFilter
- [ ] eth_uninstallFilter
- [ ] Filter 만료 정책 (5분 TTL)

Phase 3 (1주): ABI 디코딩
- [ ] ABI JSON 파싱
- [ ] 이벤트 시그니처 매칭
- [ ] 파라미터 자동 디코딩
- [ ] 함수 호출 데이터 디코딩
```

### 예상 효과
- DApp 개발자가 직접 로그 필터링 가능
- Etherscan, Blockscout 등과 API 호환
- 기존 Ethereum 라이브러리 (ethers.js, web3.js) 사용 가능

---

## 우선순위 2: WBFT JSON-RPC API ⭐⭐⭐⭐

**예상 소요**: 1주
**비즈니스 가치**: 높음
**사용자 영향도**: 중상
**난이도**: 하

### 구현 이유
- **API 일관성**: GraphQL과 JSON-RPC 간 기능 격차 해소
- **빠른 구현**: 로직은 이미 존재, API 래핑만 필요
- **검증자 모니터링**: 노드 운영자를 위한 필수 API
- **낮은 리스크**: 기존 검증된 코드 재사용

### 구현 범위
```
1일차: 기본 WBFT 메서드
- [ ] getWBFTBlockExtra(blockNumber)
- [ ] getWBFTBlockExtraByHash(blockHash)
- [ ] getEpochInfo(epochNumber)
- [ ] getLatestEpochInfo()

2일차: 검증자 통계 메서드
- [ ] getValidatorSigningStats(validatorAddress, fromBlock, toBlock)
- [ ] getAllValidatorsSigningStats(fromBlock, toBlock, limit, offset)

3일차: 검증자 활동 메서드
- [ ] getValidatorSigningActivity(validatorAddress, fromBlock, toBlock, limit, offset)
- [ ] getBlockSigners(blockNumber)

4-5일차: 테스트 및 문서화
- [ ] 통합 테스트 작성 (8개 메서드)
- [ ] ToFrontend.md 업데이트
- [ ] API 예시 코드 작성
```

### 예상 효과
- GraphQL 사용 불가능한 환경에서도 WBFT 데이터 접근
- 노드 운영자 CLI 툴 개발 가능
- API 완성도 향상

---

## 우선순위 3: Analytics API ⭐⭐⭐⭐

**예상 소요**: 3-4주
**비즈니스 가치**: 높음
**사용자 영향도**: 높음
**난이도**: 중

### 구현 이유
- **사용자 가치**: 블록체인 분석, 대시보드 제작에 필수
- **데이터 활용**: 기존 인덱싱된 데이터로 구현 가능
- **차별화**: 다른 인덱서 대비 경쟁력 확보
- **점진적 구현**: 기능별로 단계적 개발 가능

### 구현 범위
```
Week 1: Gas 통계 API
- [ ] 블록별 gas 사용량 집계
  - GET /analytics/gas/blocks?from={block}&to={block}
  - 평균 gas used, gas limit, 사용률(%)
- [ ] 주소별 gas 소비 통계
  - GET /analytics/gas/addresses?limit=100
  - Top gas 소비 주소, 총 gas 소비량
- [ ] 시간대별 gas 트렌드
  - GET /analytics/gas/trend?interval=1h&period=24h
  - 시간별/일별 gas 사용량 추이
- [ ] Gas price 추이 분석
  - 평균/중앙값/최소/최대 gas price

Week 2: 네트워크 활동 메트릭
- [ ] TPS 계산
  - 실시간 TPS (최근 N 블록 기준)
  - 평균 TPS (기간별)
- [ ] 블록 생성 시간 통계
  - 평균 블록 시간
  - 블록 시간 분포
- [ ] 트랜잭션 유형별 분포
  - Legacy, EIP-1559, Fee Delegation 비율
  - 차트 데이터 제공

Week 3: Top Addresses
- [ ] 활동 기준 Top 주소
  - 트랜잭션 수 기준
  - 시간 범위 필터
- [ ] Gas 소비 기준 Top 주소
  - 총 gas 소비량 기준
  - 평균 gas 소비량 기준
- [ ] 컨트랙트 호출 빈도
  - 가장 많이 호출된 컨트랙트
  - 함수별 호출 통계

Week 4: 데이터 집계 최적화
- [ ] PebbleDB 별도 집계 인덱스
- [ ] 캐싱 레이어 (Redis)
- [ ] Background job으로 사전 계산
- [ ] GraphQL + JSON-RPC API
```

### 예상 효과
- 블록 탐색기 대시보드 구축 가능
- 네트워크 건강도 모니터링
- 데이터 기반 의사결정 지원

---

## 우선순위 4: Fetcher 최적화 ⭐⭐⭐

**예상 소요**: 2-3주
**비즈니스 가치**: 중상
**사용자 영향도**: 중 (간접적)
**난이도**: 중상

### 구현 이유
- **성능 향상**: 인덱싱 속도 개선
- **안정성 강화**: RPC 에러 복원력 향상
- **비용 절감**: RPC 호출 최적화로 인프라 비용 감소
- **확장성**: 대용량 블록 처리 능력 향상

### 구현 범위
```
Week 1: 동적 워커 풀
- [ ] RPC rate limit 감지
  - 429 에러 핸들링
  - 자동 백오프 (exponential backoff)
- [ ] 워커 수 동적 조정
  - 에러율 기반 워커 감소
  - 성공률 기반 워커 증가
  - 최소/최대 워커 수 설정 (10-200)
- [ ] 메트릭 수집
  - 워커 활용률
  - RPC 응답 시간
  - 에러율

Week 2: Adaptive Batch Sizing
- [ ] RPC 응답 시간 측정
- [ ] 배치 크기 자동 조정
  - 느린 응답: 배치 크기 감소
  - 빠른 응답: 배치 크기 증가
- [ ] RPC 대역폭 최적화
  - 병렬 요청 수 제한
  - 요청 큐잉 전략

Week 3: 대용량 블록 최적화
- [ ] 105M gas 블록 처리 개선
- [ ] Receipt 병렬 처리 최적화
  - GetBlockReceipts 병렬 호출
  - 메모리 스트리밍 처리
- [ ] 메모리 사용량 최적화
  - 블록 데이터 스트리밍
  - 조기 GC 트리거
```

### 예상 효과
- 인덱싱 속도 30-50% 향상
- RPC 에러로 인한 중단 감소
- 대용량 블록 처리 안정성 확보

---

## 우선순위 5: Rate Limiting & Caching ⭐⭐⭐

**예상 소요**: 1-2주
**비즈니스 가치**: 중
**사용자 영향도**: 중
**난이도**: 중하

### 구현 이유
- **서비스 안정성**: API 남용 방지
- **성능 향상**: 반복 쿼리 캐싱으로 응답 속도 개선
- **비용 절감**: DB 부하 감소
- **프로덕션 필수**: 공개 API 서비스 시 필수

### 구현 범위
```
Week 1: Rate Limiting
- [ ] 엔드포인트별 rate limit
  - IP 기반 제한 (100 req/min)
  - API Key 기반 제한 (Tier별)
- [ ] Rate limit 미들웨어
  - GraphQL resolver 레벨
  - JSON-RPC 핸들러 레벨
- [ ] Redis 연동
  - Distributed rate limiting
  - Token bucket 알고리즘

Week 2: Caching
- [ ] Redis 캐싱 통합
  - 블록/트랜잭션 캐시 (TTL: 1분)
  - 집계 데이터 캐시 (TTL: 5분)
- [ ] Query result 캐싱
  - GraphQL 쿼리 결과 캐시
  - 파라미터별 캐시 키 생성
- [ ] 캐시 무효화 전략
  - 새 블록 인덱싱 시 관련 캐시 삭제
  - TTL 기반 자동 만료
```

### 예상 효과
- API 응답 속도 50-80% 향상 (캐시 히트 시)
- DB 부하 30-50% 감소
- DoS 공격 방어

---

## 우선순위 6: WBFT 모니터링 메트릭 ⭐⭐⭐

**예상 소요**: 1주
**비즈니스 가치**: 중
**사용자 영향도**: 중 (검증자 대상)
**난이도**: 하

### 구현 이유
- **운영 가시성**: 검증자 성능 모니터링
- **문제 조기 발견**: 서명 실패/지연 감지
- **거버넌스 투명성**: Priority fee 변경 추적
- **빠른 구현**: 데이터는 이미 존재

### 구현 범위
```
1-2일차: Prometheus 메트릭 노출
- [ ] /metrics 엔드포인트 추가
- [ ] 기본 메트릭
  - wbft_validator_sign_rate{validator="0x..."}
  - wbft_validator_miss_count{validator="0x..."}
  - wbft_block_round{block_number="..."}
  - wbft_gas_tip{block_number="..."}

3-4일차: 고급 메트릭
- [ ] 검증자 서명 실패/지연 감지
  - wbft_validator_consecutive_misses
  - wbft_validator_last_sign_timestamp
- [ ] Priority fee 변경 감지
  - wbft_gas_tip_change_count
  - wbft_gas_tip_current

5일차: Grafana 대시보드
- [ ] 대시보드 템플릿 작성
  - 검증자 성능 패널
  - 서명률 차트
  - Gas tip 추이
```

### 예상 효과
- 검증자 실시간 모니터링 가능
- 문제 발생 시 즉시 알림
- 운영 효율성 향상

---

## 우선순위 7: Notification System ⭐⭐

**예상 소요**: 2-3주
**비즈니스 가치**: 중하
**사용자 영향도**: 중
**난이도**: 중

### 구현 이유
- **사용자 편의성**: 중요 이벤트 알림
- **자동화**: 수동 모니터링 불필요
- **유연성**: 다양한 채널 지원

### 구현 범위
```
Week 1: Webhook
- [ ] Webhook 설정 API (CRUD)
- [ ] 이벤트 전달 시스템
  - 새 블록, 특정 주소 트랜잭션, 컨트랙트 이벤트
- [ ] 재시도 로직 (exponential backoff, 최대 3회)
- [ ] Webhook 상태 모니터링

Week 2: Email
- [ ] SMTP 설정 및 연동
- [ ] 이메일 템플릿 (HTML)
- [ ] 구독 관리 API
- [ ] 배치 전송 (5분 간격)

Week 3: Slack (선택)
- [ ] Slack webhook 연동
- [ ] Rich message 포맷팅
- [ ] 채널 라우팅
```

### 예상 효과
- 실시간 이벤트 알림
- DApp 개발자 편의성 향상

---

## 우선순위 8: Horizontal Scaling ⭐

**예상 소요**: 4-6주
**비즈니스 가치**: 낮음 (현재)
**사용자 영향도**: 낮음
**난이도**: 상

### 구현 이유
- **미래 대비**: 트래픽 증가 대비
- **고가용성**: 단일 장애점 제거
- **현재 불필요**: 단일 노드로 충분

### 구현 시기
- **권장**: 트래픽이 단일 노드 용량의 70% 초과 시
- **현재**: 보류 (우선순위 1-7 완료 후)

---

## 권장 구현 순서 요약

```
1. 이벤트 필터 시스템 (2-3주) ← 즉시 시작 권장
2. WBFT JSON-RPC API (1주)
3. Analytics API (3-4주)
4. Fetcher 최적화 (2-3주)
5. Rate Limiting & Caching (1-2주)
6. WBFT 모니터링 메트릭 (1주)
7. Notification System (2-3주)
8. Horizontal Scaling (보류)
```

**총 예상 소요**: 12-17주 (3-4개월)

---

## 분기별 권장 계획

### Q4 2025 (12월)
- ✅ 이벤트 필터 시스템
- ✅ WBFT JSON-RPC API

### Q1 2026 (1-3월)
- ✅ Analytics API
- ✅ Fetcher 최적화
- ✅ Rate Limiting & Caching

### Q2 2026 (4-6월)
- ✅ WBFT 모니터링 메트릭
- ✅ Notification System

### Q3 2026 이후
- Horizontal Scaling (필요시)

---

## 의사결정 기준

| 기준 | 가중치 | 설명 |
|------|--------|------|
| 사용자 영향도 | 40% | 최종 사용자/개발자에게 미치는 영향 |
| 비즈니스 가치 | 30% | 수익/경쟁력/차별화 기여도 |
| 기술 의존성 | 15% | 다른 기능과의 의존 관계 |
| 구현 난이도 | 10% | 개발 복잡도 및 리스크 |
| 운영 필요성 | 5% | 프로덕션 안정성 기여도 |

---

**작성자**: Claude
**검토 필요**: 비즈니스 팀, 제품 팀과 우선순위 조율
**업데이트**: 분기별 재평가 권장
