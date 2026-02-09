package eventbus

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/redis/go-redis/v9"
)

// RedisEventBus implements EventBus using Redis Pub/Sub for distributed event broadcasting
type RedisEventBus struct {
	// localBus handles local subscriptions and event delivery
	localBus *LocalEventBus

	// client is the Redis client (either standalone or cluster)
	client redis.UniversalClient

	// config holds the Redis configuration
	config config.EventBusRedisConfig

	// serializer handles event serialization/deserialization
	serializer EventSerializer

	// nodeID is the unique identifier for this node
	nodeID string

	// channelPrefix is the prefix for Redis Pub/Sub channels
	channelPrefix string

	// ctx is the context for the event bus
	ctx context.Context

	// cancel is the cancel function for the event bus
	cancel context.CancelFunc

	// wg tracks background goroutines
	wg sync.WaitGroup

	// connected indicates whether we're connected to Redis
	connected atomic.Bool

	// healthy indicates overall health status
	healthy atomic.Bool

	// stats tracks statistics
	stats struct {
		publishedRemote atomic.Uint64
		receivedRemote  atomic.Uint64
		publishErrors   atomic.Uint64
	}

	// startTime is when the event bus was created
	startTime time.Time

	// logger is the structured logger
	logger *slog.Logger
}

// Ensure RedisEventBus implements DistributedEventBus interface
var _ DistributedEventBus = (*RedisEventBus)(nil)

// NewRedisEventBus creates a new Redis EventBus
func NewRedisEventBus(cfg config.EventBusRedisConfig, nodeID string, opts ...Option) (*RedisEventBus, error) {
	if len(cfg.Addresses) == 0 {
		return nil, fmt.Errorf("%w: no Redis addresses configured", ErrInvalidConfiguration)
	}

	ctx, cancel := context.WithCancel(context.Background())

	eb := &RedisEventBus{
		localBus:      NewLocalEventBusWithConfig(constants.DefaultPublishBufferSize, constants.DefaultSubscribeBufferSize),
		config:        cfg,
		serializer:    NewJSONSerializer(),
		nodeID:        nodeID,
		channelPrefix: cfg.ChannelPrefix,
		ctx:           ctx,
		cancel:        cancel,
		startTime:     time.Now(),
		logger:        slog.Default().With("component", "redis-eventbus", "node_id", nodeID),
	}

	// Apply options
	for _, opt := range opts {
		opt(eb)
	}

	// Create Redis client
	if err := eb.createClient(); err != nil {
		cancel()
		return nil, err
	}

	return eb, nil
}

// createClient creates the appropriate Redis client based on configuration
func (eb *RedisEventBus) createClient() error {
	var tlsConfig *tls.Config
	if eb.config.TLS.Enabled {
		var err error
		tlsConfig, err = eb.buildTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
	}

	if eb.config.ClusterMode {
		eb.client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        eb.config.Addresses,
			Password:     eb.config.Password,
			PoolSize:     eb.config.PoolSize,
			MinIdleConns: eb.config.MinIdleConns,
			DialTimeout:  eb.config.DialTimeout,
			ReadTimeout:  eb.config.ReadTimeout,
			WriteTimeout: eb.config.WriteTimeout,
			TLSConfig:    tlsConfig,
		})
	} else {
		// Standalone mode - use first address
		eb.client = redis.NewClient(&redis.Options{
			Addr:         eb.config.Addresses[0],
			Password:     eb.config.Password,
			DB:           eb.config.DB,
			PoolSize:     eb.config.PoolSize,
			MinIdleConns: eb.config.MinIdleConns,
			DialTimeout:  eb.config.DialTimeout,
			ReadTimeout:  eb.config.ReadTimeout,
			WriteTimeout: eb.config.WriteTimeout,
			TLSConfig:    tlsConfig,
		})
	}

	return nil
}

// buildTLSConfig creates a TLS configuration from the config settings
func (eb *RedisEventBus) buildTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: eb.config.TLS.InsecureSkipVerify,
		ServerName:         eb.config.TLS.ServerName,
	}

	// Load CA certificate if specified
	if eb.config.TLS.CAFile != "" {
		caCert, err := os.ReadFile(eb.config.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file %s: %w", eb.config.TLS.CAFile, err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", eb.config.TLS.CAFile)
		}
		tlsConfig.RootCAs = caCertPool

		eb.logger.Info("loaded CA certificate", "file", eb.config.TLS.CAFile)
	}

	// Load client certificate and key if both are specified
	if eb.config.TLS.CertFile != "" && eb.config.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(eb.config.TLS.CertFile, eb.config.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate/key pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}

		eb.logger.Info("loaded client certificate",
			"cert_file", eb.config.TLS.CertFile,
			"key_file", eb.config.TLS.KeyFile,
		)
	} else if eb.config.TLS.CertFile != "" || eb.config.TLS.KeyFile != "" {
		// Warn if only one of cert/key is provided
		eb.logger.Warn("both cert_file and key_file must be specified for client certificate authentication",
			"cert_file", eb.config.TLS.CertFile,
			"key_file", eb.config.TLS.KeyFile,
		)
	}

	return tlsConfig, nil
}

// Connect establishes connection to Redis
func (eb *RedisEventBus) Connect(ctx context.Context) error {
	if eb.connected.Load() {
		return ErrAlreadyConnected
	}

	// Test connection
	if err := eb.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	eb.connected.Store(true)
	eb.healthy.Store(true)

	// Start subscriber goroutine
	eb.wg.Add(1)
	go eb.subscribeLoop()

	eb.logger.Info("connected to Redis",
		"addresses", eb.config.Addresses,
		"cluster_mode", eb.config.ClusterMode,
	)

	return nil
}

// Disconnect closes the connection to Redis
func (eb *RedisEventBus) Disconnect(ctx context.Context) error {
	if !eb.connected.Load() {
		return ErrNotConnected
	}

	eb.connected.Store(false)
	eb.healthy.Store(false)

	// Close Redis client
	if err := eb.client.Close(); err != nil {
		eb.logger.Error("error closing Redis client", "error", err)
	}

	eb.logger.Info("disconnected from Redis")
	return nil
}

// IsConnected returns true if connected to Redis
func (eb *RedisEventBus) IsConnected() bool {
	return eb.connected.Load()
}

// NodeID returns the unique identifier for this node
func (eb *RedisEventBus) NodeID() string {
	return eb.nodeID
}

// Run starts the event bus main loop
func (eb *RedisEventBus) Run() {
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
func (eb *RedisEventBus) Stop() {
	eb.cancel()

	// Disconnect from Redis
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = eb.Disconnect(ctx)

	// Wait for goroutines
	eb.wg.Wait()
}

// Publish publishes an event locally and to Redis
func (eb *RedisEventBus) Publish(event events.Event) bool {
	// Publish locally first
	if !eb.localBus.Publish(event) {
		return false
	}

	// Publish to Redis if connected
	if eb.connected.Load() {
		go eb.publishToRedis(event)
	}

	return true
}

// PublishWithContext publishes an event with context
func (eb *RedisEventBus) PublishWithContext(ctx context.Context, event events.Event) error {
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

// publishToRedis publishes an event to Redis Pub/Sub
func (eb *RedisEventBus) publishToRedis(event events.Event) {
	data, err := eb.serializer.Serialize(event)
	if err != nil {
		eb.stats.publishErrors.Add(1)
		eb.logger.Error("failed to serialize event", "error", err, "event_type", event.Type())
		return
	}

	// Construct channel name: prefix:eventType
	channel := fmt.Sprintf("%s:%s", eb.channelPrefix, event.Type())

	// Create message envelope with node ID to prevent echo
	envelope := redisMessage{
		NodeID: eb.nodeID,
		Data:   data,
	}

	envData, err := envelope.Marshal()
	if err != nil {
		eb.stats.publishErrors.Add(1)
		eb.logger.Error("failed to marshal envelope", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(eb.ctx, eb.config.WriteTimeout)
	defer cancel()

	if err := eb.client.Publish(ctx, channel, envData).Err(); err != nil {
		eb.stats.publishErrors.Add(1)
		eb.logger.Error("failed to publish to Redis", "error", err, "channel", channel)
		return
	}

	eb.stats.publishedRemote.Add(1)
}

// subscribeLoop handles Redis Pub/Sub subscriptions
func (eb *RedisEventBus) subscribeLoop() {
	defer eb.wg.Done()

	// Subscribe to all event type channels
	channels := eb.getSubscriptionChannels()
	pubsub := eb.client.Subscribe(eb.ctx, channels...)
	defer pubsub.Close()

	eb.logger.Info("subscribed to Redis channels", "channels", channels)

	for {
		select {
		case <-eb.ctx.Done():
			return
		default:
			msg, err := pubsub.ReceiveMessage(eb.ctx)
			if err != nil {
				if eb.ctx.Err() != nil {
					return
				}
				eb.logger.Error("error receiving message", "error", err)
				continue
			}

			eb.handleRedisMessage(msg)
		}
	}
}

// getSubscriptionChannels returns the list of channels to subscribe to
func (eb *RedisEventBus) getSubscriptionChannels() []string {
	eventTypes := []events.EventType{
		events.EventTypeBlock,
		events.EventTypeTransaction,
		events.EventTypeLog,
		events.EventTypeChainConfig,
		events.EventTypeValidatorSet,
		events.EventTypeSystemContract,
	}

	channels := make([]string, len(eventTypes))
	for i, et := range eventTypes {
		channels[i] = fmt.Sprintf("%s:%s", eb.channelPrefix, et)
	}

	return channels
}

// handleRedisMessage processes a message received from Redis
func (eb *RedisEventBus) handleRedisMessage(msg *redis.Message) {
	var envelope redisMessage
	if err := envelope.Unmarshal([]byte(msg.Payload)); err != nil {
		eb.logger.Error("failed to unmarshal envelope", "error", err)
		return
	}

	// Skip messages from this node (prevent echo)
	if envelope.NodeID == eb.nodeID {
		return
	}

	event, err := eb.serializer.Deserialize(envelope.Data)
	if err != nil {
		eb.logger.Error("failed to deserialize event", "error", err)
		return
	}

	eb.stats.receivedRemote.Add(1)

	// Deliver to local subscribers
	eb.localBus.Publish(event)
}

// Subscribe creates a new subscription for the given event types
func (eb *RedisEventBus) Subscribe(
	id events.SubscriptionID,
	eventTypes []events.EventType,
	filter *events.Filter,
	channelSize int,
) *events.Subscription {
	return eb.localBus.Subscribe(id, eventTypes, filter, channelSize)
}

// SubscribeWithOptions creates a new subscription with configurable options
func (eb *RedisEventBus) SubscribeWithOptions(
	id events.SubscriptionID,
	eventTypes []events.EventType,
	filter *events.Filter,
	opts events.SubscribeOptions,
) *events.Subscription {
	return eb.localBus.SubscribeWithOptions(id, eventTypes, filter, opts)
}

// Unsubscribe removes a subscription
func (eb *RedisEventBus) Unsubscribe(id events.SubscriptionID) {
	eb.localBus.Unsubscribe(id)
}

// SubscriberCount returns the current number of active subscribers
func (eb *RedisEventBus) SubscriberCount() int {
	return eb.localBus.SubscriberCount()
}

// Stats returns the current statistics
func (eb *RedisEventBus) Stats() (uint64, uint64, uint64) {
	return eb.localBus.Stats()
}

// SetMetrics enables Prometheus metrics for the EventBus
func (eb *RedisEventBus) SetMetrics(metrics *events.Metrics) {
	eb.localBus.SetMetrics(metrics)
}

// GetSubscriberInfo returns information about a specific subscriber
func (eb *RedisEventBus) GetSubscriberInfo(id events.SubscriptionID) *events.SubscriberInfo {
	return eb.localBus.GetSubscriberInfo(id)
}

// GetAllSubscriberInfo returns information about all subscribers
func (eb *RedisEventBus) GetAllSubscriberInfo() []events.SubscriberInfo {
	return eb.localBus.GetAllSubscriberInfo()
}

// Healthy returns true if the event bus is operational
func (eb *RedisEventBus) Healthy() bool {
	if !eb.connected.Load() {
		return eb.localBus.Healthy() // Fallback to local
	}

	// Check Redis connection
	ctx, cancel := context.WithTimeout(eb.ctx, time.Second)
	defer cancel()

	if err := eb.client.Ping(ctx).Err(); err != nil {
		eb.healthy.Store(false)
		return false
	}

	eb.healthy.Store(true)
	return true
}

// Type returns the type of event bus implementation
func (eb *RedisEventBus) Type() EventBusType {
	return EventBusTypeRedis
}

// GetHealthStatus returns detailed health status
func (eb *RedisEventBus) GetHealthStatus() HealthStatus {
	status := "healthy"
	message := "Redis event bus is operational"

	if !eb.connected.Load() {
		status = "degraded"
		message = "Not connected to Redis, using local fallback"
	} else if !eb.healthy.Load() {
		status = "unhealthy"
		message = "Redis connection unhealthy"
	}

	localHealth := eb.localBus.GetHealthStatus()
	totalEvents, totalDeliveries, droppedEvents := eb.localBus.Stats()

	return HealthStatus{
		Status:    status,
		Message:   message,
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"connected":        eb.connected.Load(),
			"cluster_mode":     eb.config.ClusterMode,
			"addresses":        eb.config.Addresses,
			"published_remote": eb.stats.publishedRemote.Load(),
			"received_remote":  eb.stats.receivedRemote.Load(),
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

// redisMessage wraps event data with metadata for Redis transmission
type redisMessage struct {
	NodeID string `json:"node_id"`
	Data   []byte `json:"data"`
}

// Marshal serializes the message to JSON
func (m *redisMessage) Marshal() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"node_id":"%s","data":%q}`, m.NodeID, string(m.Data))), nil
}

// Unmarshal deserializes the message from JSON
func (m *redisMessage) Unmarshal(data []byte) error {
	// Simple parsing without full JSON unmarshal for performance
	var msg struct {
		NodeID string `json:"node_id"`
		Data   string `json:"data"`
	}

	// Use standard JSON for correctness
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	m.NodeID = msg.NodeID
	m.Data = []byte(msg.Data)
	return nil
}
