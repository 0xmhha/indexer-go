package postgres

import (
	"context"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.AggReader = (*Store)(nil)
	_ port.AggWriter = (*Store)(nil)
)

// timeBound is a time bound as a bigint: times past the largest bigint
// (the "no bound" of math.MaxUint64) become it.
func timeBound(v uint64) int64 { return int64(min(v, math.MaxInt64)) }

// GetDexCandle implements port.AggReader.
func (s *Store) GetDexCandle(ctx context.Context, market port.DexMarketKey, interval, start uint64) (*port.DexCandle, error) {
	return getJSON[port.DexCandle](ctx, s.q(ctx),
		"SELECT data FROM agg_candles WHERE address = $1 AND market_id = $2 AND seconds = $3 AND start = $4",
		market.Address.Bytes(), i64(market.ID), i64(interval), i64(start))
}

// ListDexCandles implements port.AggReader.
func (s *Store) ListDexCandles(ctx context.Context, market port.DexMarketKey, interval, from, to uint64, page port.Page) ([]*port.DexCandle, string, error) {
	return listQuery[*port.DexCandle]{
		list:  dexMarketList("agg-candles", market) + ":" + u64s(interval),
		sql:   "SELECT data FROM agg_candles WHERE address = $1 AND market_id = $2 AND seconds = $3 AND start >= $4 AND start <= $5",
		args:  []any{market.Address.Bytes(), i64(market.ID), i64(interval), timeBound(from), timeBound(to)},
		keys:  []keyCol{{"start", kindInt, false}},
		scan:  scanJSON[port.DexCandle],
		keyOf: func(c *port.DexCandle) []string { return []string{u64s(c.Start)} },
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// GetSeriesPoint implements port.AggReader.
func (s *Store) GetSeriesPoint(ctx context.Context, key port.SeriesKey) (*port.SeriesPoint, error) {
	return getJSON[port.SeriesPoint](ctx, s.q(ctx),
		"SELECT data FROM agg_series WHERE series = $1 AND subject = $2 AND period = $3 AND start = $4",
		key.Series, key.Subject, string(key.Period), i64(key.Start))
}

// ListSeriesPoints implements port.AggReader.
func (s *Store) ListSeriesPoints(ctx context.Context, series, subject string, period port.SeriesPeriod, from, to uint64, page port.Page) ([]*port.SeriesPoint, string, error) {
	return listQuery[*port.SeriesPoint]{
		list:  "agg-series:" + series + ":" + subject + ":" + string(period),
		sql:   "SELECT data FROM agg_series WHERE series = $1 AND subject = $2 AND period = $3 AND start >= $4 AND start <= $5",
		args:  []any{series, subject, string(period), timeBound(from), timeBound(to)},
		keys:  []keyCol{{"start", kindInt, false}},
		scan:  scanJSON[port.SeriesPoint],
		keyOf: func(p *port.SeriesPoint) []string { return []string{u64s(p.Key.Start)} },
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// SaveDexCandle implements port.AggWriter.
func (s *Store) SaveDexCandle(ctx context.Context, c *port.DexCandle) error {
	return s.saveDex(ctx, "candle", c, `INSERT INTO agg_candles (address, market_id, seconds, start, data) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (address, market_id, seconds, start) DO UPDATE SET data = EXCLUDED.data`,
		c.Market.Address.Bytes(), i64(c.Market.ID), i64(c.Interval), i64(c.Start))
}

// SaveSeriesPoint implements port.AggWriter.
func (s *Store) SaveSeriesPoint(ctx context.Context, p *port.SeriesPoint) error {
	return s.saveDex(ctx, "series point", p, `INSERT INTO agg_series (series, subject, period, start, data) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (series, subject, period, start) DO UPDATE SET data = EXCLUDED.data`,
		p.Key.Series, p.Key.Subject, string(p.Key.Period), i64(p.Key.Start))
}
