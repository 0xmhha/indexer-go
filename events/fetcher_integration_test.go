package events

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// TestFetcherIntegration_EndToEnd tests the complete integration:
// Fetcher publishes events → EventBus delivers → Subscribers receive
func TestFetcherIntegration_EndToEnd(t *testing.T) {
	// Create EventBus
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	// Create subscriptions for block and transaction events
	blockSub := bus.Subscribe("block-subscriber", []EventType{EventTypeBlock}, nil, 100)
	if blockSub == nil {
		t.Fatal("failed to create block subscription")
	}

	txSub := bus.Subscribe("tx-subscriber", []EventType{EventTypeTransaction}, nil, 100)
	if txSub == nil {
		t.Fatal("failed to create transaction subscription")
	}

	time.Sleep(10 * time.Millisecond) // Let subscriptions register

	// Simulate what Fetcher does: create and publish events
	// Create a test block with 3 transactions
	header := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: common.HexToHash("0x1234"),
		Time:       uint64(time.Now().Unix()),
		GasLimit:   8000000,
		GasUsed:    150000,
		Difficulty: big.NewInt(1),
	}

	// Create 3 test transactions
	tx1 := types.NewTransaction(0, common.HexToAddress("0x1111"), big.NewInt(100), 21000, big.NewInt(1), nil)
	tx2 := types.NewTransaction(1, common.HexToAddress("0x2222"), big.NewInt(200), 21000, big.NewInt(1), nil)
	tx3 := types.NewTransaction(2, common.HexToAddress("0x3333"), big.NewInt(300), 21000, big.NewInt(1), nil)

	transactions := []*types.Transaction{tx1, tx2, tx3}

	block := types.NewBlock(header, transactions, nil, nil, trie.NewStackTrie(nil))

	// Publish BlockEvent (simulating Fetcher.FetchBlock behavior)
	blockEvent := NewBlockEvent(block)
	if !bus.Publish(blockEvent) {
		t.Fatal("failed to publish block event")
	}

	// Publish TransactionEvents for each transaction
	for i, tx := range transactions {
		txEvent := NewTransactionEvent(
			tx,
			block.NumberU64(),
			block.Hash(),
			uint(i),
			common.HexToAddress("0xaaaa"), // mock sender
			nil,                           // no receipt for this test
		)
		if !bus.Publish(txEvent) {
			t.Fatalf("failed to publish transaction event %d", i)
		}
	}

	// Verify block event delivery
	select {
	case receivedEvent := <-blockSub.Channel:
		if receivedEvent.Type() != EventTypeBlock {
			t.Errorf("expected block event, got %s", receivedEvent.Type())
		}
		blockEvt, ok := receivedEvent.(*BlockEvent)
		if !ok {
			t.Fatal("event is not a BlockEvent")
		}
		if blockEvt.Number != 100 {
			t.Errorf("expected block number 100, got %d", blockEvt.Number)
		}
		if blockEvt.TxCount != 3 {
			t.Errorf("expected 3 transactions, got %d", blockEvt.TxCount)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for block event")
	}

	// Verify transaction events delivery
	receivedTxCount := 0
	for i := 0; i < 3; i++ {
		select {
		case receivedEvent := <-txSub.Channel:
			if receivedEvent.Type() != EventTypeTransaction {
				t.Errorf("expected transaction event, got %s", receivedEvent.Type())
			}
			txEvt, ok := receivedEvent.(*TransactionEvent)
			if !ok {
				t.Fatal("event is not a TransactionEvent")
			}
			if txEvt.BlockNumber != 100 {
				t.Errorf("expected block number 100, got %d", txEvt.BlockNumber)
			}
			receivedTxCount++
		case <-time.After(1 * time.Second):
			t.Fatalf("timeout waiting for transaction event %d", i)
		}
	}

	if receivedTxCount != 3 {
		t.Errorf("expected 3 transaction events, got %d", receivedTxCount)
	}
}

// TestFetcherIntegration_FilteredSubscription tests that subscribers
// with filters only receive matching events
func TestFetcherIntegration_FilteredSubscription(t *testing.T) {
	// Create EventBus
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	addr1 := common.HexToAddress("0x1111")
	addr2 := common.HexToAddress("0x2222")
	addr3 := common.HexToAddress("0x3333")

	// Subscribe with filter for transactions from addr1
	filter1 := &Filter{
		FromAddresses: []common.Address{addr1},
	}
	filteredSub := bus.Subscribe("filtered-subscriber", []EventType{EventTypeTransaction}, filter1, 100)
	if filteredSub == nil {
		t.Fatal("failed to create filtered subscription")
	}

	// Subscribe without filter (receives all)
	allSub := bus.Subscribe("all-subscriber", []EventType{EventTypeTransaction}, nil, 100)
	if allSub == nil {
		t.Fatal("failed to create all subscription")
	}

	time.Sleep(10 * time.Millisecond)

	// Create test block
	header := &types.Header{
		Number: big.NewInt(200),
		Time:   uint64(time.Now().Unix()),
	}
	block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))

	// Publish 3 transactions with different senders
	senders := []common.Address{addr1, addr2, addr3}
	for i, sender := range senders {
		tx := types.NewTransaction(uint64(i), common.HexToAddress("0xffff"), big.NewInt(100), 21000, big.NewInt(1), nil)
		txEvent := NewTransactionEvent(
			tx,
			block.NumberU64(),
			block.Hash(),
			uint(i),
			sender,
			nil,
		)
		if !bus.Publish(txEvent) {
			t.Fatalf("failed to publish transaction event %d", i)
		}
	}

	// Filtered subscriber should receive only 1 transaction (from addr1)
	receivedFiltered := 0
	timeout := time.After(500 * time.Millisecond)
Loop1:
	for {
		select {
		case event := <-filteredSub.Channel:
			txEvt := event.(*TransactionEvent)
			if txEvt.From != addr1 {
				t.Errorf("filtered subscriber received wrong transaction, from=%s", txEvt.From.Hex())
			}
			receivedFiltered++
		case <-timeout:
			break Loop1
		}
	}

	if receivedFiltered != 1 {
		t.Errorf("filtered subscriber expected 1 event, got %d", receivedFiltered)
	}

	// Unfiltered subscriber should receive all 3 transactions
	receivedAll := 0
	timeout = time.After(500 * time.Millisecond)
Loop2:
	for {
		select {
		case <-allSub.Channel:
			receivedAll++
		case <-timeout:
			break Loop2
		}
	}

	if receivedAll != 3 {
		t.Errorf("all subscriber expected 3 events, got %d", receivedAll)
	}
}

// TestFetcherIntegration_MultipleBlocks tests event delivery for multiple blocks
func TestFetcherIntegration_MultipleBlocks(t *testing.T) {
	// Create EventBus
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	// Create subscription
	blockSub := bus.Subscribe("block-subscriber", []EventType{EventTypeBlock}, nil, 100)
	if blockSub == nil {
		t.Fatal("failed to create block subscription")
	}

	time.Sleep(10 * time.Millisecond)

	// Publish 5 blocks (simulating sequential fetching)
	numBlocks := 5
	for i := 0; i < numBlocks; i++ {
		header := &types.Header{
			Number: big.NewInt(int64(100 + i)),
			Time:   uint64(time.Now().Unix()),
		}
		block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
		blockEvent := NewBlockEvent(block)

		if !bus.Publish(blockEvent) {
			t.Fatalf("failed to publish block %d", i)
		}
	}

	// Verify all blocks received in order
	for i := 0; i < numBlocks; i++ {
		select {
		case receivedEvent := <-blockSub.Channel:
			blockEvt := receivedEvent.(*BlockEvent)
			expectedNumber := uint64(100 + i)
			if blockEvt.Number != expectedNumber {
				t.Errorf("expected block %d, got %d", expectedNumber, blockEvt.Number)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("timeout waiting for block %d", i)
		}
	}
}

// TestFetcherIntegration_ConcurrentSubscribers tests multiple subscribers
// receiving events concurrently
func TestFetcherIntegration_ConcurrentSubscribers(t *testing.T) {
	// Create EventBus
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	// Create 10 concurrent subscribers
	numSubscribers := 10
	subs := make([]*Subscription, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		sub := bus.Subscribe(
			SubscriptionID(string(rune('A'+i))),
			[]EventType{EventTypeBlock},
			nil,
			100,
		)
		if sub == nil {
			t.Fatalf("failed to create subscription %d", i)
		}
		subs[i] = sub
	}

	time.Sleep(10 * time.Millisecond)

	// Publish 3 blocks
	numBlocks := 3
	for i := 0; i < numBlocks; i++ {
		header := &types.Header{
			Number: big.NewInt(int64(300 + i)),
			Time:   uint64(time.Now().Unix()),
		}
		block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
		blockEvent := NewBlockEvent(block)

		if !bus.Publish(blockEvent) {
			t.Fatalf("failed to publish block %d", i)
		}
	}

	// Verify each subscriber received all blocks
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for subIdx, sub := range subs {
		receivedCount := 0
		for blockIdx := 0; blockIdx < numBlocks; blockIdx++ {
			select {
			case <-sub.Channel:
				receivedCount++
			case <-ctx.Done():
				t.Fatalf("subscriber %d only received %d/%d blocks", subIdx, receivedCount, numBlocks)
			}
		}
		if receivedCount != numBlocks {
			t.Errorf("subscriber %d expected %d blocks, got %d", subIdx, numBlocks, receivedCount)
		}
	}

	// Verify stats
	totalEvents, totalDeliveries, droppedEvents := bus.Stats()
	if totalEvents != uint64(numBlocks) {
		t.Errorf("expected %d total events, got %d", numBlocks, totalEvents)
	}

	expectedDeliveries := uint64(numBlocks * numSubscribers)
	if totalDeliveries != expectedDeliveries {
		t.Errorf("expected %d total deliveries, got %d", expectedDeliveries, totalDeliveries)
	}

	if droppedEvents != 0 {
		t.Errorf("expected 0 dropped events, got %d", droppedEvents)
	}
}
