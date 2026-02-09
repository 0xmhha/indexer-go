package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/segmentio/kafka-go"
)

// KafkaEventBus implements EventBus using Kafka for distributed event broadcasting.
// It follows the same adapter pattern as RedisEventBus: a local bus handles subscriptions
// and delivery, while Kafka provides cross-node event broadcasting.
type KafkaEventBus struct {
	// localBus handles local subscriptions and event delivery
	localBus *LocalEventBus

	// producer writes events to Kafka (reuses existing KafkaProducer)
	producer *KafkaProducer

	// reader consumes events from Kafka via consumer group
	reader *kafka.Reader

	// config holds the Kafka configuration
	config config.EventBusKafkaConfig

	// serializer handles event serialization/deserialization
	serializer EventSerializer

	// nodeID is the unique identifier for this node
	nodeID string

	// ctx is the context for the event bus
	ctx context.Context

	// cancel is the cancel function for the event bus
	cancel context.CancelFunc

	// wg tracks background goroutines
	wg sync.WaitGroup

	// connected indicates whether we're connected to Kafka
	connected atomic.Bool

	// healthy indicates overall health status
	healthy atomic.Bool

	// stats tracks statistics
	stats struct {
		publishedRemote atomic.Uint64
		receivedRemote  atomic.Uint64
		publishErrors   atomic.Uint64
		echoesSkipped   atomic.Uint64
	}

	// startTime is when the event bus was created
	startTime time.Time

	// logger is the structured logger
	logger *slog.Logger
}

// Ensure KafkaEventBus implements DistributedEventBus interface
var _ DistributedEventBus = (*KafkaEventBus)(nil)

// NewKafkaEventBus creates a new Kafka EventBus
func NewKafkaEventBus(cfg config.EventBusKafkaConfig, nodeID string, opts ...Option) (*KafkaEventBus, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("%w: no Kafka brokers configured", ErrInvalidConfiguration)
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("%w: no Kafka topic configured", ErrInvalidConfiguration)
	}
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("%w: no Kafka group ID configured", ErrInvalidConfiguration)
	}

	// Create the internal producer
	producer, err := NewKafkaProducer(cfg, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	eb := &KafkaEventBus{
		localBus:   NewLocalEventBusWithConfig(constants.DefaultPublishBufferSize, constants.DefaultSubscribeBufferSize),
		producer:   producer,
		config:     cfg,
		serializer: NewJSONSerializer(),
		nodeID:     nodeID,
		ctx:        ctx,
		cancel:     cancel,
		startTime:  time.Now(),
		logger:     slog.Default().With("component", "kafka-eventbus", "node_id", nodeID),
	}

	// Apply options
	for _, opt := range opts {
		opt(eb)
	}

	return eb, nil
}

// Connect establishes connection to Kafka (producer + consumer)
func (eb *KafkaEventBus) Connect(ctx context.Context) error {
	if eb.connected.Load() {
		return ErrAlreadyConnected
	}

	// Connect the producer
	if err := eb.producer.Connect(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	// Build dialer for the reader
	dialer, err := buildKafkaDialer(eb.config)
	if err != nil {
		_ = eb.producer.Disconnect(ctx)
		return fmt.Errorf("failed to build Kafka dialer: %w", err)
	}

	// Create Kafka reader (consumer group)
	readerCfg := kafka.ReaderConfig{
		Brokers:     eb.config.Brokers,
		Topic:       eb.config.Topic,
		GroupID:     eb.config.GroupID,
		StartOffset: kafka.LastOffset, // Only new messages
		MaxWait:     time.Second,
	}
	if dialer != nil {
		readerCfg.Dialer = dialer
	}

	eb.reader = kafka.NewReader(readerCfg)

	eb.connected.Store(true)
	eb.healthy.Store(true)

	// Start consumer loop
	eb.wg.Add(1)
	go eb.consumeLoop()

	eb.logger.Info("connected to Kafka",
		"brokers", eb.config.Brokers,
		"topic", eb.config.Topic,
		"group_id", eb.config.GroupID,
	)

	return nil
}

// Disconnect closes the connection to Kafka
func (eb *KafkaEventBus) Disconnect(ctx context.Context) error {
	if !eb.connected.Load() {
		return ErrNotConnected
	}

	eb.connected.Store(false)
	eb.healthy.Store(false)

	// Close reader
	if eb.reader != nil {
		if err := eb.reader.Close(); err != nil {
			eb.logger.Error("error closing Kafka reader", "error", err)
		}
	}

	// Disconnect producer
	if err := eb.producer.Disconnect(ctx); err != nil {
		eb.logger.Error("error disconnecting Kafka producer", "error", err)
	}

	eb.logger.Info("disconnected from Kafka")
	return nil
}

// IsConnected returns true if connected to Kafka
func (eb *KafkaEventBus) IsConnected() bool {
	return eb.connected.Load()
}

// NodeID returns the unique identifier for this node
func (eb *KafkaEventBus) NodeID() string {
	return eb.nodeID
}

// Producer returns the underlying KafkaProducer for health checks
func (eb *KafkaEventBus) Producer() *KafkaProducer {
	return eb.producer
}

// Run starts the event bus main loop
func (eb *KafkaEventBus) Run() {
	// Start local event bus
	go eb.localBus.Run()

	// Wait for context cancellation
	<-eb.ctx.Done()

	// Wait for all goroutines to finish
	eb.wg.Wait()

	// Stop local bus
	eb.localBus.Stop()
}

// Stop gracefully stops the event bus
func (eb *KafkaEventBus) Stop() {
	eb.cancel()

	// Disconnect from Kafka
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = eb.Disconnect(ctx)

	// Wait for goroutines
	eb.wg.Wait()
}

// Publish publishes an event locally and to Kafka
func (eb *KafkaEventBus) Publish(event events.Event) bool {
	// Publish locally first
	if !eb.localBus.Publish(event) {
		return false
	}

	// Publish to Kafka if connected
	if eb.connected.Load() {
		go eb.publishToKafka(event)
	}

	return true
}

// PublishWithContext publishes an event with context
func (eb *KafkaEventBus) PublishWithContext(ctx context.Context, event events.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if !eb.Publish(event) {
		return ErrPublishFailed
	}

	return nil
}

// publishToKafka writes an event to Kafka using the producer
func (eb *KafkaEventBus) publishToKafka(event events.Event) {
	ctx, cancel := context.WithTimeout(eb.ctx, 10*time.Second)
	defer cancel()

	if err := eb.producer.WriteEvent(ctx, event); err != nil {
		eb.stats.publishErrors.Add(1)
		eb.logger.Error("failed to publish to Kafka", "error", err, "event_type", event.Type())
		return
	}

	eb.stats.publishedRemote.Add(1)
}

// consumeLoop reads messages from Kafka and delivers them to local subscribers
func (eb *KafkaEventBus) consumeLoop() {
	defer eb.wg.Done()

	for {
		msg, err := eb.reader.ReadMessage(eb.ctx)
		if err != nil {
			if eb.ctx.Err() != nil {
				return // Context cancelled, shutting down
			}
			eb.logger.Error("error reading Kafka message", "error", err)
			continue
		}

		eb.handleKafkaMessage(msg)
	}
}

// handleKafkaMessage processes a message received from Kafka
func (eb *KafkaEventBus) handleKafkaMessage(msg kafka.Message) {
	// Check node_id header for echo prevention
	for _, h := range msg.Headers {
		if h.Key == "node_id" && string(h.Value) == eb.nodeID {
			eb.stats.echoesSkipped.Add(1)
			return
		}
	}

	// Deserialize the event
	event, err := eb.serializer.Deserialize(msg.Value)
	if err != nil {
		eb.logger.Error("failed to deserialize Kafka message",
			"error", err,
			"offset", msg.Offset,
			"partition", msg.Partition,
		)
		return
	}

	eb.stats.receivedRemote.Add(1)

	// Deliver to local subscribers
	eb.localBus.Publish(event)
}

// Subscribe creates a new subscription for the given event types
func (eb *KafkaEventBus) Subscribe(
	id events.SubscriptionID,
	eventTypes []events.EventType,
	filter *events.Filter,
	channelSize int,
) *events.Subscription {
	return eb.localBus.Subscribe(id, eventTypes, filter, channelSize)
}

// SubscribeWithOptions creates a new subscription with configurable options
func (eb *KafkaEventBus) SubscribeWithOptions(
	id events.SubscriptionID,
	eventTypes []events.EventType,
	filter *events.Filter,
	opts events.SubscribeOptions,
) *events.Subscription {
	return eb.localBus.SubscribeWithOptions(id, eventTypes, filter, opts)
}

// Unsubscribe removes a subscription
func (eb *KafkaEventBus) Unsubscribe(id events.SubscriptionID) {
	eb.localBus.Unsubscribe(id)
}

// SubscriberCount returns the current number of active subscribers
func (eb *KafkaEventBus) SubscriberCount() int {
	return eb.localBus.SubscriberCount()
}

// Stats returns the current statistics
func (eb *KafkaEventBus) Stats() (uint64, uint64, uint64) {
	return eb.localBus.Stats()
}

// SetMetrics enables Prometheus metrics for the EventBus
func (eb *KafkaEventBus) SetMetrics(metrics *events.Metrics) {
	eb.localBus.SetMetrics(metrics)
}

// GetSubscriberInfo returns information about a specific subscriber
func (eb *KafkaEventBus) GetSubscriberInfo(id events.SubscriptionID) *events.SubscriberInfo {
	return eb.localBus.GetSubscriberInfo(id)
}

// GetAllSubscriberInfo returns information about all subscribers
func (eb *KafkaEventBus) GetAllSubscriberInfo() []events.SubscriberInfo {
	return eb.localBus.GetAllSubscriberInfo()
}

// Healthy returns true if the event bus is operational
func (eb *KafkaEventBus) Healthy() bool {
	if !eb.connected.Load() {
		return eb.localBus.Healthy() // Fallback to local
	}
	return eb.healthy.Load()
}

// Type returns the type of event bus implementation
func (eb *KafkaEventBus) Type() EventBusType {
	return EventBusTypeKafka
}

// GetHealthStatus returns detailed health status
func (eb *KafkaEventBus) GetHealthStatus() HealthStatus {
	status := "healthy"
	message := "Kafka event bus is operational"

	if !eb.connected.Load() {
		status = "degraded"
		message = "Not connected to Kafka, using local fallback"
	} else if !eb.healthy.Load() {
		status = "unhealthy"
		message = "Kafka connection unhealthy"
	}

	localHealth := eb.localBus.GetHealthStatus()
	totalEvents, totalDeliveries, droppedEvents := eb.localBus.Stats()

	return HealthStatus{
		Status:    status,
		Message:   message,
		LastCheck: time.Now(),
		Details: map[string]any{
			"connected":        eb.connected.Load(),
			"brokers":          eb.config.Brokers,
			"topic":            eb.config.Topic,
			"group_id":         eb.config.GroupID,
			"published_remote": eb.stats.publishedRemote.Load(),
			"received_remote":  eb.stats.receivedRemote.Load(),
			"echoes_skipped":   eb.stats.echoesSkipped.Load(),
			"publish_errors":   eb.stats.publishErrors.Load(),
			"total_events":     totalEvents,
			"total_delivered":  totalDeliveries,
			"dropped_events":   droppedEvents,
			"subscribers":      eb.localBus.SubscriberCount(),
			"local_status":     localHealth.Status,
			"uptime":           time.Since(eb.startTime).String(),
		},
	}
}
