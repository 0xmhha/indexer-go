package events

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// BenchmarkEventBus_Publish measures event publishing performance
func BenchmarkEventBus_Publish(b *testing.B) {
	bus := NewEventBus(10000, 100)
	go bus.Run()
	defer bus.Stop()

	// Create a test block event
	header := &types.Header{
		Number: big.NewInt(1000),
		Time:   uint64(time.Now().Unix()),
	}
	block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
	event := NewBlockEvent(block)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(event)
	}
}

// BenchmarkEventBus_PublishWithSubscribers measures publishing with active subscribers
func BenchmarkEventBus_PublishWithSubscribers(b *testing.B) {
	benchmarks := []struct {
		name       string
		numSubs    int
		bufferSize int
	}{
		{"10_subscribers", 10, 1000},
		{"100_subscribers", 100, 1000},
		{"1000_subscribers", 1000, 1000},
		{"10000_subscribers", 10000, 100}, // Smaller buffer for many subscribers
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			bus := NewEventBus(10000, 100)
			go bus.Run()
			defer bus.Stop()

			// Create subscribers
			subs := make([]*Subscription, bm.numSubs)
			for i := 0; i < bm.numSubs; i++ {
				sub := bus.Subscribe(
					SubscriptionID(string(rune(i))),
					[]EventType{EventTypeBlock},
					nil,
					bm.bufferSize,
				)
				subs[i] = sub

				// Start goroutine to drain channel
				go func(s *Subscription) {
					for range s.Channel {
						// Just drain
					}
				}(sub)
			}

			time.Sleep(10 * time.Millisecond) // Let subscriptions register

			// Create test event
			header := &types.Header{
				Number: big.NewInt(1000),
				Time:   uint64(time.Now().Unix()),
			}
			block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
			event := NewBlockEvent(block)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bus.Publish(event)
			}
			b.StopTimer()

			// Cleanup
			for _, sub := range subs {
				bus.Unsubscribe(sub.ID)
			}
		})
	}
}

// BenchmarkFilter_MatchTransaction measures filter matching performance
func BenchmarkFilter_MatchTransaction(b *testing.B) {
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	benchmarks := []struct {
		name   string
		filter *Filter
	}{
		{
			name:   "empty_filter",
			filter: NewFilter(),
		},
		{
			name: "single_address_filter",
			filter: &Filter{
				FromAddresses: []common.Address{addr1},
			},
		},
		{
			name: "multiple_address_filter",
			filter: &Filter{
				FromAddresses: []common.Address{addr1, addr2},
			},
		},
		{
			name: "value_range_filter",
			filter: &Filter{
				MinValue: big.NewInt(100),
				MaxValue: big.NewInt(10000),
			},
		},
		{
			name: "complex_filter",
			filter: &Filter{
				FromAddresses: []common.Address{addr1},
				ToAddresses:   []common.Address{addr2},
				MinValue:      big.NewInt(100),
				MaxValue:      big.NewInt(10000),
				FromBlock:     1000,
				ToBlock:       2000,
			},
		},
	}

	// Create test transaction event
	tx := types.NewTransaction(0, addr2, big.NewInt(500), 21000, big.NewInt(1), nil)
	txEvent := NewTransactionEvent(tx, 1500, common.Hash{}, 0, addr1, nil)

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				bm.filter.MatchTransaction(txEvent)
			}
		})
	}
}

// BenchmarkEventBus_FilteredSubscribers measures performance with filtered subscribers
func BenchmarkEventBus_FilteredSubscribers(b *testing.B) {
	benchmarks := []struct {
		name    string
		numSubs int
	}{
		{"10_filtered_subs", 10},
		{"100_filtered_subs", 100},
		{"1000_filtered_subs", 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			bus := NewEventBus(10000, 100)
			go bus.Run()
			defer bus.Stop()

			// Create filtered subscribers (each watching different address)
			subs := make([]*Subscription, bm.numSubs)
			for i := 0; i < bm.numSubs; i++ {
				addr := common.HexToAddress(string(rune(i)))
				filter := &Filter{
					FromAddresses: []common.Address{addr},
				}
				sub := bus.Subscribe(
					SubscriptionID(string(rune(i))),
					[]EventType{EventTypeTransaction},
					filter,
					1000,
				)
				subs[i] = sub

				// Drain channel
				go func(s *Subscription) {
					for range s.Channel {
					}
				}(sub)
			}

			time.Sleep(10 * time.Millisecond)

			// Create test event that matches first subscriber only
			addr0 := common.HexToAddress(string(rune(0)))
			tx := types.NewTransaction(0, addr0, big.NewInt(100), 21000, big.NewInt(1), nil)
			event := NewTransactionEvent(tx, 1000, common.Hash{}, 0, addr0, nil)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bus.Publish(event)
			}
			b.StopTimer()

			// Cleanup
			for _, sub := range subs {
				bus.Unsubscribe(sub.ID)
			}
		})
	}
}

// BenchmarkEventBus_ConcurrentPublish measures concurrent publishing performance
func BenchmarkEventBus_ConcurrentPublish(b *testing.B) {
	bus := NewEventBus(10000, 100)
	go bus.Run()
	defer bus.Stop()

	// Create one subscriber
	sub := bus.Subscribe("test-sub", []EventType{EventTypeBlock}, nil, 10000)
	go func() {
		for range sub.Channel {
		}
	}()

	time.Sleep(10 * time.Millisecond)

	// Create test event
	header := &types.Header{
		Number: big.NewInt(1000),
		Time:   uint64(time.Now().Unix()),
	}
	block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
	event := NewBlockEvent(block)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bus.Publish(event)
		}
	})
	b.StopTimer()

	bus.Unsubscribe(sub.ID)
}

// BenchmarkEventBus_SubscribeUnsubscribe measures subscription management performance
func BenchmarkEventBus_SubscribeUnsubscribe(b *testing.B) {
	bus := NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		subID := SubscriptionID(string(rune(i % 1000)))
		sub := bus.Subscribe(subID, []EventType{EventTypeBlock}, nil, 100)
		if sub != nil {
			bus.Unsubscribe(subID)
		}
	}
}

// BenchmarkFilter_Clone measures filter cloning performance
func BenchmarkFilter_Clone(b *testing.B) {
	filter := &Filter{
		Addresses: []common.Address{
			common.HexToAddress("0x1111"),
			common.HexToAddress("0x2222"),
		},
		FromAddresses: []common.Address{
			common.HexToAddress("0x3333"),
		},
		ToAddresses: []common.Address{
			common.HexToAddress("0x4444"),
		},
		MinValue:  big.NewInt(100),
		MaxValue:  big.NewInt(10000),
		FromBlock: 1000,
		ToBlock:   2000,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter.Clone()
	}
}

// BenchmarkNewBlockEvent measures BlockEvent creation performance
func BenchmarkNewBlockEvent(b *testing.B) {
	header := &types.Header{
		Number: big.NewInt(1000),
		Time:   uint64(time.Now().Unix()),
	}

	// Create transactions
	txs := make([]*types.Transaction, 100)
	for i := 0; i < 100; i++ {
		txs[i] = types.NewTransaction(
			uint64(i),
			common.HexToAddress("0x1111"),
			big.NewInt(100),
			21000,
			big.NewInt(1),
			nil,
		)
	}

	block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewBlockEvent(block)
	}
}

// BenchmarkNewTransactionEvent measures TransactionEvent creation performance
func BenchmarkNewTransactionEvent(b *testing.B) {
	tx := types.NewTransaction(
		0,
		common.HexToAddress("0x1111"),
		big.NewInt(100),
		21000,
		big.NewInt(1),
		nil,
	)
	from := common.HexToAddress("0x2222")
	blockHash := common.HexToHash("0x3333")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewTransactionEvent(tx, 1000, blockHash, 0, from, nil)
	}
}
