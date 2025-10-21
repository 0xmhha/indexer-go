# Event Subscription API Reference

> Complete API reference for the Event Subscription System

**Version**: 1.0.0
**Last Updated**: 2025-10-20

---

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [EventBus API](#eventbus-api)
- [Event Types](#event-types)
- [Filters](#filters)
- [Subscription Management](#subscription-management)
- [Metrics & Monitoring](#metrics--monitoring)
- [Error Handling](#error-handling)
- [Best Practices](#best-practices)

---

## Overview

The Event Subscription System provides a high-performance, real-time event delivery mechanism for blockchain data. It supports:

- **100M+ events/sec** throughput
- **Sub-microsecond** latency
- **10,000+ concurrent** subscribers
- **Zero memory allocations** for core operations
- **Flexible filtering** by address, value, block range
- **Prometheus metrics** integration

---

## Quick Start

### Basic Subscription

```go
package main

import (
    "fmt"
    "github.com/0xmhha/indexer-go/events"
)

func main() {
    // Create EventBus
    bus := events.NewEventBus(1000, 100)
    go bus.Run()
    defer bus.Stop()

    // Subscribe to block events
    sub := bus.Subscribe(
        "my-subscriber",
        []events.EventType{events.EventTypeBlock},
        nil, // no filter
        100, // channel buffer size
    )

    // Receive events
    for event := range sub.Channel {
        blockEvent := event.(*events.BlockEvent)
        fmt.Printf("New block: %d\n", blockEvent.Number)
    }
}
```

---

## EventBus API

### NewEventBus

Creates a new EventBus instance.

```go
func NewEventBus(publishBufferSize, subscribeBufferSize int) *EventBus
```

**Parameters**:
- `publishBufferSize` (int): Size of the publish channel buffer (recommended: 1000-10000)
- `subscribeBufferSize` (int): Size of the subscribe channel buffer (recommended: 100)

**Returns**: `*EventBus`

**Example**:
```go
bus := events.NewEventBus(1000, 100)
```

---

### Subscribe

Creates a new subscription for events.

```go
func (eb *EventBus) Subscribe(
    id SubscriptionID,
    eventTypes []EventType,
    filter *Filter,
    channelSize int,
) *Subscription
```

**Parameters**:
- `id` (SubscriptionID): Unique identifier for this subscription
- `eventTypes` ([]EventType): Array of event types to subscribe to
- `filter` (*Filter): Optional filter (nil for no filtering)
- `channelSize` (int): Size of the event channel buffer

**Returns**: `*Subscription` or `nil` if invalid filter

**Example**:
```go
sub := bus.Subscribe(
    "block-monitor",
    []events.EventType{events.EventTypeBlock},
    nil,
    100,
)
```

---

### Unsubscribe

Removes a subscription.

```go
func (eb *EventBus) Unsubscribe(id SubscriptionID)
```

**Parameters**:
- `id` (SubscriptionID): ID of the subscription to remove

**Example**:
```go
bus.Unsubscribe("block-monitor")
```

---

### Publish

Publishes an event to all interested subscribers.

```go
func (eb *EventBus) Publish(event Event) bool
```

**Parameters**:
- `event` (Event): Event to publish

**Returns**: `bool` - true if published successfully, false if channel full

**Example**:
```go
event := events.NewBlockEvent(block)
if !bus.Publish(event) {
    log.Warn("Failed to publish event")
}
```

---

### Stats

Returns current EventBus statistics.

```go
func (eb *EventBus) Stats() (totalEvents, totalDeliveries, droppedEvents uint64)
```

**Returns**:
- `totalEvents` (uint64): Total events published
- `totalDeliveries` (uint64): Total event deliveries to subscribers
- `droppedEvents` (uint64): Total events dropped due to full channels

**Example**:
```go
total, delivered, dropped := bus.Stats()
fmt.Printf("Events: %d, Delivered: %d, Dropped: %d\n", total, delivered, dropped)
```

---

### SubscriberCount

Returns the current number of active subscribers.

```go
func (eb *EventBus) SubscriberCount() int
```

**Returns**: `int` - Number of active subscribers

**Example**:
```go
count := bus.SubscriberCount()
fmt.Printf("Active subscribers: %d\n", count)
```

---

### GetSubscriberInfo

Returns detailed information about a specific subscriber.

```go
func (eb *EventBus) GetSubscriberInfo(id SubscriptionID) *SubscriberInfo
```

**Parameters**:
- `id` (SubscriptionID): Subscriber ID

**Returns**: `*SubscriberInfo` or `nil` if not found

**Example**:
```go
info := bus.GetSubscriberInfo("block-monitor")
if info != nil {
    fmt.Printf("Received: %d, Dropped: %d\n",
        info.EventsReceived, info.EventsDropped)
}
```

---

### GetAllSubscriberInfo

Returns information about all active subscribers.

```go
func (eb *EventBus) GetAllSubscriberInfo() []SubscriberInfo
```

**Returns**: `[]SubscriberInfo` - Array of subscriber information

**Example**:
```go
allInfo := bus.GetAllSubscriberInfo()
for _, info := range allInfo {
    fmt.Printf("Subscriber %s: %d events\n", info.ID, info.EventsReceived)
}
```

---

## Event Types

### EventType

Enum of available event types.

```go
type EventType string

const (
    EventTypeBlock       EventType = "block"
    EventTypeTransaction EventType = "transaction"
)
```

### BlockEvent

Represents a new block event.

```go
type BlockEvent struct {
    Number      uint64
    Hash        string
    ParentHash  string
    Timestamp   uint64
    GasLimit    uint64
    GasUsed     uint64
    Difficulty  string
    TxCount     int
    Metadata    EventMetadata
}
```

**Example**:
```go
event := blockEvent.(*events.BlockEvent)
fmt.Printf("Block %d: %d transactions\n", event.Number, event.TxCount)
```

### TransactionEvent

Represents a transaction event.

```go
type TransactionEvent struct {
    Hash        string
    From        common.Address
    To          *common.Address
    Value       string
    Gas         uint64
    GasPrice    string
    Nonce       uint64
    Data        string
    BlockNumber uint64
    BlockHash   string
    TxIndex     uint
    Status      uint64
    Metadata    EventMetadata
}
```

**Example**:
```go
event := txEvent.(*events.TransactionEvent)
fmt.Printf("TX %s: %s -> %s\n", event.Hash, event.From.Hex(), event.To.Hex())
```

---

## Filters

### Filter Structure

```go
type Filter struct {
    // Address filters (any address in transaction)
    Addresses []common.Address

    // From address filters (transaction sender)
    FromAddresses []common.Address

    // To address filters (transaction recipient)
    ToAddresses []common.Address

    // Value range filters
    MinValue *big.Int
    MaxValue *big.Int

    // Block range filters
    FromBlock uint64
    ToBlock   uint64
}
```

### Creating Filters

#### Address Filter

Subscribe to transactions involving specific addresses:

```go
filter := &events.Filter{
    Addresses: []common.Address{
        common.HexToAddress("0x1111111111111111111111111111111111111111"),
        common.HexToAddress("0x2222222222222222222222222222222222222222"),
    },
}

sub := bus.Subscribe("address-monitor",
    []events.EventType{events.EventTypeTransaction},
    filter,
    100,
)
```

#### From/To Address Filter

Subscribe to transactions from or to specific addresses:

```go
filter := &events.Filter{
    FromAddresses: []common.Address{
        common.HexToAddress("0x1111..."),
    },
    ToAddresses: []common.Address{
        common.HexToAddress("0x2222..."),
    },
}
```

#### Value Range Filter

Subscribe to transactions within a value range:

```go
filter := &events.Filter{
    MinValue: big.NewInt(1000000000000000000), // 1 ETH
    MaxValue: big.NewInt(10000000000000000000), // 10 ETH
}
```

#### Block Range Filter

Subscribe to events within a block range:

```go
filter := &events.Filter{
    FromBlock: 1000000,
    ToBlock:   2000000,
}
```

#### Complex Filter

Combine multiple filter conditions:

```go
filter := &events.Filter{
    FromAddresses: []common.Address{
        common.HexToAddress("0x1111..."),
    },
    ToAddresses: []common.Address{
        common.HexToAddress("0x2222..."),
    },
    MinValue:  big.NewInt(1000000000000000000),
    MaxValue:  big.NewInt(10000000000000000000),
    FromBlock: 1000000,
    ToBlock:   2000000,
}
```

### Filter Validation

Filters are automatically validated:

```go
filter := &events.Filter{
    FromBlock: 2000000,
    ToBlock:   1000000, // Invalid: ToBlock < FromBlock
}

sub := bus.Subscribe("invalid", eventTypes, filter, 100)
// Returns nil due to invalid filter
```

---

## Subscription Management

### Subscription Lifecycle

```go
// 1. Create subscription
sub := bus.Subscribe("my-sub", eventTypes, filter, 100)
if sub == nil {
    log.Fatal("Failed to create subscription")
}

// 2. Receive events
go func() {
    for event := range sub.Channel {
        handleEvent(event)
    }
}()

// 3. Unsubscribe when done
defer bus.Unsubscribe("my-sub")
```

### Multiple Subscriptions

```go
// Subscribe to multiple event types
sub := bus.Subscribe(
    "multi-monitor",
    []events.EventType{
        events.EventTypeBlock,
        events.EventTypeTransaction,
    },
    nil,
    100,
)

for event := range sub.Channel {
    switch e := event.(type) {
    case *events.BlockEvent:
        fmt.Printf("Block: %d\n", e.Number)
    case *events.TransactionEvent:
        fmt.Printf("TX: %s\n", e.Hash)
    }
}
```

### Graceful Shutdown

```go
// Create context for graceful shutdown
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Start subscriber
go func() {
    for {
        select {
        case event := <-sub.Channel:
            handleEvent(event)
        case <-ctx.Done():
            bus.Unsubscribe("my-sub")
            return
        }
    }
}()

// Trigger shutdown
cancel()
```

---

## Metrics & Monitoring

### Enable Metrics

```go
// Create metrics
metrics := events.NewMetrics("indexer", "eventbus")

// Attach to EventBus
bus.SetMetrics(metrics)
```

### Available Metrics

See [METRICS_MONITORING.md](./METRICS_MONITORING.md) for complete metrics reference.

---

## Error Handling

### Subscription Errors

```go
sub := bus.Subscribe(id, eventTypes, filter, channelSize)
if sub == nil {
    // Possible causes:
    // - Invalid filter
    // - EventBus stopped
    log.Error("Failed to create subscription")
}
```

### Publish Errors

```go
if !bus.Publish(event) {
    // Publish channel is full
    // Consider:
    // - Increasing publishBufferSize
    // - Slowing down event generation
    log.Warn("Event publish failed - channel full")
}
```

### Dropped Events

Monitor dropped events via stats:

```go
_, _, dropped := bus.Stats()
if dropped > 0 {
    // Subscriber channels are full
    // Consider:
    // - Increasing subscriber channel size
    // - Faster event processing
    // - Removing slow subscribers
    log.Warn("Events dropped: %d", dropped)
}
```

---

## Best Practices

### 1. Channel Buffer Sizing

```go
// High-throughput: larger buffers
bus := events.NewEventBus(10000, 100)

// Low-latency: smaller buffers
bus := events.NewEventBus(1000, 10)

// Subscriber buffers: match expected throughput
sub := bus.Subscribe(id, types, filter, 1000) // High throughput
sub := bus.Subscribe(id, types, filter, 10)   // Low latency
```

### 2. Filter Optimization

```go
// ✅ Good: Specific address filters (fast)
filter := &events.Filter{
    FromAddresses: []common.Address{addr1, addr2},
}

// ⚠️ Slower: Value range filters (big.Int parsing)
filter := &events.Filter{
    MinValue: big.NewInt(1000),
    MaxValue: big.NewInt(10000),
}
```

### 3. Event Processing

```go
// ✅ Good: Fast processing
go func() {
    for event := range sub.Channel {
        processQuickly(event)
    }
}()

// ❌ Bad: Slow processing (blocks channel)
go func() {
    for event := range sub.Channel {
        slowDatabaseWrite(event) // Will cause drops
    }
}()

// ✅ Better: Async processing
go func() {
    for event := range sub.Channel {
        go processAsync(event) // Non-blocking
    }
}()
```

### 4. Resource Cleanup

```go
// Always unsubscribe
defer bus.Unsubscribe(id)

// Always stop EventBus
defer bus.Stop()
```

### 5. Monitoring

```go
// Check stats regularly
ticker := time.NewTicker(10 * time.Second)
go func() {
    for range ticker.C {
        total, delivered, dropped := bus.Stats()
        if dropped > 0 {
            log.Warn("Dropped events: %d", dropped)
        }
    }
}()
```

---

## Performance Characteristics

### Throughput

- **100M+ events/sec** with 10,000 subscribers
- **8.5 ns/op** per subscriber delivery
- **Zero allocations** for core operations

### Latency

- **Sub-microsecond** event delivery
- **2.8 ns** filter matching (address filters)
- **75 ns** filter matching (value range filters)

### Scalability

- **10,000+ concurrent** subscribers
- **Linear scaling** up to 1,000 subscribers
- **Constant performance** beyond 1,000 subscribers

---

## See Also

- [METRICS_MONITORING.md](./METRICS_MONITORING.md) - Prometheus metrics guide
- [BENCHMARK_RESULTS.md](./BENCHMARK_RESULTS.md) - Performance benchmarks
- [EVENT_SUBSCRIPTION_DESIGN.md](./EVENT_SUBSCRIPTION_DESIGN.md) - Architecture design
