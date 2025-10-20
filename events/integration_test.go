package events

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// TestIntegration_RealisticWorkflow tests a realistic blockchain event workflow
func TestIntegration_RealisticWorkflow(t *testing.T) {
	// Create event bus
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	// Create multiple subscribers with different interests
	blockSub := bus.Subscribe("block-monitor", []EventType{EventTypeBlock}, 100)
	txSub := bus.Subscribe("tx-monitor", []EventType{EventTypeTransaction}, 100)
	allSub := bus.Subscribe("all-monitor", []EventType{EventTypeBlock, EventTypeTransaction}, 100)

	time.Sleep(10 * time.Millisecond)

	// Simulate blockchain activity: 10 blocks with varying transaction counts
	totalBlocks := 10
	totalTransactions := 0

	for blockNum := 1; blockNum <= totalBlocks; blockNum++ {
		// Create block with transactions
		txCount := blockNum % 5 // 0-4 transactions per block
		totalTransactions += txCount

		txs := make([]*types.Transaction, txCount)
		for i := 0; i < txCount; i++ {
			tx := types.NewTransaction(
				uint64(i),
				common.HexToAddress("0x1234"),
				big.NewInt(int64(i*100)),
				21000,
				big.NewInt(1),
				nil,
			)
			txs[i] = tx
		}

		body := &types.Body{Transactions: txs}
		header := &types.Header{Number: big.NewInt(int64(blockNum))}
		block := types.NewBlock(header, body, nil, trie.NewStackTrie(nil))

		// Publish block event
		blockEvent := NewBlockEvent(block)
		if !bus.Publish(blockEvent) {
			t.Errorf("failed to publish block %d", blockNum)
		}

		// Publish transaction events
		for i, tx := range txs {
			txEvent := NewTransactionEvent(
				tx,
				uint64(blockNum),
				block.Hash(),
				uint(i),
				common.HexToAddress("0xfrom"),
				nil,
			)
			if !bus.Publish(txEvent) {
				t.Errorf("failed to publish tx %d in block %d", i, blockNum)
			}
		}
	}

	// Wait for all events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify block subscriber received all blocks
	blockCount := 0
	timeout := time.After(1 * time.Second)
drainBlockSub:
	for {
		select {
		case <-blockSub.Channel:
			blockCount++
		case <-timeout:
			break drainBlockSub
		default:
			if blockCount == totalBlocks {
				break drainBlockSub
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	if blockCount != totalBlocks {
		t.Errorf("block subscriber: expected %d blocks, got %d", totalBlocks, blockCount)
	}

	// Verify transaction subscriber received all transactions
	txCount := 0
	timeout = time.After(1 * time.Second)
drainTxSub:
	for {
		select {
		case <-txSub.Channel:
			txCount++
		case <-timeout:
			break drainTxSub
		default:
			if txCount == totalTransactions {
				break drainTxSub
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	if txCount != totalTransactions {
		t.Errorf("tx subscriber: expected %d transactions, got %d", totalTransactions, txCount)
	}

	// Verify all-events subscriber received everything
	allCount := 0
	timeout = time.After(1 * time.Second)
drainAllSub:
	for {
		select {
		case <-allSub.Channel:
			allCount++
		case <-timeout:
			break drainAllSub
		default:
			if allCount == totalBlocks+totalTransactions {
				break drainAllSub
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	expectedAll := totalBlocks + totalTransactions
	if allCount != expectedAll {
		t.Errorf("all subscriber: expected %d events, got %d", expectedAll, allCount)
	}
}

// TestIntegration_HighThroughput tests the system under high load
func TestIntegration_HighThroughput(t *testing.T) {
	// Create event bus with larger buffers
	bus := NewEventBus(10000, 1000)
	go bus.Run()
	defer bus.Stop()

	// Create many subscribers
	subscriberCount := 50
	subscribers := make([]*Subscription, subscriberCount)
	for i := 0; i < subscriberCount; i++ {
		id := SubscriptionID(string(rune('A' + (i % 26))) + string(rune('0' + (i / 26))))
		subscribers[i] = bus.Subscribe(id, []EventType{EventTypeBlock}, 1000)
	}

	time.Sleep(50 * time.Millisecond)

	// Publish many events
	eventCount := 1000
	start := time.Now()

	for i := 0; i < eventCount; i++ {
		header := &types.Header{Number: big.NewInt(int64(i))}
		block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
		event := NewBlockEvent(block)
		bus.Publish(event)
	}

	publishDuration := time.Since(start)

	// Wait for delivery
	time.Sleep(500 * time.Millisecond)

	// Check statistics
	totalEvents, totalDeliveries, droppedEvents := bus.Stats()

	t.Logf("High throughput test results:")
	t.Logf("  Subscribers: %d", subscriberCount)
	t.Logf("  Events published: %d", eventCount)
	t.Logf("  Total events: %d", totalEvents)
	t.Logf("  Total deliveries: %d", totalDeliveries)
	t.Logf("  Dropped events: %d", droppedEvents)
	t.Logf("  Publish duration: %v", publishDuration)
	t.Logf("  Throughput: %.2f events/sec", float64(eventCount)/publishDuration.Seconds())

	if totalEvents != uint64(eventCount) {
		t.Errorf("expected %d total events, got %d", eventCount, totalEvents)
	}

	// Expected deliveries = events × subscribers (if no drops)
	expectedDeliveries := uint64(eventCount * subscriberCount)
	if totalDeliveries < expectedDeliveries-uint64(subscriberCount) { // Allow some margin
		t.Errorf("expected ~%d deliveries, got %d (dropped: %d)",
			expectedDeliveries, totalDeliveries, droppedEvents)
	}

	// Verify throughput is reasonable (should handle >1000 events/sec)
	throughput := float64(eventCount) / publishDuration.Seconds()
	if throughput < 1000 {
		t.Errorf("throughput too low: %.2f events/sec (expected >1000)", throughput)
	}
}

// TestIntegration_DynamicSubscriptions tests adding/removing subscribers during operation
func TestIntegration_DynamicSubscriptions(t *testing.T) {
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	// Start with initial subscribers
	sub1 := bus.Subscribe("sub1", []EventType{EventTypeBlock}, 100)
	sub2 := bus.Subscribe("sub2", []EventType{EventTypeBlock}, 100)

	time.Sleep(10 * time.Millisecond)

	// Publish some events
	for i := 0; i < 5; i++ {
		header := &types.Header{Number: big.NewInt(int64(i))}
		block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
		event := NewBlockEvent(block)
		bus.Publish(event)
	}

	time.Sleep(50 * time.Millisecond)

	// Add new subscriber mid-stream
	sub3 := bus.Subscribe("sub3", []EventType{EventTypeBlock}, 100)

	time.Sleep(10 * time.Millisecond)

	// Publish more events
	for i := 5; i < 10; i++ {
		header := &types.Header{Number: big.NewInt(int64(i))}
		block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
		event := NewBlockEvent(block)
		bus.Publish(event)
	}

	time.Sleep(50 * time.Millisecond)

	// Remove a subscriber
	bus.Unsubscribe("sub2")

	time.Sleep(10 * time.Millisecond)

	// Publish final batch
	for i := 10; i < 15; i++ {
		header := &types.Header{Number: big.NewInt(int64(i))}
		block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
		event := NewBlockEvent(block)
		bus.Publish(event)
	}

	time.Sleep(50 * time.Millisecond)

	// Count received events
	count1 := drainChannel(sub1.Channel)
	count2 := drainChannel(sub2.Channel)
	count3 := drainChannel(sub3.Channel)

	t.Logf("Dynamic subscriptions test results:")
	t.Logf("  sub1 received: %d events (expected 15)", count1)
	t.Logf("  sub2 received: %d events (expected 10, unsubscribed after)", count2)
	t.Logf("  sub3 received: %d events (expected 10, subscribed after first 5)", count3)

	// sub1 should receive all 15 events
	if count1 != 15 {
		t.Errorf("sub1: expected 15 events, got %d", count1)
	}

	// sub2 should receive first 10 events (before unsubscribe)
	if count2 != 10 {
		t.Errorf("sub2: expected 10 events, got %d", count2)
	}

	// sub3 should receive last 10 events (subscribed after first 5)
	if count3 != 10 {
		t.Errorf("sub3: expected 10 events, got %d", count3)
	}
}

// TestIntegration_ConcurrentPublishSubscribe tests concurrent operations
func TestIntegration_ConcurrentPublishSubscribe(t *testing.T) {
	bus := NewEventBus(10000, 1000)
	go bus.Run()
	defer bus.Stop()

	var wg sync.WaitGroup
	subscriberCount := 20
	eventCount := 100

	// Create subscribers concurrently
	wg.Add(subscriberCount)
	for i := 0; i < subscriberCount; i++ {
		go func(id int) {
			defer wg.Done()
			subID := SubscriptionID(string(rune('A' + (id % 26))) + string(rune('0' + (id / 26))))
			bus.Subscribe(subID, []EventType{EventTypeBlock, EventTypeTransaction}, 500)
		}(i)
	}

	// Publish events concurrently while subscribing
	wg.Add(eventCount)
	for i := 0; i < eventCount; i++ {
		go func(num int) {
			defer wg.Done()
			if num%2 == 0 {
				// Block event
				header := &types.Header{Number: big.NewInt(int64(num))}
				block := types.NewBlock(header, &types.Body{}, nil, trie.NewStackTrie(nil))
				event := NewBlockEvent(block)
				bus.Publish(event)
			} else {
				// Transaction event
				tx := types.NewTransaction(uint64(num), common.HexToAddress("0x1"), big.NewInt(100), 21000, big.NewInt(1), nil)
				event := NewTransactionEvent(tx, uint64(num), common.Hash{}, 0, common.Address{}, nil)
				bus.Publish(event)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Verify system stability
	if count := bus.SubscriberCount(); count != subscriberCount {
		t.Errorf("expected %d subscribers, got %d", subscriberCount, count)
	}

	totalEvents, totalDeliveries, droppedEvents := bus.Stats()
	t.Logf("Concurrent operations test results:")
	t.Logf("  Subscribers: %d", subscriberCount)
	t.Logf("  Total events: %d", totalEvents)
	t.Logf("  Total deliveries: %d", totalDeliveries)
	t.Logf("  Dropped events: %d", droppedEvents)

	if totalEvents != uint64(eventCount) {
		t.Errorf("expected %d total events, got %d", eventCount, totalEvents)
	}
}

// drainChannel drains all events from a channel and returns the count
func drainChannel(ch chan Event) int {
	count := 0
	timeout := time.After(100 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				// Channel closed
				return count
			}
			count++
		case <-timeout:
			return count
		}
	}
}
