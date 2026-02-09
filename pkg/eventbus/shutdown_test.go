package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// ShutdownManager Tests
// ============================================================================

func TestNewShutdownManager(t *testing.T) {
	sm := NewShutdownManager(10 * time.Second)
	require.NotNil(t, sm)
	assert.Equal(t, 10*time.Second, sm.shutdownTimeout)
}

func TestNewShutdownManager_DefaultTimeout(t *testing.T) {
	sm := NewShutdownManager(0)
	require.NotNil(t, sm)
	assert.Equal(t, 30*time.Second, sm.shutdownTimeout)
}

func TestNewShutdownManager_NegativeTimeout(t *testing.T) {
	sm := NewShutdownManager(-5 * time.Second)
	require.NotNil(t, sm)
	assert.Equal(t, 30*time.Second, sm.shutdownTimeout)
}

func TestShutdownManager_RegisterEventBus(t *testing.T) {
	sm := NewShutdownManager(5 * time.Second)
	eb := NewLocalEventBus()
	sm.RegisterEventBus(eb)
	assert.NotNil(t, sm.eventBus)
}

func TestShutdownManager_RegisterKafkaProducer(t *testing.T) {
	sm := NewShutdownManager(5 * time.Second)
	kp := &KafkaProducer{}
	sm.RegisterKafkaProducer(kp)
	assert.NotNil(t, sm.kafkaProducer)
}

func TestShutdownManager_Shutdown_NoComponents(t *testing.T) {
	sm := NewShutdownManager(5 * time.Second)
	err := sm.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestShutdownManager_Shutdown_DoubleShutdown(t *testing.T) {
	sm := NewShutdownManager(5 * time.Second)

	// First shutdown should succeed
	err := sm.Shutdown(context.Background())
	assert.NoError(t, err)

	// Second shutdown should be a no-op (returns nil)
	err = sm.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestShutdownManager_Shutdown_WithLocalEventBus(t *testing.T) {
	sm := NewShutdownManager(5 * time.Second)
	eb := NewLocalEventBus()
	go eb.Run()

	sm.RegisterEventBus(eb)

	err := sm.Shutdown(context.Background())
	assert.NoError(t, err)
}

// ============================================================================
// MultiComponentShutdown Tests
// ============================================================================

func TestNewMultiComponentShutdown(t *testing.T) {
	mcs := NewMultiComponentShutdown(10 * time.Second)
	require.NotNil(t, mcs)
	assert.Equal(t, 10*time.Second, mcs.timeout)
}

func TestNewMultiComponentShutdown_DefaultTimeout(t *testing.T) {
	mcs := NewMultiComponentShutdown(0)
	require.NotNil(t, mcs)
	assert.Equal(t, 30*time.Second, mcs.timeout)
}

func TestNewMultiComponentShutdown_NegativeTimeout(t *testing.T) {
	mcs := NewMultiComponentShutdown(-1)
	require.NotNil(t, mcs)
	assert.Equal(t, 30*time.Second, mcs.timeout)
}

func TestMultiComponentShutdown_RegisterHook(t *testing.T) {
	mcs := NewMultiComponentShutdown(5 * time.Second)

	mcs.RegisterHook("api", ShutdownPriorityAPI, func(ctx context.Context) error {
		return nil
	})
	mcs.RegisterHook("eventbus", ShutdownPriorityEventBus, func(ctx context.Context) error {
		return nil
	})
	mcs.RegisterHook("storage", ShutdownPriorityStorage, func(ctx context.Context) error {
		return nil
	})

	// Hooks should be sorted by priority (descending)
	assert.Equal(t, 3, len(mcs.hooks))
	assert.Equal(t, "eventbus", mcs.hooks[0].name)
	assert.Equal(t, "api", mcs.hooks[1].name)
	assert.Equal(t, "storage", mcs.hooks[2].name)
}

func TestMultiComponentShutdown_Shutdown_ExecutesInOrder(t *testing.T) {
	mcs := NewMultiComponentShutdown(5 * time.Second)

	var order []string

	mcs.RegisterHook("storage", ShutdownPriorityStorage, func(ctx context.Context) error {
		order = append(order, "storage")
		return nil
	})
	mcs.RegisterHook("eventbus", ShutdownPriorityEventBus, func(ctx context.Context) error {
		order = append(order, "eventbus")
		return nil
	})
	mcs.RegisterHook("api", ShutdownPriorityAPI, func(ctx context.Context) error {
		order = append(order, "api")
		return nil
	})

	err := mcs.Shutdown(context.Background())
	assert.NoError(t, err)

	// Should execute in priority order: eventbus(100), api(50), storage(10)
	require.Equal(t, 3, len(order))
	assert.Equal(t, "eventbus", order[0])
	assert.Equal(t, "api", order[1])
	assert.Equal(t, "storage", order[2])
}

func TestMultiComponentShutdown_Shutdown_NoHooks(t *testing.T) {
	mcs := NewMultiComponentShutdown(5 * time.Second)
	err := mcs.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestMultiComponentShutdown_Shutdown_WithError(t *testing.T) {
	mcs := NewMultiComponentShutdown(5 * time.Second)

	expectedErr := errors.New("shutdown failed")
	mcs.RegisterHook("failing", 100, func(ctx context.Context) error {
		return expectedErr
	})
	mcs.RegisterHook("succeeding", 50, func(ctx context.Context) error {
		return nil
	})

	err := mcs.Shutdown(context.Background())
	assert.Equal(t, expectedErr, err)
}

func TestMultiComponentShutdown_Shutdown_MultipleErrors(t *testing.T) {
	mcs := NewMultiComponentShutdown(5 * time.Second)

	err1 := errors.New("error 1")
	err2 := errors.New("error 2")

	mcs.RegisterHook("first", 100, func(ctx context.Context) error {
		return err1
	})
	mcs.RegisterHook("second", 50, func(ctx context.Context) error {
		return err2
	})

	// Should return the first error
	err := mcs.Shutdown(context.Background())
	assert.Equal(t, err1, err)
}

// ============================================================================
// Shutdown Priority Constants Tests
// ============================================================================

func TestShutdownPriorityConstants(t *testing.T) {
	// Verify priority ordering: EventBus > Kafka > Redis > API > Storage > Cleanup
	assert.Greater(t, ShutdownPriorityEventBus, ShutdownPriorityKafka)
	assert.Greater(t, ShutdownPriorityKafka, ShutdownPriorityRedis)
	assert.Greater(t, ShutdownPriorityRedis, ShutdownPriorityAPI)
	assert.Greater(t, ShutdownPriorityAPI, ShutdownPriorityStorage)
	assert.Greater(t, ShutdownPriorityStorage, ShutdownPriorityCleanup)
}

// ============================================================================
// shutdownEventBus Tests (via Shutdown with registered EventBus)
// ============================================================================

// mockDistributedEventBus implements DistributedEventBus for testing shutdown
type mockDistributedEventBus struct {
	*LocalEventBus
	connected     bool
	disconnected  bool
	disconnectErr error
}

func (m *mockDistributedEventBus) Connect(ctx context.Context) error   { return nil }
func (m *mockDistributedEventBus) IsConnected() bool                   { return m.connected }
func (m *mockDistributedEventBus) NodeID() string                      { return "test-node" }
func (m *mockDistributedEventBus) GetHealthStatus() HealthStatus       { return HealthStatus{} }
func (m *mockDistributedEventBus) Disconnect(ctx context.Context) error {
	m.disconnected = true
	return m.disconnectErr
}
func (m *mockDistributedEventBus) Publish(event events.Event) bool       { return true }
func (m *mockDistributedEventBus) PublishWithContext(ctx context.Context, event events.Event) error {
	return nil
}

func TestShutdownManager_Shutdown_WithDistributedEventBus(t *testing.T) {
	sm := NewShutdownManager(5 * time.Second)

	local := NewLocalEventBus()
	go local.Run()

	deb := &mockDistributedEventBus{
		LocalEventBus: local,
		connected:     true,
	}
	sm.RegisterEventBus(deb)

	err := sm.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.True(t, deb.disconnected)
}

func TestShutdownManager_Shutdown_DistributedEventBus_DisconnectError(t *testing.T) {
	sm := NewShutdownManager(5 * time.Second)

	local := NewLocalEventBus()
	go local.Run()

	deb := &mockDistributedEventBus{
		LocalEventBus: local,
		connected:     true,
		disconnectErr: errors.New("disconnect failed"),
	}
	sm.RegisterEventBus(deb)

	err := sm.Shutdown(context.Background())
	// The error is reported but shutdown continues
	assert.Error(t, err)
}
