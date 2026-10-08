package app

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/features/agg"
)

// aggSources are the stores the aggregates are recomputed from.
type aggSources interface {
	port.BlockReader
	GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error)
	port.DexReader
	port.AggReader
}

// startAggApp starts the app on dir with the features (and dex.pools and
// dex.trades over the DEX scenario's venues when sc is not nil).
func startAggApp(t *testing.T, srv *testchain.Server, dir string, sc *testchain.DEXScenario, features []string, series []string, more ...func(*config.Config)) *App {
	t.Helper()
	configure := func(cfg *config.Config) {
		on := true
		for _, name := range features {
			cfg.Features[name] = config.FeatureConfig{Enabled: &on}
		}
		require.NoError(t, cfg.SetFeatureSettings(agg.TimeSeriesName, agg.TimeSeriesSettings{Series: series}))
		for _, m := range more {
			m(cfg)
		}
	}
	if sc != nil {
		return startDEXApp(t, srv, dir, sc, configure)
	}
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, dir)
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	configure(cfg)
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	return app
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// requireAggregatesRecomputed (refactoring plan R5-3) recomputes the
// candles of markets and every series point from the stored blocks,
// receipts and trades up to block to, and requires the stored aggregates
// to be exactly those.
func requireAggregatesRecomputed(t *testing.T, ctx context.Context, s aggSources, to uint64, markets []port.DexMarketKey, candles bool) {
	t.Helper()
	var trades []*port.DexTrade
	for _, m := range markets {
		page := port.FirstPage(1000)
		for {
			items, next, err := s.ListDexTrades(ctx, m, page)
			require.NoError(t, err)
			trades = append(trades, items...)
			if next == "" {
				break
			}
			page = port.Page{After: next, Limit: page.Limit}
		}
	}

	if candles {
		var intervals []uint64
		for _, name := range agg.DefaultIntervals {
			iv, err := agg.ParseInterval(name)
			require.NoError(t, err)
			intervals = append(intervals, iv)
		}
		want := map[string][]*port.DexCandle{}
		for _, c := range agg.RecomputeCandles(trades, intervals) {
			k := agg.MarketSubject(c.Market) + "/" + agg.FormatInterval(c.Interval)
			want[k] = append(want[k], c)
		}
		for _, m := range markets {
			for _, iv := range intervals {
				k := agg.MarketSubject(m) + "/" + agg.FormatInterval(iv)
				got, _, err := s.ListDexCandles(ctx, m, iv, 0, math.MaxUint64, port.FirstPage(1000))
				require.NoError(t, err)
				require.Equal(t, jsonOf(t, want[k]), jsonOf(t, nilIfEmpty(got)), "candles %s up to block %d", k, to)
			}
		}
	}

	var cs []agg.Contribution
	for h := uint64(0); h <= to; h++ {
		b, err := s.GetBlock(ctx, h)
		require.NoError(t, err)
		receipts, err := s.GetReceiptsByBlockNumber(ctx, h)
		require.NoError(t, err)
		cs = append(cs, agg.ChainContribution(b, receipts))
		cs = append(cs, agg.TransferContributions(receipts, b.Time, nil)...)
	}
	for _, tr := range trades {
		cs = append(cs, agg.TradeContribution(tr))
	}
	want := map[port.SeriesKey][]*port.SeriesPoint{}
	for _, p := range agg.RecomputeSeries(cs) {
		k := p.Key
		k.Start = 0
		want[k] = append(want[k], p)
	}
	require.NotEmpty(t, want)
	for k, points := range want {
		got, _, err := s.ListSeriesPoints(ctx, k.Series, k.Subject, k.Period, 0, math.MaxUint64, port.FirstPage(1000))
		require.NoError(t, err)
		require.Equal(t, jsonOf(t, points), jsonOf(t, got), "series %s/%s/%s up to block %d", k.Series, k.Subject, k.Period, to)
	}
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// TestAggregatesMatchRecompute (refactoring plan R5-3): candles and series
// kept block by block equal their recomputation from the stored sources:
// after indexing, after a rollback, after indexing the rolled back blocks
// again, and when the features are enabled on an indexed database
// (backfill).
func TestAggregatesMatchRecompute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("dex", func(t *testing.T) {
		sc := testchain.BuildDEX()
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		head := sc.Chain.Head()
		markets := []port.DexMarketKey{{Address: sc.V3Pool}, {Address: sc.V2Pair}, {Address: sc.OrderManager, ID: sc.PerpMarket}}
		app := startAggApp(t, srv, filepath.Join(t.TempDir(), "db"), sc,
			[]string{agg.CandlesName, agg.TimeSeriesName}, []string{agg.SeriesChain, agg.SeriesDex, agg.SeriesToken})
		defer app.Shutdown()
		require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
		s := app.storage.(aggSources)
		requireAggregatesRecomputed(t, ctx, s, head, markets, true)

		keep := sc.Trades[2].Block - 1
		_, err := app.storage.(rollbacker).RollbackTo(ctx, keep, nil)
		require.NoError(t, err)
		requireAggregatesRecomputed(t, ctx, s, keep, markets, true)
		require.NoError(t, app.fetcher.FetchRange(ctx, keep+1, head))
		requireAggregatesRecomputed(t, ctx, s, head, markets, true)

		// GraphQL serves what is stored.
		h, err := graphql.NewHandler(app.storage, zap.NewNop())
		require.NoError(t, err)
		candles, _, err := s.ListDexCandles(ctx, port.DexMarketKey{Address: sc.V3Pool}, 60, 0, math.MaxUint64, port.FirstPage(100))
		require.NoError(t, err)
		require.NotEmpty(t, candles)
		res := h.ExecuteQuery(`query($m: String!) { dexCandles(market: $m, interval: "1m") { nodes { start interval open high low close baseVolume trades } } }`,
			map[string]interface{}{"m": sc.V3Pool.Hex()})
		require.Empty(t, res.Errors)
		nodes := res.Data.(map[string]interface{})["dexCandles"].(map[string]interface{})["nodes"].([]interface{})
		require.Len(t, nodes, len(candles))
		first := nodes[0].(map[string]interface{})
		require.Equal(t, candles[0].Open.String(), first["open"])
		require.Equal(t, "1m", first["interval"])
		res = h.ExecuteQuery(`{ chainActivity(period: "month") { nodes { blocks firstBlock lastBlock } } dexVolume(market: "`+sc.V2Pair.Hex()+`", period: "day") { nodes { trades } } }`, nil)
		require.Empty(t, res.Errors)
		months := res.Data.(map[string]interface{})["chainActivity"].(map[string]interface{})["nodes"].([]interface{})
		require.Len(t, months, 1)
		require.Equal(t, map[string]interface{}{"blocks": "9", "firstBlock": "0", "lastBlock": "8"}, months[0])
		days := res.Data.(map[string]interface{})["dexVolume"].(map[string]interface{})["nodes"].([]interface{})
		require.Equal(t, []interface{}{map[string]interface{}{"trades": "1"}}, days)
	})

	t.Run("backfill", func(t *testing.T) {
		sc := testchain.BuildDefault()
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		head := sc.Chain.Head()
		dir := filepath.Join(t.TempDir(), "db")
		app := startAggApp(t, srv, dir, nil, nil, nil)
		require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
		app.Shutdown()

		// Enabled on the indexed database: the stored blocks are processed
		// in the background.
		app = startAggApp(t, srv, dir, nil, []string{agg.TimeSeriesName}, []string{agg.SeriesChain, agg.SeriesToken})
		defer app.Shutdown()
		app.fetcher.WaitBackground() // order-independent features backfill online
		s := app.storage.(aggSources)
		requireAggregatesRecomputed(t, ctx, s, head, nil, false)
		day, _, err := s.ListSeriesPoints(ctx, agg.SeriesToken, lowerHex(sc.ERC20.Hex()), port.SeriesDay, 0, math.MaxUint64, port.FirstPage(10))
		require.NoError(t, err)
		require.NotEmpty(t, day, "the scenario's token transfers are counted")
		h, err := graphql.NewHandler(app.storage, zap.NewNop())
		require.NoError(t, err)
		res := h.ExecuteQuery(`query($t: String!) { tokenTransferVolume(token: $t, period: "day") { nodes { transfers volume } } }`,
			map[string]interface{}{"t": sc.ERC20.Hex()})
		require.Empty(t, res.Errors)
		nodes := res.Data.(map[string]interface{})["tokenTransferVolume"].(map[string]interface{})["nodes"].([]interface{})
		require.Len(t, nodes, len(day))
		require.Equal(t, day[0].Values["volume"].String(), nodes[0].(map[string]interface{})["volume"])
	})
}

func lowerHex(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'F' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
