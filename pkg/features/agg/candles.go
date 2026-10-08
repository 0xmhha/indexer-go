package agg

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
)

// DefaultIntervals are the candle intervals when none are configured.
var DefaultIntervals = []string{"1m", "5m", "15m", "1h", "4h", "1d"}

// CandleSettings are the settings of agg.candles.
//
//	features:
//	  agg.candles:
//	    enabled: true
//	    intervals: [1m, 5m, 15m, 1h, 4h, 1d]
type CandleSettings struct {
	Intervals []string `yaml:"intervals"`
}

// intervals returns the configured intervals in seconds, ascending.
func (st CandleSettings) intervals() ([]uint64, error) {
	names := st.Intervals
	if len(names) == 0 {
		names = DefaultIntervals
	}
	seen := map[uint64]bool{}
	var out []uint64
	for _, n := range names {
		v, err := ParseInterval(n)
		if err != nil {
			return nil, fmt.Errorf("features.%s.intervals: %w", CandlesName, err)
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

type candleStore interface {
	port.DexReader
	port.AggReader
	port.AggWriter
}

type candlesFeature struct{}

func (candlesFeature) Name() string       { return CandlesName }
func (candlesFeature) Requires() []string { return []string{dex.TradesName} }

// agg.candles is order-independent: a candle is a merge that keeps the
// first and last trade by position.
func (candlesFeature) OrderIndependent() bool { return true }

func (candlesFeature) Register(r feature.Registrar) error {
	store, ok := r.Deps().Storage.(candleStore)
	if !ok {
		return errors.New("storage does not support candles")
	}
	var st CandleSettings
	if err := r.Deps().DecodeSettings(CandlesName, &st); err != nil {
		return err
	}
	intervals, err := st.intervals()
	if err != nil {
		return err
	}
	r.OnBlock(&candles{store: store, intervals: intervals})
	return nil
}

func init() { feature.Register(candlesFeature{}) }

type candles struct {
	store     candleStore
	intervals []uint64
}

type candleKey struct {
	market          port.DexMarketKey
	interval, start uint64
}

// HandleBlock merges the block's trades (recorded by dex.trades) into their
// candles, each candle read and written once.
func (c *candles) HandleBlock(ctx context.Context, b *feature.Block) error {
	trades, err := c.store.ListDexTradesInBlock(ctx, b.Model.Number)
	if err != nil || len(trades) == 0 {
		return err
	}
	touched := map[candleKey]*port.DexCandle{}
	var order []candleKey
	for _, t := range trades {
		for _, iv := range c.intervals {
			k := candleKey{t.Market, iv, t.Timestamp - t.Timestamp%iv}
			cur, ok := touched[k]
			if !ok {
				cur, err = c.store.GetDexCandle(ctx, k.market, iv, k.start)
				if errors.Is(err, port.ErrNotFound) {
					cur, err = nil, nil
				}
				if err != nil {
					return fmt.Errorf("read candle: %w", err)
				}
				order = append(order, k)
			}
			touched[k] = MergeTrade(cur, t, iv)
		}
	}
	for _, k := range order {
		if err := c.store.SaveDexCandle(ctx, touched[k]); err != nil {
			return fmt.Errorf("save candle: %w", err)
		}
	}
	return nil
}

// RecomputeCandles builds the candles of trades from scratch, the trades in
// any order: what agg.candles keeps for them. They are sorted by market,
// interval and start.
func RecomputeCandles(trades []*port.DexTrade, intervals []uint64) []*port.DexCandle {
	byKey := map[candleKey]*port.DexCandle{}
	for _, t := range trades {
		for _, iv := range intervals {
			k := candleKey{t.Market, iv, t.Timestamp - t.Timestamp%iv}
			byKey[k] = MergeTrade(byKey[k], t, iv)
		}
	}
	out := make([]*port.DexCandle, 0, len(byKey))
	for _, c := range byKey {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if c := a.Market.Address.Cmp(b.Market.Address); c != 0 {
			return c < 0
		}
		if a.Market.ID != b.Market.ID {
			return a.Market.ID < b.Market.ID
		}
		if a.Interval != b.Interval {
			return a.Interval < b.Interval
		}
		return a.Start < b.Start
	})
	return out
}
