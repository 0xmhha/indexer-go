package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/features/agg"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
	"github.com/0xmhha/indexer-go/pkg/features/dex/orderbook"
)

// dexReorgFeatures are every DEX feature and the aggregates.
var dexReorgFeatures = []string{orderbook.Name, agg.CandlesName, agg.TimeSeriesName}

func startDEXReorgApp(t *testing.T, srv *testchain.Server, dir string, sc *testchain.DEXScenario, more ...func(*config.Config)) *App {
	t.Helper()
	return startAggApp(t, srv, dir, sc, dexReorgFeatures, []string{agg.SeriesChain, agg.SeriesDex, agg.SeriesToken}, more...)
}

// bookState is what a book holds, for comparison.
func bookState(t *testing.T, b *orderbook.Book) string {
	t.Helper()
	d := b.Depth(0, 0)
	out, err := json.Marshal(struct {
		Market port.DexMarket
		Ticks  []*port.DexTick
		Orders []*port.DexOrder
		Depth  orderbook.Depth
	}{b.Market, b.Ticks, b.Orders, d})
	require.NoError(t, err)
	return string(out)
}

// tradesAbove returns the positions (block, log index) of the stored trades
// in blocks keep+1..head, newest first: the trades a rollback to keep
// withdraws, in order.
func tradesAbove(t *testing.T, ctx context.Context, s dexStore, keep, head uint64) [][2]uint64 {
	t.Helper()
	var out [][2]uint64
	for b := head; b > keep; b-- {
		trades, err := s.ListDexTradesInBlock(ctx, b)
		require.NoError(t, err)
		for i := len(trades) - 1; i >= 0; i-- {
			out = append(out, [2]uint64{trades[i].BlockNumber, uint64(trades[i].LogIndex)})
		}
	}
	return out
}

// TestDEXReorg (refactoring plan R5-4): the live loop indexes the DEX
// scenario, the chain replaces the blocks after the perpetual orders with a
// competing branch (other fills, a cancel, another swap, a new V3
// position), and the indexer must end with exactly the storage of a
// database that indexed the new chain from scratch: markets, trades,
// orders, ticks, open orders, candles and series. dexTrade subscribers get
// every removed trade again with removed set, newest first, after the
// reorg event and before the new branch's trades; the order books follow.
func TestDEXReorg(t *testing.T) {
	sc := testchain.BuildDEX()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := filepath.Join(t.TempDir(), "db")
	app := startDEXReorgApp(t, srv, dir, sc)
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	s := app.storage.(dexStore)

	sc.Fork()
	newHead := sc.Chain.Head()
	removedWant := tradesAbove(t, ctx, s, 4, 8)
	require.Len(t, removedWant, 4, "the operator's three fills and the last V3 swap")
	outboxBefore, err := s.ReadOutbox(ctx, 0, 100_000)
	require.NoError(t, err)

	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(loopCtx) }()
	require.Eventually(t, func() bool {
		b, err := app.storage.GetBlock(ctx, newHead)
		return err == nil && b.Hash == sc.Chain.Block(newHead).Block.Hash()
	}, time.Minute, 20*time.Millisecond, "the new branch is indexed")

	// The order books follow the reorganization (reorg and dexMarket events,
	// delivered by the relay while the loop runs).
	perp := port.DexMarketKey{Address: sc.OrderManager, ID: sc.PerpMarket}
	settled := map[port.DexMarketKey]func(b *orderbook.Book) bool{
		perp:                 func(b *orderbook.Book) bool { return b.Height >= 5 && len(b.Orders) == 0 }, // filled and cancelled
		{Address: sc.V2Pair}: func(b *orderbook.Book) bool { return b.Market.Reserve0 != nil && b.Market.Reserve0.Int64() == 10050 },
		{Address: sc.V3Pool}: func(b *orderbook.Book) bool { return len(b.Ticks) == 4 }, // the new position's bounds too
	}
	reorged := map[port.DexMarketKey]string{}
	for k, done := range settled {
		require.Eventually(t, func() bool { b, _ := app.orderBook.Book(k); return b != nil && done(b) }, 10*time.Second, 10*time.Millisecond,
			"the book of %s/%d follows the new branch", k.Address.Hex(), k.ID)
		b, _ := app.orderBook.Book(k)
		reorged[k] = bookState(t, b)
	}
	stop()
	<-done
	count, depth := app.fetcher.Reorgs()
	require.Equal(t, [2]uint64{1, 4}, [2]uint64{count, depth}, "one reorganization of four blocks")

	// The trades are the new chain's.
	requireTrades(t, ctx, s, sc, newHead)

	// Events after the indexing: the reorg event, every removed trade newest
	// first, then the new branch's trades.
	entries, err := s.ReadOutbox(ctx, outboxBefore[len(outboxBefore)-1].Seq, 100_000) // after the last
	require.NoError(t, err)
	var sawReorg bool
	var removed [][2]uint64
	var added int
	for _, e := range entries {
		switch e.Type {
		case string(events.EventTypeReorg):
			require.Empty(t, removed, "the reorg event comes first")
			sawReorg = true
		case string(dex.EventTypeTrade):
			var ev dex.TradeEvent
			require.NoError(t, json.Unmarshal(e.Data, &ev))
			if ev.Removed {
				require.True(t, sawReorg)
				require.Zero(t, added, "removals precede the new branch's trades")
				removed = append(removed, [2]uint64{ev.Trade.BlockNumber, uint64(ev.Trade.LogIndex)})
			} else {
				added++
			}
		}
	}
	assert.Equal(t, removedWant, removed)
	assert.Equal(t, 3, added, "the new branch's three trades")

	app.Shutdown()

	// Exactly the storage, aggregates and books of indexing the new chain.
	fresh := filepath.Join(t.TempDir(), "fresh")
	freshApp := startDEXReorgApp(t, srv, fresh, sc)
	require.NoError(t, freshApp.fetcher.FetchRange(ctx, 0, newHead))
	books, err := orderbook.NewService(freshApp.storage.(orderbook.Store), nil, orderbook.Settings{ReconcileInterval: "0"}, nil)
	require.NoError(t, err)
	require.NoError(t, books.Start(ctx))
	require.NoError(t, books.Sync(ctx))
	for k, state := range reorged {
		b, _ := books.Book(k)
		require.NotNil(t, b)
		assert.Equal(t, bookState(t, b), state, "book of %s/%d", k.Address.Hex(), k.ID)
	}
	books.Close()
	freshApp.Shutdown()
	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

// TestDEXReorgWithoutOutbox: with eventbus.outbox off the removed trades
// are published on the bus right after the rollback commits, newest first,
// before the new branch's trades.
func TestDEXReorgWithoutOutbox(t *testing.T) {
	sc := testchain.BuildDEX()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	app := startDEXReorgApp(t, srv, filepath.Join(t.TempDir(), "db"), sc, func(c *config.Config) { c.EventBus.Outbox = false })
	defer app.Shutdown()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	removedWant := tradesAbove(t, ctx, app.storage.(dexStore), 4, sc.Chain.Head())
	require.Len(t, removedWant, 4)

	sub := app.eventBus.Subscribe("test-trades", []events.EventType{dex.EventTypeTrade}, nil, 1000)
	require.NotNil(t, sub)
	sc.Fork()
	newHead := sc.Chain.Head()
	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(loopCtx) }()
	defer func() { stop(); <-done }()

	var removed [][2]uint64
	added := 0
	deadline := time.After(time.Minute)
	for added < 3 {
		select {
		case ev := <-sub.Channel:
			tr := ev.(*dex.TradeEvent)
			if tr.Removed {
				require.Zero(t, added, "removals precede the new branch's trades")
				removed = append(removed, [2]uint64{tr.Trade.BlockNumber, uint64(tr.Trade.LogIndex)})
			} else {
				require.LessOrEqual(t, tr.Trade.BlockNumber, newHead)
				added++
			}
		case <-deadline:
			t.Fatalf("got %d removed and %d new trades", len(removed), added)
		}
	}
	assert.Equal(t, removedWant, removed)
}
