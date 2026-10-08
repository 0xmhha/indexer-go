package port

import (
	"context"
	"math/big"
)

// Aggregates (refactoring plan R5-3): candles of DEX markets and time series
// of chain activity, DEX volume and token transfers, kept up to date block
// by block. Each record is the merge of the trades or blocks in its time
// bucket; merging is independent of order, so recomputing a bucket from its
// sources gives the same record.

// DexCandle is a market's OHLCV candle of one interval: the trades whose
// block time falls in [Start, Start+Interval).
type DexCandle struct {
	Market   DexMarketKey `json:"market"`
	Interval uint64       `json:"interval"` // seconds
	Start    uint64       `json:"start"`    // Unix seconds, a multiple of Interval
	// Open and Close are the prices (times DexPriceScale) of the first and
	// last trade, by block and log index, kept with their positions.
	Open          *big.Int `json:"open"`
	OpenBlock     uint64   `json:"openBlock"`
	OpenLogIndex  uint     `json:"openLogIndex"`
	Close         *big.Int `json:"close"`
	CloseBlock    uint64   `json:"closeBlock"`
	CloseLogIndex uint     `json:"closeLogIndex"`
	High          *big.Int `json:"high"`
	Low           *big.Int `json:"low"`
	// BaseVolume and QuoteVolume are the traded amounts in raw units.
	BaseVolume  *big.Int `json:"baseVolume"`
	QuoteVolume *big.Int `json:"quoteVolume"`
	Trades      uint64   `json:"trades"`
}

// SeriesPeriod is a calendar period of a time series, in UTC.
type SeriesPeriod string

const (
	SeriesDay   SeriesPeriod = "day"
	SeriesWeek  SeriesPeriod = "week" // ISO weeks, from Monday
	SeriesMonth SeriesPeriod = "month"
)

// SeriesKey identifies a point of a time series: the series (for example
// "chain"), what it measures (a market or token; empty for the chain), the
// period and the period's start (Unix seconds).
type SeriesKey struct {
	Series  string       `json:"series"`
	Subject string       `json:"subject"`
	Period  SeriesPeriod `json:"period"`
	Start   uint64       `json:"start"`
}

// SeriesPoint is the value of a series in one period: named values, each
// merged by its series' rule (sums, or the first and last block).
type SeriesPoint struct {
	Key    SeriesKey           `json:"key"`
	Values map[string]*big.Int `json:"values"`
}

// AggReader reads the aggregates.
type AggReader interface {
	// GetDexCandle returns a candle, ErrNotFound when its bucket has no
	// trade.
	GetDexCandle(ctx context.Context, market DexMarketKey, interval, start uint64) (*DexCandle, error)
	// ListDexCandles returns one page of a market's candles of an interval
	// that start in [from, to], oldest first. Buckets without trades have
	// no candle.
	ListDexCandles(ctx context.Context, market DexMarketKey, interval, from, to uint64, page Page) ([]*DexCandle, string, error)
	// GetSeriesPoint returns a point, ErrNotFound when the period has none.
	GetSeriesPoint(ctx context.Context, key SeriesKey) (*SeriesPoint, error)
	// ListSeriesPoints returns one page of the points of a series, subject
	// and period that start in [from, to], oldest first.
	ListSeriesPoints(ctx context.Context, series, subject string, period SeriesPeriod, from, to uint64, page Page) ([]*SeriesPoint, string, error)
}

// AggWriter writes the aggregates; writing a candle or point again replaces
// it.
type AggWriter interface {
	SaveDexCandle(ctx context.Context, candle *DexCandle) error
	SaveSeriesPoint(ctx context.Context, point *SeriesPoint) error
}
