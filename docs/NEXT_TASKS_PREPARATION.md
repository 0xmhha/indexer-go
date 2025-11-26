# Next Tasks Preparation - Indexer-Go Development Roadmap

**Created**: 2025-01-26
**Status**: Ready for Implementation
**Previous Completion**: ✅ Phase A - System Contracts (Complete)

---

## 📋 Overview

Phase A (System Contracts)가 완료되었습니다. 다음 3개 페이즈가 우선순위 순으로 대기 중입니다:

1. **Phase B**: Consensus Enhancement Phase 6 (Event System Integration) - 2~3일
2. **Phase C**: Rate Limiting & Caching - 1~2주
3. **Phase D**: WBFT Monitoring & Metrics - 1주

---

## 🎯 Phase B: Consensus Enhancement Phase 6 (권장 다음 작업)

### 우선순위: **HIGH (P2)**
### 예상 소요시간: **2~3일**
### 복잡도: **Medium**

### 목표
WBFT 합의 데이터에 대한 **실시간 이벤트 시스템** 구축하여 프론트엔드가 WebSocket을 통해 합의 상태를 실시간으로 모니터링할 수 있도록 지원.

### 현재 상태
- ✅ Phases 1-5 완료 (Types, RPC, Parser, Storage, GraphQL API)
- ❌ Phase 6 미완성: Event System Integration
- ⚠️ 합의 데이터는 저장되지만 실시간 이벤트가 발행되지 않음
- ⚠️ WebSocket 구독 스키마는 정의되어 있으나 resolver가 연결되지 않음

### 구현 필요 항목

#### 1. Consensus Event Types 정의
**파일**: `events/consensus_events.go` (신규 생성)

```go
// 4가지 새로운 이벤트 타입 정의
type ConsensusDataEvent struct {
    BaseEvent
    Data *consensus.ConsensusData
}

type RoundChangeEvent struct {
    BaseEvent
    BlockNumber   uint64
    Round         uint32
    PreviousRound uint32
    Proposer      common.Address
}

type EpochChangeEvent struct {
    BaseEvent
    EpochNumber        uint64
    BlockNumber        uint64
    PreviousValidators []common.Address
    NewValidators      []common.Address
}

type ValidatorParticipationEvent struct {
    BaseEvent
    BlockNumber  uint64
    ValidatorAddr common.Address
    SignedPrepare bool
    SignedCommit  bool
}
```

#### 2. Fetcher Integration
**파일**: `fetch/consensus.go` (수정)

현재 `ProcessConsensusData` 함수에 이벤트 발행 로직 추가:
```go
func (f *Fetcher) ProcessConsensusData(...) error {
    // 기존 코드...

    // 🆕 이벤트 발행 추가
    if f.eventBus != nil {
        // ConsensusDataEvent 발행
        f.eventBus.Publish(events.NewConsensusDataEvent(consensusData))

        // RoundChange 감지시 RoundChangeEvent 발행
        if consensusData.Round > 0 {
            f.eventBus.Publish(events.NewRoundChangeEvent(...))
        }

        // Epoch 경계 감지시 EpochChangeEvent 발행
        if consensusData.IsEpochBoundary {
            f.eventBus.Publish(events.NewEpochChangeEvent(...))
        }
    }

    return nil
}
```

#### 3. GraphQL Subscription Resolvers
**파일**: `api/graphql/resolvers.go` (수정)

현재 비어있는 subscription resolver 구현:
```go
// Subscription resolver
func (r *subscriptionResolver) NewConsensusData(ctx context.Context) (<-chan *ConsensusData, error) {
    ch := make(chan *ConsensusData)

    // EventBus 구독
    subscription := r.eventBus.Subscribe(events.EventTypeConsensusData)

    go func() {
        defer close(ch)
        for event := range subscription.Events() {
            if consensusEvent, ok := event.(*events.ConsensusDataEvent); ok {
                ch <- convertConsensusData(consensusEvent.Data)
            }
        }
    }()

    return ch, nil
}

// 나머지 3개 subscription도 동일 패턴으로 구현:
// - RoundChangeOccurred
// - EpochChanged
// - ValidatorParticipationUpdate
```

#### 4. WebSocket Connection Setup
**파일**: `api/graphql/server.go` (수정)

WebSocket 핸들러가 이미 설정되어 있는지 확인하고, 없다면 추가:
```go
// WebSocket endpoint for subscriptions
srv.AddTransport(&transport.Websocket{
    KeepAlivePingInterval: 10 * time.Second,
    Upgrader: websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool {
            return true // 프로덕션에서는 제한 필요
        },
    },
})
```

### 테스트 계획

#### Unit Tests
```bash
# 이벤트 타입 테스트
go test ./events -run TestConsensusEvents

# EventBus 통합 테스트
go test ./fetch -run TestConsensusEventPublishing

# Subscription resolver 테스트
go test ./api/graphql -run TestSubscriptionResolvers
```

#### Integration Test
```bash
# WebSocket 구독 E2E 테스트
go test ./test/integration -run TestConsensusSubscription
```

### 예상 결과
✅ 프론트엔드가 다음 실시간 구독 가능:
- 새로운 블록의 합의 데이터
- 라운드 변경 알림 (합의 실패/재시도)
- 에포크 변경 및 검증자 세트 업데이트
- 검증자별 서명 참여 현황

### 파일 변경 요약
| 파일 | 작업 | 라인 수 |
|------|------|---------|
| `events/consensus_events.go` | 신규 생성 | ~150 |
| `fetch/consensus.go` | 수정 (이벤트 발행 추가) | +30 |
| `api/graphql/resolvers.go` | 수정 (4개 resolver 구현) | +120 |
| `api/graphql/server.go` | 검토/수정 (WebSocket 설정) | +20 |
| **Total** | | **~320 lines** |

---

## 🚀 Phase C: Rate Limiting & Caching (중기 작업)

### 우선순위: **MEDIUM (P5)**
### 예상 소요시간: **1~2주**
### 복잡도: **High**

### 목표
프로덕션 배포를 위한 **성능 최적화** 및 **DoS 방어** 인프라 구축.

### 현재 상태
- ⚠️ Rate Limiting: 인메모리만 지원 (분산 환경 미지원)
- ❌ Redis Caching: 전혀 구현되지 않음
- ❌ Query Result Cache: 모든 쿼리가 PebbleDB 직접 접근
- ❌ CDN Integration: 정적 데이터 캐싱 없음

### 구현 필요 항목

#### 1. Redis Integration
**신규 패키지**: `cache/`

**파일 구조**:
```
cache/
├── cache.go           # Cache interface 정의
├── redis.go           # Redis 구현
├── memory.go          # Fallback 메모리 캐시
├── config.go          # Redis 설정
└── middleware.go      # HTTP middleware
```

**핵심 인터페이스**:
```go
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Clear(ctx context.Context) error
}

type RedisCache struct {
    client *redis.Client
    prefix string
    ttl    time.Duration
}
```

#### 2. Query Result Caching
**수정 파일**:
- `api/graphql/resolvers.go` - 각 resolver에 캐시 레이어 추가
- `api/jsonrpc/methods.go` - JSON-RPC 메서드에 캐시 추가

**캐시 전략**:
```go
// Example: Block 조회 캐싱
func (r *queryResolver) Block(ctx context.Context, height uint64) (*Block, error) {
    // 1. 캐시 확인
    cacheKey := fmt.Sprintf("block:%d", height)
    if cached, err := r.cache.Get(ctx, cacheKey); err == nil {
        return unmarshalBlock(cached), nil
    }

    // 2. DB 조회
    block, err := r.storage.GetBlock(ctx, height)
    if err != nil {
        return nil, err
    }

    // 3. 캐시 저장 (TTL: 10분, 블록은 immutable)
    if data, err := json.Marshal(block); err == nil {
        r.cache.Set(ctx, cacheKey, data, 10*time.Minute)
    }

    return block, nil
}
```

**캐시 대상 우선순위**:
1. **Block data** (immutable) - TTL: 무제한
2. **Transaction receipts** (immutable) - TTL: 무제한
3. **System contract data** (frequent queries) - TTL: 1분
4. **Validator stats** (periodic updates) - TTL: 30초
5. **Latest block** (high frequency) - TTL: 3초

#### 3. Distributed Rate Limiting
**신규 패키지**: `ratelimit/`

**Redis 기반 Token Bucket**:
```go
type RedisRateLimiter struct {
    cache  cache.Cache
    limits map[string]*RateLimit // IP별/API키별 제한
}

type RateLimit struct {
    Requests int           // 요청 수 제한
    Window   time.Duration // 시간 윈도우
}

// IP별 제한: 100 req/min
// API 키별 제한: 1000 req/min (authenticated)
```

#### 4. Cache Invalidation
**수정 파일**: `fetch/fetcher.go`

블록 저장시 관련 캐시 무효화:
```go
func (f *Fetcher) FetchBlock(ctx context.Context, height uint64) error {
    // 블록 처리...

    // 캐시 무효화
    if f.cache != nil {
        f.cache.Delete(ctx, "latest_block")
        f.cache.Delete(ctx, fmt.Sprintf("block:%d", height))
    }

    return nil
}
```

### 성능 목표
| 지표 | 현재 | 목표 | 개선율 |
|------|------|------|--------|
| GraphQL 응답시간 | 50-200ms | 5-50ms | **75-90%** |
| DB 쿼리 수 | 100% | 30-50% | **50-70% 감소** |
| 동시 요청 처리 | 100 req/s | 500+ req/s | **400%** |
| Cache Hit Rate | 0% | 60-80% | N/A |

### 의존성 추가
```bash
# Redis client
go get github.com/redis/go-redis/v9

# Distributed rate limiting
go get github.com/go-redis/redis_rate/v10
```

### 파일 변경 요약
| 파일/패키지 | 작업 | 라인 수 |
|-------------|------|---------|
| `cache/` 패키지 | 신규 생성 | ~500 |
| `ratelimit/` 패키지 | 개선 | ~300 |
| `api/graphql/resolvers.go` | 캐시 통합 | +200 |
| `api/jsonrpc/methods.go` | 캐시 통합 | +150 |
| `fetch/fetcher.go` | 무효화 로직 | +50 |
| `config/config.go` | Redis 설정 | +30 |
| **Total** | | **~1,230 lines** |

---

## 📊 Phase D: WBFT Monitoring & Metrics (장기 작업)

### 우선순위: **MEDIUM (P6)**
### 예상 소요시간: **1주**
### 복잡도: **Medium**

### 목표
운영 모니터링을 위한 **Prometheus 메트릭** 노출 및 **Grafana 대시보드** 템플릿 제공.

### 현재 상태
- ❌ Prometheus endpoint 없음
- ❌ 합의 관련 메트릭 수집 없음
- ❌ 검증자 성능 메트릭 없음
- ❌ 시스템 헬스 체크 API 없음

### 구현 필요 항목

#### 1. Prometheus Metrics
**신규 패키지**: `metrics/`

**수집할 메트릭**:
```go
// Consensus Metrics
var (
    // 검증자 서명율
    validatorSignRate = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "wbft_validator_sign_rate",
            Help: "Validator signing participation rate",
        },
        []string{"validator", "type"}, // type: prepare|commit
    )

    // 라운드 변경 횟수
    roundChanges = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "wbft_round_changes_total",
            Help: "Total number of round changes (consensus failures)",
        },
        []string{"block"},
    )

    // 에포크 전환
    epochChanges = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "wbft_epoch_changes_total",
            Help: "Total number of epoch changes",
        },
    )

    // 검증자 세트 크기
    validatorSetSize = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "wbft_validator_set_size",
            Help: "Current number of active validators",
        },
    )
)

// System Metrics
var (
    // API 요청 지연시간
    apiLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "api_request_duration_seconds",
            Help: "API request latency in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method", "endpoint"},
    )

    // 캐시 히트율
    cacheHitRate = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "cache_requests_total",
            Help: "Total cache requests by result",
        },
        []string{"type", "result"}, // result: hit|miss
    )

    // 블록 처리 시간
    blockProcessingTime = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name: "block_processing_duration_seconds",
            Help: "Time to process and index a block",
            Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
        },
    )
)
```

#### 2. Metrics Collection Integration
**수정 파일**:
- `fetch/consensus.go` - 합의 메트릭 수집
- `api/graphql/server.go` - API 메트릭 미들웨어
- `cache/redis.go` - 캐시 메트릭

```go
// Example: fetch/consensus.go
func (f *Fetcher) ProcessConsensusData(...) error {
    // 검증자 참여율 메트릭 업데이트
    for _, validator := range consensusData.Validators {
        signRate := calculateSignRate(validator)
        metrics.ValidatorSignRate.WithLabelValues(
            validator.Hex(),
            "prepare",
        ).Set(signRate)
    }

    // 라운드 변경 카운트
    if consensusData.Round > 0 {
        metrics.RoundChanges.WithLabelValues(
            fmt.Sprintf("%d", consensusData.BlockNumber),
        ).Inc()
    }

    return nil
}
```

#### 3. Prometheus HTTP Endpoint
**수정 파일**: `cmd/indexer/main.go`

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

// Metrics endpoint 추가
http.Handle("/metrics", promhttp.Handler())
logger.Info("Metrics server listening", zap.String("endpoint", ":9090/metrics"))
```

#### 4. Grafana Dashboard Template
**신규 파일**: `monitoring/grafana/`

**제공할 대시보드**:
1. **WBFT Consensus Dashboard**
   - 검증자 서명율 (시계열)
   - 라운드 변경 빈도
   - 에포크 전환 타임라인
   - 검증자별 성능 비교

2. **System Performance Dashboard**
   - API 응답 시간 분포
   - 캐시 히트율
   - 블록 처리 속도
   - 에러율

3. **Alert Rules**
   ```yaml
   # alerts.yml
   groups:
     - name: wbft_consensus
       rules:
         - alert: HighRoundChangeRate
           expr: rate(wbft_round_changes_total[5m]) > 0.1
           annotations:
             summary: "High consensus failure rate detected"

         - alert: LowValidatorParticipation
           expr: wbft_validator_sign_rate < 0.8
           annotations:
             summary: "Validator {{ $labels.validator }} has low participation"
   ```

### 모니터링 스택 설정
```yaml
# docker-compose.monitoring.yml
version: '3.8'
services:
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./monitoring/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - ./monitoring/grafana/dashboards:/etc/grafana/provisioning/dashboards
```

### 파일 변경 요약
| 파일/패키지 | 작업 | 라인 수 |
|-------------|------|---------|
| `metrics/` 패키지 | 신규 생성 | ~400 |
| `fetch/consensus.go` | 메트릭 수집 | +50 |
| `api/graphql/server.go` | 미들웨어 | +30 |
| `cache/redis.go` | 메트릭 수집 | +20 |
| `cmd/indexer/main.go` | Endpoint 추가 | +10 |
| `monitoring/` 디렉토리 | Grafana 대시보드 | ~300 (JSON) |
| **Total** | | **~810 lines** |

---

## 🎯 작업 추천 순서

### Option 1: 순차 실행 (권장)
```
Phase B (2-3일) → Phase C (1-2주) → Phase D (1주)
총 소요시간: 약 2.5-3.5주
```

**장점**:
- 각 페이즈 완전히 테스트 후 다음 단계
- 의존성 최소화
- 안정적인 진행

**단점**:
- 전체 완료까지 시간 소요

---

### Option 2: 병렬 실행 (빠른 완료)
```
Week 1: Phase B + Phase C (Redis 설정)
Week 2: Phase C (캐싱 구현) + Phase D (메트릭 정의)
Week 3: Phase D (대시보드) + 통합 테스트
```

**장점**:
- 2-3주 안에 모두 완료 가능
- 빠른 프로덕션 준비

**단점**:
- 복잡도 증가
- 의존성 관리 필요
- 테스트 병목 가능성

---

### Option 3: 단계별 선택 (유연한 접근)
```
1단계: Phase B (필수, 2-3일)
평가 후 선택:
  → 성능 이슈 있음 → Phase C
  → 모니터링 필요 → Phase D
  → 둘 다 필요 → 병렬 진행
```

**장점**:
- 현재 우선순위에 맞춤
- 유연한 리소스 배분

**단점**:
- 의사결정 필요

---

## 📝 작업 시작 체크리스트

### Phase B 시작 전
- [ ] 현재 코드 백업/커밋
- [ ] `docs/CONSENSUS_ENHANCEMENT_PLAN.md` 재검토
- [ ] EventBus 구현 확인 (`events/eventbus.go`)
- [ ] WebSocket 테스트 클라이언트 준비 (Postman/Insomnia)
- [ ] 브랜치 생성: `feature/consensus-phase-6`

### Phase C 시작 전
- [ ] Redis 서버 설치 및 테스트
- [ ] 캐싱 전략 문서화
- [ ] 성능 벤치마크 baseline 측정
- [ ] 브랜치 생성: `feature/redis-caching`

### Phase D 시작 전
- [ ] Prometheus + Grafana Docker 환경 준비
- [ ] 메트릭 요구사항 확정
- [ ] 알림 규칙 설계
- [ ] 브랜치 생성: `feature/prometheus-metrics`

---

## 🤔 의사결정 질문

다음 작업을 결정하기 위한 질문들:

1. **즉시 프로덕션 배포가 필요한가?**
   - Yes → Phase C (캐싱) 우선
   - No → Phase B (이벤트) 먼저

2. **프론트엔드에서 실시간 모니터링이 급한가?**
   - Yes → Phase B 최우선
   - No → Phase C 또는 D 선택

3. **현재 API 성능에 문제가 있는가?**
   - Yes → Phase C 긴급
   - No → Phase B 또는 D 선택

4. **운영 팀에서 메트릭을 요청했는가?**
   - Yes → Phase D 포함 필요
   - No → Phase B + C 먼저

5. **개발 리소스는?**
   - 1명 → 순차 실행
   - 2명 이상 → 병렬 가능

---

## 📞 다음 단계

**추천**:
```bash
# Phase B를 먼저 시작하는 것을 권장합니다
# 이유:
# 1. 소요시간 짧음 (2-3일)
# 2. Consensus 기능 완성도 향상
# 3. Phase C/D와 독립적으로 작업 가능
# 4. 프론트엔드 팀에 즉시 가치 제공
```

**시작 명령**:
```bash
git checkout -b feature/consensus-phase-6
```

어떤 Phase를 먼저 시작할지 결정해주세요! 🚀
