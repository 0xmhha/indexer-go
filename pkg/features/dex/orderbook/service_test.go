package orderbook

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
)

// memStore holds DEX state in maps; heights counts GetLatestHeight calls
// so a test can move the height while a book is read.
type memStore struct {
	port.DexReader // unused methods
	mu             sync.Mutex
	height         uint64
	onHeight       func(call int) uint64
	calls          int
	markets        map[port.DexMarketKey]*port.DexMarket
	ticks          map[port.DexMarketKey][]*port.DexTick
	orders         map[port.DexMarketKey][]*port.DexOrder
}

func newMemStore() *memStore {
	return &memStore{height: 10, markets: map[port.DexMarketKey]*port.DexMarket{},
		ticks: map[port.DexMarketKey][]*port.DexTick{}, orders: map[port.DexMarketKey][]*port.DexOrder{}}
}

func (s *memStore) GetLatestHeight(context.Context) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.onHeight != nil {
		return s.onHeight(s.calls), nil
	}
	return s.height, nil
}

func (s *memStore) GetDexMarket(_ context.Context, k port.DexMarketKey) (*port.DexMarket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.markets[k]; ok {
		c := *m
		return &c, nil
	}
	return nil, port.ErrNotFound
}

func (s *memStore) ListDexMarkets(context.Context, port.Page) ([]*port.DexMarket, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*port.DexMarket
	for _, m := range s.markets {
		out = append(out, m)
	}
	return out, "", nil
}

func (s *memStore) ListDexTicks(_ context.Context, k port.DexMarketKey) ([]*port.DexTick, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*port.DexTick{}, s.ticks[k]...), nil
}

func (s *memStore) ListDexOpenOrders(_ context.Context, k port.DexMarketKey, _ port.Page) ([]*port.DexOrder, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*port.DexOrder
	for _, o := range s.orders[k] {
		if o.Status.Resting() {
			out = append(out, o)
		}
	}
	return out, "", nil
}

func (s *memStore) set(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

// node answers calls by contract and call data, and records the blocks
// asked for.
type node struct {
	mu      sync.Mutex
	answers map[common.Address]map[string][]byte
	blocks  map[uint64]bool
}

func newNode() *node {
	return &node{answers: map[common.Address]map[string][]byte{}, blocks: map[uint64]bool{}}
}

func (n *node) answer(to common.Address, data []byte, words ...*big.Int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.answers[to] == nil {
		n.answers[to] = map[string][]byte{}
	}
	var out []byte
	for _, w := range words {
		out = append(out, Word(w)...)
	}
	n.answers[to][hex.EncodeToString(data)] = out
}

func (n *node) CallContract(_ context.Context, call ethereum.CallMsg, block interface{}) ([]byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.blocks[block.(*big.Int).Uint64()] = true
	if out, ok := n.answers[*call.To][hex.EncodeToString(call.Data)]; ok {
		return out, nil
	}
	return nil, errors.New("execution reverted")
}

func (n *node) CodeAt(context.Context, common.Address, interface{}) ([]byte, error) { return nil, nil }

var (
	pairKey = port.DexMarketKey{Address: common.HexToAddress("0x00000000000000000000000000000000000b2001")}
	poolKey = port.DexMarketKey{Address: common.HexToAddress("0x00000000000000000000000000000000000b3001")}
	perpKey = port.DexMarketKey{Address: common.HexToAddress("0x00000000000000000000000000000000000e0002"), ID: 7}
)

func n(v int64) *big.Int { return big.NewInt(v) }

// fixture is a pair, a pool with one position and an order book with two
// resting orders, and a node agreeing with them.
func fixture() (*memStore, *node) {
	s, nd := newMemStore(), newNode()
	s.markets[pairKey] = &port.DexMarket{Key: pairKey, Venue: port.DexUniswapV2, Reserve0: e18(100), Reserve1: e18(200)}
	l := e18(1000)
	s.markets[poolKey] = &port.DexMarket{Key: poolKey, Venue: port.DexUniswapV3, TickSpacing: 60, SqrtPriceX96: q96, Tick: 0, Liquidity: l}
	s.ticks[poolKey] = []*port.DexTick{
		{Market: poolKey, Tick: -600, LiquidityGross: l, LiquidityNet: l},
		{Market: poolKey, Tick: 600, LiquidityGross: l, LiquidityNet: new(big.Int).Neg(l)},
	}
	s.markets[perpKey] = &port.DexMarket{Key: perpKey, Venue: port.DexPerpOrderBook}
	order := func(id int64, side port.DexSide, size, filled, price int64, status port.DexOrderStatus) *port.DexOrder {
		return &port.DexOrder{Market: perpKey, ID: common.BigToHash(n(id)), Side: side, Size: n(size), Filled: n(filled), Price: e18(price), Status: status}
	}
	s.orders[perpKey] = []*port.DexOrder{
		order(1, port.DexSell, 10, 4, 50, port.DexOrderPartiallyFilled),
		order(2, port.DexBuy, 5, 0, 49, port.DexOrderOpen),
		order(3, port.DexBuy, 5, 5, 48, port.DexOrderFilled),
	}

	nd.answer(pairKey.Address, CallData(sigGetReserves), e18(100), e18(200), n(1_700_000_000))
	nd.answer(poolKey.Address, CallData(sigSlot0), q96, n(0), n(0), n(0), n(0), n(0), n(1))
	nd.answer(poolKey.Address, CallData(sigLiquidity), l)
	nd.answer(poolKey.Address, CallData(sigTicks, n(-600)), l, l)
	nd.answer(poolKey.Address, CallData(sigTicks, n(600)), l, new(big.Int).Neg(l))
	// Compressed ticks -10 and 10: word -1 bit 246, word 0 bit 10.
	nd.answer(poolKey.Address, CallData(sigTickBitmap, n(-1)), new(big.Int).Lsh(n(1), 246))
	nd.answer(poolKey.Address, CallData(sigTickBitmap, n(0)), new(big.Int).Lsh(n(1), 10))
	chainOrder := func(id, status, size, filled, price int64) {
		words := make([]*big.Int, orderWords)
		for i := range words {
			words[i] = n(0)
		}
		words[0], words[orderWordStatus], words[orderWordSize], words[orderWordFilled], words[orderWordPrice] = n(id), n(status), n(size), n(filled), e18(price)
		nd.answer(perpKey.Address, CallData(sigGetOrder, n(id)), words...)
	}
	chainOrder(1, chainOrderPartiallyFilled, 10, 4, 50)
	chainOrder(2, chainOrderOpen, 5, 0, 49)
	return s, nd
}

func start(t *testing.T, s *memStore, nd *node) *Service {
	t.Helper()
	svc, err := NewService(s, nd, Settings{ReconcileInterval: "0"}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Sync(context.Background()))
	return svc
}

func TestBooksInSyncWithChain(t *testing.T) {
	s, nd := fixture()
	svc := start(t, s, nd)

	books := svc.Books()
	require.Len(t, books, 3)
	perp, _ := svc.Book(perpKey)
	require.NotNil(t, perp)
	assert.Equal(t, uint64(10), perp.Height)
	d := perp.Depth(0, 0)
	require.Len(t, d.Asks, 1)
	assert.Equal(t, "6", d.Asks[0].Base.String(), "what remains of the partly filled order")
	require.Len(t, d.Bids, 1, "the filled order is not in the book")

	results := svc.Reconcile(context.Background())
	require.Len(t, results, 3)
	for k, r := range results {
		assert.True(t, r.InSync(), "%s/%d: %v %s", k.Address.Hex(), k.ID, r.Mismatches, r.Err)
		assert.Equal(t, uint64(10), r.Block)
	}
	assert.Equal(t, map[uint64]bool{10: true}, nd.blocks, "the chain is read at the book's height")
	_, r := svc.Book(poolKey)
	assert.True(t, r.InSync(), "the latest result is kept with the book")
}

// TestBooksDifferFromChain: each kind of difference is reported.
func TestBooksDifferFromChain(t *testing.T) {
	s, nd := fixture()
	l := e18(1000)
	// The pair's reserve, the pool's liquidity and a tick the index missed,
	// an order cancelled on chain and one with another fill.
	nd.answer(pairKey.Address, CallData(sigGetReserves), e18(100), e18(201), n(0))
	nd.answer(poolKey.Address, CallData(sigLiquidity), new(big.Int).Add(l, n(1)))
	nd.answer(poolKey.Address, CallData(sigTickBitmap, n(0)), new(big.Int).Or(new(big.Int).Lsh(n(1), 10), new(big.Int).Lsh(n(1), 3)))
	words := func(status, size, filled, price int64) []*big.Int {
		w := make([]*big.Int, orderWords)
		for i := range w {
			w[i] = n(0)
		}
		w[orderWordStatus], w[orderWordSize], w[orderWordFilled], w[orderWordPrice] = n(status), n(size), n(filled), e18(price)
		return w
	}
	nd.answer(perpKey.Address, CallData(sigGetOrder, n(1)), words(chainOrderPartiallyFilled, 10, 5, 50)...)
	nd.answer(perpKey.Address, CallData(sigGetOrder, n(2)), words(4, 5, 0, 49)...)
	svc := start(t, s, nd)

	results := svc.Reconcile(context.Background())
	mismatches := func(k port.DexMarketKey) []string {
		require.NotNil(t, results[k])
		require.Empty(t, results[k].Err)
		m := append([]string{}, results[k].Mismatches...)
		sort.Strings(m)
		return m
	}
	assert.Equal(t, []string{"reserve1: indexed 200000000000000000000, chain 201000000000000000000"}, mismatches(pairKey))
	assert.Equal(t, []string{"liquidity: indexed 1000000000000000000000, chain 1000000000000000000001", "tick 180: initialized on chain, not indexed"}, mismatches(poolKey))
	assert.Equal(t, []string{
		"order 0x0000000000000000000000000000000000000000000000000000000000000001 filled: indexed 4, chain 5",
		"order 0x0000000000000000000000000000000000000000000000000000000000000002: resting in the book, status 4 on chain",
	}, mismatches(perpKey))
	assert.False(t, results[perpKey].InSync())

	nd.answers[poolKey.Address] = nil
	results = svc.Reconcile(context.Background())
	assert.Contains(t, results[poolKey].Err, "execution reverted", "a failed call is an error, not a mismatch")
}

// TestBookUpdatesAreAtomic: a change publishes a new book; a reader keeps
// the book it loaded, unchanged.
func TestBookUpdatesAreAtomic(t *testing.T) {
	s, nd := fixture()
	svc := start(t, s, nd)
	before, _ := svc.Book(pairKey)
	s.set(func() {
		s.markets[pairKey].Reserve0 = e18(50)
		s.height = 11
	})
	svc.Notify(pairKey)
	require.Eventually(t, func() bool {
		b, _ := svc.Book(pairKey)
		return b.Height == 11
	}, 5*time.Second, time.Millisecond)
	after, _ := svc.Book(pairKey)
	assert.Equal(t, e18(100).String(), before.Market.Reserve0.String(), "the old book is not modified")
	assert.Equal(t, e18(50).String(), after.Market.Reserve0.String())
	assert.Equal(t, "4000000000000000000", after.Depth(0, 0).Mid.String())
}

// TestBookReadDuringIngest: a book whose reads straddle an indexed block is
// read again; one that never settles has height 0 and is not compared.
func TestBookReadDuringIngest(t *testing.T) {
	s, nd := fixture()
	s.onHeight = func(call int) uint64 { return 10 + uint64(min(call, 3)) } // settles after two reads
	svc := start(t, s, nd)
	for _, b := range svc.Books() {
		assert.Equal(t, uint64(13), b.Height)
	}
	s.set(func() { s.onHeight = func(call int) uint64 { return uint64(call) } })
	results := svc.Reconcile(context.Background())
	assert.Equal(t, "blocks were indexed while the book was read", results[pairKey].Err)
}

// TestFollowEvents: a dexMarket event reloads its market and starts a book
// for a new one; a reorg reloads every market and drops one rolled back.
func TestFollowEvents(t *testing.T) {
	s, nd := fixture()
	bus := events.NewEventBus(16, 16)
	go bus.Run()
	defer bus.Stop()
	svc, err := NewService(s, nd, Settings{ReconcileInterval: "0"}, zap.NewNop())
	require.NoError(t, err)
	defer svc.Close()
	require.NoError(t, svc.Follow(bus))
	require.NoError(t, svc.Start(context.Background()))

	other := port.DexMarketKey{Address: common.HexToAddress("0x00000000000000000000000000000000000b2002")}
	s.set(func() {
		s.markets[other] = &port.DexMarket{Key: other, Venue: port.DexUniswapV2, Reserve0: n(1), Reserve1: n(1)}
	})
	bus.Publish(&dex.MarketEvent{Market: other, Venue: port.DexUniswapV2, BlockNumber: 11})
	require.Eventually(t, func() bool { b, _ := svc.Book(other); return b != nil }, 5*time.Second, time.Millisecond)

	s.set(func() {
		delete(s.markets, other)
		s.markets[pairKey].Reserve1 = e18(1)
	})
	bus.Publish(&events.ReorgEvent{})
	require.Eventually(t, func() bool {
		b, _ := svc.Book(other)
		p, _ := svc.Book(pairKey)
		return b == nil && p != nil && p.Market.Reserve1.Cmp(e18(1)) == 0
	}, 5*time.Second, time.Millisecond)
}

func TestSettings(t *testing.T) {
	c, err := Settings{}.config()
	require.NoError(t, err)
	assert.Equal(t, config{stepBps: DefaultStepBps, levels: DefaultLevels, reconcile: time.Minute}, c)
	for name, st := range map[string]Settings{
		"step":     {StepBps: MaxStepBps + 1},
		"levels":   {Levels: -1},
		"interval": {ReconcileInterval: "often"},
	} {
		_, err := st.config()
		assert.Error(t, err, name)
	}
	c, err = Settings{ReconcileInterval: "0s"}.config()
	require.NoError(t, err)
	assert.Zero(t, c.reconcile)

	svc, err := NewService(newMemStore(), nil, Settings{}, nil)
	require.NoError(t, err)
	Attach("store", svc)
	assert.Same(t, svc, Lookup("store"))
	svc.Close()
	assert.Nil(t, Lookup("store"), "closing detaches")
}
