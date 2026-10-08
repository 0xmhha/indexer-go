package app

import (
	"context"
	"math/big"
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
	"github.com/0xmhha/indexer-go/pkg/features/dex/orderbook"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

func e18(v int64) *big.Int { return new(big.Int).Mul(big.NewInt(v), port.DexPriceScale) }

// answerDEXState makes the test chain's contracts report the DEX scenario's
// state after its last block, as the real contracts would: the pair's
// reserves, the pool's price, liquidity, ticks and tick bitmap, and the two
// resting orders.
func answerDEXState(sc *testchain.DEXScenario, reserve1 *big.Int) {
	call := func(sig string, args ...int64) string {
		var a []*big.Int
		for _, v := range args {
			a = append(a, big.NewInt(v))
		}
		return common.Bytes2Hex(orderbook.CallData(sig, a...))
	}
	words := func(vs ...*big.Int) []byte {
		var out []byte
		for _, v := range vs {
			out = append(out, orderbook.Word(v)...)
		}
		return out
	}
	n := big.NewInt
	sc.Chain.SetContract(sc.V2Pair, testchain.ContractMock{call("getReserves()"): words(n(10100), reserve1, n(0))})
	l := n(1_000_000)
	q96 := new(big.Int).Lsh(n(1), 96)
	sc.Chain.SetContract(sc.V3Pool, testchain.ContractMock{
		call("slot0()"):               words(q96, n(-3), n(0), n(0), n(0), n(0), n(1)),
		call("liquidity()"):           words(l),
		call("ticks(int24)", -600):    words(l, l),
		call("ticks(int24)", 600):     words(l, new(big.Int).Neg(l)),
		call("tickBitmap(int16)", -1): words(new(big.Int).Lsh(n(1), 246)), // tick -600 (compressed -10)
		call("tickBitmap(int16)", 0):  words(new(big.Int).Lsh(n(1), 10)),  // tick 600
	})
	order := func(id, size, filled int64, price *big.Int) []byte {
		w := make([]*big.Int, 15)
		for i := range w {
			w[i] = n(0)
		}
		w[0], w[5], w[8], w[9], w[10] = n(id), n(2), n(size), n(filled), price // status 2: partially filled
		return words(w...)
	}
	sc.Chain.SetContract(sc.OrderManager, testchain.ContractMock{
		call("getOrder(bytes32)", 1): order(1, 10, 4, e18(50)),
		call("getOrder(bytes32)", 2): order(2, 10, 4, e18(51)),
	})
}

// TestDEXOrderBookMatchesChain (refactoring plan R5-2): the books built from
// the indexed DEX scenario follow each indexed block and equal the
// contracts' state on chain; a difference is reported.
func TestDEXOrderBookMatchesChain(t *testing.T) {
	sc := testchain.BuildDEX()
	answerDEXState(sc, big.NewInt(19804))
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cfg := func(c *config.Config) {
		on := true
		c.Features[orderbook.Name] = config.FeatureConfig{Enabled: &on}
		require.NoError(t, c.SetFeatureSettings(orderbook.Name, orderbook.Settings{StepBps: 100, Levels: 5, ReconcileInterval: "0"}))
	}
	app := startDEXApp(t, srv, filepath.Join(t.TempDir(), "db"), sc, cfg)
	defer app.Shutdown()
	require.NotNil(t, app.orderBook)
	books := app.orderBook
	perp := port.DexMarketKey{Address: sc.OrderManager, ID: sc.PerpMarket}

	// Up to block 4 the three orders rest: the books follow the blocks.
	ordersAt := sc.Trades[2].Block - 1
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, ordersAt))
	require.Eventually(t, func() bool {
		b, _ := books.Book(perp)
		return b != nil && len(b.Orders) == 3
	}, 10*time.Second, 10*time.Millisecond, "the order book after the orders")
	b, _ := books.Book(perp)
	d := b.Depth(0, 0)
	assert.Equal(t, []string{"51:10", "49:5"}, levelStrings(d.Bids))
	assert.Equal(t, []string{"50:10"}, levelStrings(d.Asks))

	require.NoError(t, app.fetcher.FetchRange(ctx, ordersAt+1, head))
	require.Eventually(t, func() bool {
		b, _ := books.Book(perp)
		return b != nil && len(b.Orders) == 2
	}, 10*time.Second, 10*time.Millisecond, "the order book after the fills")
	require.NoError(t, books.Sync(ctx))

	b, _ = books.Book(perp)
	assert.Equal(t, head, b.Height)
	d = b.Depth(0, 0)
	assert.Equal(t, []string{"51:6"}, levelStrings(d.Bids), "what remains of alice's order")
	assert.Equal(t, []string{"50:6"}, levelStrings(d.Asks), "what remains of bob's order")

	pair, _ := books.Book(port.DexMarketKey{Address: sc.V2Pair})
	d = pair.Depth(0, 0)
	assert.Equal(t, new(big.Int).Quo(e18(19804), big.NewInt(10100)).String(), d.Mid.String())
	assert.Len(t, d.Asks, 5)
	pool, _ := books.Book(port.DexMarketKey{Address: sc.V3Pool})
	assert.Len(t, pool.Ticks, 2)
	assert.Positive(t, pool.Depth(0, 0).Asks[0].Base.Sign(), "the position covers the next 1%")

	// Every book equals the chain at the indexed head.
	results := books.Reconcile(ctx)
	require.Len(t, results, 3, "the foreign factory's pool has no book")
	for k, r := range results {
		assert.True(t, r.InSync(), "%s/%d: %v %s", k.Address.Hex(), k.ID, r.Mismatches, r.Err)
		assert.Equal(t, head, r.Block)
	}

	// GraphQL serves the book with its comparison.
	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	res := h.ExecuteQuery(`query($m: String!) { dexOrderBook(market: $m, marketId: "7") {
		venue blockNumber midPrice bids { price baseAmount quoteAmount } asks { price baseAmount }
		reconciliation { blockNumber inSync mismatches error } } }`, map[string]interface{}{"m": sc.OrderManager.Hex()})
	require.Empty(t, res.Errors)
	book := res.Data.(map[string]interface{})["dexOrderBook"].(map[string]interface{})
	assert.Equal(t, "perp_orderbook", book["venue"])
	assert.Equal(t, new(big.Int).Rsh(e18(101), 1).String(), book["midPrice"], "between 50 and 51")
	assert.Equal(t, []interface{}{map[string]interface{}{"price": e18(51).String(), "baseAmount": "6", "quoteAmount": "306"}}, book["bids"])
	assert.Equal(t, true, book["reconciliation"].(map[string]interface{})["inSync"])
	res = h.ExecuteQuery(`{ dexOrderBook(market: "0x00000000000000000000000000000000000B2001", levels: 2, stepBps: 50) { stepBps asks { price } } }`, nil)
	require.Empty(t, res.Errors)
	assert.Len(t, res.Data.(map[string]interface{})["dexOrderBook"].(map[string]interface{})["asks"], 2)

	// A pair whose reserve differs on chain is reported.
	answerDEXState(sc, big.NewInt(19805))
	results = books.Reconcile(ctx)
	r := results[port.DexMarketKey{Address: sc.V2Pair}]
	require.NotNil(t, r)
	assert.Equal(t, []string{"reserve1: indexed 19804, chain 19805"}, r.Mismatches)
	assert.True(t, results[perp].InSync())
}

// levelStrings shows levels as "<price>:<base>" with whole prices.
func levelStrings(levels []orderbook.Level) []string {
	var out []string
	for _, l := range levels {
		out = append(out, new(big.Int).Quo(l.Price, port.DexPriceScale).String()+":"+l.Base.String())
	}
	return out
}
