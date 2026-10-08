package agg

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
	"github.com/0xmhha/indexer-go/pkg/features/token"
	"github.com/0xmhha/indexer-go/pkg/storage/history"
)

// Series names.
const (
	// SeriesChain is the chain's activity per period (subject ""): blocks,
	// transactions, gasUsed, fees (gas used times the price paid, in wei),
	// firstBlock and lastBlock.
	SeriesChain = "chain"
	// SeriesDex is a DEX market's volume per period (subject MarketSubject):
	// trades, baseVolume, quoteVolume (raw units).
	SeriesDex = "dex"
	// SeriesToken is a token's transfers per period (subject the token's
	// lower-case address): transfers, and volume for ERC-20 tokens (raw
	// units).
	SeriesToken = "token"
)

// TimeSeriesSettings are the settings of agg.timeseries.
//
//	features:
//	  agg.timeseries:
//	    enabled: true
//	    series: [chain, dex, token]   # dex needs dex.trades
type TimeSeriesSettings struct {
	Series []string `yaml:"series"`
}

type seriesStore interface {
	port.AggReader
	port.AggWriter
}

type timeSeriesFeature struct{}

func (timeSeriesFeature) Name() string       { return TimeSeriesName }
func (timeSeriesFeature) Requires() []string { return nil }

// After: the dex series reads the trades dex.trades recorded in the block.
func (timeSeriesFeature) After() []string { return []string{dex.TradesName} }

// agg.timeseries is order-independent: points are sums and the first and
// last block.
func (timeSeriesFeature) OrderIndependent() bool { return true }

func (timeSeriesFeature) Register(r feature.Registrar) error {
	deps := r.Deps()
	store, ok := deps.Storage.(seriesStore)
	if !ok {
		return errors.New("storage does not support time series")
	}
	var st TimeSeriesSettings
	if err := deps.DecodeSettings(TimeSeriesName, &st); err != nil {
		return err
	}
	ts := &timeSeries{store: store}
	names := st.Series
	if len(names) == 0 {
		names = []string{SeriesChain}
	}
	for _, n := range names {
		switch n {
		case SeriesChain:
			ts.chain = true
		case SeriesDex:
			if !r.Enabled(dex.TradesName) {
				return fmt.Errorf("features.%s.series %q needs %s", TimeSeriesName, n, dex.TradesName)
			}
			if ts.trades, ok = deps.Storage.(port.DexReader); !ok {
				return errors.New("storage does not support DEX trades")
			}
		case SeriesToken:
			ts.tokens = true
			ts.excluded = token.ExcludedContract(deps.Profile)
		default:
			return fmt.Errorf("features.%s.series %q is not one of %s, %s, %s", TimeSeriesName, n, SeriesChain, SeriesDex, SeriesToken)
		}
	}
	r.OnBlock(ts)
	return nil
}

func init() { feature.Register(timeSeriesFeature{}) }

type timeSeries struct {
	store         seriesStore
	chain, tokens bool
	trades        port.DexReader  // nil without the dex series
	excluded      *common.Address // not a token (token.ExcludedContract)
}

// Contribution is what one source adds to a series in its periods.
type Contribution struct {
	Series, Subject string
	Time            uint64
	Values          map[string]*big.Int
}

func one() *big.Int { return big.NewInt(1) }

// ChainContribution is a block's contribution to the chain series.
func ChainContribution(b *model.Block, receipts []*model.Receipt) Contribution {
	txs := make(map[common.Hash]*model.Transaction, len(b.Transactions))
	for _, tx := range b.Transactions {
		txs[tx.Hash] = tx
	}
	fees := new(big.Int)
	for _, r := range receipts {
		if price := history.ReceiptGasPrice(r, txs[r.TxHash]); price != nil {
			fees.Add(fees, new(big.Int).Mul(new(big.Int).SetUint64(r.GasUsed), price))
		}
	}
	n := new(big.Int).SetUint64(b.Number)
	return Contribution{Series: SeriesChain, Time: b.Time, Values: map[string]*big.Int{
		"blocks": one(), "transactions": big.NewInt(int64(len(b.Transactions))),
		"gasUsed": new(big.Int).SetUint64(b.GasUsed), "fees": fees, ValueFirstBlock: n, ValueLastBlock: new(big.Int).Set(n),
	}}
}

// TradeContribution is a trade's contribution to the dex series.
func TradeContribution(t *port.DexTrade) Contribution {
	return Contribution{Series: SeriesDex, Subject: MarketSubject(t.Market), Time: t.Timestamp, Values: map[string]*big.Int{
		"trades": one(), "baseVolume": t.BaseAmount, "quoteVolume": t.QuoteAmount,
	}}
}

// TransferContribution is a token transfer's contribution to the token
// series.
func TransferContribution(t token.Transfer, time uint64) Contribution {
	values := map[string]*big.Int{"transfers": one()}
	if !t.ERC721 {
		values["volume"] = t.Value
	}
	return Contribution{Series: SeriesToken, Subject: strings.ToLower(t.Token.Hex()), Time: time, Values: values}
}

// TransferContributions are the token transfer contributions of a block's
// receipts.
func TransferContributions(receipts []*model.Receipt, time uint64, excluded *common.Address) []Contribution {
	var out []Contribution
	for _, r := range receipts {
		for _, l := range r.Logs {
			if t, ok := token.ReadTransfer(gethconv.LogToGeth(l), excluded); ok {
				out = append(out, TransferContribution(t, time))
			}
		}
	}
	return out
}

// HandleBlock merges the block's contributions into their points, each
// point read and written once.
func (ts *timeSeries) HandleBlock(ctx context.Context, b *feature.Block) error {
	var cs []Contribution
	if ts.chain {
		cs = append(cs, ChainContribution(b.Model, b.Receipts))
	}
	if ts.trades != nil {
		trades, err := ts.trades.ListDexTradesInBlock(ctx, b.Model.Number)
		if err != nil {
			return fmt.Errorf("read the block's trades: %w", err)
		}
		for _, t := range trades {
			cs = append(cs, TradeContribution(t))
		}
	}
	if ts.tokens {
		cs = append(cs, TransferContributions(b.Receipts, b.Model.Time, ts.excluded)...)
	}
	touched := map[port.SeriesKey]*port.SeriesPoint{}
	var order []port.SeriesKey
	for _, c := range cs {
		for _, period := range Periods {
			k := port.SeriesKey{Series: c.Series, Subject: c.Subject, Period: period, Start: PeriodStart(period, c.Time)}
			p, ok := touched[k]
			if !ok {
				var err error
				p, err = ts.store.GetSeriesPoint(ctx, k)
				if errors.Is(err, port.ErrNotFound) {
					p, err = &port.SeriesPoint{Key: k}, nil
				}
				if err != nil {
					return fmt.Errorf("read series point: %w", err)
				}
				touched[k] = p
				order = append(order, k)
			}
			p.Values = MergeValues(p.Values, c.Values)
		}
	}
	for _, k := range order {
		if err := ts.store.SaveSeriesPoint(ctx, touched[k]); err != nil {
			return fmt.Errorf("save series point: %w", err)
		}
	}
	return nil
}

// RecomputeSeries builds the points of contributions from scratch, in any
// order: what agg.timeseries keeps for them. They are sorted by key.
func RecomputeSeries(cs []Contribution) []*port.SeriesPoint {
	byKey := map[port.SeriesKey]*port.SeriesPoint{}
	for _, c := range cs {
		for _, period := range Periods {
			k := port.SeriesKey{Series: c.Series, Subject: c.Subject, Period: period, Start: PeriodStart(period, c.Time)}
			p, ok := byKey[k]
			if !ok {
				p = &port.SeriesPoint{Key: k}
				byKey[k] = p
			}
			p.Values = MergeValues(p.Values, c.Values)
		}
	}
	out := make([]*port.SeriesPoint, 0, len(byKey))
	for _, p := range byKey {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Key, out[j].Key
		if a.Series != b.Series {
			return a.Series < b.Series
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		if a.Period != b.Period {
			return a.Period < b.Period
		}
		return a.Start < b.Start
	})
	return out
}
