package porttest

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Addresses of the DEX fixtures.
var (
	dexFactory = common.HexToAddress("0x000000000000000000000000000000000000de01")
	dexManager = common.HexToAddress("0x000000000000000000000000000000000000de02")
	dexTokenA  = common.HexToAddress("0x000000000000000000000000000000000000de0a")
	dexTokenB  = common.HexToAddress("0x000000000000000000000000000000000000de0b")
	dexAlice   = common.HexToAddress("0x000000000000000000000000000000000000de11")
	dexBob     = common.HexToAddress("0x000000000000000000000000000000000000de12")
)

// dexPool returns the key of fixture pool n.
func dexPool(n uint64) port.DexMarketKey {
	return port.DexMarketKey{Address: common.BigToAddress(new(big.Int).SetUint64(0xde00_0000 + n))}
}

// sameJSON asserts two records encode alike (big integers compare by
// value, not by representation).
func sameJSON(t *testing.T, want, got any, msg ...any) {
	t.Helper()
	w, err := json.Marshal(want)
	require.NoError(t, err)
	g, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, string(w), string(g), msg...)
}

func dexTrade(market port.DexMarketKey, block uint64, logIndex uint, taker, maker common.Address) *port.DexTrade {
	return &port.DexTrade{
		Market: market, Venue: port.DexUniswapV3, BlockNumber: block, TxHash: fixtureHash("dex-trade", block),
		LogIndex: logIndex, Timestamp: 1_700_000_000 + block, Side: port.DexBuy,
		BaseAmount: big.NewInt(int64(block)), QuoteAmount: big.NewInt(int64(2 * block)),
		Price: new(big.Int).Mul(big.NewInt(2), port.DexPriceScale), Taker: taker, Sender: dexFactory, Maker: maker,
		SqrtPriceX96: new(big.Int).Lsh(big.NewInt(1), 96), Tick: -7,
	}
}

// testDex checks DexReader and DexWriter: markets are found by key and
// listed in registration order; trades are listed per market and per
// trader (taker and maker, once each) newest first; liquidity changes per
// market newest first; orders are found by manager and id and listed per
// market newest first, resting orders oldest first while they rest; ticks
// are listed per pool in tick order until their gross liquidity is zero;
// writing a market, order or tick again replaces it.
func testDex(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[dexStore](t, newStore)
		_, err := s.GetDexMarket(ctx, dexPool(1))
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetDexOrder(ctx, dexManager, fixtureHash("order", 1))
		assert.ErrorIs(t, err, port.ErrNotFound)
		markets, next, err := s.ListDexMarkets(ctx, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, markets)
		assert.Empty(t, next)
		trades, next, err := s.ListDexTrades(ctx, dexPool(1), port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, trades)
		assert.Empty(t, next)
		trades, _, err = s.ListDexTradesByTrader(ctx, dexAlice, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, trades)
	})

	t.Run("Markets", func(t *testing.T) {
		s := open[dexStore](t, newStore)
		var want []*port.DexMarket
		// Registered out of key order: listed by registration.
		for i, n := range []uint64{3, 1, 2, 4} {
			m := &port.DexMarket{
				Key: dexPool(n), Venue: port.DexUniswapV3, Creator: dexFactory, Base: dexTokenA, Quote: dexTokenB,
				Fee: 3000, TickSpacing: 60, CreatedBlock: 10 + uint64(i), CreatedTx: fixtureHash("pool", n), CreatedLogIndex: uint(i),
			}
			require.NoError(t, s.SaveDexMarket(ctx, m))
			want = append(want, m)
		}
		perp := &port.DexMarket{
			Key: port.DexMarketKey{Address: dexManager, ID: 7}, Venue: port.DexPerpOrderBook, Creator: dexFactory,
			Quote: dexTokenB, CreatedBlock: 20, CreatedTx: fixtureHash("market", 7),
		}
		require.NoError(t, s.SaveDexMarket(ctx, perp))
		want = append(want, perp)

		got, err := s.GetDexMarket(ctx, dexPool(1))
		require.NoError(t, err)
		sameJSON(t, want[1], got)
		got, err = s.GetDexMarket(ctx, perp.Key)
		require.NoError(t, err)
		sameJSON(t, perp, got)
		_, err = s.GetDexMarket(ctx, port.DexMarketKey{Address: dexManager, ID: 8})
		assert.ErrorIs(t, err, port.ErrNotFound, "another market of the same contract")

		// The state is replaced, the registration kept.
		updated := *want[0]
		updated.SqrtPriceX96, updated.Tick, updated.Liquidity, updated.UpdatedBlock = big.NewInt(12345), 42, big.NewInt(99), 30
		require.NoError(t, s.SaveDexMarket(ctx, &updated))
		want[0] = &updated
		got, err = s.GetDexMarket(ctx, updated.Key)
		require.NoError(t, err)
		sameJSON(t, &updated, got)

		key := func(m *port.DexMarket) port.DexMarketKey { return m.Key }
		checkPaging(t, want, key, func(page port.Page) ([]*port.DexMarket, string, error) { return s.ListDexMarkets(ctx, page) })
	})

	t.Run("Trades", func(t *testing.T) {
		s := open[dexStore](t, newStore)
		var pool1, alice []*port.DexTrade
		// Saved oldest first, two in block 5 and one in another pool.
		for _, tr := range []*port.DexTrade{
			dexTrade(dexPool(1), 4, 0, dexAlice, common.Address{}),
			dexTrade(dexPool(1), 5, 1, dexBob, dexAlice),
			dexTrade(dexPool(2), 5, 2, dexAlice, dexAlice),
			dexTrade(dexPool(1), 5, 3, dexBob, common.Address{}),
			dexTrade(dexPool(1), 9, 0, dexAlice, dexBob),
		} {
			require.NoError(t, s.SaveDexTrade(ctx, tr))
			if tr.Market == dexPool(1) {
				pool1 = append([]*port.DexTrade{tr}, pool1...)
			}
			if tr.Taker == dexAlice || tr.Maker == dexAlice {
				alice = append([]*port.DexTrade{tr}, alice...)
			}
		}
		// Writing a trade again replaces it.
		require.NoError(t, s.SaveDexTrade(ctx, pool1[0]))

		page, _, err := s.ListDexTrades(ctx, dexPool(1), port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, page, len(pool1))
		for i := range pool1 {
			sameJSON(t, pool1[i], page[i], "trade %d", i)
		}
		key := func(tr *port.DexTrade) [2]uint64 { return [2]uint64{tr.BlockNumber, uint64(tr.LogIndex)} }
		checkPaging(t, pool1, key, func(page port.Page) ([]*port.DexTrade, string, error) {
			return s.ListDexTrades(ctx, dexPool(1), page)
		})
		checkPaging(t, alice, key, func(page port.Page) ([]*port.DexTrade, string, error) {
			return s.ListDexTradesByTrader(ctx, dexAlice, page)
		})
		checkCursorFromOtherList(t,
			func(page port.Page) ([]*port.DexTrade, string, error) { return s.ListDexTrades(ctx, dexPool(1), page) },
			func(page port.Page) ([]*port.DexTrade, string, error) {
				return s.ListDexTradesByTrader(ctx, dexAlice, page)
			})
	})

	t.Run("Liquidity", func(t *testing.T) {
		s := open[dexStore](t, newStore)
		var want []*port.DexLiquidity
		for i, block := range []uint64{3, 3, 6, 8} {
			l := &port.DexLiquidity{
				Market: dexPool(1), Venue: port.DexUniswapV3, BlockNumber: block, TxHash: fixtureHash("liq", uint64(i)),
				LogIndex: uint(i), Timestamp: 1_700_000_000 + block, Kind: port.DexAddLiquidity, Owner: dexAlice,
				Amount0: big.NewInt(int64(100 + i)), Amount1: big.NewInt(int64(200 + i)), TickLower: -120, TickUpper: 120,
				Liquidity: big.NewInt(int64(1000 + i)),
			}
			if i == 3 {
				l.Kind = port.DexRemoveLiquidity
			}
			require.NoError(t, s.SaveDexLiquidity(ctx, l))
			want = append([]*port.DexLiquidity{l}, want...)
		}
		other := *want[0]
		other.Market, other.LogIndex = dexPool(2), 9 // a log of another pool
		require.NoError(t, s.SaveDexLiquidity(ctx, &other))

		page, _, err := s.ListDexLiquidity(ctx, dexPool(1), port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, page, len(want))
		for i := range want {
			sameJSON(t, want[i], page[i], "change %d", i)
		}
		checkPaging(t, want, func(l *port.DexLiquidity) uint { return l.LogIndex },
			func(page port.Page) ([]*port.DexLiquidity, string, error) {
				return s.ListDexLiquidity(ctx, dexPool(1), page)
			})
	})

	t.Run("Orders", func(t *testing.T) {
		s := open[dexStore](t, newStore)
		market := port.DexMarketKey{Address: dexManager, ID: 7}
		var want []*port.DexOrder
		for i := range uint64(4) {
			o := &port.DexOrder{
				Market: market, ID: fixtureHash("order", i), Trader: dexAlice, Side: port.DexSell, Type: 1,
				Size: big.NewInt(int64(10 + i)), Price: new(big.Int).Mul(big.NewInt(int64(50+i)), port.DexPriceScale),
				Filled: big.NewInt(0), Status: port.DexOrderOpen,
				CreatedBlock: 3 + i/2, CreatedTx: fixtureHash("order-tx", i), CreatedLogIndex: uint(i), UpdatedBlock: 3 + i/2,
			}
			require.NoError(t, s.SaveDexOrder(ctx, o))
			want = append([]*port.DexOrder{o}, want...)
		}
		otherMarket := *want[0]
		otherMarket.Market.ID, otherMarket.ID = 8, fixtureHash("order", 99)
		require.NoError(t, s.SaveDexOrder(ctx, &otherMarket))

		// A fill replaces the order and keeps its place.
		filled := *want[2]
		filled.Filled, filled.Status, filled.UpdatedBlock = big.NewInt(5), port.DexOrderPartiallyFilled, 9
		require.NoError(t, s.SaveDexOrder(ctx, &filled))
		want[2] = &filled

		got, err := s.GetDexOrder(ctx, dexManager, filled.ID)
		require.NoError(t, err)
		sameJSON(t, &filled, got)
		_, err = s.GetDexOrder(ctx, dexFactory, filled.ID)
		assert.ErrorIs(t, err, port.ErrNotFound, "the same id at another manager")

		page, _, err := s.ListDexOrders(ctx, market, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, page, len(want))
		for i := range want {
			sameJSON(t, want[i], page[i], "order %d", i)
		}
		checkPaging(t, want, func(o *port.DexOrder) common.Hash { return o.ID },
			func(page port.Page) ([]*port.DexOrder, string, error) { return s.ListDexOrders(ctx, market, page) })
	})

	t.Run("OpenOrders", func(t *testing.T) {
		s := open[dexStore](t, newStore)
		market := port.DexMarketKey{Address: dexManager, ID: 7}
		var orders []*port.DexOrder
		for i, status := range []port.DexOrderStatus{port.DexOrderOpen, port.DexOrderPending, port.DexOrderOpen, port.DexOrderPartiallyFilled, port.DexOrderOpen} {
			o := &port.DexOrder{
				Market: market, ID: fixtureHash("open", uint64(i)), Trader: dexBob, Side: port.DexBuy, Type: 1,
				Size: big.NewInt(10), Price: port.DexPriceScale, Filled: big.NewInt(0), Status: status,
				CreatedBlock: 3 + uint64(i), CreatedTx: fixtureHash("open-tx", uint64(i)), UpdatedBlock: 3 + uint64(i),
			}
			require.NoError(t, s.SaveDexOrder(ctx, o))
			orders = append(orders, o)
		}
		other := *orders[0]
		other.Market.ID, other.ID = 8, fixtureHash("open", 99)
		require.NoError(t, s.SaveDexOrder(ctx, &other))

		// Order 2 fills, order 4 is cancelled, the pending order 1 triggers.
		for i, status := range map[int]port.DexOrderStatus{2: port.DexOrderFilled, 4: port.DexOrderCancelled, 1: port.DexOrderOpen} {
			changed := *orders[i]
			changed.Status = status
			require.NoError(t, s.SaveDexOrder(ctx, &changed))
			orders[i] = &changed
		}
		want := []*port.DexOrder{orders[0], orders[1], orders[3]}
		page, _, err := s.ListDexOpenOrders(ctx, market, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, page, len(want))
		for i := range want {
			sameJSON(t, want[i], page[i], "open order %d", i)
		}
		checkPaging(t, want, func(o *port.DexOrder) common.Hash { return o.ID },
			func(page port.Page) ([]*port.DexOrder, string, error) { return s.ListDexOpenOrders(ctx, market, page) })
		all, _, err := s.ListDexOrders(ctx, market, port.FirstPage(10))
		require.NoError(t, err)
		assert.Len(t, all, len(orders), "every order stays listed")
	})

	t.Run("Ticks", func(t *testing.T) {
		s := open[dexStore](t, newStore)
		tick := func(market port.DexMarketKey, i int32, gross, net int64) *port.DexTick {
			return &port.DexTick{Market: market, Tick: i, LiquidityGross: big.NewInt(gross), LiquidityNet: big.NewInt(net)}
		}
		empty, err := s.ListDexTicks(ctx, dexPool(1))
		require.NoError(t, err)
		assert.Empty(t, empty)
		// Saved out of order, negative ticks included; one of another pool.
		for _, tk := range []*port.DexTick{
			tick(dexPool(1), 600, 100, -100), tick(dexPool(1), -600, 100, 100), tick(dexPool(1), -887220, 5, 5),
			tick(dexPool(1), 0, 7, -7), tick(dexPool(2), 60, 1, 1), tick(dexPool(1), 887220, 5, -5),
		} {
			require.NoError(t, s.SaveDexTick(ctx, tk))
		}
		// Tick 0 changes, tick 600 is emptied.
		require.NoError(t, s.SaveDexTick(ctx, tick(dexPool(1), 0, 9, 3)))
		require.NoError(t, s.SaveDexTick(ctx, tick(dexPool(1), 600, 0, 0)))
		one, err := s.GetDexTick(ctx, dexPool(1), -600)
		require.NoError(t, err)
		sameJSON(t, tick(dexPool(1), -600, 100, 100), one)
		_, err = s.GetDexTick(ctx, dexPool(1), 600)
		assert.ErrorIs(t, err, port.ErrNotFound, "an emptied tick")
		_, err = s.GetDexTick(ctx, dexPool(2), 0)
		assert.ErrorIs(t, err, port.ErrNotFound)
		got, err := s.ListDexTicks(ctx, dexPool(1))
		require.NoError(t, err)
		want := []*port.DexTick{tick(dexPool(1), -887220, 5, 5), tick(dexPool(1), -600, 100, 100), tick(dexPool(1), 0, 9, 3), tick(dexPool(1), 887220, 5, -5)}
		require.Len(t, got, len(want))
		for i := range want {
			sameJSON(t, want[i], got[i], "tick %d", i)
		}
	})
}
