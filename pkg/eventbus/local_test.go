package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocalEventBus(t *testing.T) {
	eb := NewLocalEventBus()
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
	assert.True(t, eb.Healthy())
}

func TestNewLocalEventBusWithConfig(t *testing.T) {
	eb := NewLocalEventBusWithConfig(500, 50)
	require.NotNil(t, eb)
	assert.Equal(t, EventBusTypeLocal, eb.Type())
}

func TestNewLocalEventBusWithConfig_Defaults(t *testing.T) {
	// Test with zero values - should use defaults
	eb := NewLocalEventBusWithConfig(0, 0)
	require.NotNil(t, eb)
}

func TestLocalEventBus_PublishSubscribe(t *testing.T) {
	eb := NewLocalEventBus()

	// Start the event bus in a goroutine
	go eb.Run()
	defer eb.Stop()

	// Wait for bus to start
	time.Sleep(10 * time.Millisecond)

	// Create subscription
	sub := eb.Subscribe(
		"test-sub-1",
		[]events.EventType{events.EventTypeBlock},
		nil,
		10,
	)
	require.NotNil(t, sub)

	// Publish an event
	blockEvent := &events.BlockEvent{
		Number:    100,
		Hash:      common.HexToHash("0x1234"),
		CreatedAt: time.Now(),
		TxCount:   5,
	}

	ok := eb.Publish(blockEvent)
	assert.True(t, ok)

	// Wait for event delivery
	select {
	case received := <-sub.Channel:
		assert.Equal(t, events.EventTypeBlock, received.Type())
		be, ok := received.(*events.BlockEvent)
		require.True(t, ok)
		assert.Equal(t, uint64(100), be.Number)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestLocalEventBus_PublishWithContext(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	// Test successful publish
	ctx := context.Background()
	blockEvent := &events.BlockEvent{
		Number:    200,
		Hash:      common.HexToHash("0x5678"),
		CreatedAt: time.Now(),
	}

	err := eb.PublishWithContext(ctx, blockEvent)
	assert.NoError(t, err)

	// Test cancelled context
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = eb.PublishWithContext(cancelCtx, blockEvent)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestLocalEventBus_MultipleSubscribers(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	// Create multiple subscriptions
	sub1 := eb.Subscribe("sub-1", []events.EventType{events.EventTypeBlock}, nil, 10)
	sub2 := eb.Subscribe("sub-2", []events.EventType{events.EventTypeBlock}, nil, 10)
	sub3 := eb.Subscribe("sub-3", []events.EventType{events.EventTypeTransaction}, nil, 10)

	require.NotNil(t, sub1)
	require.NotNil(t, sub2)
	require.NotNil(t, sub3)

	assert.Equal(t, 3, eb.SubscriberCount())

	// Publish block event
	blockEvent := &events.BlockEvent{
		Number:    300,
		Hash:      common.HexToHash("0xabc"),
		CreatedAt: time.Now(),
	}
	eb.Publish(blockEvent)

	// Both block subscribers should receive the event
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		select {
		case <-sub1.Channel:
		case <-time.After(100 * time.Millisecond):
			t.Error("sub1 timeout")
		}
	}()

	go func() {
		defer wg.Done()
		select {
		case <-sub2.Channel:
		case <-time.After(100 * time.Millisecond):
			t.Error("sub2 timeout")
		}
	}()

	wg.Wait()

	// Transaction subscriber should NOT receive block event
	select {
	case <-sub3.Channel:
		t.Error("sub3 should not receive block event")
	case <-time.After(50 * time.Millisecond):
		// Expected - no event received
	}
}

func TestLocalEventBus_Unsubscribe(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	sub := eb.Subscribe("test-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)
	assert.Equal(t, 1, eb.SubscriberCount())

	eb.Unsubscribe("test-sub")
	assert.Equal(t, 0, eb.SubscriberCount())
}

func TestLocalEventBus_Stats(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	sub := eb.Subscribe("stats-sub", []events.EventType{events.EventTypeBlock}, nil, 10)
	require.NotNil(t, sub)

	// Publish some events
	for i := 0; i < 5; i++ {
		eb.Publish(&events.BlockEvent{
			Number:    uint64(i),
			CreatedAt: time.Now(),
		})
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	// Drain the channel
	for i := 0; i < 5; i++ {
		select {
		case <-sub.Channel:
		case <-time.After(100 * time.Millisecond):
		}
	}

	totalEvents, totalDeliveries, droppedEvents := eb.Stats()
	assert.Equal(t, uint64(5), totalEvents)
	assert.Equal(t, uint64(5), totalDeliveries)
	assert.Equal(t, uint64(0), droppedEvents)
}

func TestLocalEventBus_GetSubscriberInfo(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	sub := eb.Subscribe(
		"info-sub",
		[]events.EventType{events.EventTypeBlock, events.EventTypeTransaction},
		nil,
		10,
	)
	require.NotNil(t, sub)

	info := eb.GetSubscriberInfo("info-sub")
	require.NotNil(t, info)
	assert.Equal(t, events.SubscriptionID("info-sub"), info.ID)
	assert.Len(t, info.EventTypes, 2)
	assert.False(t, info.HasFilter)

	// Non-existent subscriber
	noInfo := eb.GetSubscriberInfo("non-existent")
	assert.Nil(t, noInfo)
}

func TestLocalEventBus_GetAllSubscriberInfo(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	eb.Subscribe("sub-a", []events.EventType{events.EventTypeBlock}, nil, 10)
	eb.Subscribe("sub-b", []events.EventType{events.EventTypeTransaction}, nil, 10)
	eb.Subscribe("sub-c", []events.EventType{events.EventTypeLog}, nil, 10)

	allInfo := eb.GetAllSubscriberInfo()
	assert.Len(t, allInfo, 3)
}

func TestLocalEventBus_GetDetailedStats(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	stats := eb.GetDetailedStats()
	assert.Equal(t, uint64(0), stats.TotalEventsPublished)
	assert.Equal(t, 0, stats.ActiveSubscribers)
	assert.True(t, stats.Uptime > 0)
}

func TestLocalEventBus_GetHealthStatus(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()

	time.Sleep(10 * time.Millisecond)

	health := eb.GetHealthStatus()
	assert.Equal(t, "healthy", health.Status)
	assert.Contains(t, health.Message, "operational")
	assert.NotNil(t, health.Details)

	eb.Stop()

	health = eb.GetHealthStatus()
	assert.Equal(t, "unhealthy", health.Status)
}

func TestLocalEventBus_UnderlyingBus(t *testing.T) {
	eb := NewLocalEventBus()
	bus := eb.UnderlyingBus()
	require.NotNil(t, bus)
}

func TestLocalEventBus_WithFilter(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	// Create filter for specific block numbers
	filter := &events.Filter{
		FromBlock: 100,
		ToBlock:   200,
	}

	sub := eb.Subscribe(
		"filtered-sub",
		[]events.EventType{events.EventTypeBlock},
		filter,
		10,
	)
	require.NotNil(t, sub)

	// Publish event within filter range
	eb.Publish(&events.BlockEvent{
		Number:    150,
		CreatedAt: time.Now(),
	})

	// Publish event outside filter range
	eb.Publish(&events.BlockEvent{
		Number:    50,
		CreatedAt: time.Now(),
	})

	// Should only receive the filtered event
	received := 0
	timeout := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case <-sub.Channel:
			received++
		case <-timeout:
			break loop
		}
	}

	assert.Equal(t, 1, received)
}

func TestLocalEventBus_SubscribeWithOptions(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	opts := events.SubscribeOptions{
		ChannelSize: 50,
		ReplayLast:  0,
	}

	sub := eb.SubscribeWithOptions(
		"opts-sub",
		[]events.EventType{events.EventTypeBlock},
		nil,
		opts,
	)
	require.NotNil(t, sub)
	assert.Equal(t, 1, eb.SubscriberCount())
}

func TestLocalEventBus_ConcurrentPublish(t *testing.T) {
	eb := NewLocalEventBus()
	go eb.Run()
	defer eb.Stop()

	time.Sleep(10 * time.Millisecond)

	sub := eb.Subscribe("concurrent-sub", []events.EventType{events.EventTypeBlock}, nil, 1000)
	require.NotNil(t, sub)

	// Concurrent publishing
	var wg sync.WaitGroup
	numPublishers := 10
	eventsPerPublisher := 100

	for i := 0; i < numPublishers; i++ {
		wg.Add(1)
		go func(publisherID int) {
			defer wg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				eb.Publish(&events.BlockEvent{
					Number:    uint64(publisherID*eventsPerPublisher + j),
					CreatedAt: time.Now(),
				})
			}
		}(i)
	}

	wg.Wait()

	// Wait for delivery
	time.Sleep(100 * time.Millisecond)

	// Verify stats
	totalEvents, _, _ := eb.Stats()
	assert.Equal(t, uint64(numPublishers*eventsPerPublisher), totalEvents)
}
