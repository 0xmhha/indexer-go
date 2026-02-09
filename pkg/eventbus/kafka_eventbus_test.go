package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validKafkaConfig() config.EventBusKafkaConfig {
	return config.EventBusKafkaConfig{
		Enabled: true,
		Brokers: []string{"localhost:9092"},
		Topic:   "test-events",
		GroupID: "test-group",
	}
}

func TestNewKafkaEventBus(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")

	require.NoError(t, err)
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeKafka, eb.Type())
	assert.Equal(t, "node-1", eb.NodeID())
	assert.False(t, eb.IsConnected())
}

func TestNewKafkaEventBus_NoBrokers(t *testing.T) {
	cfg := validKafkaConfig()
	cfg.Brokers = nil

	eb, err := NewKafkaEventBus(cfg, "node-1")

	assert.Nil(t, eb)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestNewKafkaEventBus_NoTopic(t *testing.T) {
	cfg := validKafkaConfig()
	cfg.Topic = ""

	eb, err := NewKafkaEventBus(cfg, "node-1")

	assert.Nil(t, eb)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestNewKafkaEventBus_NoGroupID(t *testing.T) {
	cfg := validKafkaConfig()
	cfg.GroupID = ""

	eb, err := NewKafkaEventBus(cfg, "node-1")

	assert.Nil(t, eb)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestKafkaEventBus_LocalDelivery(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Start local bus in background
	go eb.localBus.Run()
	defer eb.localBus.Stop()

	// Subscribe
	sub := eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)

	// Publish (not connected, so only local delivery)
	blockEvent := &events.BlockEvent{
		Number:    100,
		Hash:      common.HexToHash("0x1234"),
		CreatedAt: time.Now(),
		TxCount:   5,
	}
	ok := eb.Publish(blockEvent)
	assert.True(t, ok)

	// Verify local delivery
	select {
	case evt := <-sub.Channel:
		assert.Equal(t, events.EventTypeBlock, evt.Type())
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for local event delivery")
	}
}

func TestKafkaEventBus_EchoPrevention(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Start local bus in background
	go eb.localBus.Run()
	defer eb.localBus.Stop()

	// Subscribe
	sub := eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)

	// Simulate a Kafka message from the same node (echo)
	blockEvent := &events.BlockEvent{
		Number:    200,
		Hash:      common.HexToHash("0x2345"),
		CreatedAt: time.Now(),
		TxCount:   3,
	}
	data, err := eb.serializer.Serialize(blockEvent)
	require.NoError(t, err)

	msg := kafka.Message{
		Value: data,
		Headers: []kafka.Header{
			{Key: "node_id", Value: []byte("node-1")}, // Same node
			{Key: "event_type", Value: []byte(string(events.EventTypeBlock))},
		},
	}

	eb.handleKafkaMessage(msg)

	// Should NOT be delivered (echo prevention)
	select {
	case <-sub.Channel:
		t.Fatal("should not have received echo message")
	case <-time.After(100 * time.Millisecond):
		// Expected: no delivery
	}

	assert.Equal(t, uint64(1), eb.stats.echoesSkipped.Load())
	assert.Equal(t, uint64(0), eb.stats.receivedRemote.Load())
}

func TestKafkaEventBus_RemoteDelivery(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Start local bus in background
	go eb.localBus.Run()
	defer eb.localBus.Stop()

	// Subscribe
	sub := eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)

	// Simulate a Kafka message from a different node
	blockEvent := &events.BlockEvent{
		Number:    300,
		Hash:      common.HexToHash("0x3456"),
		CreatedAt: time.Now(),
		TxCount:   2,
	}
	data, err := eb.serializer.Serialize(blockEvent)
	require.NoError(t, err)

	msg := kafka.Message{
		Value: data,
		Headers: []kafka.Header{
			{Key: "node_id", Value: []byte("node-2")}, // Different node
			{Key: "event_type", Value: []byte(string(events.EventTypeBlock))},
		},
	}

	eb.handleKafkaMessage(msg)

	// Should be delivered
	select {
	case evt := <-sub.Channel:
		assert.Equal(t, events.EventTypeBlock, evt.Type())
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remote event delivery")
	}

	assert.Equal(t, uint64(0), eb.stats.echoesSkipped.Load())
	assert.Equal(t, uint64(1), eb.stats.receivedRemote.Load())
}

func TestKafkaEventBus_BadData(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Simulate a Kafka message with bad data (should not panic)
	msg := kafka.Message{
		Value: []byte("this is not valid json"),
		Headers: []kafka.Header{
			{Key: "node_id", Value: []byte("node-2")},
		},
	}

	// Should not panic
	assert.NotPanics(t, func() {
		eb.handleKafkaMessage(msg)
	})

	assert.Equal(t, uint64(0), eb.stats.receivedRemote.Load())
}

func TestKafkaEventBus_DelegatesMethods(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Start local bus in background
	go eb.localBus.Run()
	defer eb.localBus.Stop()

	// Subscribe
	sub := eb.Subscribe("delegate-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)
	assert.Equal(t, 1, eb.SubscriberCount())

	// SubscribeWithOptions
	sub2 := eb.SubscribeWithOptions("delegate-sub-2", []events.EventType{events.EventTypeTransaction}, nil, events.SubscribeOptions{
		ChannelSize: 5,
	})
	require.NotNil(t, sub2)
	assert.Equal(t, 2, eb.SubscriberCount())

	// GetSubscriberInfo
	info := eb.GetSubscriberInfo("delegate-sub")
	require.NotNil(t, info)

	// GetAllSubscriberInfo
	allInfo := eb.GetAllSubscriberInfo()
	assert.Len(t, allInfo, 2)

	// Stats
	totalEvents, totalDeliveries, droppedEvents := eb.Stats()
	assert.Equal(t, uint64(0), totalEvents)
	assert.Equal(t, uint64(0), totalDeliveries)
	assert.Equal(t, uint64(0), droppedEvents)

	// Unsubscribe
	eb.Unsubscribe("delegate-sub")
	assert.Equal(t, 1, eb.SubscriberCount())

	eb.Unsubscribe("delegate-sub-2")
	assert.Equal(t, 0, eb.SubscriberCount())
}

func TestKafkaEventBus_HealthStatus(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Not connected - should be degraded
	health := eb.GetHealthStatus()
	assert.Equal(t, "degraded", health.Status)
	assert.Contains(t, health.Message, "Not connected")
	assert.NotNil(t, health.Details)
	assert.Equal(t, false, health.Details["connected"])
	assert.Equal(t, cfg.Brokers, health.Details["brokers"])
	assert.Equal(t, cfg.Topic, health.Details["topic"])
	assert.Equal(t, cfg.GroupID, health.Details["group_id"])
	assert.NotEmpty(t, health.Details["uptime"])

	// Healthy() without connection should fallback to local
	assert.True(t, eb.Healthy())
}

func TestKafkaEventBus_PublishWithContext_Cancelled(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Start local bus in background
	go eb.localBus.Run()
	defer eb.localBus.Stop()

	// Use an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	blockEvent := &events.BlockEvent{
		Number:    400,
		Hash:      common.HexToHash("0x4567"),
		CreatedAt: time.Now(),
		TxCount:   1,
	}
	err = eb.PublishWithContext(ctx, blockEvent)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestKafkaEventBus_ProducerAccessor(t *testing.T) {
	cfg := validKafkaConfig()
	eb, err := NewKafkaEventBus(cfg, "node-1")
	require.NoError(t, err)

	producer := eb.Producer()
	require.NotNil(t, producer)
}
