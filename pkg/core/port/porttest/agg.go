package porttest

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// testAgg checks AggReader and AggWriter: candles and series points are
// found by key and listed by start within a time range, oldest first, per
// market and interval or per series, subject and period; writing one again
// replaces it.
func testAgg(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("Candles", func(t *testing.T) {
		s := open[aggStore](t, newStore)
		_, err := s.GetDexCandle(ctx, dexPool(1), 60, 0)
		assert.ErrorIs(t, err, port.ErrNotFound)

		candle := func(market port.DexMarketKey, interval, start uint64, trades uint64) *port.DexCandle {
			p := big.NewInt(int64(start))
			return &port.DexCandle{Market: market, Interval: interval, Start: start,
				Open: p, OpenBlock: start, OpenLogIndex: 1, Close: p, CloseBlock: start + 1, CloseLogIndex: 2,
				High: p, Low: p, BaseVolume: big.NewInt(int64(trades) * 10), QuoteVolume: big.NewInt(int64(trades) * 20), Trades: trades}
		}
		var want []*port.DexCandle
		// Saved out of order; others of another interval and market.
		for _, start := range []uint64{180, 60, 1_700_000_040, 120} {
			c := candle(dexPool(1), 60, start, 1)
			require.NoError(t, s.SaveDexCandle(ctx, c))
		}
		require.NoError(t, s.SaveDexCandle(ctx, candle(dexPool(1), 300, 0, 1)))
		require.NoError(t, s.SaveDexCandle(ctx, candle(dexPool(2), 60, 120, 1)))
		require.NoError(t, s.SaveDexCandle(ctx, candle(port.DexMarketKey{Address: dexManager, ID: 7}, 60, 120, 1)))
		updated := candle(dexPool(1), 60, 120, 5)
		require.NoError(t, s.SaveDexCandle(ctx, updated))
		for _, start := range []uint64{60, 120, 180, 1_700_000_040} {
			c := candle(dexPool(1), 60, start, 1)
			if start == 120 {
				c = updated
			}
			want = append(want, c)
		}

		got, err := s.GetDexCandle(ctx, dexPool(1), 60, 120)
		require.NoError(t, err)
		sameJSON(t, updated, got)
		all, _, err := s.ListDexCandles(ctx, dexPool(1), 60, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, all, len(want))
		for i := range want {
			sameJSON(t, want[i], all[i], "candle %d", i)
		}
		ranged, _, err := s.ListDexCandles(ctx, dexPool(1), 60, 120, 180, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, ranged, 2, "both bounds are inclusive")
		assert.Equal(t, [2]uint64{120, 180}, [2]uint64{ranged[0].Start, ranged[1].Start})
		checkPaging(t, want, func(c *port.DexCandle) uint64 { return c.Start },
			func(page port.Page) ([]*port.DexCandle, string, error) {
				return s.ListDexCandles(ctx, dexPool(1), 60, 0, math.MaxUint64, page)
			})
	})

	t.Run("Series", func(t *testing.T) {
		s := open[aggStore](t, newStore)
		key := func(series, subject string, period port.SeriesPeriod, start uint64) port.SeriesKey {
			return port.SeriesKey{Series: series, Subject: subject, Period: period, Start: start}
		}
		_, err := s.GetSeriesPoint(ctx, key("chain", "", port.SeriesDay, 0))
		assert.ErrorIs(t, err, port.ErrNotFound)
		point := func(k port.SeriesKey, v int64) *port.SeriesPoint {
			return &port.SeriesPoint{Key: k, Values: map[string]*big.Int{"transactions": big.NewInt(v), "gasUsed": big.NewInt(v * 21000)}}
		}
		var want []*port.SeriesPoint
		for _, start := range []uint64{86400 * 3, 0, 86400} {
			require.NoError(t, s.SaveSeriesPoint(ctx, point(key("chain", "", port.SeriesDay, start), 1)))
		}
		// Other periods, subjects and series.
		require.NoError(t, s.SaveSeriesPoint(ctx, point(key("chain", "", port.SeriesWeek, 0), 9)))
		require.NoError(t, s.SaveSeriesPoint(ctx, point(key("dex", "0xabc:0", port.SeriesDay, 0), 9)))
		require.NoError(t, s.SaveSeriesPoint(ctx, point(key("dex", "0xabc:1", port.SeriesDay, 86400), 9)))
		require.NoError(t, s.SaveSeriesPoint(ctx, point(key("chainx", "", port.SeriesDay, 0), 9)))
		replaced := point(key("chain", "", port.SeriesDay, 86400), 7)
		require.NoError(t, s.SaveSeriesPoint(ctx, replaced))
		want = []*port.SeriesPoint{point(key("chain", "", port.SeriesDay, 0), 1), replaced, point(key("chain", "", port.SeriesDay, 86400*3), 1)}

		got, err := s.GetSeriesPoint(ctx, replaced.Key)
		require.NoError(t, err)
		sameJSON(t, replaced, got)
		all, _, err := s.ListSeriesPoints(ctx, "chain", "", port.SeriesDay, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, all, len(want))
		for i := range want {
			sameJSON(t, want[i], all[i], "point %d", i)
		}
		ranged, _, err := s.ListSeriesPoints(ctx, "chain", "", port.SeriesDay, 1, 86400*3, port.FirstPage(10))
		require.NoError(t, err)
		assert.Len(t, ranged, 2)
		dex, _, err := s.ListSeriesPoints(ctx, "dex", "0xabc:0", port.SeriesDay, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		assert.Len(t, dex, 1, "subjects are separate")
		checkPaging(t, want, func(p *port.SeriesPoint) uint64 { return p.Key.Start },
			func(page port.Page) ([]*port.SeriesPoint, string, error) {
				return s.ListSeriesPoints(ctx, "chain", "", port.SeriesDay, 0, math.MaxUint64, page)
			})
	})
}
