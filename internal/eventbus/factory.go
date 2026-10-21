package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
)

// Factory creates EventBus instances based on configuration
type Factory struct {
	config *config.Config
	logger *slog.Logger
}

// NewFactory creates a new EventBus factory
func NewFactory(cfg *config.Config) *Factory {
	return &Factory{
		config: cfg,
		logger: slog.Default().With("component", "eventbus-factory"),
	}
}

// Create creates an EventBus based on the configuration
func (f *Factory) Create() (EventBus, error) {
	return f.CreateWithContext(context.Background())
}

// CreateWithContext creates an EventBus with the given context
func (f *Factory) CreateWithContext(ctx context.Context) (EventBus, error) {
	switch f.config.EventBus.Type {
	case "local", "":
		return f.createLocal()
	case "redis":
		return f.createRedis(ctx)
	case "kafka":
		return f.createKafka(ctx)
	case "hybrid":
		return f.createHybrid(ctx)
	default:
		return nil, fmt.Errorf("%w: unknown event bus type %q", ErrInvalidConfiguration, f.config.EventBus.Type)
	}
}

// createLocal creates a local in-process EventBus
func (f *Factory) createLocal() (EventBus, error) {
	f.logger.Info("creating local event bus",
		"publish_buffer_size", f.config.EventBus.PublishBufferSize,
		"history_size", f.config.EventBus.HistorySize,
	)

	return NewLocalEventBusWithConfig(
		f.config.EventBus.PublishBufferSize,
		f.config.EventBus.HistorySize,
	), nil
}

// createRedis creates a Redis Pub/Sub EventBus
func (f *Factory) createRedis(ctx context.Context) (EventBus, error) {
	if !f.config.EventBus.Redis.Enabled {
		f.logger.Warn("redis event bus requested but not enabled, falling back to local")
		return f.createLocal()
	}

	f.logger.Info("creating Redis event bus",
		"addresses", f.config.EventBus.Redis.Addresses,
		"cluster_mode", f.config.EventBus.Redis.ClusterMode,
		"channel_prefix", f.config.EventBus.Redis.ChannelPrefix,
	)

	eb, err := NewRedisEventBus(f.config.EventBus.Redis, f.config.Node.ID)
	if err != nil {
		f.logger.Error("failed to create Redis event bus", "error", err)
		return nil, err
	}

	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := eb.Connect(connectCtx); err != nil {
		f.logger.Error("failed to connect to Redis", "error", err)
		// Return with fallback to local-only mode
		f.logger.Warn("Redis connection failed, Redis features disabled")
		return eb, nil // Return anyway, it will operate in degraded mode
	}

	return eb, nil
}

// createKafka creates a Kafka EventBus
func (f *Factory) createKafka(ctx context.Context) (EventBus, error) {
	if !f.config.EventBus.Kafka.Enabled {
		f.logger.Warn("kafka event bus requested but not enabled, falling back to local")
		return f.createLocal()
	}

	f.logger.Info("creating Kafka event bus",
		"brokers", f.config.EventBus.Kafka.Brokers,
		"topic", f.config.EventBus.Kafka.Topic,
		"group_id", f.config.EventBus.Kafka.GroupID,
	)

	eb, err := NewKafkaEventBus(f.config.EventBus.Kafka, f.config.Node.ID)
	if err != nil {
		f.logger.Error("failed to create Kafka event bus", "error", err)
		return nil, err
	}

	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := eb.Connect(connectCtx); err != nil {
		f.logger.Error("failed to connect to Kafka", "error", err)
		// Return with fallback to local-only mode
		f.logger.Warn("Kafka connection failed, Kafka features disabled")
		return eb, nil // Return anyway, it will operate in degraded mode
	}

	return eb, nil
}

// createHybrid creates a hybrid EventBus (local + Redis + optional Kafka)
func (f *Factory) createHybrid(ctx context.Context) (EventBus, error) {
	f.logger.Info("creating hybrid event bus")

	// For hybrid mode, prefer Redis if enabled, then Kafka
	if f.config.EventBus.Redis.Enabled {
		return f.createRedis(ctx)
	}
	if f.config.EventBus.Kafka.Enabled {
		return f.createKafka(ctx)
	}

	// Fall back to local
	return f.createLocal()
}

// NewEventBus is a convenience function that creates an EventBus based on configuration
func NewEventBus(cfg *config.Config) (EventBus, error) {
	return NewFactory(cfg).Create()
}

// NewEventBusWithContext creates an EventBus with the given context
func NewEventBusWithContext(ctx context.Context, cfg *config.Config) (EventBus, error) {
	return NewFactory(cfg).CreateWithContext(ctx)
}

// CreateLocalEventBus is a convenience function for creating a local event bus
func CreateLocalEventBus(publishBufferSize, historySize int) *LocalEventBus {
	return NewLocalEventBusWithConfig(publishBufferSize, historySize)
}

// CreateDefaultLocalEventBus creates a local event bus with default settings
func CreateDefaultLocalEventBus() *LocalEventBus {
	return NewLocalEventBus()
}
