package eventbus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Option Functions Tests
// ============================================================================

func TestWithPublishBufferSize(t *testing.T) {
	eb := NewLocalEventBus()
	opt := WithPublishBufferSize(2000)

	// Should not panic and apply to LocalEventBus
	assert.NotPanics(t, func() {
		opt(eb)
	})
}

func TestWithHistorySize(t *testing.T) {
	eb := NewLocalEventBus()
	opt := WithHistorySize(500)

	// Should not panic and apply to LocalEventBus
	assert.NotPanics(t, func() {
		opt(eb)
	})
}

func TestWithMetrics(t *testing.T) {
	eb := NewLocalEventBus()
	opt := WithMetrics(nil)

	// Should not panic when applied to EventBus
	assert.NotPanics(t, func() {
		opt(eb)
	})
}

func TestWithPublishBufferSize_NonImplementor(t *testing.T) {
	opt := WithPublishBufferSize(100)
	// Apply to something that doesn't implement SetPublishBufferSize
	assert.NotPanics(t, func() {
		opt("not an event bus")
	})
}

func TestWithHistorySize_NonImplementor(t *testing.T) {
	opt := WithHistorySize(100)
	// Apply to something that doesn't implement SetHistorySize
	assert.NotPanics(t, func() {
		opt("not an event bus")
	})
}

func TestWithMetrics_NonEventBus(t *testing.T) {
	opt := WithMetrics(nil)
	// Apply to something that isn't an EventBus
	assert.NotPanics(t, func() {
		opt("not an event bus")
	})
}

// ============================================================================
// NewLocalEventBusWithOptions Tests
// ============================================================================

func TestNewLocalEventBusWithOptions(t *testing.T) {
	eb := NewLocalEventBusWithOptions()
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestNewLocalEventBusWithOptions_WithOpts(t *testing.T) {
	eb := NewLocalEventBusWithOptions(
		WithPublishBufferSize(2000),
		WithHistorySize(500),
	)
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

// ============================================================================
// LocalEventBus setter no-ops Tests
// ============================================================================

func TestLocalEventBus_SetPublishBufferSize(t *testing.T) {
	eb := NewLocalEventBus()
	// Should be a no-op, just verify no panic
	assert.NotPanics(t, func() {
		eb.SetPublishBufferSize(2000)
	})
}

func TestLocalEventBus_SetHistorySize(t *testing.T) {
	eb := NewLocalEventBus()
	// Should be a no-op, just verify no panic
	assert.NotPanics(t, func() {
		eb.SetHistorySize(500)
	})
}

func TestLocalEventBus_SetMetrics(t *testing.T) {
	eb := NewLocalEventBus()
	// Should not panic
	assert.NotPanics(t, func() {
		eb.SetMetrics(nil)
	})
}

// ============================================================================
// NewEventBusWithContext Tests
// ============================================================================

// Note: NewEventBusWithContext is at 0% coverage
// We can only test the local path without external services
