package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// NewKafkaProducer Tests
// ============================================================================

func TestNewKafkaProducer_Valid(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test-events",
	}

	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)
	require.NotNil(t, kp)
	assert.False(t, kp.IsConnected())
}

func TestNewKafkaProducer_NoBrokers(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: nil,
		Topic:   "test-events",
	}

	kp, err := NewKafkaProducer(cfg, "node-1")
	assert.Nil(t, kp)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestNewKafkaProducer_EmptyBrokers(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{},
		Topic:   "test-events",
	}

	kp, err := NewKafkaProducer(cfg, "node-1")
	assert.Nil(t, kp)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestNewKafkaProducer_NoTopic(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "",
	}

	kp, err := NewKafkaProducer(cfg, "node-1")
	assert.Nil(t, kp)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

// ============================================================================
// KafkaProducer Property Tests
// ============================================================================

func TestKafkaProducer_IsConnected_Initially(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	assert.False(t, kp.IsConnected())
}

func TestKafkaProducer_Disconnect_NotConnected(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	err = kp.Disconnect(context.Background())
	assert.ErrorIs(t, err, ErrNotConnected)
}

func TestKafkaProducer_Stats(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	stats := kp.Stats()
	assert.Equal(t, uint64(0), stats.MessagesWritten)
	assert.Equal(t, uint64(0), stats.BytesWritten)
	assert.Equal(t, uint64(0), stats.Errors)
	assert.False(t, stats.Connected)
	assert.True(t, stats.Uptime > 0)
}

func TestKafkaProducer_GetHealthStatus_NotConnected(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	health := kp.GetHealthStatus()
	assert.Equal(t, "unhealthy", health.Status)
	assert.Contains(t, health.Message, "Not connected")
	assert.NotNil(t, health.Details)
	assert.Equal(t, false, health.Details["connected"])
	assert.Equal(t, cfg.Brokers, health.Details["brokers"])
	assert.Equal(t, cfg.Topic, health.Details["topic"])
}

func TestKafkaProducer_WriteEvent_NotConnected(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	blockEvent := &events.BlockEvent{
		Number:    100,
		CreatedAt: time.Now(),
	}

	err = kp.WriteEvent(context.Background(), blockEvent)
	assert.ErrorIs(t, err, ErrNotConnected)
}

// ============================================================================
// getPartitionKey Tests
// ============================================================================

func TestGetPartitionKey_BlockEvent(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	event := &events.BlockEvent{Number: 12345}
	key := kp.getPartitionKey(event)
	assert.Equal(t, "block:12345", key)
}

func TestGetPartitionKey_TransactionEvent(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	txHash := common.HexToHash("0xabc")
	event := &events.TransactionEvent{Hash: txHash}
	key := kp.getPartitionKey(event)
	assert.Equal(t, txHash.Hex(), key)
}

func TestGetPartitionKey_SystemContractEvent(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	contract := common.HexToAddress("0x1234")
	event := &events.SystemContractEvent{Contract: contract}
	key := kp.getPartitionKey(event)
	assert.Contains(t, key, "syscontract:")
}

func TestGetPartitionKey_LogEvent_WithLog(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	event := &events.LogEvent{
		Log: &types.Log{
			Address: common.HexToAddress("0xabc"),
			Index:   5,
		},
	}
	key := kp.getPartitionKey(event)
	assert.Contains(t, key, "log:")
}

func TestGetPartitionKey_LogEvent_NilLog(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	event := &events.LogEvent{Log: nil}
	key := kp.getPartitionKey(event)
	assert.Equal(t, string(events.EventTypeLog), key)
}

func TestGetPartitionKey_ChainConfigEvent(t *testing.T) {
	cfg := config.EventBusKafkaConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}
	kp, err := NewKafkaProducer(cfg, "node-1")
	require.NoError(t, err)

	event := &events.ChainConfigEvent{Parameter: "gasLimit"}
	key := kp.getPartitionKey(event)
	// Falls through to default: event type string
	assert.Equal(t, string(events.EventTypeChainConfig), key)
}
