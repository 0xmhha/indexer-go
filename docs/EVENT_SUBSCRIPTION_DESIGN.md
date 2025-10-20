# 실시간 이벤트 구독 시스템 설계 문서

## 📋 목차
1. [요구사항 분석](#요구사항-분석)
2. [현재 시스템 분석](#현재-시스템-분석)
3. [설계 철학](#설계-철학)
4. [상세 설계](#상세-설계)
5. [성능 최적화 전략](#성능-최적화-전략)
6. [구현 계획](#구현-계획)
7. [테스트 전략](#테스트-전략)
8. [확장성 고려사항](#확장성-고려사항)

---

## 요구사항 분석

### 핵심 요구사항
1. **실시간 이벤트 전달**
   - Full node RPC를 통해 생성되는 블록/트랜잭션 정보를 실시간 감지
   - DB에 저장과 동시에 구독자에게 이벤트 전달

2. **유연한 필터링**
   - 각 구독자는 서로 다른 필터 조건 설정 가능
   - 주소 기반 필터 (from, to, contract)
   - 이벤트 타입 기반 필터 (transfer, swap, mint 등)
   - 금액 범위 필터
   - 블록 범위 필터

3. **고성능 처리**
   - 다수의 구독자가 존재해도 성능 저하 최소화
   - 블록 생성마다 빠른 이벤트 처리 (< 100ms 목표)
   - Non-blocking 이벤트 전달

4. **확장성**
   - 수평적 확장 가능한 아키텍처
   - 구독자 수에 선형적으로 확장

5. **성능 벤치마크**
   - 최대 동시 구독자 수 측정
   - 이벤트 전달 지연시간 측정
   - 처리량(throughput) 측정

---

## 현재 시스템 분석

### 기존 구현 상태

#### ✅ 완성된 부분
1. **WebSocket Hub/Client 시스템** (`api/websocket/`)
   - Hub: 중앙 메시지 브로커 역할
   - Client: 개별 WebSocket 연결 관리
   - 기본 pub/sub 패턴 구현됨

2. **구독 관리**
   - `SubscriptionType`: newBlock, newTransaction
   - Client별 구독 상태 관리
   - Subscribe/Unsubscribe 메시지 처리

3. **메시지 전달 메커니즘**
   - Non-blocking send (buffered channels)
   - Ping/Pong 헬스체크
   - Graceful connection close

#### ❌ 미구현 부분
1. **이벤트 소스 연결**
   - Fetcher → WebSocket Hub 연결 없음
   - 실제 블록/트랜잭션 데이터 전달 없음

2. **필터링 시스템**
   - 구독 필터 설정 기능 없음
   - 모든 이벤트를 모든 구독자에게 전달 (비효율)

3. **성능 최적화**
   - 이벤트 배칭 없음
   - 필터 매칭 최적화 없음
   - 구독자별 우선순위 없음

4. **모니터링/메트릭**
   - 성능 메트릭 수집 없음
   - 구독자 통계 없음

---

## 설계 철학

### 핵심 원칙

1. **Single Parse, Multiple Dispatch**
   - 블록/트랜잭션을 한 번만 파싱
   - 모든 필터에 대해 한 번에 매칭
   - 결과를 각 구독자에게 분배

2. **Event-Driven Architecture**
   - Fetcher가 이벤트 소스
   - Event Bus가 중앙 디스패처
   - 구독자는 수동적 리스너

3. **Zero-Copy where possible**
   - 데이터 복사 최소화
   - 포인터 전달
   - 구독자별 뷰(view) 생성

4. **Back-pressure Handling**
   - 느린 구독자가 시스템 전체를 막지 않음
   - Buffered channels + timeout
   - Drop policy 명확화

5. **Horizontal Scalability**
   - 상태를 외부 저장소에 (필요시 Redis)
   - Stateless event processing
   - 샤딩 가능한 구조

---

## 상세 설계

### 아키텍처 다이어그램

```
┌─────────────────────────────────────────────────────────────┐
│                        Indexer Node                         │
│                                                               │
│  ┌──────────┐                                                │
│  │ Fetcher  │──────────┐                                     │
│  └──────────┘          │                                     │
│       │                │                                     │
│       │ New Block      │                                     │
│       ↓                ↓                                     │
│  ┌──────────┐    ┌─────────────┐                           │
│  │ Storage  │    │ Event Bus   │                           │
│  └──────────┘    └─────────────┘                           │
│                         │                                    │
│                         │ Publish Event                     │
│                         ↓                                    │
│              ┌────────────────────┐                         │
│              │  Filter Matcher    │                         │
│              └────────────────────┘                         │
│                         │                                    │
│              ┌──────────┴──────────┐                        │
│              ↓                     ↓                        │
│       ┌─────────────┐      ┌─────────────┐                │
│       │  Subscriber │ ...  │  Subscriber │                │
│       │  Pool #1    │      │  Pool #N    │                │
│       └─────────────┘      └─────────────┘                │
│              │                     │                        │
└──────────────┼─────────────────────┼────────────────────────┘
               │                     │
               ↓                     ↓
        ┌──────────┐          ┌──────────┐
        │WebSocket │   ...    │WebSocket │
        │Client #1 │          │Client #N │
        └──────────┘          └──────────┘
```

### 주요 컴포넌트

#### 1. Event Bus (중앙 이벤트 디스패처)

```go
type EventBus struct {
    // Event channels
    blockEvents chan *BlockEvent
    txEvents    chan *TransactionEvent

    // Subscriber registry
    subscribers map[string]*Subscriber
    mu          sync.RWMutex

    // Filter index for fast matching
    filterIndex *FilterIndex

    // Metrics
    metrics *EventBusMetrics

    // Worker pool
    workers []*EventWorker
}
```

**책임:**
- Fetcher로부터 이벤트 수신
- 필터 매칭
- 구독자별 이벤트 라우팅
- 성능 메트릭 수집

**최적화:**
- Channel buffering (1000+ events)
- Worker pool (CPU 코어 수만큼)
- Filter indexing (주소 → 구독자 맵)

#### 2. Filter System (필터링 엔진)

```go
type Filter struct {
    // Address filters
    FromAddresses []common.Address
    ToAddresses   []common.Address
    Contracts     []common.Address

    // Event type filters
    EventTypes []string // "transfer", "swap", "mint" 등

    // Value filters
    MinValue *big.Int
    MaxValue *big.Int

    // Block range filters
    FromBlock uint64
    ToBlock   uint64

    // Advanced filters
    Topics [][]common.Hash // EVM log topics
}

type FilterIndex struct {
    // Address → Subscribers mapping
    addressIndex map[common.Address]map[string]bool

    // Event Type → Subscribers mapping
    eventTypeIndex map[string]map[string]bool

    // Bloom filters for fast negative matching
    bloomFilter *bloom.BloomFilter
}
```

**필터 매칭 알고리즘:**
1. **Fast Path (Bloom Filter)**
   - 주소/이벤트가 없으면 즉시 스킵
   - False positive 가능, false negative 없음

2. **Index Lookup**
   - 주소 인덱스에서 관련 구독자 찾기
   - O(1) 평균 시간복잡도

3. **Full Match**
   - 나머지 필터 조건 검증
   - 금액, 블록 범위 등

#### 3. Subscriber (구독자 관리)

```go
type Subscriber struct {
    ID     string
    Filter *Filter

    // Event delivery channel
    events chan *Event

    // Back-pressure handling
    buffer    int
    dropCount atomic.Uint64

    // Statistics
    stats *SubscriberStats
}

type SubscriberStats struct {
    EventsSent    atomic.Uint64
    EventsDropped atomic.Uint64
    LastEventTime atomic.Value // time.Time
    AvgLatency    atomic.Value // time.Duration
}
```

**이벤트 전달 전략:**
1. **Non-blocking Send**
   ```go
   select {
   case sub.events <- event:
       sub.stats.EventsSent.Add(1)
   default:
       sub.stats.EventsDropped.Add(1)
       // Log warning
   }
   ```

2. **Buffer Sizing**
   - 기본: 256 events
   - 조절 가능 (구독자별 설정)

3. **Drop Policy**
   - Drop oldest (FIFO)
   - 또는 drop newest (LIFO)
   - 메트릭 추적

#### 4. Event Worker Pool

```go
type EventWorker struct {
    id       int
    eventBus *EventBus
    events   chan *RawEvent
    quit     chan struct{}
}

func (w *EventWorker) Run() {
    for {
        select {
        case event := <-w.events:
            w.processEvent(event)
        case <-w.quit:
            return
        }
    }
}

func (w *EventWorker) processEvent(event *RawEvent) {
    // 1. Parse event
    parsed := parseEvent(event)

    // 2. Find matching subscribers
    subscribers := w.eventBus.filterIndex.Match(parsed)

    // 3. Dispatch to subscribers
    for _, sub := range subscribers {
        w.dispatch(sub, parsed)
    }
}
```

**워커 풀 크기:**
- 기본: runtime.NumCPU()
- CPU bound 작업이므로 코어 수에 맞춤
- 조절 가능

---

## 성능 최적화 전략

### 1. 필터 매칭 최적화

#### A. 인덱스 기반 매칭
```go
// Before: O(N * M) - N subscribers, M filter conditions
for _, sub := range subscribers {
    if matchFilter(event, sub.Filter) {
        send(event, sub)
    }
}

// After: O(log N + K) - K matched subscribers
subscribers := filterIndex.Lookup(event.Address)
for _, sub := range subscribers {
    if matchRemainingFilters(event, sub.Filter) {
        send(event, sub)
    }
}
```

**성능 개선:**
- 1000 구독자: ~1000x faster
- 10000 구독자: ~10000x faster

#### B. Bloom Filter 사용
```go
// Quick negative match
if !bloomFilter.Test(event.Address) {
    return nil // No subscribers
}
```

**효과:**
- False positive rate: < 1%
- Memory: ~10KB per 10000 addresses
- Lookup: O(k) where k = hash functions

### 2. 이벤트 배칭

```go
type EventBatch struct {
    Events    []*Event
    Timestamp time.Time
}

// Batch events every 10ms or 100 events
const (
    batchInterval = 10 * time.Millisecond
    batchSize     = 100
)
```

**이점:**
- Network overhead 감소
- JSON encoding 오버헤드 감소 (bulk encoding)
- 처리량 증가: ~10x

### 3. Zero-Copy 전략

```go
// Bad: Copy data for each subscriber
for _, sub := range subscribers {
    data := copyEvent(event)  // Expensive!
    sub.events <- data
}

// Good: Share read-only data
eventPtr := &event
for _, sub := range subscribers {
    sub.events <- eventPtr  // Just pointer copy
}
```

**주의사항:**
- Immutable events
- Copy-on-write if modification needed

### 4. 메모리 풀링

```go
var eventPool = sync.Pool{
    New: func() interface{} {
        return &Event{}
    },
}

func getEvent() *Event {
    return eventPool.Get().(*Event)
}

func putEvent(e *Event) {
    e.Reset()
    eventPool.Put(e)
}
```

**효과:**
- GC pressure 감소
- Allocation 감소: ~50%

---

## 구현 계획

### Phase 1: 이벤트 소스 연결 (1-2일)

**목표:** Fetcher와 WebSocket Hub 연결

**작업:**
1. Event Bus 구현
   - `events/bus.go`
   - 기본 pub/sub 기능

2. Fetcher 수정
   - 블록 저장 후 이벤트 발행
   - `fetcher.ProcessBlock()` 수정

3. WebSocket 연동
   - Hub → Event Bus 연결
   - 기존 구독자에게 이벤트 전달

**테스트:**
- 단일 구독자 end-to-end 테스트
- 이벤트 전달 지연시간 측정

### Phase 2: 필터 시스템 구현 (2-3일)

**목표:** 유연한 필터링 기능

**작업:**
1. Filter 타입 정의
   - `events/filter.go`
   - 다양한 필터 조건 구조체

2. Filter Matcher 구현
   - `events/matcher.go`
   - 필터 매칭 로직

3. 구독 API 확장
   - WebSocket subscribe 메시지에 filter 추가
   - Filter validation

**테스트:**
- 다양한 필터 조건 테스트
- 필터 매칭 성능 테스트

### Phase 3: 성능 최적화 (2-3일)

**목표:** 대규모 구독자 처리

**작업:**
1. Filter Index 구현
   - Address indexing
   - Event type indexing

2. Bloom Filter 적용
   - 빠른 negative matching

3. Worker Pool 구현
   - Parallel event processing

4. Event Batching
   - 배치 전송 옵션

**테스트:**
- 1000+ 구독자 부하 테스트
- 필터 매칭 벤치마크

### Phase 4: 모니터링 & 메트릭 (1-2일)

**목표:** 관측 가능성 확보

**작업:**
1. 메트릭 수집
   - Prometheus metrics
   - 구독자 수, 이벤트 처리 속도 등

2. 헬스 체크 개선
   - Event Bus 상태
   - 구독자 통계

3. 로깅 강화
   - Structured logging
   - Debug mode

**테스트:**
- 메트릭 검증
- 대시보드 구성

### Phase 5: 벤치마크 & 문서화 (1-2일)

**목표:** 성능 검증 및 문서화

**작업:**
1. 벤치마크 테스트 작성
   - `events/benchmark_test.go`
   - 다양한 시나리오

2. 성능 리포트 생성
   - 최대 구독자 수
   - 응답 시간 분포

3. 문서 작성
   - API 문서
   - 사용 가이드

---

## 테스트 전략

### 단위 테스트

```go
// Filter matching test
func TestFilterMatch(t *testing.T) {
    filter := &Filter{
        FromAddresses: []common.Address{addr1},
        MinValue: big.NewInt(1000),
    }

    // Should match
    event1 := &Event{From: addr1, Value: big.NewInt(2000)}
    assert.True(t, filter.Match(event1))

    // Should not match
    event2 := &Event{From: addr2, Value: big.NewInt(500)}
    assert.False(t, filter.Match(event2))
}
```

### 통합 테스트

```go
// End-to-end subscription test
func TestEventSubscription(t *testing.T) {
    // Setup
    eventBus := NewEventBus()
    wsServer := NewWebSocketServer(eventBus)

    // Connect client
    client := connectTestClient()

    // Subscribe with filter
    filter := &Filter{FromAddresses: []common.Address{testAddr}}
    client.Subscribe("newTransaction", filter)

    // Publish event
    event := &TransactionEvent{From: testAddr, To: addr2}
    eventBus.Publish(event)

    // Verify delivery
    received := client.WaitForEvent(1 * time.Second)
    assert.Equal(t, event, received)
}
```

### 성능 벤치마크

```go
// Subscriber count benchmark
func BenchmarkSubscribers(b *testing.B) {
    for _, numSubs := range []int{10, 100, 1000, 10000} {
        b.Run(fmt.Sprintf("%d_subscribers", numSubs), func(b *testing.B) {
            eventBus := setupEventBus(numSubs)

            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                event := createTestEvent()
                eventBus.Publish(event)
            }
        })
    }
}

// Latency benchmark
func BenchmarkEventLatency(b *testing.B) {
    eventBus := setupEventBus(1000)

    latencies := make([]time.Duration, b.N)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        start := time.Now()
        event := createTestEvent()
        eventBus.Publish(event)
        // Wait for delivery confirmation
        <-eventBus.delivered
        latencies[i] = time.Since(start)
    }

    // Calculate percentiles
    p50, p95, p99 := calculatePercentiles(latencies)
    b.ReportMetric(float64(p50.Microseconds()), "p50_us")
    b.ReportMetric(float64(p95.Microseconds()), "p95_us")
    b.ReportMetric(float64(p99.Microseconds()), "p99_us")
}
```

### 부하 테스트

```bash
# Gradual load increase
vegeta attack -duration=60s -rate=100/s -targets=targets.txt | vegeta report

# Spike test
vegeta attack -duration=10s -rate=1000/s -targets=targets.txt | vegeta report

# Sustained load
vegeta attack -duration=300s -rate=500/s -targets=targets.txt | vegeta report
```

---

## 확장성 고려사항

### 수평 확장 (Horizontal Scaling)

#### Redis 기반 Pub/Sub

```go
type RedisEventBus struct {
    client *redis.Client
    pubsub *redis.PubSub
}

func (r *RedisEventBus) Publish(event *Event) {
    data, _ := json.Marshal(event)
    r.client.Publish(ctx, "events", data)
}

func (r *RedisEventBus) Subscribe(channel string) <-chan *Event {
    // Multiple indexer nodes subscribe to same Redis channel
    // Each node handles subset of WebSocket connections
}
```

**이점:**
- 여러 indexer 노드가 부하 분산
- WebSocket 연결 분산
- 구독자 수 무제한 확장

#### Kafka 기반 Event Streaming

```go
type KafkaEventBus struct {
    producer sarama.SyncProducer
    consumer sarama.ConsumerGroup
}

// Partitioning by address for ordered delivery
func (k *KafkaEventBus) Publish(event *Event) {
    partition := hashAddress(event.From) % numPartitions
    k.producer.SendMessage(&sarama.ProducerMessage{
        Topic:     "blockchain-events",
        Partition: partition,
        Key:       sarama.StringEncoder(event.From.Hex()),
        Value:     sarama.ByteEncoder(eventData),
    })
}
```

**이점:**
- 이벤트 영속성
- 재처리 가능
- 순서 보장 (파티션 내)

### 성능 목표

| 메트릭 | 목표 | 측정 방법 |
|--------|------|-----------|
| 최대 동시 구독자 | 10,000+ | Benchmark test |
| 이벤트 전달 지연 (p50) | < 10ms | Latency benchmark |
| 이벤트 전달 지연 (p99) | < 100ms | Latency benchmark |
| 처리량 | 1000+ events/sec | Throughput test |
| 메모리 사용량 | < 2GB @ 10K subs | Memory profiling |
| CPU 사용률 | < 50% @ 1K events/s | CPU profiling |

---

## 구현 우선순위

### High Priority (Must Have)
1. ✅ Event Bus 기본 구현
2. ✅ Fetcher 연동
3. ✅ 주소 기반 필터링
4. ✅ 성능 벤치마크

### Medium Priority (Should Have)
1. Event type 필터링
2. Filter indexing
3. Bloom filter 최적화
4. 메트릭 수집

### Low Priority (Nice to Have)
1. Redis/Kafka 통합
2. 고급 필터링 (Topics, Value range)
3. Rate limiting per subscriber
4. Event replay 기능

---

## 결론

현재 indexer-go는 이미 견고한 WebSocket pub/sub 기반을 가지고 있습니다. 필요한 것은:

1. **이벤트 소스 연결**: Fetcher → Event Bus → WebSocket
2. **필터 시스템**: 유연한 구독 조건
3. **성능 최적화**: 인덱싱, 배칭, 워커 풀
4. **벤치마킹**: 성능 검증

이 설계를 따르면 **10,000+ 동시 구독자**를 **10ms 이하의 지연시간**으로 처리할 수 있을 것으로 예상됩니다.
