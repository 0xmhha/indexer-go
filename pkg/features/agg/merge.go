// Package agg is the aggregate features (refactoring plan R5-3), kept in
// the block's storage transaction so a rollback reverts them with the
// block:
//
//   - agg.candles: OHLCV candles of every DEX market, per configured
//     interval, from the trades dex.trades records;
//   - agg.timeseries: day, ISO week and month series of chain activity,
//     DEX volume per market and token transfers per token.
//
// Every record is a merge of its sources (trades, blocks, transfers) that
// does not depend on their order, and each committed block is applied once,
// so the records equal what recomputing them from the stored sources gives
// (Recompute*).
package agg

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Feature names.
const (
	CandlesName    = "agg.candles"
	TimeSeriesName = "agg.timeseries"
)

// Periods are the series periods, in order.
var Periods = []port.SeriesPeriod{port.SeriesDay, port.SeriesWeek, port.SeriesMonth}

const day = 86400

// PeriodStart returns the start (Unix seconds, UTC) of the period holding
// t: the day, the ISO week (from Monday) or the month.
func PeriodStart(period port.SeriesPeriod, t uint64) uint64 {
	days := t / day
	switch period {
	case port.SeriesWeek:
		// 1970-01-01 was a Thursday: day d is (d+3)%7 days after a Monday.
		return (days - (days+3)%7) * day
	case port.SeriesMonth:
		u := time.Unix(int64(t), 0).UTC()
		return uint64(time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC).Unix())
	default:
		return days * day
	}
}

// ParseInterval reads a candle interval such as 1m, 15m, 4h or 1d.
func ParseInterval(s string) (uint64, error) {
	units := map[byte]uint64{'s': 1, 'm': 60, 'h': 3600, 'd': day}
	if len(s) < 2 {
		return 0, fmt.Errorf("interval %q is not <number><s|m|h|d>", s)
	}
	unit, ok := units[s[len(s)-1]]
	n, err := strconv.ParseUint(s[:len(s)-1], 10, 32)
	if !ok || err != nil || n == 0 {
		return 0, fmt.Errorf("interval %q is not <number><s|m|h|d>", s)
	}
	if v := n * unit; v <= 30*day {
		return v, nil
	}
	return 0, fmt.Errorf("interval %q is longer than 30 days", s)
}

// FormatInterval writes an interval in the largest unit that divides it.
func FormatInterval(v uint64) string {
	for _, u := range []struct {
		secs uint64
		unit string
	}{{day, "d"}, {3600, "h"}, {60, "m"}} {
		if v%u.secs == 0 {
			return strconv.FormatUint(v/u.secs, 10) + u.unit
		}
	}
	return strconv.FormatUint(v, 10) + "s"
}

// before reports whether the log position (b1, l1) precedes (b2, l2).
func before(b1 uint64, l1 uint, b2 uint64, l2 uint) bool {
	return b1 < b2 || (b1 == b2 && l1 < l2)
}

// MergeTrade adds a trade to its candle (nil starts one).
func MergeTrade(c *port.DexCandle, t *port.DexTrade, interval uint64) *port.DexCandle {
	if c == nil {
		c = &port.DexCandle{Market: t.Market, Interval: interval, Start: t.Timestamp - t.Timestamp%interval,
			Open: t.Price, OpenBlock: t.BlockNumber, OpenLogIndex: t.LogIndex,
			Close: t.Price, CloseBlock: t.BlockNumber, CloseLogIndex: t.LogIndex,
			High: t.Price, Low: t.Price, BaseVolume: new(big.Int), QuoteVolume: new(big.Int)}
	} else {
		if before(t.BlockNumber, t.LogIndex, c.OpenBlock, c.OpenLogIndex) {
			c.Open, c.OpenBlock, c.OpenLogIndex = t.Price, t.BlockNumber, t.LogIndex
		}
		if before(c.CloseBlock, c.CloseLogIndex, t.BlockNumber, t.LogIndex) {
			c.Close, c.CloseBlock, c.CloseLogIndex = t.Price, t.BlockNumber, t.LogIndex
		}
		if t.Price.Cmp(c.High) > 0 {
			c.High = t.Price
		}
		if t.Price.Cmp(c.Low) < 0 {
			c.Low = t.Price
		}
	}
	c.BaseVolume = new(big.Int).Add(c.BaseVolume, t.BaseAmount)
	c.QuoteVolume = new(big.Int).Add(c.QuoteVolume, t.QuoteAmount)
	c.Trades++
	return c
}

// Values merged as the first or last block of a period; every other value
// is a sum.
const (
	ValueFirstBlock = "firstBlock"
	ValueLastBlock  = "lastBlock"
)

// MergeValues adds a contribution to a point's values (nil starts them).
func MergeValues(into, add map[string]*big.Int) map[string]*big.Int {
	if into == nil {
		into = map[string]*big.Int{}
	}
	for name, v := range add {
		cur, ok := into[name]
		switch {
		case !ok:
			into[name] = new(big.Int).Set(v)
		case name == ValueFirstBlock:
			if v.Cmp(cur) < 0 {
				into[name] = new(big.Int).Set(v)
			}
		case name == ValueLastBlock:
			if v.Cmp(cur) > 0 {
				into[name] = new(big.Int).Set(v)
			}
		default:
			into[name] = new(big.Int).Add(cur, v)
		}
	}
	return into
}

// MarketSubject is the series subject of a DEX market: its lower-case
// address and id.
func MarketSubject(k port.DexMarketKey) string {
	return strings.ToLower(k.Address.Hex()) + ":" + strconv.FormatUint(k.ID, 10)
}
