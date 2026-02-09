package eventbus

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/compress"
	"github.com/segmentio/kafka-go/sasl"
)

// KafkaProducer handles event streaming to Kafka
type KafkaProducer struct {
	// writer is the Kafka writer
	writer *kafka.Writer

	// config holds the Kafka configuration
	config config.EventBusKafkaConfig

	// serializer handles event serialization
	serializer EventSerializer

	// nodeID is the unique identifier for this node
	nodeID string

	// ctx is the context for the producer
	ctx context.Context

	// cancel is the cancel function
	cancel context.CancelFunc

	// wg tracks background goroutines
	wg sync.WaitGroup

	// connected indicates whether we're connected
	connected atomic.Bool

	// stats tracks statistics
	stats struct {
		messagesWritten atomic.Uint64
		bytesWritten    atomic.Uint64
		errors          atomic.Uint64
	}

	// startTime is when the producer was created
	startTime time.Time

	// logger is the structured logger
	logger *slog.Logger
}

// NewKafkaProducer creates a new Kafka producer
func NewKafkaProducer(cfg config.EventBusKafkaConfig, nodeID string) (*KafkaProducer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("%w: no Kafka brokers configured", ErrInvalidConfiguration)
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("%w: no Kafka topic configured", ErrInvalidConfiguration)
	}

	ctx, cancel := context.WithCancel(context.Background())

	kp := &KafkaProducer{
		config:     cfg,
		serializer: NewJSONSerializer(),
		nodeID:     nodeID,
		ctx:        ctx,
		cancel:     cancel,
		startTime:  time.Now(),
		logger:     slog.Default().With("component", "kafka-producer", "node_id", nodeID),
	}

	return kp, nil
}

// Connect establishes connection to Kafka
func (kp *KafkaProducer) Connect(ctx context.Context) error {
	if kp.connected.Load() {
		return ErrAlreadyConnected
	}

	// Configure compression
	var compression compress.Codec
	switch kp.config.Compression {
	case "gzip":
		compression = &compress.GzipCodec
	case "snappy":
		compression = &compress.SnappyCodec
	case "lz4":
		compression = &compress.Lz4Codec
	case "zstd":
		compression = &compress.ZstdCodec
	default:
		compression = nil
	}

	// Configure TLS if enabled
	var tlsConfig *tls.Config
	if kp.config.TLS.Enabled {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: kp.config.TLS.InsecureSkipVerify,
			ServerName:         kp.config.TLS.ServerName,
		}
	}

	// Create transport with SASL if configured
	var transport *kafka.Transport
	if kp.config.SASLUsername != "" && kp.config.SASLPassword != "" {
		mechanism, err := kp.createSASLMechanism()
		if err != nil {
			return fmt.Errorf("failed to create SASL mechanism: %w", err)
		}
		transport = &kafka.Transport{
			SASL: mechanism,
			TLS:  tlsConfig,
		}
	} else if tlsConfig != nil {
		transport = &kafka.Transport{
			TLS: tlsConfig,
		}
	}

	// Create Kafka writer
	writerConfig := kafka.WriterConfig{
		Brokers:      kp.config.Brokers,
		Topic:        kp.config.Topic,
		BatchSize:    kp.config.BatchSize,
		BatchTimeout: time.Duration(kp.config.LingerMs) * time.Millisecond,
		Async:        true, // Async for better performance
	}

	if compression != nil {
		writerConfig.CompressionCodec = compression
	}

	kp.writer = kafka.NewWriter(writerConfig)
	if transport != nil {
		kp.writer.Transport = transport
	}

	// Set required acks
	switch kp.config.RequiredAcks {
	case 0:
		kp.writer.RequiredAcks = kafka.RequireNone
	case 1:
		kp.writer.RequiredAcks = kafka.RequireOne
	default:
		kp.writer.RequiredAcks = kafka.RequireAll
	}

	kp.connected.Store(true)

	kp.logger.Info("connected to Kafka",
		"brokers", kp.config.Brokers,
		"topic", kp.config.Topic,
		"compression", kp.config.Compression,
	)

	return nil
}

// createSASLMechanism creates the appropriate SASL mechanism
func (kp *KafkaProducer) createSASLMechanism() (sasl.Mechanism, error) {
	return createKafkaSASLMechanism(kp.config)
}

// Disconnect closes the connection to Kafka
func (kp *KafkaProducer) Disconnect(ctx context.Context) error {
	if !kp.connected.Load() {
		return ErrNotConnected
	}

	kp.connected.Store(false)

	if kp.writer != nil {
		if err := kp.writer.Close(); err != nil {
			kp.logger.Error("error closing Kafka writer", "error", err)
			return err
		}
	}

	kp.logger.Info("disconnected from Kafka")
	return nil
}

// IsConnected returns true if connected to Kafka
func (kp *KafkaProducer) IsConnected() bool {
	return kp.connected.Load()
}

// WriteEvent writes an event to Kafka
func (kp *KafkaProducer) WriteEvent(ctx context.Context, event events.Event) error {
	if !kp.connected.Load() {
		return ErrNotConnected
	}

	data, err := kp.serializer.Serialize(event)
	if err != nil {
		kp.stats.errors.Add(1)
		return fmt.Errorf("%w: %v", ErrSerializationFailed, err)
	}

	// Create Kafka message
	msg := kafka.Message{
		Key:   []byte(kp.getPartitionKey(event)),
		Value: data,
		Headers: []kafka.Header{
			{Key: "event_type", Value: []byte(event.Type())},
			{Key: "node_id", Value: []byte(kp.nodeID)},
			{Key: "timestamp", Value: []byte(event.Timestamp().Format(time.RFC3339Nano))},
		},
	}

	if err := kp.writer.WriteMessages(ctx, msg); err != nil {
		kp.stats.errors.Add(1)
		return fmt.Errorf("failed to write to Kafka: %w", err)
	}

	kp.stats.messagesWritten.Add(1)
	kp.stats.bytesWritten.Add(uint64(len(data)))

	return nil
}

// WriteEventAsync writes an event to Kafka asynchronously
func (kp *KafkaProducer) WriteEventAsync(event events.Event) {
	kp.wg.Add(1)
	go func() {
		defer kp.wg.Done()

		ctx, cancel := context.WithTimeout(kp.ctx, 10*time.Second)
		defer cancel()

		if err := kp.WriteEvent(ctx, event); err != nil {
			kp.logger.Error("async write failed", "error", err, "event_type", event.Type())
		}
	}()
}

// getPartitionKey returns a key for partitioning based on event type
func (kp *KafkaProducer) getPartitionKey(event events.Event) string {
	switch e := event.(type) {
	case *events.BlockEvent:
		return fmt.Sprintf("block:%d", e.Number)
	case *events.TransactionEvent:
		return e.Hash.Hex()
	case *events.LogEvent:
		if e.Log != nil {
			return fmt.Sprintf("log:%s:%d", e.Log.Address.Hex(), e.Log.Index)
		}
	case *events.SystemContractEvent:
		return fmt.Sprintf("syscontract:%s", e.Contract.Hex())
	}
	return string(event.Type())
}

// Stop gracefully stops the producer
func (kp *KafkaProducer) Stop() {
	kp.cancel()

	// Wait for pending writes
	kp.wg.Wait()

	// Disconnect
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = kp.Disconnect(ctx)
}

// Stats returns producer statistics
func (kp *KafkaProducer) Stats() KafkaProducerStats {
	return KafkaProducerStats{
		MessagesWritten: kp.stats.messagesWritten.Load(),
		BytesWritten:    kp.stats.bytesWritten.Load(),
		Errors:          kp.stats.errors.Load(),
		Connected:       kp.connected.Load(),
		Uptime:          time.Since(kp.startTime),
	}
}

// KafkaProducerStats contains producer statistics
type KafkaProducerStats struct {
	MessagesWritten uint64        `json:"messages_written"`
	BytesWritten    uint64        `json:"bytes_written"`
	Errors          uint64        `json:"errors"`
	Connected       bool          `json:"connected"`
	Uptime          time.Duration `json:"uptime"`
}

// GetHealthStatus returns the health status of the producer
func (kp *KafkaProducer) GetHealthStatus() HealthStatus {
	status := "healthy"
	message := "Kafka producer is operational"

	if !kp.connected.Load() {
		status = "unhealthy"
		message = "Not connected to Kafka"
	}

	stats := kp.Stats()

	return HealthStatus{
		Status:    status,
		Message:   message,
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"connected":        stats.Connected,
			"brokers":          kp.config.Brokers,
			"topic":            kp.config.Topic,
			"messages_written": stats.MessagesWritten,
			"bytes_written":    stats.BytesWritten,
			"errors":           stats.Errors,
			"uptime":           stats.Uptime.String(),
		},
	}
}
