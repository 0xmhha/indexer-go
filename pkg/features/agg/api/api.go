// Package api serves the aggregates of agg.candles and agg.timeseries over
// GraphQL (refactoring plan R5-3): candles of a DEX market and day, week and
// month series of chain activity, DEX volume and token transfers, oldest
// first within a time range, a page at a time. It is a GraphQL extension,
// linked in with the features.
package api

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/features/agg"
)

func init() { graphql.RegisterExtension("agg", register) }

var (
	candleType = gql.NewObject(gql.ObjectConfig{
		Name:        "DexCandle",
		Description: "OHLCV of a market's trades whose block time is in [start, start + interval); prices are quote per base times 1e18",
		Fields: gql.Fields{
			"start":       {Type: gql.NewNonNull(graphql.BigIntType), Description: "Unix seconds"},
			"interval":    {Type: gql.NewNonNull(gql.String), Description: "For example 1m, 1h, 1d"},
			"open":        {Type: gql.NewNonNull(graphql.BigIntType)},
			"high":        {Type: gql.NewNonNull(graphql.BigIntType)},
			"low":         {Type: gql.NewNonNull(graphql.BigIntType)},
			"close":       {Type: gql.NewNonNull(graphql.BigIntType)},
			"baseVolume":  {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
			"quoteVolume": {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
			"trades":      {Type: gql.NewNonNull(graphql.BigIntType)},
		},
	})
	chainActivityType = gql.NewObject(gql.ObjectConfig{
		Name: "ChainActivity",
		Fields: gql.Fields{
			"start":        {Type: gql.NewNonNull(graphql.BigIntType), Description: "Unix seconds (UTC day, ISO week from Monday, or month)"},
			"period":       {Type: gql.NewNonNull(gql.String)},
			"blocks":       {Type: gql.NewNonNull(graphql.BigIntType)},
			"transactions": {Type: gql.NewNonNull(graphql.BigIntType)},
			"gasUsed":      {Type: gql.NewNonNull(graphql.BigIntType)},
			"fees":         {Type: gql.NewNonNull(graphql.BigIntType), Description: "Gas used times the price paid, in wei"},
			"firstBlock":   {Type: gql.NewNonNull(graphql.BigIntType)},
			"lastBlock":    {Type: gql.NewNonNull(graphql.BigIntType)},
		},
	})
	dexVolumeType = gql.NewObject(gql.ObjectConfig{
		Name: "DexVolume",
		Fields: gql.Fields{
			"start":       {Type: gql.NewNonNull(graphql.BigIntType)},
			"period":      {Type: gql.NewNonNull(gql.String)},
			"trades":      {Type: gql.NewNonNull(graphql.BigIntType)},
			"baseVolume":  {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
			"quoteVolume": {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
		},
	})
	tokenVolumeType = gql.NewObject(gql.ObjectConfig{
		Name: "TokenTransferVolume",
		Fields: gql.Fields{
			"start":     {Type: gql.NewNonNull(graphql.BigIntType)},
			"period":    {Type: gql.NewNonNull(gql.String)},
			"transfers": {Type: gql.NewNonNull(graphql.BigIntType)},
			"volume":    {Type: graphql.BigIntType, Description: "ERC-20 amount moved, raw units; null for ERC-721 tokens"},
		},
	})
)

func connection(name string, node *gql.Object) *gql.Object {
	return gql.NewObject(gql.ObjectConfig{
		Name: name,
		Fields: gql.Fields{
			"nodes":    {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(node)))},
			"pageInfo": {Type: gql.NewNonNull(graphql.PageInfoType())},
		},
	})
}

var (
	candleConnection      = connection("DexCandleConnection", candleType)
	chainActivityConn     = connection("ChainActivityConnection", chainActivityType)
	dexVolumeConnection   = connection("DexVolumeConnection", dexVolumeType)
	tokenVolumeConnection = connection("TokenTransferVolumeConnection", tokenVolumeType)
)

var errUnsupported = errors.New("the storage does not keep aggregates")

func register(e *graphql.Extension) {
	r, _ := e.Storage().(port.AggReader)
	rangeArgs := func(more gql.FieldConfigArgument) gql.FieldConfigArgument {
		more["from"] = &gql.ArgumentConfig{Type: gql.String, Description: "Earliest start, Unix seconds (default 0)"}
		more["to"] = &gql.ArgumentConfig{Type: gql.String, Description: "Latest start, Unix seconds (default no limit)"}
		more["pagination"] = &gql.ArgumentConfig{Type: graphql.PaginationInputType()}
		return more
	}
	period := &gql.ArgumentConfig{Type: gql.NewNonNull(gql.String), Description: "day, week (ISO, from Monday) or month, UTC"}
	market := &gql.ArgumentConfig{Type: gql.NewNonNull(gql.String), Description: "Pool, pair or order manager address"}
	marketID := &gql.ArgumentConfig{Type: gql.String, Description: "Perpetual market id (default 0)"}

	e.AddQuery("dexCandles", &gql.Field{
		Type: gql.NewNonNull(candleConnection), Description: "A market's candles of one interval, oldest first (agg.candles); intervals without trades have none",
		Args: rangeArgs(gql.FieldConfigArgument{"market": market, "marketId": marketID,
			"interval": {Type: gql.NewNonNull(gql.String), Description: "A configured interval, for example 1m, 1h, 1d"}}),
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			key, err := marketKey(p.Args["market"], p.Args["marketId"])
			if err != nil {
				return nil, err
			}
			iv, err := agg.ParseInterval(str(p.Args["interval"]))
			if err != nil {
				return nil, err
			}
			from, to, err := timeRange(p)
			if err != nil {
				return nil, err
			}
			pg := graphql.Page(p, 0)
			items, next, err := r.ListDexCandles(p.Context, key, iv, from, to, pg)
			return pageOf(items, pg, next, candleMap), err
		},
	})
	series := func(name string, conn *gql.Object, desc string, args gql.FieldConfigArgument,
		subject func(p gql.ResolveParams) (string, error), node func(*port.SeriesPoint) map[string]interface{}) {
		args["period"] = period
		e.AddQuery(name, &gql.Field{
			Type: gql.NewNonNull(conn), Description: desc, Args: rangeArgs(args),
			Resolve: func(p gql.ResolveParams) (interface{}, error) {
				if r == nil {
					return nil, errUnsupported
				}
				sub, err := subject(p)
				if err != nil {
					return nil, err
				}
				per, err := periodOf(p.Args["period"])
				if err != nil {
					return nil, err
				}
				from, to, err := timeRange(p)
				if err != nil {
					return nil, err
				}
				pg := graphql.Page(p, 0)
				items, next, err := r.ListSeriesPoints(p.Context, seriesName(name), sub, per, from, to, pg)
				return pageOf(items, pg, next, node), err
			},
		})
	}
	series("chainActivity", chainActivityConn, "The chain's activity per period, oldest first (agg.timeseries series chain)",
		gql.FieldConfigArgument{}, func(gql.ResolveParams) (string, error) { return "", nil },
		pointMap("blocks", "transactions", "gasUsed", "fees", agg.ValueFirstBlock, agg.ValueLastBlock))
	series("dexVolume", dexVolumeConnection, "A market's trades and volume per period, oldest first (agg.timeseries series dex)",
		gql.FieldConfigArgument{"market": market, "marketId": marketID},
		func(p gql.ResolveParams) (string, error) {
			key, err := marketKey(p.Args["market"], p.Args["marketId"])
			return agg.MarketSubject(key), err
		},
		pointMap("trades", "baseVolume", "quoteVolume"))
	series("tokenTransferVolume", tokenVolumeConnection, "A token's transfers per period, oldest first (agg.timeseries series token)",
		gql.FieldConfigArgument{"token": {Type: gql.NewNonNull(gql.String), Description: "Token contract address"}},
		func(p gql.ResolveParams) (string, error) {
			a, err := address(p.Args["token"])
			return strings.ToLower(a.Hex()), err
		},
		optionalPointMap(pointMap("transfers"), "volume"))
}

// seriesName is the stored series a query reads.
func seriesName(query string) string {
	switch query {
	case "dexVolume":
		return agg.SeriesDex
	case "tokenTransferVolume":
		return agg.SeriesToken
	default:
		return agg.SeriesChain
	}
}

func str(v interface{}) string { s, _ := v.(string); return s }

func address(v interface{}) (common.Address, error) {
	s := str(v)
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("invalid address %q", s)
	}
	return common.HexToAddress(s), nil
}

func marketKey(addr, id interface{}) (port.DexMarketKey, error) {
	a, err := address(addr)
	if err != nil {
		return port.DexMarketKey{}, err
	}
	key := port.DexMarketKey{Address: a}
	if s := str(id); s != "" {
		if key.ID, err = strconv.ParseUint(s, 10, 64); err != nil {
			return port.DexMarketKey{}, fmt.Errorf("invalid marketId %q", s)
		}
	}
	return key, nil
}

func periodOf(v interface{}) (port.SeriesPeriod, error) {
	for _, p := range agg.Periods {
		if string(p) == str(v) {
			return p, nil
		}
	}
	return "", fmt.Errorf("invalid period %q: day, week or month", str(v))
}

// timeRange reads the from and to arguments.
func timeRange(p gql.ResolveParams) (from, to uint64, err error) {
	to = math.MaxUint64
	for name, dst := range map[string]*uint64{"from": &from, "to": &to} {
		if s := str(p.Args[name]); s != "" {
			if *dst, err = strconv.ParseUint(s, 10, 64); err != nil {
				return 0, 0, fmt.Errorf("invalid %s %q: Unix seconds", name, s)
			}
		}
	}
	return from, to, nil
}

func u64(v uint64) string { return strconv.FormatUint(v, 10) }

func pageOf[T any](items []T, pg port.Page, next string, node func(T) map[string]interface{}) map[string]interface{} {
	nodes := make([]map[string]interface{}, len(items))
	for i, it := range items {
		nodes[i] = node(it)
	}
	return map[string]interface{}{"nodes": nodes, "pageInfo": graphql.CursorPageInfo(pg, next)}
}

func candleMap(c *port.DexCandle) map[string]interface{} {
	return map[string]interface{}{
		"start": u64(c.Start), "interval": agg.FormatInterval(c.Interval),
		"open": c.Open.String(), "high": c.High.String(), "low": c.Low.String(), "close": c.Close.String(),
		"baseVolume": c.BaseVolume.String(), "quoteVolume": c.QuoteVolume.String(), "trades": u64(c.Trades),
	}
}

// pointMap maps a point to its start, period and the named values (0 when
// a value is missing).
func pointMap(names ...string) func(*port.SeriesPoint) map[string]interface{} {
	return func(p *port.SeriesPoint) map[string]interface{} {
		out := map[string]interface{}{"start": u64(p.Key.Start), "period": string(p.Key.Period)}
		for _, n := range names {
			v := p.Values[n]
			if v == nil {
				v = new(big.Int)
			}
			out[n] = v.String()
		}
		return out
	}
}

// optionalPointMap adds values that are null when missing.
func optionalPointMap(base func(*port.SeriesPoint) map[string]interface{}, names ...string) func(*port.SeriesPoint) map[string]interface{} {
	return func(p *port.SeriesPoint) map[string]interface{} {
		out := base(p)
		for _, n := range names {
			if v := p.Values[n]; v != nil {
				out[n] = v.String()
			} else {
				out[n] = nil
			}
		}
		return out
	}
}
