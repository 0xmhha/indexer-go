package orderbook

import (
	"math/big"
	"sort"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Level is one price level of a book side: the base the market gives (asks)
// or takes (bids) between the previous level and Price, and the quote paid
// or received for it. Prices are quote per base in raw units times
// port.DexPriceScale, like trade prices; amounts are raw units.
type Level struct {
	Price *big.Int
	Base  *big.Int
	Quote *big.Int
}

// Depth is a book's price levels: bids from the highest price down, asks
// from the lowest up. Mid is the market's current price (pools and pairs)
// or the middle of the best bid and ask (order books; nil without both).
type Depth struct {
	Mid  *big.Int
	Bids []Level
	Asks []Level
}

const bps = 10_000

// levelPrice is the price i steps of stepBps above (dir 1) or below (dir -1)
// p; nil when it is not positive.
func levelPrice(p *big.Int, i, stepBps, dir int) *big.Int {
	f := int64(bps + dir*i*stepBps)
	if f <= 0 {
		return nil
	}
	out := new(big.Int).Mul(p, big.NewInt(f))
	out.Quo(out, big.NewInt(bps))
	if out.Sign() <= 0 {
		return nil
	}
	return out
}

func nonNegative(v *big.Int) *big.Int {
	if v.Sign() < 0 {
		return new(big.Int)
	}
	return v
}

// v2Depth is the depth of a constant-product pair with reserves r0 (base)
// and r1 (quote): moving the price from P0 = r1/r0 to p leaves
// x = sqrt(k/p) base and y = sqrt(k*p) quote in the pair (k = r0*r1), so a
// level's base is the change of x between its price and the previous one.
// Amounts are what the pair gives or takes, before the swap fee.
func v2Depth(r0, r1 *big.Int, stepBps, levels int) Depth {
	if r0 == nil || r1 == nil || r0.Sign() <= 0 || r1.Sign() <= 0 {
		return Depth{}
	}
	scale := port.DexPriceScale
	k := new(big.Int).Mul(r0, r1)
	p0 := new(big.Int).Quo(new(big.Int).Mul(r1, scale), r0)
	at := func(p *big.Int) (x, y *big.Int) {
		x = new(big.Int).Sqrt(new(big.Int).Quo(new(big.Int).Mul(k, scale), p))
		y = new(big.Int).Sqrt(new(big.Int).Quo(new(big.Int).Mul(k, p), scale))
		return x, y
	}
	d := Depth{Mid: p0}
	for _, dir := range []int{1, -1} {
		prevX, prevY := r0, r1
		for i := 1; i <= levels; i++ {
			p := levelPrice(p0, i, stepBps, dir)
			if p == nil {
				break
			}
			x, y := at(p)
			if dir > 0 {
				d.Asks = append(d.Asks, Level{Price: p, Base: nonNegative(new(big.Int).Sub(prevX, x)), Quote: nonNegative(new(big.Int).Sub(y, prevY))})
			} else {
				d.Bids = append(d.Bids, Level{Price: p, Base: nonNegative(new(big.Int).Sub(x, prevX)), Quote: nonNegative(new(big.Int).Sub(prevY, y))})
			}
			prevX, prevY = x, y
		}
	}
	return d
}

// sqrtAtPrice is the Q64.96 square root price of a scaled price, within
// TickMath's bounds.
func sqrtAtPrice(p *big.Int) *big.Int {
	s := new(big.Int).Sqrt(new(big.Int).Quo(new(big.Int).Mul(p, q192), port.DexPriceScale))
	if s.Cmp(minSqrtRatio) < 0 {
		return new(big.Int).Set(minSqrtRatio)
	}
	if s.Cmp(maxSqrtRatio) > 0 {
		return new(big.Int).Set(maxSqrtRatio)
	}
	return s
}

// segment adds the amounts liquidity l holds between square root prices
// a < b: base L*(b-a)/(a*b) (in Q96) and quote L*(b-a).
func segment(level *Level, a, b, l *big.Int) {
	if l.Sign() <= 0 || b.Cmp(a) <= 0 {
		return
	}
	diff := new(big.Int).Sub(b, a)
	base := new(big.Int).Mul(l, diff)
	base.Mul(base, q96).Quo(base, new(big.Int).Mul(a, b))
	quote := new(big.Int).Mul(l, diff)
	quote.Quo(quote, q96)
	level.Base.Add(level.Base, base)
	level.Quote.Add(level.Quote, quote)
}

// v3Depth is the depth of a concentrated-liquidity pool at square root price
// sqrtP and tick, with in-range liquidity l and its initialized ticks (in
// tick order): walking up (asks) or down (bids) from the current price, each
// range between initialized ticks holds its liquidity, which changes by a
// tick's net liquidity when the walk crosses it, as a swap would.
func v3Depth(sqrtP *big.Int, tick int32, l *big.Int, ticks []*port.DexTick, stepBps, levels int) Depth {
	if sqrtP == nil || sqrtP.Sign() <= 0 {
		return Depth{}
	}
	if l == nil {
		l = new(big.Int)
	}
	p0 := new(big.Int).Mul(sqrtP, sqrtP)
	p0.Mul(p0, port.DexPriceScale).Rsh(p0, 192)
	d := Depth{Mid: p0}
	above := sort.Search(len(ticks), func(i int) bool { return ticks[i].Tick > tick })

	// Asks: the price rises, base leaves the pool.
	cur, liq, next := sqrtP, new(big.Int).Set(l), above
	for i := 1; i <= levels; i++ {
		p := levelPrice(p0, i, stepBps, 1)
		target := sqrtAtPrice(p)
		level := Level{Price: p, Base: new(big.Int), Quote: new(big.Int)}
		for next < len(ticks) {
			at := SqrtRatioAtTick(ticks[next].Tick)
			if at.Cmp(target) > 0 {
				break
			}
			segment(&level, cur, at, liq)
			cur = maxBig(cur, at)
			liq.Add(liq, ticks[next].LiquidityNet)
			next++
		}
		segment(&level, cur, target, liq)
		cur = maxBig(cur, target)
		d.Asks = append(d.Asks, level)
	}

	// Bids: the price falls, base enters the pool.
	cur, liq, next = sqrtP, new(big.Int).Set(l), above-1
	for i := 1; i <= levels; i++ {
		p := levelPrice(p0, i, stepBps, -1)
		if p == nil {
			break
		}
		target := sqrtAtPrice(p)
		level := Level{Price: p, Base: new(big.Int), Quote: new(big.Int)}
		for next >= 0 {
			at := SqrtRatioAtTick(ticks[next].Tick)
			if at.Cmp(target) < 0 {
				break
			}
			segment(&level, at, cur, liq)
			cur = minBig(cur, at)
			liq.Sub(liq, ticks[next].LiquidityNet)
			next--
		}
		segment(&level, target, cur, liq)
		cur = minBig(cur, target)
		d.Bids = append(d.Bids, level)
	}
	return d
}

func maxBig(a, b *big.Int) *big.Int {
	if a.Cmp(b) >= 0 {
		return a
	}
	return b
}

func minBig(a, b *big.Int) *big.Int {
	if a.Cmp(b) <= 0 {
		return a
	}
	return b
}

// orderLevels aggregates resting orders with a price into levels: bids
// (buy orders) from the highest price down, asks (sell orders) from the
// lowest up. An order's amount is what remains of it.
func orderLevels(orders []*port.DexOrder) (bids, asks []Level) {
	type side struct {
		at     map[string]*Level
		levels []*Level
	}
	sides := map[port.DexSide]*side{port.DexBuy: {at: map[string]*Level{}}, port.DexSell: {at: map[string]*Level{}}}
	for _, o := range orders {
		if !o.Status.Resting() || o.Price == nil || o.Price.Sign() <= 0 || o.Size == nil {
			continue
		}
		remaining := new(big.Int).Set(o.Size)
		if o.Filled != nil {
			remaining.Sub(remaining, o.Filled)
		}
		s, ok := sides[o.Side]
		if !ok || remaining.Sign() <= 0 {
			continue
		}
		key := o.Price.String()
		lv, ok := s.at[key]
		if !ok {
			lv = &Level{Price: new(big.Int).Set(o.Price), Base: new(big.Int), Quote: new(big.Int)}
			s.at[key] = lv
			s.levels = append(s.levels, lv)
		}
		lv.Base.Add(lv.Base, remaining)
	}
	collect := func(s *side, desc bool) []Level {
		sort.Slice(s.levels, func(i, j int) bool {
			c := s.levels[i].Price.Cmp(s.levels[j].Price)
			return (desc && c > 0) || (!desc && c < 0)
		})
		out := make([]Level, len(s.levels))
		for i, lv := range s.levels {
			lv.Quote.Quo(new(big.Int).Mul(lv.Base, lv.Price), port.DexPriceScale)
			out[i] = *lv
		}
		return out
	}
	return collect(sides[port.DexBuy], true), collect(sides[port.DexSell], false)
}

// bookDepth is the depth of an order book's levels, at most levels a side.
func bookDepth(bids, asks []Level, levels int) Depth {
	d := Depth{Bids: bids[:min(levels, len(bids))], Asks: asks[:min(levels, len(asks))]}
	if len(bids) > 0 && len(asks) > 0 {
		d.Mid = new(big.Int).Add(bids[0].Price, asks[0].Price)
		d.Mid.Rsh(d.Mid, 1)
	}
	return d
}
