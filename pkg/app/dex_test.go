package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// startDEXApp starts the app on dir with dex.pools and dex.trades over the
// scenario's venues; configure changes the configuration further.
func startDEXApp(t *testing.T, srv *testchain.Server, dir string, sc *testchain.DEXScenario, configure ...func(*config.Config)) *App {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, dir)
	cfg.API.Enabled = false
	on := true
	cfg.Features = map[string]config.FeatureConfig{dex.PoolsName: {Enabled: &on}, dex.TradesName: {Enabled: &on}}
	require.NoError(t, cfg.SetFeatureSettings(dex.PoolsName, dex.Settings{Venues: []dex.Venue{
		{Type: string(port.DexUniswapV3), Factory: sc.V3Factory.Hex()},
		{Type: string(port.DexUniswapV2), Factory: sc.V2Factory.Hex()},
		{Type: string(port.DexPerpOrderBook), Engine: sc.Engine.Hex(), OrderManager: sc.OrderManager.Hex()},
	}}))
	for _, c := range configure {
		c(cfg)
	}
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	return app
}

type dexStore interface {
	port.DexReader
	port.Outbox
}

// expectedTrades are the scenario's trades up to block to, as the store
// records them (newest first per market), keyed by market.
func expectedTrades(sc *testchain.DEXScenario, to uint64) map[port.DexMarketKey][]testchain.DEXTrade {
	out := map[port.DexMarketKey][]testchain.DEXTrade{}
	for i := len(sc.Trades) - 1; i >= 0; i-- {
		tr := sc.Trades[i]
		if tr.Block <= to {
			k := port.DexMarketKey{Address: tr.Market, ID: tr.MarketID}
			out[k] = append(out[k], tr)
		}
	}
	return out
}

// requireTrades compares the stored trades of every market with the
// scenario's trades up to block to.
func requireTrades(t *testing.T, ctx context.Context, s dexStore, sc *testchain.DEXScenario, to uint64) {
	t.Helper()
	markets := []port.DexMarketKey{{Address: sc.V3Pool}, {Address: sc.V2Pair}, {Address: sc.OrderManager, ID: sc.PerpMarket}, {Address: sc.FakePool}}
	want := expectedTrades(sc, to)
	for _, m := range markets {
		got, _, err := s.ListDexTrades(ctx, m, port.FirstPage(100))
		require.NoError(t, err)
		require.Len(t, got, len(want[m]), "trades of %s/%d up to block %d", m.Address.Hex(), m.ID, to)
		for i, w := range want[m] {
			g := got[i]
			side := port.DexSell
			if w.Buy {
				side = port.DexBuy
			}
			assert.Equal(t, w.Block, g.BlockNumber, "trade %d of %s", i, m.Address.Hex())
			assert.Equal(t, port.DexVenue(w.Venue), g.Venue)
			assert.Equal(t, side, g.Side, "side of trade %d of %s", i, m.Address.Hex())
			assert.Equal(t, w.Base.String(), g.BaseAmount.String(), "base")
			assert.Equal(t, w.Quote.String(), g.QuoteAmount.String(), "quote")
			assert.Equal(t, w.Price.String(), g.Price.String(), "price")
			assert.Equal(t, w.Taker, g.Taker, "taker")
			assert.Equal(t, w.Maker, g.Maker, "maker")
		}
	}
}

// TestDEXTradesReproduced (refactoring plan R5-1): indexing a chain where a
// Uniswap V3 pool, a Uniswap V2 pair and a perpetual order book market trade
// records exactly the trades the scenario made, once each, with their side,
// amounts, price and parties; a foreign factory's pool is ignored; every
// trade's event is in the outbox with its block; rolling back removes the
// trades and the market state of the rolled-back blocks.
func TestDEXTradesReproduced(t *testing.T) {
	sc := testchain.BuildDEX()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	app := startDEXApp(t, srv, filepath.Join(t.TempDir(), "db"), sc)
	defer app.Shutdown()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	s := app.storage.(dexStore)

	requireTrades(t, ctx, s, sc, head)

	markets, _, err := s.ListDexMarkets(ctx, port.FirstPage(10))
	require.NoError(t, err)
	var keys []port.DexMarketKey
	for _, m := range markets {
		keys = append(keys, m.Key)
	}
	assert.Equal(t, []port.DexMarketKey{{Address: sc.V3Pool}, {Address: sc.V2Pair}, {Address: sc.OrderManager, ID: sc.PerpMarket}}, keys,
		"registered in chain order; the foreign factory's pool is not")
	pair, err := s.GetDexMarket(ctx, port.DexMarketKey{Address: sc.V2Pair})
	require.NoError(t, err)
	assert.Equal(t, "10100/19804", pair.Reserve0.String()+"/"+pair.Reserve1.String())
	pool, err := s.GetDexMarket(ctx, port.DexMarketKey{Address: sc.V3Pool})
	require.NoError(t, err)
	assert.Equal(t, int32(-3), pool.Tick, "the pool's tick after its last swap")
	ticks, err := s.ListDexTicks(ctx, pool.Key)
	require.NoError(t, err)
	require.Len(t, ticks, 2, "the bounds of the one position")
	assert.Equal(t, [2]int32{-600, 600}, [2]int32{ticks[0].Tick, ticks[1].Tick})
	assert.Equal(t, "1000000/-1000000", ticks[0].LiquidityNet.String()+"/"+ticks[1].LiquidityNet.String())

	byAlice, _, err := s.ListDexTradesByTrader(ctx, sc.Accounts[1].Address, port.FirstPage(100))
	require.NoError(t, err)
	assert.Len(t, byAlice, 4, "alice took four trades")
	liquidity, _, err := s.ListDexLiquidity(ctx, port.DexMarketKey{Address: sc.V3Pool}, port.FirstPage(10))
	require.NoError(t, err)
	assert.Len(t, liquidity, 1)
	orders, _, err := s.ListDexOrders(ctx, port.DexMarketKey{Address: sc.OrderManager, ID: sc.PerpMarket}, port.FirstPage(10))
	require.NoError(t, err)
	require.Len(t, orders, 3)
	assert.Equal(t, port.DexOrderFilled, orders[0].Status, "solo, filled by the operator")
	assert.Equal(t, port.DexOrderPartiallyFilled, orders[1].Status)

	// GraphQL answers what the store holds, a page at a time.
	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	q := `query($m: String!, $after: String) { dexTrades(market: $m, marketId: "7", pagination: {limit: 2, after: $after}) {
		nodes { side baseAmount quoteAmount price taker maker takerOrder makerOrder } pageInfo { hasNextPage endCursor } } }`
	var sides []string
	var after interface{}
	for {
		res := h.ExecuteQuery(q, map[string]interface{}{"m": sc.OrderManager.Hex(), "after": after})
		require.Empty(t, res.Errors)
		conn := res.Data.(map[string]interface{})["dexTrades"].(map[string]interface{})
		for _, n := range conn["nodes"].([]interface{}) {
			sides = append(sides, n.(map[string]interface{})["side"].(string))
		}
		info := conn["pageInfo"].(map[string]interface{})
		if !info["hasNextPage"].(bool) {
			break
		}
		after = info["endCursor"]
	}
	assert.Equal(t, []string{"sell", "buy", "buy"}, sides, "the perpetual trades, newest first, over two pages")
	res := h.ExecuteQuery(`{ dexMarkets { nodes { address venue } } }`, nil)
	require.Empty(t, res.Errors)
	assert.Len(t, res.Data.(map[string]interface{})["dexMarkets"].(map[string]interface{})["nodes"], 3)

	// Every trade's event is in the outbox once.
	entries, err := s.ReadOutbox(ctx, 0, 10_000)
	require.NoError(t, err)
	var eventTrades []common.Hash
	for _, e := range entries {
		if e.Type != string(dex.EventTypeTrade) {
			continue
		}
		var ev dex.TradeEvent
		require.NoError(t, json.Unmarshal(e.Data, &ev))
		eventTrades = append(eventTrades, ev.Trade.TxHash)
	}
	assert.Len(t, eventTrades, len(sc.Trades))

	// Roll back to the block before the operator's fills: their trades go,
	// and so do the fills on the orders; the pool's state returns too.
	keep := sc.Trades[2].Block - 1
	_, err = app.storage.(rollbacker).RollbackTo(ctx, keep, nil)
	require.NoError(t, err)
	requireTrades(t, ctx, s, sc, keep)
	order, err := s.GetDexOrder(ctx, sc.OrderManager, orders[2].ID)
	require.NoError(t, err)
	assert.Equal(t, port.DexOrderOpen, order.Status, "the fill was rolled back")
	assert.Zero(t, order.Filled.Sign())
	pool, err = s.GetDexMarket(ctx, port.DexMarketKey{Address: sc.V3Pool})
	require.NoError(t, err)
	assert.Equal(t, int32(19), pool.Tick, "the pool's tick after its first swap")

	// Indexing the rolled-back blocks again records the same trades.
	require.NoError(t, app.fetcher.FetchRange(ctx, keep+1, head))
	requireTrades(t, ctx, s, sc, head)
}
