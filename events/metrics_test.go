package events

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// TestMetrics_Integration tests metrics collection with EventBus
func TestMetrics_Integration(t *testing.T) {
	// Create EventBus with metrics
	bus := NewEventBus(1000, 100)
	metrics := NewMetrics("test", "eventbus")
	bus.SetMetrics(metrics)

	go bus.Run()
	defer bus.Stop()

	// Create subscriptions
	sub1 := bus.Subscribe("sub1", []EventType{EventTypeBlock}, nil, 10)
	if sub1 == nil {
		t.Fatal("failed to create subscription 1")
	}

	sub2 := bus.Subscribe("sub2", []EventType{EventTypeTransaction}, nil, 10)
	if sub2 == nil {
		t.Fatal("failed to create subscription 2")
	}

	// Start draining channels
	go func() {
		for range sub1.Channel {
		}
	}()
	go func() {
		for range sub2.Channel {
		}
	}()

	time.Sleep(10 * time.Millisecond) // Let subscriptions register

	// Create and publish a block event
	header := &types.Header{
		Number: big.NewInt(100),
		Time:   uint64(time.Now().Unix()),
	}
	block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
	blockEvent := NewBlockEvent(block)

	if !bus.Publish(blockEvent) {
		t.Fatal("failed to publish block event")
	}

	// Create and publish a transaction event
	tx := types.NewTransaction(0, common.HexToAddress("0x1111"), big.NewInt(100), 21000, big.NewInt(1), nil)
	txEvent := NewTransactionEvent(tx, 100, common.Hash{}, 0, common.HexToAddress("0xaaaa"), nil)

	if !bus.Publish(txEvent) {
		t.Fatal("failed to publish transaction event")
	}

	// Wait for events to be delivered
	time.Sleep(100 * time.Millisecond)

	// Verify stats
	totalEvents, totalDeliveries, droppedEvents := bus.Stats()

	if totalEvents != 2 {
		t.Errorf("expected 2 total events, got %d", totalEvents)
	}

	// Should have 2 deliveries (1 block to sub1, 1 tx to sub2)
	if totalDeliveries != 2 {
		t.Errorf("expected 2 total deliveries, got %d", totalDeliveries)
	}

	if droppedEvents != 0 {
		t.Errorf("expected 0 dropped events, got %d", droppedEvents)
	}

	// Verify subscriber stats
	info1 := bus.GetSubscriberInfo("sub1")
	if info1 == nil {
		t.Fatal("failed to get subscriber info for sub1")
	}

	if info1.EventsReceived != 1 {
		t.Errorf("sub1: expected 1 event received, got %d", info1.EventsReceived)
	}

	if info1.EventsDropped != 0 {
		t.Errorf("sub1: expected 0 events dropped, got %d", info1.EventsDropped)
	}

	info2 := bus.GetSubscriberInfo("sub2")
	if info2 == nil {
		t.Fatal("failed to get subscriber info for sub2")
	}

	if info2.EventsReceived != 1 {
		t.Errorf("sub2: expected 1 event received, got %d", info2.EventsReceived)
	}

	// Cleanup
	bus.Unsubscribe("sub1")
	bus.Unsubscribe("sub2")
}

// TestMetrics_DroppedEvents tests dropped event metrics
func TestMetrics_DroppedEvents(t *testing.T) {
	// Create EventBus with very small buffer
	bus := NewEventBus(1, 10)
	metrics := NewMetrics("test_dropped", "eventbus")
	bus.SetMetrics(metrics)

	go bus.Run()
	defer bus.Stop()

	// Create subscription with small buffer
	sub := bus.Subscribe("sub", []EventType{EventTypeBlock}, nil, 1)
	if sub == nil {
		t.Fatal("failed to create subscription")
	}

	// Don't drain the channel - let it fill up
	time.Sleep(10 * time.Millisecond)

	// Publish multiple events
	for i := 0; i < 10; i++ {
		header := &types.Header{
			Number: big.NewInt(int64(i)),
			Time:   uint64(time.Now().Unix()),
		}
		block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
		event := NewBlockEvent(block)
		bus.Publish(event)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify some events were dropped
	_, _, droppedEvents := bus.Stats()
	if droppedEvents == 0 {
		t.Error("expected some dropped events, got 0")
	}

	info := bus.GetSubscriberInfo("sub")
	if info == nil {
		t.Fatal("failed to get subscriber info")
	}

	if info.EventsDropped == 0 {
		t.Error("expected some subscriber dropped events, got 0")
	}

	// Cleanup
	bus.Unsubscribe("sub")
}

// TestMetrics_FilteredEvents tests filtered event metrics
func TestMetrics_FilteredEvents(t *testing.T) {
	// Create EventBus with metrics
	bus := NewEventBus(1000, 100)
	metrics := NewMetrics("test_filtered", "eventbus")
	bus.SetMetrics(metrics)

	go bus.Run()
	defer bus.Stop()

	// Create subscription with address filter
	targetAddr := common.HexToAddress("0x1111")
	filter := &Filter{
		FromAddresses: []common.Address{targetAddr},
	}

	sub := bus.Subscribe("sub", []EventType{EventTypeTransaction}, filter, 10)
	if sub == nil {
		t.Fatal("failed to create subscription")
	}

	// Drain channel
	received := 0
	go func() {
		for range sub.Channel {
			received++
		}
	}()

	time.Sleep(10 * time.Millisecond)

	// Publish matching transaction
	tx1 := types.NewTransaction(0, common.HexToAddress("0x2222"), big.NewInt(100), 21000, big.NewInt(1), nil)
	event1 := NewTransactionEvent(tx1, 100, common.Hash{}, 0, targetAddr, nil)
	bus.Publish(event1)

	// Publish non-matching transaction
	tx2 := types.NewTransaction(1, common.HexToAddress("0x2222"), big.NewInt(200), 21000, big.NewInt(1), nil)
	event2 := NewTransactionEvent(tx2, 100, common.Hash{}, 1, common.HexToAddress("0x3333"), nil)
	bus.Publish(event2)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify only 1 event was received (the matching one)
	if received != 1 {
		t.Errorf("expected 1 received event, got %d", received)
	}

	// Cleanup
	bus.Unsubscribe("sub")
}

// TestMetrics_SubscriberInfo tests subscriber info retrieval
func TestMetrics_SubscriberInfo(t *testing.T) {
	// Create EventBus
	bus := NewEventBus(1000, 100)
	metrics := NewMetrics("test_subinfo", "eventbus")
	bus.SetMetrics(metrics)

	go bus.Run()
	defer bus.Stop()

	// Create subscription
	sub := bus.Subscribe("test-sub", []EventType{EventTypeBlock, EventTypeTransaction}, nil, 10)
	if sub == nil {
		t.Fatal("failed to create subscription")
	}

	time.Sleep(10 * time.Millisecond)

	// Get subscriber info
	info := bus.GetSubscriberInfo("test-sub")
	if info == nil {
		t.Fatal("failed to get subscriber info")
	}

	// Verify basic info
	if info.ID != "test-sub" {
		t.Errorf("expected ID 'test-sub', got '%s'", info.ID)
	}

	if len(info.EventTypes) != 2 {
		t.Errorf("expected 2 event types, got %d", len(info.EventTypes))
	}

	if info.HasFilter {
		t.Error("expected HasFilter to be false")
	}

	if info.EventsReceived != 0 {
		t.Errorf("expected 0 events received, got %d", info.EventsReceived)
	}

	// Test non-existent subscriber
	info = bus.GetSubscriberInfo("non-existent")
	if info != nil {
		t.Error("expected nil for non-existent subscriber")
	}

	// Cleanup
	bus.Unsubscribe("test-sub")
}

// TestMetrics_GetAllSubscriberInfo tests getting all subscriber info
func TestMetrics_GetAllSubscriberInfo(t *testing.T) {
	// Create EventBus
	bus := NewEventBus(1000, 100)
	metrics := NewMetrics("test_allinfo", "eventbus")
	bus.SetMetrics(metrics)

	go bus.Run()
	defer bus.Stop()

	// Create multiple subscriptions
	numSubs := 5
	for i := 0; i < numSubs; i++ {
		subID := SubscriptionID(string(rune('A' + i)))
		sub := bus.Subscribe(subID, []EventType{EventTypeBlock}, nil, 10)
		if sub == nil {
			t.Fatalf("failed to create subscription %s", subID)
		}

		// Drain channel
		go func(s *Subscription) {
			for range s.Channel {
			}
		}(sub)
	}

	time.Sleep(10 * time.Millisecond)

	// Get all subscriber info
	allInfo := bus.GetAllSubscriberInfo()

	if len(allInfo) != numSubs {
		t.Errorf("expected %d subscribers, got %d", numSubs, len(allInfo))
	}

	// Verify each subscriber
	for _, info := range allInfo {
		if info.EventsReceived != 0 {
			t.Errorf("subscriber %s: expected 0 events received, got %d", info.ID, info.EventsReceived)
		}
	}

	// Cleanup
	for i := 0; i < numSubs; i++ {
		subID := SubscriptionID(string(rune('A' + i)))
		bus.Unsubscribe(subID)
	}
}
