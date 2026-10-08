package agg

import (
	"context"
	"encoding/json"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
)

func unix(s string) uint64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return uint64(t.Unix())
}

func TestPeriodStart(t *testing.T) {
	at := unix("2026-10-08T17:30:00Z") // a Thursday
	assert.Equal(t, unix("2026-10-08T00:00:00Z"), PeriodStart(port.SeriesDay, at))
	assert.Equal(t, unix("2026-10-05T00:00:00Z"), PeriodStart(port.SeriesWeek, at), "the Monday before")
	assert.Equal(t, unix("2026-10-01T00:00:00Z"), PeriodStart(port.SeriesMonth, at))
	monday := unix("2026-10-05T00:00:00Z")
	assert.Equal(t, monday, PeriodStart(port.SeriesWeek, monday), "a Monday starts its week")
	assert.Equal(t, unix("2026-09-28T00:00:00Z"), PeriodStart(port.SeriesWeek, monday-1), "Sunday ends the week before")
	assert.Equal(t, unix("2028-02-01T00:00:00Z"), PeriodStart(port.SeriesMonth, unix("2028-02-29T23:59:59Z")))
	assert.Equal(t, unix("2025-12-29T00:00:00Z"), PeriodStart(port.SeriesWeek, unix("2026-01-01T12:00:00Z")), "a week across the year")
}

func TestIntervals(t *testing.T) {
	for s, want := range map[string]uint64{"1m": 60, "15m": 900, "4h": 14400, "1d": 86400, "30s": 30} {
		got, err := ParseInterval(s)
		require.NoError(t, err, s)
		assert.Equal(t, want, got, s)
		assert.Equal(t, s, FormatInterval(got))
	}
	for _, s := range []string{"", "m", "0m", "5x", "-1h", "31d", "1.5h"} {
		_, err := ParseInterval(s)
		assert.Error(t, err, s)
	}
	got, err := CandleSettings{Intervals: []string{"1h", "1m", "60s"}}.intervals()
	require.NoError(t, err)
	assert.Equal(t, []uint64{60, 3600}, got, "sorted, duplicates once")
}

var (
	marketA = port.DexMarketKey{Address: common.HexToAddress("0x00000000000000000000000000000000000000a1")}
	marketB = port.DexMarketKey{Address: common.HexToAddress("0x00000000000000000000000000000000000000b2"), ID: 7}
)

func trade(m port.DexMarketKey, block uint64, log uint, ts uint64, price, base int64) *port.DexTrade {
	return &port.DexTrade{Market: m, BlockNumber: block, LogIndex: log, Timestamp: ts,
		Price: big.NewInt(price), BaseAmount: big.NewInt(base), QuoteAmount: big.NewInt(price * base)}
}

func sameJSON(t *testing.T, want, got any, msg ...any) {
	t.Helper()
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	assert.JSONEq(t, string(w), string(g), msg...)
}

// TestCandlesIndependentOfOrder: open and close follow the trades'
// positions, not the order they are merged in.
func TestCandlesIndependentOfOrder(t *testing.T) {
	trades := []*port.DexTrade{
		trade(marketA, 10, 0, 1000, 100, 1),
		trade(marketA, 10, 3, 1000, 105, 2),
		trade(marketA, 11, 1, 1030, 98, 3),
		trade(marketA, 12, 0, 1075, 101, 4), // next minute
		trade(marketB, 11, 2, 1030, 7, 5),
	}
	want := RecomputeCandles(trades, []uint64{60, 300})
	require.Len(t, want, 5)
	first := want[0] // market A, 1m, [960, 1020)
	assert.Equal(t, [3]uint64{60, 960, 2}, [3]uint64{first.Interval, first.Start, first.Trades})
	five := want[2] // market A, 5m, [900, 1200)
	assert.Equal(t, uint64(300), five.Interval)
	assert.Equal(t, "100/101/105/98", five.Open.String()+"/"+five.Close.String()+"/"+five.High.String()+"/"+five.Low.String())
	assert.Equal(t, "10/1008", five.BaseVolume.String()+"/"+five.QuoteVolume.String())
	assert.Equal(t, uint64(4), five.Trades)

	rng := rand.New(rand.NewSource(1))
	for range 20 {
		shuffled := append([]*port.DexTrade{}, trades...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		sameJSON(t, want, RecomputeCandles(shuffled, []uint64{60, 300}))
	}
}

func TestMergeValues(t *testing.T) {
	cs := []map[string]*big.Int{
		{"blocks": big.NewInt(1), ValueFirstBlock: big.NewInt(5), ValueLastBlock: big.NewInt(5)},
		{"blocks": big.NewInt(1), ValueFirstBlock: big.NewInt(3), ValueLastBlock: big.NewInt(3)},
		{"blocks": big.NewInt(1), ValueFirstBlock: big.NewInt(9), ValueLastBlock: big.NewInt(9), "fees": big.NewInt(4)},
	}
	for _, order := range [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}} {
		var v map[string]*big.Int
		for _, i := range order {
			v = MergeValues(v, cs[i])
		}
		assert.Equal(t, "3/3/9/4", v["blocks"].String()+"/"+v[ValueFirstBlock].String()+"/"+v[ValueLastBlock].String()+"/"+v["fees"].String())
	}
	assert.Equal(t, "5", cs[0][ValueFirstBlock].String(), "contributions are not modified")
}

// memStore keeps candles, series points and the trades of each block.
type memStore struct {
	port.Reader    // unused
	port.DexReader // unused
	trades         map[uint64][]*port.DexTrade
	candles        map[candleKey]*port.DexCandle
	points         map[port.SeriesKey]*port.SeriesPoint
}

func newMemStore() *memStore {
	return &memStore{trades: map[uint64][]*port.DexTrade{}, candles: map[candleKey]*port.DexCandle{}, points: map[port.SeriesKey]*port.SeriesPoint{}}
}

func clone[T any](v *T) *T {
	b, _ := json.Marshal(v)
	out := new(T)
	_ = json.Unmarshal(b, out)
	return out
}

func (s *memStore) ListDexTradesInBlock(_ context.Context, n uint64) ([]*port.DexTrade, error) {
	return s.trades[n], nil
}
func (s *memStore) GetDexCandle(_ context.Context, m port.DexMarketKey, iv, start uint64) (*port.DexCandle, error) {
	if c, ok := s.candles[candleKey{m, iv, start}]; ok {
		return clone(c), nil
	}
	return nil, port.ErrNotFound
}
func (s *memStore) ListDexCandles(context.Context, port.DexMarketKey, uint64, uint64, uint64, port.Page) ([]*port.DexCandle, string, error) {
	return nil, "", nil
}
func (s *memStore) GetSeriesPoint(_ context.Context, k port.SeriesKey) (*port.SeriesPoint, error) {
	if p, ok := s.points[k]; ok {
		return clone(p), nil
	}
	return nil, port.ErrNotFound
}
func (s *memStore) ListSeriesPoints(context.Context, string, string, port.SeriesPeriod, uint64, uint64, port.Page) ([]*port.SeriesPoint, string, error) {
	return nil, "", nil
}
func (s *memStore) SaveDexCandle(_ context.Context, c *port.DexCandle) error {
	s.candles[candleKey{c.Market, c.Interval, c.Start}] = clone(c)
	return nil
}
func (s *memStore) SaveSeriesPoint(_ context.Context, p *port.SeriesPoint) error {
	s.points[p.Key] = clone(p)
	return nil
}

type registrar struct {
	deps     feature.Deps
	enabled  map[string]bool
	handlers []feature.BlockHandler
}

func (r *registrar) Deps() feature.Deps                 { return r.deps }
func (r *registrar) OnBlock(h feature.BlockHandler)     { r.handlers = append(r.handlers, h) }
func (r *registrar) Enabled(n string) bool              { return r.enabled[n] }
func (r *registrar) OnRollback(feature.RollbackHandler) {}

func register(t *testing.T, s *memStore, f feature.Feature, settings any) (feature.BlockHandler, error) {
	t.Helper()
	r := &registrar{enabled: map[string]bool{dex.TradesName: true}, deps: feature.Deps{Storage: s, Logger: zap.NewNop(),
		Settings: func(name string, into any) error {
			if settings != nil && name == f.Name() {
				b, _ := json.Marshal(settings)
				return json.Unmarshal(b, into)
			}
			return nil
		}}}
	if err := f.Register(r); err != nil {
		return nil, err
	}
	require.Len(t, r.handlers, 1)
	return r.handlers[0], nil
}

var (
	tokenAddr  = common.HexToAddress("0x00000000000000000000000000000000000070c0")
	nftAddr    = common.HexToAddress("0x00000000000000000000000000000000000070c1")
	transferTo = common.HexToHash("0x0b")
)

// block builds block n at time ts with txs transactions, each with a receipt
// using 21000 gas at price 2, the first one moving 5 tokens and an NFT.
func block(n, ts uint64, txs int) *feature.Block {
	b := &feature.Block{Model: &model.Block{Number: n, Time: ts, GasUsed: uint64(21000 * txs)}}
	topic := common.HexToHash(port.ERC20TransferTopic)
	for i := range txs {
		h := common.BigToHash(big.NewInt(int64(n*100 + uint64(i))))
		b.Model.Transactions = append(b.Model.Transactions, &model.Transaction{Hash: h, GasPrice: big.NewInt(2)})
		r := &model.Receipt{TxHash: h, GasUsed: 21000}
		if i == 0 {
			r.Logs = []*model.Log{
				{Address: tokenAddr, Topics: []common.Hash{topic, transferTo, transferTo}, Data: common.LeftPadBytes(big.NewInt(5).Bytes(), 32)},
				{Address: nftAddr, Topics: []common.Hash{topic, transferTo, transferTo, common.BigToHash(big.NewInt(1))}},
			}
		}
		b.Receipts = append(b.Receipts, r)
	}
	return b
}

// TestHandlersMatchRecompute: candles and series kept block by block equal
// what recomputing them from every source gives.
func TestHandlersMatchRecompute(t *testing.T) {
	s := newMemStore()
	candlesH, err := register(t, s, candlesFeature{}, CandleSettings{Intervals: []string{"1m", "1h"}})
	require.NoError(t, err)
	seriesH, err := register(t, s, timeSeriesFeature{}, TimeSeriesSettings{Series: []string{SeriesChain, SeriesDex, SeriesToken}})
	require.NoError(t, err)

	start := unix("2026-10-04T23:59:00Z") // Sunday, the end of a week
	var blocks []*feature.Block
	var trades []*port.DexTrade
	for i := range uint64(6) {
		ts := start + i*40
		b := block(i+1, ts, int(i%3)+1)
		blocks = append(blocks, b)
		for j := range uint(i % 3) {
			tr := trade([]port.DexMarketKey{marketA, marketB}[j%2], i+1, j, ts, int64(100+i), int64(j+1))
			s.trades[i+1] = append(s.trades[i+1], tr)
			trades = append(trades, tr)
		}
	}
	for _, b := range blocks {
		require.NoError(t, candlesH.HandleBlock(context.Background(), b))
		require.NoError(t, seriesH.HandleBlock(context.Background(), b))
	}

	want := RecomputeCandles(trades, []uint64{60, 3600})
	require.Len(t, s.candles, len(want))
	for _, c := range want {
		sameJSON(t, c, s.candles[candleKey{c.Market, c.Interval, c.Start}])
	}

	var cs []Contribution
	for _, b := range blocks {
		cs = append(cs, ChainContribution(b.Model, b.Receipts))
		cs = append(cs, TransferContributions(b.Receipts, b.Model.Time, nil)...)
	}
	for _, tr := range trades {
		cs = append(cs, TradeContribution(tr))
	}
	points := RecomputeSeries(cs)
	require.Len(t, s.points, len(points))
	for _, p := range points {
		sameJSON(t, p, s.points[p.Key], "%v", p.Key)
	}

	// The week ends after the first two blocks; the month after the first.
	week := s.points[port.SeriesKey{Series: SeriesChain, Period: port.SeriesWeek, Start: unix("2026-09-28T00:00:00Z")}]
	require.NotNil(t, week)
	assert.Equal(t, "2/1/2", week.Values["blocks"].String()+"/"+week.Values[ValueFirstBlock].String()+"/"+week.Values[ValueLastBlock].String())
	assert.Equal(t, "3", week.Values["transactions"].String())
	assert.Equal(t, "126000", week.Values["fees"].String(), "3 transactions of 21000 gas at 2")
	tokenDay := s.points[port.SeriesKey{Series: SeriesToken, Subject: "0x00000000000000000000000000000000000070c0", Period: port.SeriesDay, Start: unix("2026-10-05T00:00:00Z")}]
	require.NotNil(t, tokenDay)
	assert.Equal(t, "4/20", tokenDay.Values["transfers"].String()+"/"+tokenDay.Values["volume"].String())
	nftDay := s.points[port.SeriesKey{Series: SeriesToken, Subject: "0x00000000000000000000000000000000000070c1", Period: port.SeriesDay, Start: unix("2026-10-05T00:00:00Z")}]
	require.NotNil(t, nftDay)
	assert.Nil(t, nftDay.Values["volume"], "an NFT has no volume")
}

func TestTimeSeriesSettings(t *testing.T) {
	s := newMemStore()
	_, err := register(t, s, timeSeriesFeature{}, TimeSeriesSettings{Series: []string{"prices"}})
	assert.ErrorContains(t, err, `"prices" is not one of`)
	r := &registrar{deps: feature.Deps{Storage: s, Settings: func(_ string, into any) error {
		into.(*TimeSeriesSettings).Series = []string{SeriesDex}
		return nil
	}}}
	assert.ErrorContains(t, timeSeriesFeature{}.Register(r), "needs dex.trades")
	_, err = register(t, s, candlesFeature{}, CandleSettings{Intervals: []string{"1y"}})
	assert.Error(t, err)
}
