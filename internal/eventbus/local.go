package eventbus

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// Default configuration values for LocalEventBus
const (
	DefaultPublishBufferSize = 1000
	DefaultHistorySize       = 100
)

// LocalEventBus wraps the existing events.EventBus to implement the EventBus interface
// This provides backward compatibility while enabling future distributed implementations
type LocalEventBus struct {
	bus       *events.EventBus
	startTime time.Time
	healthy   atomic.Bool
}

// Ensure LocalEventBus implements EventBus interface
var _ EventBus = (*LocalEventBus)(nil)

// NewLocalEventBus creates a new local in-process event bus with default settings
func NewLocalEventBus() *LocalEventBus {
	return NewLocalEventBusWithConfig(DefaultPublishBufferSize, DefaultHistorySize)
}

// NewLocalEventBusWithConfig creates a new local event bus with custom configuration
func NewLocalEventBusWithConfig(publishBufferSize, historySize int) *LocalEventBus {
	if publishBufferSize <= 0 {
		publishBufferSize = DefaultPublishBufferSize
	}
	if historySize <= 0 {
		historySize = DefaultHistorySize
	}

	eb := &LocalEventBus{
		bus:       events.NewEventBusWithHistory(publishBufferSize, historySize),
		startTime: time.Now(),
	}
	eb.healthy.Store(true)
	return eb
}

// NewLocalEventBusWithOptions creates a new local event bus with functional options
func NewLocalEventBusWithOptions(opts ...Option) *LocalEventBus {
	eb := NewLocalEventBus()
	for _, opt := range opts {
		opt(eb)
	}
	return eb
}

// Run starts the event bus main loop
func (eb *LocalEventBus) Run() {
	eb.healthy.Store(true)
	eb.bus.Run()
	eb.healthy.Store(false)
}

// Stop gracefully stops the event bus
func (eb *LocalEventBus) Stop() {
	eb.healthy.Store(false)
	eb.bus.Stop()
}

// Publish publishes an event to all interested subscribers
func (eb *LocalEventBus) Publish(event events.Event) bool {
	return eb.bus.Publish(event)
}

// PublishWithContext publishes an event with context for cancellation
func (eb *LocalEventBus) PublishWithContext(ctx context.Context, event events.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		if eb.bus.Publish(event) {
			return nil
		}
		return ErrPublishFailed
	}
}

// Subscribe creates a new subscription for the given event types
func (eb *LocalEventBus) Subscribe(
	id events.SubscriptionID,
	eventTypes []events.EventType,
	filter *events.Filter,
	channelSize int,
) *events.Subscription {
	return eb.bus.Subscribe(id, eventTypes, filter, channelSize)
}

// SubscribeWithOptions creates a new subscription with configurable options
func (eb *LocalEventBus) SubscribeWithOptions(
	id events.SubscriptionID,
	eventTypes []events.EventType,
	filter *events.Filter,
	opts events.SubscribeOptions,
) *events.Subscription {
	return eb.bus.SubscribeWithOptions(id, eventTypes, filter, opts)
}

// Unsubscribe removes a subscription
func (eb *LocalEventBus) Unsubscribe(id events.SubscriptionID) {
	eb.bus.Unsubscribe(id)
}

// SubscriberCount returns the current number of active subscribers
func (eb *LocalEventBus) SubscriberCount() int {
	return eb.bus.SubscriberCount()
}

// Stats returns the current statistics
func (eb *LocalEventBus) Stats() (uint64, uint64, uint64) {
	return eb.bus.Stats()
}

// SetMetrics enables Prometheus metrics for the EventBus
func (eb *LocalEventBus) SetMetrics(metrics *events.Metrics) {
	eb.bus.SetMetrics(metrics)
}

// GetSubscriberInfo returns information about a specific subscriber
func (eb *LocalEventBus) GetSubscriberInfo(id events.SubscriptionID) *events.SubscriberInfo {
	return eb.bus.GetSubscriberInfo(id)
}

// GetAllSubscriberInfo returns information about all subscribers
func (eb *LocalEventBus) GetAllSubscriberInfo() []events.SubscriberInfo {
	return eb.bus.GetAllSubscriberInfo()
}

// Healthy returns true if the event bus is operational
func (eb *LocalEventBus) Healthy() bool {
	return eb.healthy.Load()
}

// Type returns the type of event bus implementation
func (eb *LocalEventBus) Type() EventBusType {
	return EventBusTypeLocal
}

// GetDetailedStats returns detailed statistics about the event bus
func (eb *LocalEventBus) GetDetailedStats() EventBusStats {
	totalEvents, totalDeliveries, droppedEvents := eb.bus.Stats()

	stats := EventBusStats{
		TotalEventsPublished: totalEvents,
		TotalEventsDelivered: totalDeliveries,
		TotalEventsDropped:   droppedEvents,
		ActiveSubscribers:    eb.bus.SubscriberCount(),
		Uptime:               time.Since(eb.startTime),
		EventsByType:         make(map[events.EventType]uint64),
	}

	return stats
}

// GetHealthStatus returns the health status of the event bus
func (eb *LocalEventBus) GetHealthStatus() HealthStatus {
	status := "healthy"
	message := "Local event bus is operational"

	if !eb.healthy.Load() {
		status = "unhealthy"
		message = "Local event bus is stopped"
	}

	totalEvents, totalDeliveries, droppedEvents := eb.bus.Stats()
	dropRate := float64(0)
	if totalEvents > 0 {
		dropRate = float64(droppedEvents) / float64(totalEvents) * 100
	}

	// Consider degraded if drop rate is high
	if dropRate > 5.0 && status == "healthy" {
		status = "degraded"
		message = "High event drop rate detected"
	}

	return HealthStatus{
		Status:    status,
		Message:   message,
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"total_events":    totalEvents,
			"total_delivered": totalDeliveries,
			"dropped_events":  droppedEvents,
			"drop_rate":       dropRate,
			"subscribers":     eb.bus.SubscriberCount(),
			"uptime":          time.Since(eb.startTime).String(),
		},
	}
}

// SetPublishBufferSize is a no-op for local event bus (buffer size set at creation)
// This method exists to satisfy the Option pattern
func (eb *LocalEventBus) SetPublishBufferSize(_ int) {
	// Buffer size cannot be changed after creation for local event bus
}

// SetHistorySize is a no-op for local event bus (history size set at creation)
// This method exists to satisfy the Option pattern
func (eb *LocalEventBus) SetHistorySize(_ int) {
	// History size cannot be changed after creation for local event bus
}

// UnderlyingBus returns the underlying events.EventBus for backward compatibility
// This method should only be used during migration and will be deprecated
func (eb *LocalEventBus) UnderlyingBus() *events.EventBus {
	return eb.bus
}
