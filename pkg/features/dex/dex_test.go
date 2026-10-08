package dex

import (
	"context"
	"encoding/json"
	"math/big"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// memStore keeps DEX records in maps (the storage adapters are checked by
// porttest; this checks what the features write).
type memStore struct {
	port.Reader // unused
	markets     map[port.DexMarketKey]*port.DexMarket
	trades      []*port.DexTrade
	liquidity   []*port.DexLiquidity
	orders      map[[2]common.Hash]*port.DexOrder
	ticks       map[tickKey]*port.DexTick
}

func newMemStore() *memStore {
	return &memStore{markets: map[port.DexMarketKey]*port.DexMarket{}, orders: map[[2]common.Hash]*port.DexOrder{},
		ticks: map[tickKey]*port.DexTick{}}
}

func clone[T any](v *T) *T {
	b, _ := json.Marshal(v)
	out := new(T)
	_ = json.Unmarshal(b, out)
	return out
}

func (s *memStore) GetDexMarket(_ context.Context, k port.DexMarketKey) (*port.DexMarket, error) {
	if m, ok := s.markets[k]; ok {
		return clone(m), nil
	}
	return nil, port.ErrNotFound
}
func (s *memStore) ListDexMarkets(context.Context, port.Page) ([]*port.DexMarket, string, error) {
	return nil, "", nil
}
func (s *memStore) ListDexTrades(context.Context, port.DexMarketKey, port.Page) ([]*port.DexTrade, string, error) {
	return nil, "", nil
}
func (s *memStore) ListDexTradesInBlock(_ context.Context, n uint64) ([]*port.DexTrade, error) {
	var out []*port.DexTrade
	for _, t := range s.trades {
		if t.BlockNumber == n {
			out = append(out, clone(t))
		}
	}
	return out, nil
}
func (s *memStore) ListDexTradesByTrader(context.Context, common.Address, port.Page) ([]*port.DexTrade, string, error) {
	return nil, "", nil
}
func (s *memStore) ListDexLiquidity(context.Context, port.DexMarketKey, port.Page) ([]*port.DexLiquidity, string, error) {
	return nil, "", nil
}
func (s *memStore) GetDexOrder(_ context.Context, m common.Address, id common.Hash) (*port.DexOrder, error) {
	if o, ok := s.orders[orderKey(m, id)]; ok {
		return clone(o), nil
	}
	return nil, port.ErrNotFound
}
func (s *memStore) ListDexOrders(context.Context, port.DexMarketKey, port.Page) ([]*port.DexOrder, string, error) {
	return nil, "", nil
}
func (s *memStore) ListDexOpenOrders(context.Context, port.DexMarketKey, port.Page) ([]*port.DexOrder, string, error) {
	return nil, "", nil
}
func (s *memStore) GetDexTick(_ context.Context, m port.DexMarketKey, i int32) (*port.DexTick, error) {
	if t, ok := s.ticks[tickKey{m, i}]; ok {
		return clone(t), nil
	}
	return nil, port.ErrNotFound
}
func (s *memStore) ListDexTicks(context.Context, port.DexMarketKey) ([]*port.DexTick, error) {
	return nil, nil
}
func (s *memStore) SaveDexTick(_ context.Context, t *port.DexTick) error {
	if t.LiquidityGross.Sign() == 0 {
		delete(s.ticks, tickKey{t.Market, t.Tick})
	} else {
		s.ticks[tickKey{t.Market, t.Tick}] = clone(t)
	}
	return nil
}
func (s *memStore) SaveDexMarket(_ context.Context, m *port.DexMarket) error {
	s.markets[m.Key] = clone(m)
	return nil
}
func (s *memStore) SaveDexTrade(_ context.Context, t *port.DexTrade) error {
	s.trades = append(s.trades, clone(t))
	return nil
}
func (s *memStore) SaveDexLiquidity(_ context.Context, l *port.DexLiquidity) error {
	s.liquidity = append(s.liquidity, clone(l))
	return nil
}
func (s *memStore) SaveDexOrder(_ context.Context, o *port.DexOrder) error {
	s.orders[orderKey(o.Market.Address, o.ID)] = clone(o)
	return nil
}

// registrar collects the handlers of features registered with deps.
type registrar struct {
	deps      feature.Deps
	handlers  []feature.BlockHandler
	rollbacks []feature.RollbackHandler
}

func (r *registrar) Deps() feature.Deps             { return r.deps }
func (r *registrar) OnBlock(h feature.BlockHandler) { r.handlers = append(r.handlers, h) }
func (r *registrar) Enabled(string) bool            { return true }
func (r *registrar) OnRollback(h feature.RollbackHandler) {
	r.rollbacks = append(r.rollbacks, h)
}

var (
	v3Factory = common.HexToAddress("0x0000000000000000000000000000000000f30001")
	v2Factory = common.HexToAddress("0x0000000000000000000000000000000000f20001")
	engine    = common.HexToAddress("0x0000000000000000000000000000000000e00001")
	manager   = common.HexToAddress("0x0000000000000000000000000000000000e00002")
	pool      = common.HexToAddress("0x0000000000000000000000000000000000b00001")
	pair      = common.HexToAddress("0x0000000000000000000000000000000000b00002")
	fake      = common.HexToAddress("0x0000000000000000000000000000000000bad001")
	tokenA    = common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	tokenB    = common.HexToAddress("0x000000000000000000000000000000000000bbbb")
	router    = common.HexToAddress("0x0000000000000000000000000000000000c00001")
	alice     = common.HexToAddress("0x0000000000000000000000000000000000a11ce0")
	bob       = common.HexToAddress("0x0000000000000000000000000000000000b0b000")
)

func testSettings() Settings {
	return Settings{Venues: []Venue{
		{Type: "uniswap_v3", Factory: v3Factory.Hex()},
		{Type: "uniswap_v2", Factory: v2Factory.Hex()},
		{Type: "perp_orderbook", Engine: engine.Hex(), OrderManager: manager.Hex()},
	}}
}

// setup registers both features over store and returns their handlers in
// execution order, and the published events.
func setup(t *testing.T, store *memStore, st Settings) ([]feature.BlockHandler, *[]events.Event) {
	t.Helper()
	var published []events.Event
	r := &registrar{deps: feature.Deps{
		Storage: store, Logger: zap.NewNop(),
		Publish: func(e events.Event) bool { published = append(published, e); return true },
		Settings: func(name string, into any) error {
			if name == PoolsName {
				*(into.(*Settings)) = st
			}
			return nil
		},
	}}
	require.NoError(t, poolsFeature{}.Register(r))
	require.NoError(t, tradesFeature{}.Register(r))
	return r.handlers, &published
}

// chainBlock builds block n whose transactions carry the given logs (one
// transaction per slice), numbering logs across the block.
func chainBlock(n uint64, txs ...[]*model.Log) *feature.Block {
	b := &feature.Block{Model: &model.Block{Number: n, Time: 1_700_000_000 + n}}
	idx := uint(0)
	for i, logs := range txs {
		tx := common.BigToHash(new(big.Int).SetUint64(n<<16 | uint64(i)))
		for _, l := range logs {
			l.BlockNumber, l.TxHash, l.Index = n, tx, idx
			idx++
		}
		b.Receipts = append(b.Receipts, &model.Receipt{TxHash: tx, TxIndex: uint(i), Logs: logs})
	}
	return b
}

// byType splits published events into trade and market events.
func byType(published []events.Event) (trades, markets []events.Event) {
	for _, e := range published {
		switch e.Type() {
		case EventTypeTrade:
			trades = append(trades, e)
		case EventTypeMarket:
			markets = append(markets, e)
		}
	}
	return trades, markets
}

func run(t *testing.T, handlers []feature.BlockHandler, b *feature.Block) {
	t.Helper()
	for _, h := range handlers {
		require.NoError(t, h.HandleBlock(context.Background(), b))
	}
}

func word(v int64) []byte {
	b := new(big.Int).SetInt64(v)
	if v < 0 {
		b.Add(b, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return common.LeftPadBytes(b.Bytes(), 32)
}

func addrWord(a common.Address) []byte { return common.LeftPadBytes(a.Bytes(), 32) }

func topic(a common.Address) common.Hash { return common.BytesToHash(a.Bytes()) }

func topicInt(v int64) common.Hash { return common.BytesToHash(word(v)) }

func data(words ...[]byte) []byte {
	var out []byte
	for _, w := range words {
		out = append(out, w...)
	}
	return out
}

func scaled(v int64) *big.Int { return new(big.Int).Mul(big.NewInt(v), port.DexPriceScale) }

func TestTopics(t *testing.T) {
	// Published topics of the Uniswap events.
	for name, c := range map[string]struct{ got, want common.Hash }{
		"V3 PoolCreated": {TopicV3PoolCreated, common.HexToHash("0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118")},
		"V3 Swap":        {TopicV3Swap, common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67")},
		"V3 Mint":        {TopicV3Mint, common.HexToHash("0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde")},
		"V3 Burn":        {TopicV3Burn, common.HexToHash("0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c")},
		"V2 PairCreated": {TopicV2PairCreated, common.HexToHash("0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9")},
		"V2 Swap":        {TopicV2Swap, common.HexToHash("0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822")},
		"V2 Sync":        {TopicV2Sync, common.HexToHash("0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1")},
	} {
		assert.Equal(t, c.want, c.got, name)
	}
}

func TestSettings(t *testing.T) {
	_, err := testSettings().venues()
	require.NoError(t, err)
	for name, st := range map[string]Settings{
		"empty":        {},
		"unknown type": {Venues: []Venue{{Type: "uniswap_v4", Factory: v3Factory.Hex()}}},
		"bad factory":  {Venues: []Venue{{Type: "uniswap_v3", Factory: "factory"}}},
		"no manager":   {Venues: []Venue{{Type: "perp_orderbook", Engine: engine.Hex()}}},
	} {
		_, err := st.venues()
		assert.Error(t, err, name)
	}
	r := &registrar{deps: feature.Deps{Storage: newMemStore()}}
	assert.ErrorContains(t, poolsFeature{}.Register(r), "venues is empty", "dex.pools without venues")
}

func TestUniswapV3(t *testing.T) {
	store := newMemStore()
	handlers, published := setup(t, store, testSettings())
	key := port.DexMarketKey{Address: pool}

	poolCreated := func(factory, p common.Address) *model.Log {
		return &model.Log{Address: factory, Topics: []common.Hash{TopicV3PoolCreated, topic(tokenA), topic(tokenB), common.BigToHash(big.NewInt(3000))},
			Data: data(word(60), addrWord(p))}
	}
	swap := func(at common.Address, a0, a1 int64) *model.Log {
		return &model.Log{Address: at, Topics: []common.Hash{TopicV3Swap, topic(router), topic(alice)},
			Data: data(word(a0), word(a1), word(1<<40), word(5000), word(-120))}
	}
	run(t, handlers, chainBlock(1,
		[]*model.Log{poolCreated(v3Factory, pool), poolCreated(fake, fake)}, // a factory not configured
		[]*model.Log{{Address: pool, Topics: []common.Hash{TopicV3Initialize}, Data: data(word(1<<40), word(-100))}},
		[]*model.Log{{Address: pool, Topics: []common.Hash{TopicV3Mint, topic(bob), topicInt(-600), topicInt(600)},
			Data: data(addrWord(router), word(7000), word(100), word(400))}},
	))
	require.Len(t, store.markets, 1, "only the configured factory registers")
	m := store.markets[key]
	assert.Equal(t, port.DexUniswapV3, m.Venue)
	assert.Equal(t, v3Factory, m.Creator)
	assert.Equal(t, uint32(3000), m.Fee)
	assert.Equal(t, int32(60), m.TickSpacing)
	assert.Equal(t, int32(-100), m.Tick)
	require.Len(t, store.liquidity, 1)
	l := store.liquidity[0]
	assert.Equal(t, port.DexAddLiquidity, l.Kind)
	assert.Equal(t, bob, l.Owner)
	assert.Equal(t, [2]int32{-600, 600}, [2]int32{l.TickLower, l.TickUpper})
	assert.Equal(t, "7000/100/400", l.Liquidity.String()+"/"+l.Amount0.String()+"/"+l.Amount1.String())
	assert.Equal(t, "7000", m.Liquidity.String(), "the position is in range")

	run(t, handlers, chainBlock(2,
		[]*model.Log{swap(pool, -1000, 2500)}, // base out of the pool: a buy
		[]*model.Log{swap(pool, 400, -900)},   // base in: a sell
		[]*model.Log{swap(fake, -1, 1)},       // not a registered pool
		[]*model.Log{{Address: pool, Topics: []common.Hash{TopicV3Burn, topic(bob), topicInt(-600), topicInt(600)},
			Data: data(word(0), word(0), word(0))}}, // a poke
	))
	require.Len(t, store.trades, 2)
	buy, sell := store.trades[0], store.trades[1]
	assert.Equal(t, port.DexBuy, buy.Side)
	assert.Equal(t, "1000/2500", buy.BaseAmount.String()+"/"+buy.QuoteAmount.String())
	assert.Equal(t, new(big.Int).Div(scaled(25), big.NewInt(10)).String(), buy.Price.String(), "2.5 quote per base")
	assert.Equal(t, alice, buy.Taker)
	assert.Equal(t, router, buy.Sender)
	assert.Equal(t, int32(-120), buy.Tick)
	assert.Equal(t, uint64(2), buy.BlockNumber)
	assert.Equal(t, uint64(1_700_000_002), buy.Timestamp)
	assert.Equal(t, port.DexSell, sell.Side)
	assert.Equal(t, "400/900", sell.BaseAmount.String()+"/"+sell.QuoteAmount.String())
	assert.Len(t, store.liquidity, 1, "a burn of no liquidity is no change")
	assert.Equal(t, "5000", store.markets[key].Liquidity.String(), "state from the last swap")
	assert.Equal(t, uint64(2), store.markets[key].UpdatedBlock)

	tradeEvents, marketEvents := byType(*published)
	require.Len(t, tradeEvents, 2)
	ev := tradeEvents[0].(*TradeEvent)
	assert.Equal(t, buy.TxHash, ev.Trade.TxHash)
	_, err := events.MarshalEvent(ev)
	assert.NoError(t, err, "the trade event has a codec for the outbox")
	require.Len(t, marketEvents, 2, "the pool changed in both blocks, once each")
	me := marketEvents[1].(*MarketEvent)
	assert.Equal(t, key, me.Market)
	assert.Equal(t, port.DexUniswapV3, me.Venue)
	assert.Equal(t, uint64(2), me.BlockNumber)
	_, err = events.MarshalEvent(me)
	assert.NoError(t, err, "the market event has a codec for the outbox")
}

func TestUniswapV2(t *testing.T) {
	store := newMemStore()
	handlers, _ := setup(t, store, testSettings())
	key := port.DexMarketKey{Address: pair}
	run(t, handlers, chainBlock(1,
		[]*model.Log{{Address: v2Factory, Topics: []common.Hash{TopicV2PairCreated, topic(tokenA), topic(tokenB)}, Data: data(addrWord(pair), word(1))}},
		[]*model.Log{
			{Address: pair, Topics: []common.Hash{TopicV2Mint, topic(router)}, Data: data(word(1000), word(3000))},
			{Address: pair, Topics: []common.Hash{TopicV2Sync}, Data: data(word(1000), word(3000))},
		},
		[]*model.Log{
			// 30 of token1 in, 10 of token0 out: a buy of base.
			{Address: pair, Topics: []common.Hash{TopicV2Swap, topic(router), topic(alice)}, Data: data(word(0), word(30), word(10), word(0))},
			{Address: pair, Topics: []common.Hash{TopicV2Sync}, Data: data(word(990), word(3030))},
		},
		[]*model.Log{{Address: pair, Topics: []common.Hash{TopicV2Burn, topic(router), topic(bob)}, Data: data(word(99), word(303))}},
	))
	m := store.markets[key]
	require.NotNil(t, m)
	assert.Equal(t, port.DexUniswapV2, m.Venue)
	assert.Equal(t, "990/3030", m.Reserve0.String()+"/"+m.Reserve1.String())
	require.Len(t, store.trades, 1)
	tr := store.trades[0]
	assert.Equal(t, port.DexBuy, tr.Side)
	assert.Equal(t, "10/30", tr.BaseAmount.String()+"/"+tr.QuoteAmount.String())
	assert.Equal(t, scaled(3).String(), tr.Price.String())
	assert.Equal(t, alice, tr.Taker)
	require.Len(t, store.liquidity, 2)
	assert.Equal(t, port.DexAddLiquidity, store.liquidity[0].Kind)
	assert.Equal(t, router, store.liquidity[0].Owner)
	assert.Equal(t, port.DexRemoveLiquidity, store.liquidity[1].Kind)
	assert.Equal(t, bob, store.liquidity[1].Owner, "a burn's tokens go to its recipient")
}

func perpOrder(id common.Hash, trader common.Address, market, side, size, price int64) *model.Log {
	return &model.Log{Address: manager, Topics: []common.Hash{TopicPerpOrderCreated, id, topic(trader), common.BigToHash(big.NewInt(market))},
		Data: data(word(side), word(1), word(size), scaledWord(price))}
}

func scaledWord(v int64) []byte { return common.LeftPadBytes(scaled(v).Bytes(), 32) }

func partialFill(id common.Hash, size, total, remaining, price int64) *model.Log {
	return &model.Log{Address: manager, Topics: []common.Hash{TopicPerpOrderPartiallyFill, id},
		Data: data(word(size), word(total), word(remaining), scaledWord(price))}
}

func TestPerpOrderBook(t *testing.T) {
	store := newMemStore()
	handlers, published := setup(t, store, testSettings())
	market := port.DexMarketKey{Address: manager, ID: 7}
	makerID, takerID, soloID, gone := common.HexToHash("0x01"), common.HexToHash("0x02"), common.HexToHash("0x03"), common.HexToHash("0x04")

	run(t, handlers, chainBlock(1,
		[]*model.Log{
			{Address: engine, Topics: []common.Hash{TopicPerpMarketCreated, common.BigToHash(big.NewInt(7)), {}, topic(tokenB)}, Data: word(20)},
			{Address: fake, Topics: []common.Hash{TopicPerpMarketCreated, common.BigToHash(big.NewInt(8)), {}, topic(tokenB)}, Data: word(20)},
		},
		[]*model.Log{perpOrder(makerID, bob, 7, 1, 10, 50)},   // short limit 10 @ 50
		[]*model.Log{perpOrder(takerID, alice, 7, 0, 10, 51)}, // long limit 10 @ 51
		[]*model.Log{perpOrder(soloID, alice, 7, 0, 5, 49)},
		[]*model.Log{perpOrder(gone, bob, 7, 1, 1, 60)},
	))
	require.Len(t, store.markets, 1, "only the configured engine registers")
	assert.Equal(t, port.DexPerpOrderBook, store.markets[market].Venue)
	assert.Equal(t, engine, store.markets[market].Creator)
	require.Len(t, store.orders, 4)

	run(t, handlers, chainBlock(2,
		// matchOrders(maker, taker, 4, 50): two fills, then the match.
		[]*model.Log{
			partialFill(makerID, 4, 4, 6, 50),
			partialFill(takerID, 4, 4, 6, 50),
			{Address: manager, Topics: []common.Hash{TopicPerpOrdersMatched, makerID, takerID}, Data: data(word(4), scaledWord(50))},
		},
		// fillOrder(solo, 5, 48): a fill against the operator.
		[]*model.Log{partialFill(soloID, 5, 5, 0, 48)},
		// executeMarketOrder(bob, 7, short, 2, 47).
		[]*model.Log{{Address: manager, Topics: []common.Hash{TopicPerpMarketOrderExecuted, topic(bob), common.BigToHash(big.NewInt(7))},
			Data: data(word(1), word(2), scaledWord(47))}},
		[]*model.Log{{Address: manager, Topics: []common.Hash{TopicPerpOrderCancelled, gone, topic(bob)}, Data: data(word(32), word(0))}},
	))

	require.Len(t, store.trades, 3, "a match is one trade, not three")
	match, solo, marketOrder := store.trades[0], store.trades[1], store.trades[2]
	assert.Equal(t, port.DexBuy, match.Side, "the taker order is long")
	assert.Equal(t, alice, match.Taker)
	assert.Equal(t, bob, match.Maker)
	assert.Equal(t, [2]common.Hash{makerID, takerID}, [2]common.Hash{match.MakerOrder, match.TakerOrder})
	assert.Equal(t, "4", match.BaseAmount.String())
	assert.Equal(t, "200", match.QuoteAmount.String())
	assert.Equal(t, scaled(50).String(), match.Price.String())
	assert.Equal(t, market, match.Market)

	assert.Equal(t, alice, solo.Taker)
	assert.Equal(t, soloID, solo.TakerOrder)
	assert.Equal(t, scaled(48).String(), solo.Price.String())
	assert.Equal(t, common.Address{}, solo.Maker)

	assert.Equal(t, port.DexSell, marketOrder.Side)
	assert.Equal(t, bob, marketOrder.Taker)
	assert.Equal(t, "94", marketOrder.QuoteAmount.String())

	status := func(id common.Hash) port.DexOrderStatus { return store.orders[orderKey(manager, id)].Status }
	assert.Equal(t, port.DexOrderPartiallyFilled, status(makerID))
	assert.Equal(t, "4", store.orders[orderKey(manager, makerID)].Filled.String())
	assert.Equal(t, port.DexOrderFilled, status(soloID))
	assert.Equal(t, port.DexOrderCancelled, status(gone))
	tradeEvents, marketEvents := byType(*published)
	assert.Len(t, tradeEvents, 3)
	require.Len(t, marketEvents, 2, "the market changed in both blocks; the foreign engine's market did not")
	for _, e := range marketEvents {
		assert.Equal(t, market, e.(*MarketEvent).Market)
	}

	// Trades are listed by log: each has its own.
	var idx []int
	for _, tr := range store.trades {
		idx = append(idx, int(tr.LogIndex))
	}
	assert.True(t, sort.IntsAreSorted(idx))
}

// TestMatchClaimsOnlyItsFills: a fill of the same order at another size or
// price, earlier in the transaction, stays a trade of its own.
func TestMatchClaimsOnlyItsFills(t *testing.T) {
	a, b := common.HexToHash("0x0a"), common.HexToHash("0x0b")
	logs := []*model.Log{
		partialFill(a, 1, 1, 9, 50), // a separate fill of a
		partialFill(a, 4, 5, 5, 51),
		partialFill(b, 4, 4, 6, 51),
		{Address: manager, Topics: []common.Hash{TopicPerpOrdersMatched, a, b}, Data: data(word(4), scaledWord(51))},
	}
	for i, l := range logs {
		l.Index = uint(i)
	}
	assert.Equal(t, map[uint]bool{1: true, 2: true}, matchedFills(logs))
}

// TestUniswapV3Ticks: positions keep the gross and net liquidity of their
// bound ticks as the pool does, change the in-range liquidity only when the
// current tick is inside them, and a tick left with no liquidity is removed.
func TestUniswapV3Ticks(t *testing.T) {
	store := newMemStore()
	handlers, _ := setup(t, store, testSettings())
	key := port.DexMarketKey{Address: pool}
	position := func(topic0 common.Hash, lower, upper, liquidity int64) *model.Log {
		if topic0 == TopicV3Mint {
			return &model.Log{Address: pool, Topics: []common.Hash{TopicV3Mint, topic(bob), topicInt(lower), topicInt(upper)},
				Data: data(addrWord(router), word(liquidity), word(1), word(1))}
		}
		return &model.Log{Address: pool, Topics: []common.Hash{TopicV3Burn, topic(bob), topicInt(lower), topicInt(upper)},
			Data: data(word(liquidity), word(1), word(1))}
	}
	ticks := func() map[int32]string {
		out := map[int32]string{}
		for k, tk := range store.ticks {
			out[k.tick] = tk.LiquidityGross.String() + "/" + tk.LiquidityNet.String()
		}
		return out
	}
	run(t, handlers, chainBlock(1,
		[]*model.Log{
			{Address: v3Factory, Topics: []common.Hash{TopicV3PoolCreated, topic(tokenA), topic(tokenB), common.BigToHash(big.NewInt(3000))},
				Data: data(word(60), addrWord(pool))},
			{Address: pool, Topics: []common.Hash{TopicV3Initialize}, Data: data(word(1<<40), word(10))},
		},
		[]*model.Log{position(TopicV3Mint, -120, 120, 1000)}, // in range
		[]*model.Log{position(TopicV3Mint, 120, 240, 300)},   // above: shares tick 120
		[]*model.Log{position(TopicV3Mint, -60, 0, 50)},      // below the current tick
	))
	assert.Equal(t, map[int32]string{-120: "1000/1000", 120: "1300/-700", 240: "300/-300", -60: "50/50", 0: "50/-50"}, ticks())
	assert.Equal(t, "1000", store.markets[key].Liquidity.String(), "only the position around tick 10")

	run(t, handlers, chainBlock(2,
		[]*model.Log{position(TopicV3Burn, -120, 120, 400)},
		[]*model.Log{position(TopicV3Burn, -60, 0, 50)}, // the position is closed
	))
	assert.Equal(t, map[int32]string{-120: "600/600", 120: "900/-300", 240: "300/-300"}, ticks(), "ticks -60 and 0 are removed")
	assert.Equal(t, "600", store.markets[key].Liquidity.String())
	assert.Equal(t, uint64(2), store.markets[key].UpdatedBlock)
}

// TestPerpTriggerOrders: stop and take profit orders wait for their
// trigger, and rest once triggered.
func TestPerpTriggerOrders(t *testing.T) {
	store := newMemStore()
	handlers, _ := setup(t, store, testSettings())
	stop, limit := common.HexToHash("0x0e"), common.HexToHash("0x0f")
	created := perpOrder(stop, bob, 7, 1, 3, 40)
	created.Data = data(word(1), word(3), word(3), scaledWord(40)) // stop limit
	run(t, handlers, chainBlock(1,
		[]*model.Log{{Address: engine, Topics: []common.Hash{TopicPerpMarketCreated, common.BigToHash(big.NewInt(7)), {}, topic(tokenB)}, Data: word(20)}},
		[]*model.Log{created, perpOrder(limit, bob, 7, 1, 3, 40)},
	))
	status := func(id common.Hash) port.DexOrderStatus { return store.orders[orderKey(manager, id)].Status }
	assert.Equal(t, port.DexOrderPending, status(stop))
	assert.Equal(t, port.DexOrderOpen, status(limit))
	assert.False(t, status(stop).Resting())

	run(t, handlers, chainBlock(2, []*model.Log{{Address: manager, Topics: []common.Hash{TopicPerpOrderTriggered, stop},
		Data: data(scaledWord(41), scaledWord(40))}}))
	assert.Equal(t, port.DexOrderOpen, status(stop))
	assert.Equal(t, uint64(2), store.orders[orderKey(manager, stop)].UpdatedBlock)
}

// TestTradesWithdrawnOnRollback: a rolled-back block's trades are published
// again with Removed set, newest first.
func TestTradesWithdrawnOnRollback(t *testing.T) {
	store := newMemStore()
	var published []events.Event
	r := &registrar{deps: feature.Deps{Storage: store, Logger: zap.NewNop(),
		Publish: func(e events.Event) bool { published = append(published, e); return true },
		Settings: func(name string, into any) error {
			if name == PoolsName {
				*(into.(*Settings)) = testSettings()
			}
			return nil
		}}}
	require.NoError(t, poolsFeature{}.Register(r))
	require.NoError(t, tradesFeature{}.Register(r))
	require.Len(t, r.rollbacks, 1, "dex.trades withdraws its trades")
	run(t, r.handlers, chainBlock(1, []*model.Log{{Address: v2Factory, Topics: []common.Hash{TopicV2PairCreated, topic(tokenA), topic(tokenB)}, Data: data(addrWord(pair), word(1))}}))
	swap := func(in0, out0 int64) *model.Log {
		return &model.Log{Address: pair, Topics: []common.Hash{TopicV2Swap, topic(router), topic(alice)}, Data: data(word(in0), word(10), word(out0), word(0))}
	}
	run(t, r.handlers, chainBlock(2, []*model.Log{swap(0, 5)}, []*model.Log{swap(3, 0)}))

	withdrawn, err := r.rollbacks[0].HandleRollback(context.Background(), &port.OrphanedBlock{Block: &model.Block{Number: 2}})
	require.NoError(t, err)
	require.Len(t, withdrawn, 2)
	for i, wantLog := range []uint{1, 0} {
		ev := withdrawn[i].(*TradeEvent)
		assert.True(t, ev.Removed)
		assert.Equal(t, wantLog, ev.Trade.LogIndex, "newest first")
		_, err := events.MarshalEvent(ev)
		assert.NoError(t, err)
	}
	none, err := r.rollbacks[0].HandleRollback(context.Background(), &port.OrphanedBlock{Block: &model.Block{Number: 1}})
	require.NoError(t, err)
	assert.Empty(t, none)
}
