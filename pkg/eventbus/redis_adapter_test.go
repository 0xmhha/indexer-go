package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// NewRedisEventBus Tests
// ============================================================================

func validRedisConfig() config.EventBusRedisConfig {
	return config.EventBusRedisConfig{
		Enabled:       true,
		Addresses:     []string{"localhost:6379"},
		ChannelPrefix: "indexer",
		DialTimeout:   5 * time.Second,
		ReadTimeout:   5 * time.Second,
		WriteTimeout:  5 * time.Second,
		PoolSize:      10,
		MinIdleConns:  2,
	}
}

func TestNewRedisEventBus_Valid(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")

	require.NoError(t, err)
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeRedis, eb.Type())
	assert.Equal(t, "node-1", eb.NodeID())
	assert.False(t, eb.IsConnected())
}

func TestNewRedisEventBus_NoAddresses(t *testing.T) {
	cfg := validRedisConfig()
	cfg.Addresses = nil

	eb, err := NewRedisEventBus(cfg, "node-1")

	assert.Nil(t, eb)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestNewRedisEventBus_EmptyAddresses(t *testing.T) {
	cfg := validRedisConfig()
	cfg.Addresses = []string{}

	eb, err := NewRedisEventBus(cfg, "node-1")

	assert.Nil(t, eb)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestNewRedisEventBus_WithOptions(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1",
		WithPublishBufferSize(2000),
		WithHistorySize(500),
	)

	require.NoError(t, err)
	require.NotNil(t, eb)
}

// ============================================================================
// RedisEventBus Property Tests (no connection required)
// ============================================================================

func TestRedisEventBus_Type(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	assert.Equal(t, EventBusTypeRedis, eb.Type())
}

func TestRedisEventBus_NodeID(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "test-node-42")
	require.NoError(t, err)

	assert.Equal(t, "test-node-42", eb.NodeID())
}

func TestRedisEventBus_IsConnected_Initially(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	assert.False(t, eb.IsConnected())
}

func TestRedisEventBus_Disconnect_NotConnected(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	err = eb.Disconnect(nil)
	assert.ErrorIs(t, err, ErrNotConnected)
}

// ============================================================================
// RedisEventBus Delegate Methods Tests (no connection required)
// ============================================================================

func TestRedisEventBus_Subscribe(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()

	sub := eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)
	assert.Equal(t, 1, eb.SubscriberCount())
}

func TestRedisEventBus_SubscribeWithOptions(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()

	sub := eb.SubscribeWithOptions("test-sub", []events.EventType{events.EventTypeBlock}, nil, events.SubscribeOptions{
		ChannelSize: 50,
	})
	require.NotNil(t, sub)
	assert.Equal(t, 1, eb.SubscriberCount())
}

func TestRedisEventBus_Unsubscribe(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()

	eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	assert.Equal(t, 1, eb.SubscriberCount())

	eb.Unsubscribe("test-sub")
	assert.Equal(t, 0, eb.SubscriberCount())
}

func TestRedisEventBus_Stats(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	totalEvents, totalDeliveries, droppedEvents := eb.Stats()
	assert.Equal(t, uint64(0), totalEvents)
	assert.Equal(t, uint64(0), totalDeliveries)
	assert.Equal(t, uint64(0), droppedEvents)
}

func TestRedisEventBus_SetMetrics(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	// Should not panic
	assert.NotPanics(t, func() {
		eb.SetMetrics(nil)
	})
}

func TestRedisEventBus_GetSubscriberInfo(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()

	eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)

	info := eb.GetSubscriberInfo("test-sub")
	require.NotNil(t, info)
	assert.Equal(t, events.SubscriptionID("test-sub"), info.ID)

	// Non-existent subscriber
	info = eb.GetSubscriberInfo("non-existent")
	assert.Nil(t, info)
}

func TestRedisEventBus_GetAllSubscriberInfo(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()

	eb.Subscribe("sub-1", []events.EventType{events.EventTypeBlock}, nil, 10)
	eb.Subscribe("sub-2", []events.EventType{events.EventTypeTransaction}, nil, 10)

	allInfo := eb.GetAllSubscriberInfo()
	assert.Len(t, allInfo, 2)
}

func TestRedisEventBus_Healthy_NotConnected(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()
	time.Sleep(10 * time.Millisecond)

	// Not connected - should fallback to local bus healthy state
	assert.True(t, eb.Healthy())
}

func TestRedisEventBus_GetHealthStatus_NotConnected(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	health := eb.GetHealthStatus()
	assert.Equal(t, "degraded", health.Status)
	assert.Contains(t, health.Message, "Not connected")
	assert.NotNil(t, health.Details)
	assert.Equal(t, false, health.Details["connected"])
	assert.Equal(t, cfg.Addresses, health.Details["addresses"])
	assert.NotEmpty(t, health.Details["uptime"])
}

// ============================================================================
// RedisEventBus Publish Tests (local-only, no Redis connection)
// ============================================================================

func TestRedisEventBus_Publish_LocalOnly(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()
	time.Sleep(10 * time.Millisecond)

	sub := eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)

	blockEvent := &events.BlockEvent{
		Number:    100,
		CreatedAt: time.Now(),
	}

	ok := eb.Publish(blockEvent)
	assert.True(t, ok)

	// Verify local delivery
	select {
	case evt := <-sub.Channel:
		assert.Equal(t, events.EventTypeBlock, evt.Type())
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestRedisEventBus_PublishWithContext_Cancelled(t *testing.T) {
	cfg := validRedisConfig()
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	go eb.localBus.Run()
	defer eb.localBus.Stop()

	// Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	blockEvent := &events.BlockEvent{Number: 100, CreatedAt: time.Now()}
	err = eb.PublishWithContext(ctx, blockEvent)
	assert.Error(t, err)
}

// ============================================================================
// redisMessage Tests
// ============================================================================

func TestRedisMessage_Marshal(t *testing.T) {
	msg := &redisMessage{
		NodeID: "node-1",
		Data:   []byte(`{"type":"block","data":{}}`),
	}

	data, err := msg.Marshal()
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Contains(t, string(data), "node-1")
}

func TestRedisMessage_Unmarshal(t *testing.T) {
	// First marshal
	original := &redisMessage{
		NodeID: "node-2",
		Data:   []byte("hello world"),
	}

	data, err := original.Marshal()
	require.NoError(t, err)

	// Then unmarshal
	restored := &redisMessage{}
	err = restored.Unmarshal(data)
	require.NoError(t, err)
	assert.Equal(t, "node-2", restored.NodeID)
}

func TestRedisMessage_Unmarshal_InvalidJSON(t *testing.T) {
	msg := &redisMessage{}
	err := msg.Unmarshal([]byte("not json"))
	assert.Error(t, err)
}

func TestRedisMessage_Unmarshal_EmptyData(t *testing.T) {
	msg := &redisMessage{}
	err := msg.Unmarshal([]byte(`{"node_id":"n1","data":""}`))
	assert.NoError(t, err)
	assert.Equal(t, "n1", msg.NodeID)
}

// ============================================================================
// getSubscriptionChannels Tests
// ============================================================================

func TestRedisEventBus_GetSubscriptionChannels(t *testing.T) {
	cfg := validRedisConfig()
	cfg.ChannelPrefix = "test-prefix"
	eb, err := NewRedisEventBus(cfg, "node-1")
	require.NoError(t, err)

	channels := eb.getSubscriptionChannels()
	assert.NotEmpty(t, channels)
	// All channels should start with the prefix
	for _, ch := range channels {
		assert.Contains(t, ch, "test-prefix:")
	}
}
