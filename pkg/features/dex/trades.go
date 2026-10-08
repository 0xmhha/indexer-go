package dex

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// EventTypeTrade is the event of a recorded trade.
const EventTypeTrade events.EventType = "dexTrade"

// TradeEvent is published for every trade dex.trades records, after its
// block commits.
type TradeEvent struct {
	events.Stream // change stream position (R3-1)
	Trade         port.DexTrade
}

// Type implements events.Event.
func (e *TradeEvent) Type() events.EventType { return EventTypeTrade }

// Timestamp implements events.Event: the block time.
func (e *TradeEvent) Timestamp() time.Time { return time.Unix(int64(e.Trade.Timestamp), 0) }

type tradesFeature struct{}

func (tradesFeature) Name() string       { return TradesName }
func (tradesFeature) Requires() []string { return []string{PoolsName} }

// dex.trades records each trade under its own log, so the order blocks are
// processed in does not change the result; it reads markets and orders
// that dex.pools keeps, which run before it in every block.
func (tradesFeature) OrderIndependent() bool { return true }

func (tradesFeature) Register(r feature.Registrar) error {
	store, ok := r.Deps().Storage.(dexStore)
	if !ok {
		return fmt.Errorf("storage does not support DEX indexing")
	}
	r.OnBlock(&trades{store: store, publish: r.Deps().Publish})
	return nil
}

func init() {
	feature.Register(tradesFeature{})
	events.RegisterCodec(EventTypeTrade, events.StructCodec[TradeEvent]())
}

type trades struct {
	store   dexStore
	publish func(events.Event) bool
}

// HandleBlock implements feature.BlockHandler.
func (t *trades) HandleBlock(ctx context.Context, b *feature.Block) error {
	for _, receipt := range b.Receipts {
		found, err := t.receiptTrades(ctx, b, receipt)
		if err != nil {
			return fmt.Errorf("DEX trades of %s: %w", receipt.TxHash.Hex(), err)
		}
		for _, tr := range found {
			if err := t.store.SaveDexTrade(ctx, tr); err != nil {
				return fmt.Errorf("save DEX trade %s/%d: %w", tr.TxHash.Hex(), tr.LogIndex, err)
			}
			if t.publish != nil {
				t.publish(&TradeEvent{Trade: *tr})
			}
		}
	}
	return nil
}

// receiptTrades returns the trades a transaction's logs record, in log
// order.
func (t *trades) receiptTrades(ctx context.Context, b *feature.Block, receipt *model.Receipt) ([]*port.DexTrade, error) {
	matched := matchedFills(receipt.Logs)
	var out []*port.DexTrade
	for _, log := range receipt.Logs {
		if log == nil || len(log.Topics) == 0 {
			continue
		}
		e := event{log}
		var (
			tr  *port.DexTrade
			err error
		)
		switch {
		case e.is(TopicV3Swap, 3, 5) || e.is(TopicV2Swap, 3, 4):
			tr, err = t.swap(ctx, e)
		case e.is(TopicPerpMarketOrderExecuted, 3, 3):
			tr, err = t.marketOrder(ctx, e)
		case e.is(TopicPerpOrdersMatched, 3, 2):
			tr, err = t.match(ctx, e)
		case e.is(TopicPerpOrderPartiallyFill, 2, 4) && !matched[log.Index]:
			tr, err = t.fill(ctx, e)
		}
		if err != nil {
			return nil, err
		}
		if tr != nil {
			tr.BlockNumber, tr.TxHash, tr.LogIndex, tr.Timestamp = log.BlockNumber, log.TxHash, log.Index, b.Model.Time
			out = append(out, tr)
		}
	}
	return out, nil
}

// matchedFills returns the OrderPartiallyFilled logs a later OrdersMatched
// of the same contract and transaction accounts for: matching two orders
// fills each (maker, then taker) and then reports the match, which is the
// trade. Each match takes the nearest earlier unclaimed fill of each of its
// orders with the match's size and price.
func matchedFills(logs []*model.Log) map[uint]bool {
	claimed := map[uint]bool{}
	for i, log := range logs {
		if log == nil {
			continue
		}
		m := event{log}
		if !m.is(TopicPerpOrdersMatched, 3, 2) {
			continue
		}
		for _, id := range []common.Hash{m.Topics[1], m.Topics[2]} {
			for j := i - 1; j >= 0; j-- {
				f := event{logs[j]}
				if logs[j] == nil || claimed[f.Index] || f.Address != m.Address || !f.is(TopicPerpOrderPartiallyFill, 2, 4) ||
					f.Topics[1] != id || f.uint(0).Cmp(m.uint(0)) != 0 || f.uint(3).Cmp(m.uint(1)) != 0 {
					continue
				}
				claimed[f.Index] = true
				break
			}
		}
	}
	return claimed
}

// market returns a registered market, nil when key names none.
func (t *trades) market(ctx context.Context, key port.DexMarketKey) (*port.DexMarket, error) {
	m, err := t.store.GetDexMarket(ctx, key)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	return m, err
}

// order returns an order, nil when unknown (created before indexing).
func (t *trades) order(ctx context.Context, manager common.Address, id common.Hash) (*port.DexOrder, error) {
	o, err := t.store.GetDexOrder(ctx, manager, id)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	return o, err
}

// swap reads a swap of a registered pool or pair. Amounts are the pool's:
// positive in, negative out; the taker buys base (token0) when base left
// the pool.
func (t *trades) swap(ctx context.Context, e event) (*port.DexTrade, error) {
	m, err := t.market(ctx, port.DexMarketKey{Address: e.Address})
	if err != nil || m == nil {
		return nil, err
	}
	tr := &port.DexTrade{Market: m.Key, Venue: m.Venue, Sender: e.topicAddress(1)}
	var amount0, amount1 *big.Int
	switch {
	case m.Venue == port.DexUniswapV3 && e.Topics[0] == TopicV3Swap:
		amount0, amount1 = e.int(0), e.int(1)
		tr.Taker = e.topicAddress(2)
		tr.SqrtPriceX96, tr.Tick = e.uint(2), int32Of(e.int(4))
	case m.Venue == port.DexUniswapV2 && e.Topics[0] == TopicV2Swap:
		amount0 = new(big.Int).Sub(e.uint(0), e.uint(2))
		amount1 = new(big.Int).Sub(e.uint(1), e.uint(3))
		tr.Taker = e.topicAddress(2)
	default:
		return nil, nil // a swap event of another venue kind at this address
	}
	if amount0.Sign() == 0 && amount1.Sign() == 0 {
		return nil, nil
	}
	tr.Side = port.DexSell
	if amount0.Sign() < 0 {
		tr.Side = port.DexBuy
	}
	tr.BaseAmount, tr.QuoteAmount = new(big.Int).Abs(amount0), new(big.Int).Abs(amount1)
	tr.Price = priceOf(tr.BaseAmount, tr.QuoteAmount)
	return tr, nil
}

// priceOf is quote per base times DexPriceScale (0 without base).
func priceOf(base, quote *big.Int) *big.Int {
	if base.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Div(new(big.Int).Mul(quote, port.DexPriceScale), base)
}

// fillTrade is a perpetual fill of size at price (scaled by
// DexPriceScale).
func fillTrade(market port.DexMarketKey, side port.DexSide, size, price *big.Int) *port.DexTrade {
	return &port.DexTrade{
		Market: market, Venue: port.DexPerpOrderBook, Side: side,
		BaseAmount: size, Price: price,
		QuoteAmount: new(big.Int).Div(new(big.Int).Mul(size, price), port.DexPriceScale),
	}
}

// marketOrder reads a market order executed at once.
func (t *trades) marketOrder(ctx context.Context, e event) (*port.DexTrade, error) {
	key := port.DexMarketKey{Address: e.Address, ID: e.topicUint(2).Uint64()}
	m, err := t.market(ctx, key)
	if err != nil || m == nil || m.Venue != port.DexPerpOrderBook {
		return nil, err
	}
	tr := fillTrade(key, sideOf(e.uint(0)), e.uint(1), e.uint(2))
	tr.Taker = e.topicAddress(1)
	return tr, nil
}

// match reads two orders matched: the taker order's side and trader take,
// the maker order's trader makes.
func (t *trades) match(ctx context.Context, e event) (*port.DexTrade, error) {
	maker, err := t.order(ctx, e.Address, e.Topics[1])
	if err != nil {
		return nil, err
	}
	taker, err := t.order(ctx, e.Address, e.Topics[2])
	if err != nil {
		return nil, err
	}
	if taker == nil && maker == nil {
		return nil, nil // both created before indexing started
	}
	var tr *port.DexTrade
	if taker != nil {
		tr = fillTrade(taker.Market, taker.Side, e.uint(0), e.uint(1))
		tr.Taker = taker.Trader
	} else {
		side := port.DexBuy
		if maker.Side == port.DexBuy {
			side = port.DexSell
		}
		tr = fillTrade(maker.Market, side, e.uint(0), e.uint(1))
	}
	if maker != nil {
		tr.Maker = maker.Trader
	}
	tr.MakerOrder, tr.TakerOrder = e.Topics[1], e.Topics[2]
	return tr, nil
}

// fill reads an order filled against the operator rather than another
// order: the order's trader takes.
func (t *trades) fill(ctx context.Context, e event) (*port.DexTrade, error) {
	o, err := t.order(ctx, e.Address, e.Topics[1])
	if err != nil || o == nil {
		return nil, err
	}
	tr := fillTrade(o.Market, o.Side, e.uint(0), e.uint(3))
	tr.Taker, tr.TakerOrder = o.Trader, o.ID
	return tr, nil
}
