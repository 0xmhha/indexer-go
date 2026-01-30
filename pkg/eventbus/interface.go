package eventbus

import (
	"context"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// Publisher defines the interface for publishing events
type Publisher interface {
	// Publish publishes an event to all interested subscribers
	// Returns true if the event was successfully queued for publishing
	Publish(event events.Event) bool

	// PublishWithContext publishes an event with context for cancellation
	PublishWithContext(ctx context.Context, event events.Event) error
}

// Subscriber defines the interface for subscribing to events
type Subscriber interface {
	// Subscribe creates a new subscription for the given event types
	// Returns a Subscription that can be used to receive events
	Subscribe(
		id events.SubscriptionID,
		eventTypes []events.EventType,
		filter *events.Filter,
		channelSize int,
	) *events.Subscription

	// SubscribeWithOptions creates a new subscription with configurable options
	SubscribeWithOptions(
		id events.SubscriptionID,
		eventTypes []events.EventType,
		filter *events.Filter,
		opts events.SubscribeOptions,
	) *events.Subscription

	// Unsubscribe removes a subscription
	Unsubscribe(id events.SubscriptionID)
}

// EventBus defines the complete interface for an event bus implementation
type EventBus interface {
	Publisher
	Subscriber

	// Run starts the event bus main loop
	// This should be called in a goroutine
	Run()

	// Stop gracefully stops the event bus
	Stop()

	// SubscriberCount returns the current number of active subscribers
	SubscriberCount() int

	// Stats returns the current statistics
	// Returns (totalEvents, totalDeliveries, droppedEvents)
	Stats() (uint64, uint64, uint64)

	// SetMetrics enables Prometheus metrics for the EventBus
	SetMetrics(metrics *events.Metrics)

	// GetSubscriberInfo returns information about a specific subscriber
	GetSubscriberInfo(id events.SubscriptionID) *events.SubscriberInfo

	// GetAllSubscriberInfo returns information about all subscribers
	GetAllSubscriberInfo() []events.SubscriberInfo

	// Healthy returns true if the event bus is operational
	Healthy() bool

	// Type returns the type of event bus implementation
	Type() EventBusType
}

// EventBusType represents the type of event bus implementation
type EventBusType string

const (
	// EventBusTypeLocal represents an in-process local event bus
	EventBusTypeLocal EventBusType = "local"

	// EventBusTypeRedis represents a Redis Pub/Sub event bus
	EventBusTypeRedis EventBusType = "redis"

	// EventBusTypeKafka represents a Kafka event bus
	EventBusTypeKafka EventBusType = "kafka"

	// EventBusTypeHybrid represents a hybrid event bus (local + distributed)
	EventBusTypeHybrid EventBusType = "hybrid"
)

// DistributedEventBus extends EventBus with distributed-specific functionality
type DistributedEventBus interface {
	EventBus

	// Connect establishes connection to the distributed backend
	Connect(ctx context.Context) error

	// Disconnect closes the connection to the distributed backend
	Disconnect(ctx context.Context) error

	// IsConnected returns true if connected to the distributed backend
	IsConnected() bool

	// NodeID returns the unique identifier for this node
	NodeID() string
}

// EventSerializer defines the interface for serializing/deserializing events
type EventSerializer interface {
	// Serialize converts an event to bytes
	Serialize(event events.Event) ([]byte, error)

	// Deserialize converts bytes back to an event
	Deserialize(data []byte) (events.Event, error)

	// ContentType returns the MIME type of the serialized format
	ContentType() string
}

// EventBusStats provides detailed statistics about event bus operations
type EventBusStats struct {
	// TotalEventsPublished is the total number of events published
	TotalEventsPublished uint64 `json:"total_events_published"`

	// TotalEventsDelivered is the total number of events delivered to subscribers
	TotalEventsDelivered uint64 `json:"total_events_delivered"`

	// TotalEventsDropped is the number of events dropped due to full channels
	TotalEventsDropped uint64 `json:"total_events_dropped"`

	// ActiveSubscribers is the current number of active subscribers
	ActiveSubscribers int `json:"active_subscribers"`

	// PublishChannelUtilization is the current utilization of the publish channel (0-100%)
	PublishChannelUtilization float64 `json:"publish_channel_utilization"`

	// AverageDeliveryLatency is the average time to deliver an event
	AverageDeliveryLatency time.Duration `json:"average_delivery_latency"`

	// EventsByType tracks events published by type
	EventsByType map[events.EventType]uint64 `json:"events_by_type"`

	// LastEventTime is when the last event was published
	LastEventTime time.Time `json:"last_event_time"`

	// Uptime is how long the event bus has been running
	Uptime time.Duration `json:"uptime"`
}

// HealthStatus represents the health status of an event bus component
type HealthStatus struct {
	// Status is the overall status: "healthy", "degraded", "unhealthy"
	Status string `json:"status"`

	// Message provides additional context about the status
	Message string `json:"message,omitempty"`

	// LastCheck is when the health was last checked
	LastCheck time.Time `json:"last_check"`

	// Details contains component-specific health details
	Details map[string]interface{} `json:"details,omitempty"`
}

// Option defines a functional option for configuring event bus implementations
type Option func(interface{})

// WithPublishBufferSize sets the publish buffer size
func WithPublishBufferSize(size int) Option {
	return func(eb interface{}) {
		if setter, ok := eb.(interface{ SetPublishBufferSize(int) }); ok {
			setter.SetPublishBufferSize(size)
		}
	}
}

// WithHistorySize sets the event history size for replay
func WithHistorySize(size int) Option {
	return func(eb interface{}) {
		if setter, ok := eb.(interface{ SetHistorySize(int) }); ok {
			setter.SetHistorySize(size)
		}
	}
}

// WithMetrics enables Prometheus metrics collection
func WithMetrics(metrics *events.Metrics) Option {
	return func(eb interface{}) {
		if eb, ok := eb.(EventBus); ok {
			eb.SetMetrics(metrics)
		}
	}
}
