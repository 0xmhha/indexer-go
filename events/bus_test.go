package events

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

func TestEventBus_BasicPubSub(t *testing.T) {
	// Create event bus
	bus := NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()

	// Create a subscription
	sub := bus.Subscribe("test-sub", []EventType{EventTypeBlock}, nil, 10)
	if sub == nil {
		t.Fatal("subscription should not be nil")
	}

	// Give the subscription time to register
	time.Sleep(10 * time.Millisecond)

	// Publish a block event
	block := types.NewBlock(&types.Header{Number: big.NewInt(1)}, nil, nil, nil, trie.NewStackTrie(nil))
	event := NewBlockEvent(block)

	if !bus.Publish(event) {
		t.Fatal("publish should succeed")
	}

	// Wait for event delivery
	select {
	case receivedEvent := <-sub.Channel:
		if receivedEvent.Type() != EventTypeBlock {
			t.Errorf("expected block event, got %s", receivedEvent.Type())
		}
		blockEvent, ok := receivedEvent.(*BlockEvent)
		if !ok {
			t.Fatal("event should be a BlockEvent")
		}
		if blockEvent.Number != 1 {
			t.Errorf("expected block number 1, got %d", blockEvent.Number)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()

	// Create multiple subscriptions
	sub1 := bus.Subscribe("sub1", []EventType{EventTypeBlock}, nil, 10)
	sub2 := bus.Subscribe("sub2", []EventType{EventTypeBlock}, nil, 10)
	sub3 := bus.Subscribe("sub3", []EventType{EventTypeBlock}, nil, 10)

	time.Sleep(10 * time.Millisecond)

	// Verify subscriber count
	if count := bus.SubscriberCount(); count != 3 {
		t.Errorf("expected 3 subscribers, got %d", count)
	}

	// Publish an event
	block := types.NewBlock(&types.Header{Number: big.NewInt(1)}, nil, nil, nil, trie.NewStackTrie(nil))
	event := NewBlockEvent(block)
	bus.Publish(event)

	// All three subscribers should receive the event
	subs := []*Subscription{sub1, sub2, sub3}
	for i, sub := range subs {
		select {
		case <-sub.Channel:
			// Success
		case <-time.After(1 * time.Second):
			t.Errorf("subscriber %d did not receive event", i+1)
		}
	}
}

func TestEventBus_EventTypeFiltering(t *testing.T) {
	bus := NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()

	// Create subscriptions with different event type filters
	blockSub := bus.Subscribe("block-sub", []EventType{EventTypeBlock}, nil, 10)
	txSub := bus.Subscribe("tx-sub", []EventType{EventTypeTransaction}, nil, 10)
	bothSub := bus.Subscribe("both-sub", []EventType{EventTypeBlock, EventTypeTransaction}, nil, 10)

	time.Sleep(10 * time.Millisecond)

	// Publish a block event
	block := types.NewBlock(&types.Header{Number: big.NewInt(1)}, nil, nil, nil, trie.NewStackTrie(nil))
	blockEvent := NewBlockEvent(block)
	bus.Publish(blockEvent)

	// Publish a transaction event
	tx := types.NewTransaction(0, common.HexToAddress("0x1"), big.NewInt(100), 21000, big.NewInt(1), nil)
	txEvent := NewTransactionEvent(tx, 1, common.Hash{}, 0, common.Address{}, nil)
	bus.Publish(txEvent)

	// Give time for delivery
	time.Sleep(50 * time.Millisecond)

	// blockSub should receive only block event
	select {
	case event := <-blockSub.Channel:
		if event.Type() != EventTypeBlock {
			t.Error("blockSub should only receive block events")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("blockSub did not receive block event")
	}

	// Should not have any more events
	select {
	case <-blockSub.Channel:
		t.Error("blockSub should not receive transaction event")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}

	// txSub should receive only transaction event
	select {
	case event := <-txSub.Channel:
		if event.Type() != EventTypeTransaction {
			t.Error("txSub should only receive transaction events")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("txSub did not receive transaction event")
	}

	// bothSub should receive both events
	receivedBlock := false
	receivedTx := false

	for i := 0; i < 2; i++ {
		select {
		case event := <-bothSub.Channel:
			if event.Type() == EventTypeBlock {
				receivedBlock = true
			} else if event.Type() == EventTypeTransaction {
				receivedTx = true
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("bothSub did not receive all events")
		}
	}

	if !receivedBlock || !receivedTx {
		t.Error("bothSub should receive both block and transaction events")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()

	// Create a subscription
	sub := bus.Subscribe("test-sub", []EventType{EventTypeBlock}, nil, 10)
	time.Sleep(10 * time.Millisecond)

	if count := bus.SubscriberCount(); count != 1 {
		t.Errorf("expected 1 subscriber, got %d", count)
	}

	// Unsubscribe
	bus.Unsubscribe("test-sub")
	time.Sleep(10 * time.Millisecond)

	if count := bus.SubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers, got %d", count)
	}

	// Publish an event - subscriber should not receive it
	block := types.NewBlock(&types.Header{Number: big.NewInt(1)}, nil, nil, nil, trie.NewStackTrie(nil))
	event := NewBlockEvent(block)
	bus.Publish(event)

	// Channel should be closed or no event received
	select {
	case _, ok := <-sub.Channel:
		if ok {
			t.Error("unsubscribed channel should not receive events")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected - no event received
	}
}

func TestEventBus_Stats(t *testing.T) {
	bus := NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()

	// Create subscriptions
	sub1 := bus.Subscribe("sub1", []EventType{EventTypeBlock}, nil, 10)
	sub2 := bus.Subscribe("sub2", []EventType{EventTypeBlock}, nil, 10)
	time.Sleep(10 * time.Millisecond)

	// Publish events
	for i := 0; i < 5; i++ {
		block := types.NewBlock(&types.Header{Number: big.NewInt(int64(i))}, nil, nil, nil, trie.NewStackTrie(nil))
		event := NewBlockEvent(block)
		bus.Publish(event)
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	// Check stats
	totalEvents, totalDeliveries, droppedEvents := bus.Stats()

	if totalEvents != 5 {
		t.Errorf("expected 5 total events, got %d", totalEvents)
	}

	// Should be 10 deliveries (5 events × 2 subscribers)
	if totalDeliveries != 10 {
		t.Errorf("expected 10 total deliveries, got %d", totalDeliveries)
	}

	if droppedEvents != 0 {
		t.Errorf("expected 0 dropped events, got %d", droppedEvents)
	}

	// Drain channels
	for i := 0; i < 5; i++ {
		<-sub1.Channel
		<-sub2.Channel
	}
}

func TestEventBus_DroppedEvents(t *testing.T) {
	bus := NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()

	// Create subscription with small buffer
	sub := bus.Subscribe("test-sub", []EventType{EventTypeBlock}, nil, 1)
	time.Sleep(10 * time.Millisecond)

	// Publish many events quickly to overflow buffer
	for i := 0; i < 10; i++ {
		block := types.NewBlock(&types.Header{Number: big.NewInt(int64(i))}, nil, nil, nil, trie.NewStackTrie(nil))
		event := NewBlockEvent(block)
		bus.Publish(event)
	}

	time.Sleep(50 * time.Millisecond)

	// Check stats - some events should be dropped
	_, _, droppedEvents := bus.Stats()

	if droppedEvents == 0 {
		t.Error("expected some events to be dropped due to small buffer")
	}

	// Drain channel
	for {
		select {
		case <-sub.Channel:
		default:
			return
		}
	}
}

func TestEventBus_Stop(t *testing.T) {
	bus := NewEventBus(100, 10)
	go bus.Run()

	// Create subscription
	sub := bus.Subscribe("test-sub", []EventType{EventTypeBlock}, nil, 10)
	time.Sleep(10 * time.Millisecond)

	// Stop the bus
	bus.Stop()

	// Try to publish after stop - should fail
	block := types.NewBlock(&types.Header{Number: big.NewInt(1)}, nil, nil, nil, trie.NewStackTrie(nil))
	event := NewBlockEvent(block)

	if bus.Publish(event) {
		t.Error("publish should fail after stop")
	}

	// Subscription channel should be closed
	select {
	case _, ok := <-sub.Channel:
		if ok {
			t.Error("subscription channel should be closed after stop")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("subscription channel was not closed")
	}
}

func TestEventBus_ConcurrentOperations(t *testing.T) {
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	// Create multiple subscriptions concurrently
	done := make(chan bool)
	subscriberCount := 10

	for i := 0; i < subscriberCount; i++ {
		go func(id int) {
			subID := SubscriptionID(string(rune('A' + id)))
			bus.Subscribe(subID, []EventType{EventTypeBlock}, nil, 100)
			done <- true
		}(i)
	}

	// Wait for all subscriptions
	for i := 0; i < subscriberCount; i++ {
		<-done
	}

	time.Sleep(50 * time.Millisecond)

	// Publish events concurrently
	publishCount := 100
	for i := 0; i < publishCount; i++ {
		go func(num int) {
			block := types.NewBlock(&types.Header{Number: big.NewInt(int64(num))}, nil, nil, nil, trie.NewStackTrie(nil))
			event := NewBlockEvent(block)
			bus.Publish(event)
		}(i)
	}

	time.Sleep(100 * time.Millisecond)

	// Check that we have correct subscriber count
	if count := bus.SubscriberCount(); count != subscriberCount {
		t.Errorf("expected %d subscribers, got %d", subscriberCount, count)
	}

	// Check stats
	totalEvents, _, _ := bus.Stats()
	if totalEvents != uint64(publishCount) {
		t.Errorf("expected %d total events, got %d", publishCount, totalEvents)
	}
}
