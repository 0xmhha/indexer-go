package storage

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.AggReader = (*PebbleStorage)(nil)
	_ port.AggWriter = (*PebbleStorage)(nil)
)

// Aggregate keys (refactoring plan R5-3), starts big-endian so a series
// sorts by time.
//
//	/agg/candle/<market><interval><start>                  candle (JSON)
//	/agg/series/<series>/<subject>/<period>/<start>        point (JSON)
const (
	prefixAggCandle = "/agg/candle/"
	prefixAggSeries = "/agg/series/"
)

func init() {
	RegisterKeyspace("agg", ChainData, prefixAggCandle, prefixAggSeries)
}

func be64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

func candlePrefix(market port.DexMarketKey, interval uint64) []byte {
	return dexKey(prefixAggCandle, dexMarketBytes(market), be64(interval))
}

// seriesPrefix is the key prefix of a series, subject and period. Names
// may not contain "/", which separates them.
func seriesPrefix(series, subject string, period port.SeriesPeriod) ([]byte, error) {
	for _, part := range []string{series, subject, string(period)} {
		if strings.Contains(part, "/") {
			return nil, fmt.Errorf("series name %q contains \"/\"", part)
		}
	}
	return []byte(prefixAggSeries + series + "/" + subject + "/" + string(period) + "/"), nil
}

// timeRange returns the bounds of the keys under prefix whose start is in
// [from, to].
func timeRange(prefix []byte, from, to uint64) (lower, upper []byte) {
	lower = append(append([]byte{}, prefix...), be64(from)...)
	if to == math.MaxUint64 {
		return lower, prefixUpperBound(prefix)
	}
	return lower, append(append([]byte{}, prefix...), be64(to+1)...)
}

func aggPage[T any](ctx context.Context, s *PebbleStorage, lower, upper []byte, page port.Page) ([]*T, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	entries, next, err := s.scanPage(ctx, lower, upper, false, page, limit, nil)
	if err != nil {
		return nil, "", err
	}
	out := make([]*T, 0, len(entries))
	for _, e := range entries {
		v := new(T)
		if err := json.Unmarshal(e.Value, v); err != nil {
			return nil, "", fmt.Errorf("decode %q: %w", e.Key, err)
		}
		out = append(out, v)
	}
	return out, next, nil
}

// GetDexCandle implements port.AggReader.
func (s *PebbleStorage) GetDexCandle(ctx context.Context, market port.DexMarketKey, interval, start uint64) (*port.DexCandle, error) {
	return getDexJSON[port.DexCandle](ctx, s, dexKey(string(candlePrefix(market, interval)), be64(start)))
}

// ListDexCandles implements port.AggReader.
func (s *PebbleStorage) ListDexCandles(ctx context.Context, market port.DexMarketKey, interval, from, to uint64, page port.Page) ([]*port.DexCandle, string, error) {
	lower, upper := timeRange(candlePrefix(market, interval), from, to)
	return aggPage[port.DexCandle](ctx, s, lower, upper, page)
}

// GetSeriesPoint implements port.AggReader.
func (s *PebbleStorage) GetSeriesPoint(ctx context.Context, key port.SeriesKey) (*port.SeriesPoint, error) {
	prefix, err := seriesPrefix(key.Series, key.Subject, key.Period)
	if err != nil {
		return nil, err
	}
	return getDexJSON[port.SeriesPoint](ctx, s, dexKey(string(prefix), be64(key.Start)))
}

// ListSeriesPoints implements port.AggReader.
func (s *PebbleStorage) ListSeriesPoints(ctx context.Context, series, subject string, period port.SeriesPeriod, from, to uint64, page port.Page) ([]*port.SeriesPoint, string, error) {
	prefix, err := seriesPrefix(series, subject, period)
	if err != nil {
		return nil, "", err
	}
	lower, upper := timeRange(prefix, from, to)
	return aggPage[port.SeriesPoint](ctx, s, lower, upper, page)
}

// SaveDexCandle implements port.AggWriter.
func (s *PebbleStorage) SaveDexCandle(ctx context.Context, c *port.DexCandle) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.putDex(ctx, [2][]byte{dexKey(string(candlePrefix(c.Market, c.Interval)), be64(c.Start)), data})
}

// SaveSeriesPoint implements port.AggWriter.
func (s *PebbleStorage) SaveSeriesPoint(ctx context.Context, p *port.SeriesPoint) error {
	prefix, err := seriesPrefix(p.Key.Series, p.Key.Subject, p.Key.Period)
	if err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.putDex(ctx, [2][]byte{dexKey(string(prefix), be64(p.Key.Start)), data})
}
