package eventbus

import (
	"testing"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_CreateLocal(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type:              "local",
			PublishBufferSize: 500,
			HistorySize:       50,
		},
		Node: config.NodeConfig{
			ID:   "test-node",
			Role: "all",
		},
	}

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	require.NoError(t, err)
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestFactory_CreateLocal_Default(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "", // Empty should default to local
		},
	}
	cfg.SetDefaults()

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	require.NoError(t, err)
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestFactory_CreateRedis_NotEnabled(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "redis",
			Redis: config.EventBusRedisConfig{
				Enabled: false, // Not enabled
			},
		},
	}
	cfg.SetDefaults()

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	require.NoError(t, err)
	require.NotNil(t, eb)
	// Should fall back to local when Redis is not enabled
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestFactory_CreateKafka_Enabled(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "kafka",
			Kafka: config.EventBusKafkaConfig{
				Enabled: true,
				Brokers: []string{"localhost:9092"},
				Topic:   "test",
				GroupID: "test-group",
			},
		},
		Node: config.NodeConfig{
			ID:   "test-node",
			Role: "all",
		},
	}
	cfg.SetDefaults()

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	require.NoError(t, err)
	require.NotNil(t, eb)
	// Should create a KafkaEventBus (may be in degraded mode if broker is unreachable)
	assert.Equal(t, EventBusTypeKafka, eb.Type())
}

func TestFactory_CreateKafka_NotEnabled(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "kafka",
			Kafka: config.EventBusKafkaConfig{
				Enabled: false,
			},
		},
	}
	cfg.SetDefaults()

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	require.NoError(t, err)
	require.NotNil(t, eb)
	// Should fall back to local when Kafka is not enabled
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestFactory_CreateHybrid(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "hybrid",
			Redis: config.EventBusRedisConfig{
				Enabled: false,
			},
			Kafka: config.EventBusKafkaConfig{
				Enabled: false,
			},
		},
	}
	cfg.SetDefaults()

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	require.NoError(t, err)
	require.NotNil(t, eb)
	// Falls back to local when neither Redis nor Kafka is enabled
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestFactory_CreateHybrid_KafkaFallback(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "hybrid",
			Redis: config.EventBusRedisConfig{
				Enabled: false,
			},
			Kafka: config.EventBusKafkaConfig{
				Enabled: true,
				Brokers: []string{"localhost:9092"},
				Topic:   "test",
				GroupID: "test-group",
			},
		},
		Node: config.NodeConfig{
			ID:   "test-node",
			Role: "all",
		},
	}
	cfg.SetDefaults()

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	require.NoError(t, err)
	require.NotNil(t, eb)
	// Should use Kafka when Redis is not enabled but Kafka is
	assert.Equal(t, EventBusTypeKafka, eb.Type())
}

func TestFactory_InvalidType(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "invalid",
		},
	}

	factory := NewFactory(cfg)
	eb, err := factory.Create()

	assert.Error(t, err)
	assert.Nil(t, eb)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestFactory_MustCreate_Success(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "local",
		},
	}
	cfg.SetDefaults()

	factory := NewFactory(cfg)

	eb, err := factory.Create()
	require.NoError(t, err)
	require.NotNil(t, eb)
}

func TestFactory_Create_InvalidType(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "invalid",
		},
	}

	factory := NewFactory(cfg)

	_, err := factory.Create()
	assert.Error(t, err)
}

func TestNewEventBus_Convenience(t *testing.T) {
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Type: "local",
		},
	}
	cfg.SetDefaults()

	eb, err := NewEventBus(cfg)
	require.NoError(t, err)
	require.NotNil(t, eb)
}

func TestCreateLocalEventBus(t *testing.T) {
	eb := CreateLocalEventBus(500, 50)
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestCreateDefaultLocalEventBus(t *testing.T) {
	eb := CreateDefaultLocalEventBus()
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}
