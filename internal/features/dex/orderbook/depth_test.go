package orderbook

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func e18(v int64) *big.Int { return new(big.Int).Mul(big.NewInt(v), port.DexPriceScale) }

// near asserts got is within 1e-9 of want, relatively.
func near(t *testing.T, want, got *big.Int, msg ...any) {
	t.Helper()
	if want.Sign() == 0 {
		assert.True(t, got.CmpAbs(big.NewInt(2)) <= 0, msg...)
		return
	}
	diff := new(big.Float).SetInt(new(big.Int).Sub(got, want))
	rel, _ := new(big.Float).Quo(diff, new(big.Float).SetInt(want)).Float64()
	assert.InDelta(t, 0, rel, 1e-9, msg...)
}

func sum(levels []Level) (base, quote *big.Int) {
	base, quote = new(big.Int), new(big.Int)
	for _, l := range levels {
		base.Add(base, l.Base)
		quote.Add(quote, l.Quote)
	}
	return base, quote
}

// TestV2Depth: taking every ask up to a level leaves the pair at the level's
// price, and every bid likewise; the pair's invariant holds.
func TestV2Depth(t *testing.T) {
	r0, r1 := e18(1_000_000), e18(2_000_000)
	d := v2Depth(r0, r1, 10, 20)
	assert.Equal(t, e18(2).String(), d.Mid.String())
	require.Len(t, d.Asks, 20)
	require.Len(t, d.Bids, 20)
	k := new(big.Int).Mul(r0, r1)
	for i := range d.Asks {
		base, quote := sum(d.Asks[:i+1])
		x, y := new(big.Int).Sub(r0, base), new(big.Int).Add(r1, quote)
		near(t, d.Asks[i].Price, new(big.Int).Quo(new(big.Int).Mul(y, port.DexPriceScale), x), "price after ask %d", i)
		near(t, k, new(big.Int).Mul(x, y), "invariant after ask %d", i)
		if i > 0 {
			assert.Equal(t, 1, d.Asks[i].Price.Cmp(d.Asks[i-1].Price), "asks rise")
		}
		base, quote = sum(d.Bids[:i+1])
		x, y = new(big.Int).Add(r0, base), new(big.Int).Sub(r1, quote)
		near(t, d.Bids[i].Price, new(big.Int).Quo(new(big.Int).Mul(y, port.DexPriceScale), x), "price after bid %d", i)
	}
	near(t, new(big.Int).Quo(new(big.Int).Mul(e18(2), big.NewInt(10_200)), big.NewInt(bps)), d.Asks[19].Price, "20 steps of 10 bps")

	assert.Empty(t, v2Depth(nil, r1, 10, 20).Asks, "an unfunded pair has no depth")
	assert.Len(t, v2Depth(r0, r1, 5_000, 20).Bids, 1, "bids stop before a zero price")
}

// TestV3DepthMatchesV2: a position over (almost) the whole price range is a
// constant-product pair with virtual reserves L/sqrtP and L*sqrtP.
func TestV3DepthMatchesV2(t *testing.T) {
	l := e18(1_000_000)
	sqrtP := SqrtRatioAtTick(6931) // about price 2
	ticks := []*port.DexTick{
		{Tick: -887220, LiquidityGross: l, LiquidityNet: l},
		{Tick: 887220, LiquidityGross: l, LiquidityNet: new(big.Int).Neg(l)},
	}
	v3 := v3Depth(sqrtP, 6931, l, ticks, 10, 20)
	x := new(big.Int).Quo(new(big.Int).Mul(l, q96), sqrtP)
	y := new(big.Int).Quo(new(big.Int).Mul(l, sqrtP), q96)
	v2 := v2Depth(x, y, 10, 20)
	near(t, v2.Mid, v3.Mid)
	for i := range v2.Asks {
		near(t, v2.Asks[i].Price, v3.Asks[i].Price, "ask price %d", i)
		near(t, v2.Asks[i].Base, v3.Asks[i].Base, "ask base %d", i)
		near(t, v2.Asks[i].Quote, v3.Asks[i].Quote, "ask quote %d", i)
		near(t, v2.Bids[i].Base, v3.Bids[i].Base, "bid base %d", i)
		near(t, v2.Bids[i].Quote, v3.Bids[i].Quote, "bid quote %d", i)
	}
}

// amount0 and amount1 are a position's token amounts between square root
// prices a < b (the formulas of the V3 whitepaper).
func amount0(l, a, b *big.Int) *big.Int {
	v := new(big.Int).Mul(l, new(big.Int).Sub(b, a))
	v.Mul(v, q96)
	return v.Quo(v, new(big.Int).Mul(a, b))
}

func amount1(l, a, b *big.Int) *big.Int {
	v := new(big.Int).Mul(l, new(big.Int).Sub(b, a))
	return v.Quo(v, q96)
}

// TestV3DepthCrossesTicks: the walk adds a position's liquidity where its
// range starts and stops where the last range ends.
func TestV3DepthCrossesTicks(t *testing.T) {
	l1, l2 := e18(1000), e18(3000)
	ticks := []*port.DexTick{
		{Tick: -600, LiquidityGross: l1, LiquidityNet: l1},
		{Tick: 300, LiquidityGross: l2, LiquidityNet: l2},
		{Tick: 600, LiquidityGross: l1, LiquidityNet: new(big.Int).Neg(l1)},
		{Tick: 1200, LiquidityGross: l2, LiquidityNet: new(big.Int).Neg(l2)},
	}
	at := SqrtRatioAtTick
	d := v3Depth(q96, 0, l1, ticks, 100, 20) // price 1, 1% steps

	// Every ask together is what both positions hold above the price, and
	// the levels past tick 1200 (price 1.1275) are empty.
	base, quote := sum(d.Asks)
	near(t, new(big.Int).Add(amount0(l1, q96, at(600)), amount0(l2, at(300), at(1200))), base)
	near(t, new(big.Int).Add(amount1(l1, q96, at(600)), amount1(l2, at(300), at(1200))), quote)
	for i, lv := range d.Asks {
		if i >= 13 {
			assert.Zero(t, lv.Base.Sign(), "ask %d is past the last range", i)
		} else {
			assert.Positive(t, lv.Base.Sign(), "ask %d", i)
		}
	}
	// Level 4 (1.03 to 1.04, ticks 296 to 392) crosses tick 300: denser
	// than level 2.
	assert.Equal(t, 1, d.Asks[3].Base.Cmp(new(big.Int).Mul(d.Asks[1].Base, big.NewInt(3))))

	// Bids hold only the first position, down to tick -600 (price 0.9418).
	base, _ = sum(d.Bids)
	near(t, amount0(l1, at(-600), q96), base)
	assert.Zero(t, d.Bids[6].Base.Sign(), "bid 7 (0.93) is below the range")
	assert.Positive(t, d.Bids[5].Base.Sign())

	assert.Empty(t, v3Depth(nil, 0, l1, ticks, 100, 20).Asks, "an uninitialized pool has no depth")
}

func TestOrderLevels(t *testing.T) {
	order := func(side port.DexSide, size, filled, price int64, status port.DexOrderStatus) *port.DexOrder {
		return &port.DexOrder{ID: common.BigToHash(big.NewInt(size*1000 + price)), Side: side, Size: big.NewInt(size),
			Filled: big.NewInt(filled), Price: e18(price), Status: status}
	}
	orders := []*port.DexOrder{
		order(port.DexBuy, 10, 4, 50, port.DexOrderPartiallyFilled),
		order(port.DexBuy, 5, 0, 49, port.DexOrderOpen),
		order(port.DexBuy, 3, 0, 50, port.DexOrderOpen),
		order(port.DexSell, 2, 0, 53, port.DexOrderOpen),
		order(port.DexSell, 7, 0, 52, port.DexOrderOpen),
		order(port.DexSell, 9, 0, 51, port.DexOrderPending),   // not triggered
		order(port.DexSell, 9, 9, 51, port.DexOrderFilled),    // done
		order(port.DexBuy, 4, 0, 0, port.DexOrderOpen),        // no price
		order(port.DexSell, 1, 0, 52, port.DexOrderCancelled), // gone
	}
	bids, asks := orderLevels(orders)
	show := func(levels []Level) []string {
		var out []string
		for _, l := range levels {
			out = append(out, new(big.Int).Quo(l.Price, port.DexPriceScale).String()+":"+l.Base.String()+":"+l.Quote.String())
		}
		return out
	}
	assert.Equal(t, []string{"50:9:450", "49:5:245"}, show(bids))
	assert.Equal(t, []string{"52:7:364", "53:2:106"}, show(asks))

	d := bookDepth(bids, asks, 1)
	assert.Len(t, d.Bids, 1)
	assert.Len(t, d.Asks, 1)
	assert.Equal(t, e18(51).String(), d.Mid.String())
	assert.Nil(t, bookDepth(bids, nil, 5).Mid)
}
