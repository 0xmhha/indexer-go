# Metrics & Monitoring Guide

Monitoring reference for the Event Subscription System, covering Prometheus integration, dashboards, and alerting workflows.

**Last Updated**: 2025-11-20

---

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Available Metrics](#available-metrics)
- [HTTP Endpoints](#http-endpoints)
- [Prometheus Configuration](#prometheus-configuration)
- [Grafana Dashboards](#grafana-dashboards)
- [Alerting Rules](#alerting-rules)
- [Troubleshooting](#troubleshooting)

---

## Overview

The Event Subscription System provides comprehensive Prometheus metrics for monitoring:

- **Real-time metrics** collection
- **Zero performance overhead** when disabled
- **Sub-microsecond** metric recording
- **Production-ready** monitoring
- **Grafana-compatible** metrics

---

## Quick Start

### 1. Enable Metrics

```go
package main

import (
    "github.com/0xmhha/indexer-go/events"
    "github.com/0xmhha/indexer-go/api"
)

func main() {
    // Create EventBus
    bus := events.NewEventBus(1000, 100)

    // Enable metrics
    metrics := events.NewMetrics("indexer", "eventbus")
    bus.SetMetrics(metrics)

    // Start EventBus
    go bus.Run()
    defer bus.Stop()

    // Create API server with metrics endpoint
    server, _ := api.NewServer(config, logger, storage)
    server.SetEventBus(bus) // Attach EventBus for /health and /subscribers

    // Metrics available at: http://localhost:8080/metrics
    server.Start()
}
```

### 2. Access Metrics

```bash
# Prometheus metrics
curl http://localhost:8080/metrics

# Health check with EventBus stats
curl http://localhost:8080/health

# Subscriber statistics
curl http://localhost:8080/subscribers
```

---

## Available Metrics

### Gauges (Current Values)

#### indexer_eventbus_subscribers_total

Current number of active subscribers.

**Type**: Gauge
**Labels**: None

```promql
indexer_eventbus_subscribers_total
```

**Example**:
```
indexer_eventbus_subscribers_total 42
```

---

#### indexer_eventbus_subscribers_by_type

Current number of subscribers by event type.

**Type**: Gauge
**Labels**: `event_type` (block, transaction)

```promql
indexer_eventbus_subscribers_by_type{event_type="block"}
indexer_eventbus_subscribers_by_type{event_type="transaction"}
```

**Example**:
```
indexer_eventbus_subscribers_by_type{event_type="block"} 25
indexer_eventbus_subscribers_by_type{event_type="transaction"} 17
```

---

#### indexer_eventbus_publish_channel_size

Current size of the publish channel buffer.

**Type**: Gauge
**Labels**: None

```promql
indexer_eventbus_publish_channel_size
```

**Use Case**: Monitor channel pressure
```promql
# Alert if publish channel >80% full
indexer_eventbus_publish_channel_size > 800  # Assuming buffer size 1000
```

---

#### indexer_eventbus_subscribe_channel_size

Current size of the subscribe channel buffer.

**Type**: Gauge
**Labels**: None

```promql
indexer_eventbus_subscribe_channel_size
```

---

### Counters (Cumulative Values)

#### indexer_eventbus_events_published_total

Total number of events published.

**Type**: Counter
**Labels**: `event_type` (block, transaction)

```promql
indexer_eventbus_events_published_total{event_type="block"}
indexer_eventbus_events_published_total{event_type="transaction"}
```

**Queries**:
```promql
# Events per second (last 5 minutes)
rate(indexer_eventbus_events_published_total[5m])

# Total events published (all types)
sum(indexer_eventbus_events_published_total)
```

---

#### indexer_eventbus_events_delivered_total

Total number of events delivered to subscribers.

**Type**: Counter
**Labels**: `event_type` (block, transaction)

```promql
indexer_eventbus_events_delivered_total{event_type="block"}
```

**Queries**:
```promql
# Delivery rate per second
rate(indexer_eventbus_events_delivered_total[5m])

# Delivery success rate
rate(indexer_eventbus_events_delivered_total[5m]) /
rate(indexer_eventbus_events_published_total[5m])
```

---

#### indexer_eventbus_events_dropped_total

Total number of events dropped due to full channels.

**Type**: Counter
**Labels**: `event_type` (block, transaction)

```promql
indexer_eventbus_events_dropped_total{event_type="transaction"}
```

**Queries**:
```promql
# Drop rate per second
rate(indexer_eventbus_events_dropped_total[5m])

# Drop rate percentage
rate(indexer_eventbus_events_dropped_total[5m]) /
rate(indexer_eventbus_events_published_total[5m]) * 100
```

---

#### indexer_eventbus_events_filtered_total

Total number of events filtered out by subscriber filters.

**Type**: Counter
**Labels**: `event_type`, `filter_type` (filtered)

```promql
indexer_eventbus_events_filtered_total{event_type="transaction",filter_type="filtered"}
```

**Queries**:
```promql
# Filter efficiency (events filtered / events published)
rate(indexer_eventbus_events_filtered_total[5m]) /
rate(indexer_eventbus_events_published_total[5m]) * 100
```

---

#### indexer_eventbus_subscriptions_total

Total number of subscription requests.

**Type**: Counter
**Labels**: None

```promql
indexer_eventbus_subscriptions_total
```

---

#### indexer_eventbus_unsubscriptions_total

Total number of unsubscription requests.

**Type**: Counter
**Labels**: None

```promql
indexer_eventbus_unsubscriptions_total
```

---

### Histograms (Distributions)

#### indexer_eventbus_event_delivery_duration_seconds

Event delivery duration in seconds.

**Type**: Histogram
**Labels**: `event_type` (block, transaction)
**Buckets**: 1μs, 5μs, 10μs, 50μs, 100μs, 500μs, 1ms, 5ms, 10ms

```promql
indexer_eventbus_event_delivery_duration_seconds{event_type="block"}
```

**Queries**:
```promql
# p50 delivery latency
histogram_quantile(0.5,
  rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m])
)

# p95 delivery latency
histogram_quantile(0.95,
  rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m])
)

# p99 delivery latency
histogram_quantile(0.99,
  rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m])
)
```

---

#### indexer_eventbus_filter_matching_duration_seconds

Filter matching duration in seconds.

**Type**: Histogram
**Labels**: `event_type`, `has_filter` (true, false)
**Buckets**: 1μs, 5μs, 10μs, 50μs, 100μs, 500μs, 1ms

```promql
indexer_eventbus_filter_matching_duration_seconds{has_filter="true"}
```

**Queries**:
```promql
# Average filter matching time
rate(indexer_eventbus_filter_matching_duration_seconds_sum[5m]) /
rate(indexer_eventbus_filter_matching_duration_seconds_count[5m])

# p99 filter matching time
histogram_quantile(0.99,
  rate(indexer_eventbus_filter_matching_duration_seconds_bucket[5m])
)
```

---

#### indexer_eventbus_broadcast_duration_seconds

Event broadcast duration in seconds.

**Type**: Histogram
**Labels**: None
**Buckets**: 10μs, 50μs, 100μs, 500μs, 1ms, 5ms, 10ms, 50ms, 100ms

```promql
indexer_eventbus_broadcast_duration_seconds
```

**Queries**:
```promql
# p99 broadcast latency
histogram_quantile(0.99,
  rate(indexer_eventbus_broadcast_duration_seconds_bucket[5m])
)
```

---

## HTTP Endpoints

### GET /metrics

Prometheus metrics endpoint.

**Example**:
```bash
curl http://localhost:8080/metrics
```

**Response** (Prometheus format):
```
# HELP indexer_eventbus_subscribers_total Current number of active subscribers
# TYPE indexer_eventbus_subscribers_total gauge
indexer_eventbus_subscribers_total 42

# HELP indexer_eventbus_events_published_total Total number of events published
# TYPE indexer_eventbus_events_published_total counter
indexer_eventbus_events_published_total{event_type="block"} 1000
indexer_eventbus_events_published_total{event_type="transaction"} 5000
```

---

### GET /health

Health check with EventBus status.

**Example**:
```bash
curl http://localhost:8080/health
```

**Response** (JSON):
```json
{
  "status": "ok",
  "timestamp": "2025-10-20T20:00:00Z",
  "eventbus": {
    "subscribers": 42,
    "total_events": 6000,
    "total_deliveries": 252000,
    "dropped_events": 0
  }
}
```

---

### GET /subscribers

Detailed subscriber statistics.

**Example**:
```bash
curl http://localhost:8080/subscribers
```

**Response** (JSON):
```json
{
  "total_count": 2,
  "subscribers": [
    {
      "ID": "block-monitor",
      "EventTypes": ["block"],
      "HasFilter": false,
      "EventsReceived": 1000,
      "EventsDropped": 0,
      "LastEventTime": "2025-10-20T20:00:00Z",
      "CreatedAt": "2025-10-20T19:00:00Z",
      "Uptime": "1h0m0s"
    },
    {
      "ID": "tx-analyzer",
      "EventTypes": ["transaction"],
      "HasFilter": true,
      "EventsReceived": 5000,
      "EventsDropped": 3,
      "LastEventTime": "2025-10-20T20:00:00Z",
      "CreatedAt": "2025-10-20T19:30:00Z",
      "Uptime": "30m0s"
    }
  ]
}
```

---

## Prometheus Configuration

### Scrape Configuration

Add to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'indexer-eventbus'
    scrape_interval: 15s
    scrape_timeout: 10s
    static_configs:
      - targets: ['localhost:8080']
        labels:
          service: 'indexer'
          component: 'eventbus'
```

### Recording Rules

Create `eventbus_rules.yml`:

```yaml
groups:
  - name: eventbus_recording_rules
    interval: 30s
    rules:
      # Event rates
      - record: indexer:eventbus:events_published:rate5m
        expr: rate(indexer_eventbus_events_published_total[5m])

      - record: indexer:eventbus:events_delivered:rate5m
        expr: rate(indexer_eventbus_events_delivered_total[5m])

      - record: indexer:eventbus:events_dropped:rate5m
        expr: rate(indexer_eventbus_events_dropped_total[5m])

      # Latency percentiles
      - record: indexer:eventbus:delivery_latency:p50
        expr: histogram_quantile(0.5,
          rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m]))

      - record: indexer:eventbus:delivery_latency:p95
        expr: histogram_quantile(0.95,
          rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m]))

      - record: indexer:eventbus:delivery_latency:p99
        expr: histogram_quantile(0.99,
          rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m]))

      # Drop rate percentage
      - record: indexer:eventbus:drop_rate_percent
        expr: (rate(indexer_eventbus_events_dropped_total[5m]) /
               rate(indexer_eventbus_events_published_total[5m])) * 100
```

---

## Grafana Dashboards

### Key Panels

#### 1. Event Throughput

```promql
# Events per second by type
sum(rate(indexer_eventbus_events_published_total[5m])) by (event_type)
```

**Visualization**: Time series graph
**Legend**: Block Events, Transaction Events

---

#### 2. Delivery Success Rate

```promql
# Delivery rate
rate(indexer_eventbus_events_delivered_total[5m])

# Drop rate
rate(indexer_eventbus_events_dropped_total[5m])
```

**Visualization**: Time series graph
**Legend**: Delivered, Dropped

---

#### 3. Latency Heatmap

```promql
# p50, p95, p99 latency
histogram_quantile(0.50, rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m]))
histogram_quantile(0.95, rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m]))
histogram_quantile(0.99, rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m]))
```

**Visualization**: Heatmap or Graph
**Unit**: seconds

---

#### 4. Active Subscribers

```promql
# Total subscribers
indexer_eventbus_subscribers_total

# Subscribers by type
indexer_eventbus_subscribers_by_type
```

**Visualization**: Stat panel or Gauge

---

#### 5. Channel Pressure

```promql
# Publish channel utilization (%)
(indexer_eventbus_publish_channel_size / 1000) * 100

# Subscribe channel utilization (%)
(indexer_eventbus_subscribe_channel_size / 100) * 100
```

**Visualization**: Gauge (0-100%)
**Thresholds**: Warning >70%, Critical >90%

---

## Alerting Rules

### Critical Alerts

```yaml
groups:
  - name: eventbus_critical_alerts
    rules:
      # High drop rate
      - alert: EventBusHighDropRate
        expr: |
          (rate(indexer_eventbus_events_dropped_total[5m]) /
           rate(indexer_eventbus_events_published_total[5m])) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "EventBus dropping >5% of events"
          description: "Drop rate: {{ $value | humanizePercentage }}"

      # No subscribers
      - alert: EventBusNoSubscribers
        expr: indexer_eventbus_subscribers_total == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "EventBus has no active subscribers"

      # High latency
      - alert: EventBusHighLatency
        expr: |
          histogram_quantile(0.99,
            rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m])
          ) > 0.001  # 1ms
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "EventBus p99 latency >1ms"
          description: "p99 latency: {{ $value }}s"
```

### Warning Alerts

```yaml
groups:
  - name: eventbus_warning_alerts
    rules:
      # Channel pressure
      - alert: EventBusChannelPressure
        expr: indexer_eventbus_publish_channel_size > 800
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Publish channel >80% full"
          description: "Channel size: {{ $value }}"

      # Subscriber drops
      - alert: EventBusSubscriberDrops
        expr: |
          increase(indexer_eventbus_events_dropped_total[10m]) > 100
        labels:
          severity: warning
        annotations:
          summary: "Subscriber experiencing drops"
          description: "Dropped {{ $value }} events in 10m"
```

---

## Troubleshooting

### High Drop Rate

**Symptom**: `indexer_eventbus_events_dropped_total` increasing

**Causes**:
1. Subscriber channels full (slow processing)
2. Subscriber not draining channel
3. Buffer too small

**Solutions**:
```go
// 1. Increase subscriber channel size
sub := bus.Subscribe(id, types, filter, 10000) // Larger buffer

// 2. Faster event processing
go func() {
    for event := range sub.Channel {
        go processAsync(event) // Non-blocking
    }
}()

// 3. Monitor subscriber stats
info := bus.GetSubscriberInfo(id)
if info.EventsDropped > 0 {
    log.Warn("Subscriber %s dropping events", id)
}
```

---

### High Latency

**Symptom**: High p99 delivery latency

**Causes**:
1. Too many subscribers
2. Complex filters
3. Slow filter matching

**Solutions**:
```go
// 1. Use simple address filters (fast)
filter := &events.Filter{
    FromAddresses: []common.Address{addr},
}

// 2. Avoid value range filters (slower)
// Instead of:
filter := &events.Filter{
    MinValue: big.NewInt(100),
    MaxValue: big.NewInt(1000),
}
// Use application-level filtering
```

---

### No Events Delivered

**Symptom**: `indexer_eventbus_events_delivered_total` not increasing

**Checks**:
```bash
# 1. Check subscribers
curl http://localhost:8080/subscribers

# 2. Check EventBus status
curl http://localhost:8080/health

# 3. Check event publication
# Verify events_published_total is increasing
```

---

### Memory Growth

**Symptom**: Memory usage increasing

**Causes**:
1. Subscribers not unsubscribing
2. Event channel buffers too large
3. Dropped events accumulating

**Solutions**:
```go
// 1. Always unsubscribe
defer bus.Unsubscribe(id)

// 2. Monitor subscriber count
count := bus.SubscriberCount()
if count > expected {
    log.Warn("Unexpected subscriber count: %d", count)
}

// 3. Check for leaked subscriptions
allInfo := bus.GetAllSubscriberInfo()
for _, info := range allInfo {
    if time.Since(info.CreatedAt) > 24*time.Hour {
        log.Warn("Long-lived subscriber: %s", info.ID)
    }
}
```

---

## Performance Tuning

### Optimal Configuration

```go
// Production configuration
bus := events.NewEventBus(
    10000, // publishBufferSize: balance memory vs throughput
    100,   // subscribeBufferSize: typically 100 is sufficient
)

// Enable metrics
metrics := events.NewMetrics("indexer", "eventbus")
bus.SetMetrics(metrics)

// Subscriber channel sizing
sub := bus.Subscribe(
    id,
    eventTypes,
    filter,
    1000, // Balance: 100-10000 depending on throughput
)
```

### Monitoring Targets

- **Throughput**: 1M+ events/sec
- **Latency (p99)**: <1ms
- **Drop Rate**: <0.1%
- **Channel Utilization**: <70%

---

## See Also

- [EVENT_SUBSCRIPTION_API.md](./EVENT_SUBSCRIPTION_API.md) - API reference
- [BENCHMARK_RESULTS.md](./BENCHMARK_RESULTS.md) - Performance benchmarks
- [EVENT_SUBSCRIPTION_DESIGN.md](./EVENT_SUBSCRIPTION_DESIGN.md) - Architecture design
