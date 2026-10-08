// Package orderbook is the DEX order book (refactoring plan R5-2): the price
// levels of every registered market, kept in memory and checked against the
// chain.
//
// Each market has an actor that owns its book. When a block changes the
// market (dex.pools publishes a dexMarket event after the block commits) or a
// reorganization rolls blocks back, the actor reads the market's state from
// storage and publishes a new immutable Book with one atomic store; readers
// load the current Book without locks (read-copy-update). What a book holds
// depends on the venue:
//
//   - perpetual order books: the resting orders, aggregated by price;
//   - Uniswap V3 pools: the price, in-range liquidity and initialized ticks,
//     whose depth is the liquidity between prices;
//   - Uniswap V2 pairs: the reserves, whose depth follows the constant
//     product curve.
//
// Every reconcile interval each book is read again and compared with the
// contract's own state at the book's block (eth_call): reserves, price,
// liquidity, ticks and the tick bitmap, or every resting order.
//
//	features:
//	  dex.orderbook:
//	    enabled: true
//	    step_bps: 10             # price step of pool and pair levels
//	    levels: 20               # levels per side
//	    reconcile_interval: 1m   # 0 turns the comparison off
package orderbook

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
)

// Name is the feature name.
const Name = "dex.orderbook"

// Limits of the settings and of a depth request.
const (
	DefaultStepBps   = 10
	DefaultLevels    = 20
	MaxLevels        = 200
	MaxStepBps       = 5_000
	defaultReconcile = time.Minute
)

// Settings are the settings of dex.orderbook.
type Settings struct {
	StepBps           int    `yaml:"step_bps"`
	Levels            int    `yaml:"levels"`
	ReconcileInterval string `yaml:"reconcile_interval"`
}

// config is Settings checked, with defaults applied.
type config struct {
	stepBps, levels int
	reconcile       time.Duration
}

func (st Settings) config() (config, error) {
	c := config{stepBps: st.StepBps, levels: st.Levels, reconcile: defaultReconcile}
	if c.stepBps == 0 {
		c.stepBps = DefaultStepBps
	}
	if c.levels == 0 {
		c.levels = DefaultLevels
	}
	if c.stepBps < 1 || c.stepBps > MaxStepBps {
		return c, fmt.Errorf("features.%s.step_bps %d is not between 1 and %d", Name, st.StepBps, MaxStepBps)
	}
	if c.levels < 1 || c.levels > MaxLevels {
		return c, fmt.Errorf("features.%s.levels %d is not between 1 and %d", Name, st.Levels, MaxLevels)
	}
	if st.ReconcileInterval != "" {
		d, err := time.ParseDuration(st.ReconcileInterval)
		if err != nil || d < 0 {
			return c, fmt.Errorf("features.%s.reconcile_interval %q is not a duration", Name, st.ReconcileInterval)
		}
		c.reconcile = d
	}
	return c, nil
}

// orderbookFeature makes dex.orderbook a feature: it is enabled and
// configured like one and needs dex.pools, but keeps no data of its own and
// has no block handler; the API process builds the books (Service).
type orderbookFeature struct{}

func (orderbookFeature) Name() string       { return Name }
func (orderbookFeature) Requires() []string { return []string{dex.PoolsName} }

func (orderbookFeature) Register(r feature.Registrar) error {
	var st Settings
	if err := r.Deps().DecodeSettings(Name, &st); err != nil {
		return err
	}
	_, err := st.config()
	return err
}

func init() { feature.Register(orderbookFeature{}) }

// Store is the storage the books are read from.
type Store interface {
	port.DexReader
	GetLatestHeight(ctx context.Context) (uint64, error)
}

// Book is a market's book at one indexed height. It is never modified once
// published.
type Book struct {
	Market port.DexMarket
	// Height is the indexed height the book was read at; 0 when blocks were
	// indexed while it was read (the next change or reconciliation reads it
	// again).
	Height uint64
	// Ticks are a pool's initialized ticks, in tick order.
	Ticks []*port.DexTick
	// Orders are an order book's resting orders, oldest first.
	Orders     []*port.DexOrder
	bids, asks []Level
	defaults   config
}

// Depth returns the book's price levels: at most levels a side, pool and
// pair levels stepBps apart (0 takes the configured value). Order book
// levels are the orders' own prices.
func (b *Book) Depth(stepBps, levels int) Depth {
	stepBps = b.StepBps(stepBps)
	if levels <= 0 {
		levels = b.defaults.levels
	}
	levels = min(levels, MaxLevels)
	m := b.Market
	switch m.Venue {
	case port.DexUniswapV2:
		return v2Depth(m.Reserve0, m.Reserve1, stepBps, levels)
	case port.DexUniswapV3:
		return v3Depth(m.SqrtPriceX96, m.Tick, m.Liquidity, b.Ticks, stepBps, levels)
	default:
		return bookDepth(b.bids, b.asks, levels)
	}
}

// StepBps is the price step Depth uses for stepBps.
func (b *Book) StepBps(stepBps int) int {
	if stepBps <= 0 {
		return b.defaults.stepBps
	}
	return min(stepBps, MaxStepBps)
}

// Reconciliation is the result of comparing a book with the chain.
type Reconciliation struct {
	// Block is the height compared (the book's height).
	Block uint64
	At    time.Time
	// Mismatches describe every difference found; empty when the book
	// equals the chain.
	Mismatches []string
	// Err is why the comparison could not be made, empty when it was.
	Err string
}

// InSync reports whether the comparison was made and found no difference.
func (r *Reconciliation) InSync() bool { return r != nil && r.Err == "" && len(r.Mismatches) == 0 }

// Service keeps the books of every registered market.
type Service struct {
	store  Store
	caller feature.ContractReader
	cfg    config
	logger *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	actors map[port.DexMarketKey]*actor
	sub    *events.Subscription
	bus    *events.EventBus
}

// NewService creates the books' service over store. caller reads contract
// state for reconciliation; nil turns it off.
func NewService(store Store, caller feature.ContractReader, settings Settings, logger *zap.Logger) (*Service, error) {
	cfg, err := settings.config()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{store: store, caller: caller, cfg: cfg, logger: logger.Named("orderbook"),
		ctx: ctx, cancel: cancel, actors: map[port.DexMarketKey]*actor{}}, nil
}

// Follow makes the books follow the event bus: a dexMarket event reloads
// its market, a reorg event every market. Call it before Start so no
// change made while the markets load is missed.
func (s *Service) Follow(bus *events.EventBus) error {
	sub := bus.Subscribe(events.SubscriptionID("dex-orderbook"), []events.EventType{dex.EventTypeMarket, events.EventTypeReorg}, nil, 0)
	if sub == nil {
		return errors.New("subscribe to the event bus")
	}
	s.mu.Lock()
	s.sub, s.bus = sub, bus
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case ev, ok := <-sub.Channel:
				if !ok {
					return
				}
				switch e := ev.(type) {
				case *dex.MarketEvent:
					s.Notify(e.Market)
				case *events.ReorgEvent:
					s.NotifyAll()
				}
			}
		}
	}()
	return nil
}

// Start loads every registered market and starts the periodic
// reconciliation.
func (s *Service) Start(ctx context.Context) error {
	page := port.FirstPage(500)
	for {
		markets, next, err := s.store.ListDexMarkets(ctx, page)
		if err != nil {
			return fmt.Errorf("list DEX markets: %w", err)
		}
		for _, m := range markets {
			s.Notify(m.Key)
		}
		if next == "" {
			break
		}
		page = port.Page{After: next, Limit: page.Limit}
	}
	if s.caller != nil && s.cfg.reconcile > 0 {
		s.wg.Add(1)
		go s.reconcileLoop()
	}
	return nil
}

// Notify makes a market's actor reload its book, starting the actor for a
// market not seen before.
func (s *Service) Notify(key port.DexMarketKey) {
	s.actor(key).wakeUp()
}

// NotifyAll makes every actor reload its book.
func (s *Service) NotifyAll() {
	for _, a := range s.actorList() {
		a.wakeUp()
	}
}

func (s *Service) actor(key port.DexMarketKey) *actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.actors[key]
	if !ok {
		a = &actor{svc: s, key: key, wake: make(chan struct{}, 1), requests: make(chan func())}
		s.actors[key] = a
		s.wg.Add(1)
		go a.run()
	}
	return a
}

// actorList returns the actors in market order.
func (s *Service) actorList() []*actor {
	s.mu.Lock()
	out := make([]*actor, 0, len(s.actors))
	for _, a := range s.actors {
		out = append(out, a)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if c := out[i].key.Address.Cmp(out[j].key.Address); c != 0 {
			return c < 0
		}
		return out[i].key.ID < out[j].key.ID
	})
	return out
}

// Book returns a market's current book and its latest reconciliation (nil
// before the first); nil when the market has no book.
func (s *Service) Book(key port.DexMarketKey) (*Book, *Reconciliation) {
	s.mu.Lock()
	a, ok := s.actors[key]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}
	return a.book.Load(), a.recon.Load()
}

// Books returns the current book of every market, in market order.
func (s *Service) Books() []*Book {
	var out []*Book
	for _, a := range s.actorList() {
		if b := a.book.Load(); b != nil {
			out = append(out, b)
		}
	}
	return out
}

// Sync reloads every book and waits until they are loaded.
func (s *Service) Sync(ctx context.Context) error {
	for _, a := range s.actorList() {
		if err := a.do(ctx, func() { a.reload() }); err != nil {
			return err
		}
	}
	return nil
}

// Close stops the actors and the reconciliation.
func (s *Service) Close() {
	s.mu.Lock()
	sub, bus := s.sub, s.bus
	s.mu.Unlock()
	if sub != nil {
		bus.Unsubscribe(sub.ID)
	}
	s.cancel()
	s.wg.Wait()
	detach(s)
}

// actor owns one market's book: every load and comparison of the market
// runs on its goroutine, one at a time.
type actor struct {
	svc      *Service
	key      port.DexMarketKey
	book     atomic.Pointer[Book]
	recon    atomic.Pointer[Reconciliation]
	wake     chan struct{} // capacity 1: wake-ups coalesce
	requests chan func()
}

func (a *actor) wakeUp() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// do runs fn on the actor's goroutine and waits for it.
func (a *actor) do(ctx context.Context, fn func()) error {
	done := make(chan struct{})
	select {
	case a.requests <- func() { fn(); close(done) }:
	case <-ctx.Done():
		return ctx.Err()
	case <-a.svc.ctx.Done():
		return errors.New("order book service closed")
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *actor) run() {
	defer a.svc.wg.Done()
	for {
		select {
		case <-a.svc.ctx.Done():
			return
		case <-a.wake:
			a.reload()
		case fn := <-a.requests:
			fn()
		}
	}
}

// reload reads the market from storage and publishes its book.
func (a *actor) reload() {
	b, err := a.svc.load(a.svc.ctx, a.key)
	if err != nil {
		if a.svc.ctx.Err() == nil {
			a.svc.logger.Warn("Failed to load order book", zap.String("market", a.key.Address.Hex()), zap.Uint64("id", a.key.ID), zap.Error(err))
		}
		return
	}
	a.book.Store(b)
}

// load reads a market's book. The indexed height is read before and after
// the state; when they differ a block was indexed in between and the reads
// are made again (three times at most, then the book has height 0).
func (s *Service) load(ctx context.Context, key port.DexMarketKey) (*Book, error) {
	var b *Book
	for range 3 {
		before, err := s.height(ctx)
		if err != nil {
			return nil, err
		}
		m, err := s.store.GetDexMarket(ctx, key)
		if errors.Is(err, port.ErrNotFound) {
			return nil, nil // not registered (or rolled back)
		}
		if err != nil {
			return nil, err
		}
		b = &Book{Market: *m, defaults: s.cfg}
		switch m.Venue {
		case port.DexUniswapV3:
			if b.Ticks, err = s.store.ListDexTicks(ctx, key); err != nil {
				return nil, err
			}
		case port.DexPerpOrderBook:
			if b.Orders, err = s.openOrders(ctx, key); err != nil {
				return nil, err
			}
			b.bids, b.asks = orderLevels(b.Orders)
		}
		after, err := s.height(ctx)
		if err != nil {
			return nil, err
		}
		if before == after {
			b.Height = after
			return b, nil
		}
	}
	return b, nil
}

func (s *Service) height(ctx context.Context) (uint64, error) {
	h, err := s.store.GetLatestHeight(ctx)
	if errors.Is(err, port.ErrNotFound) {
		return 0, nil
	}
	return h, err
}

func (s *Service) openOrders(ctx context.Context, key port.DexMarketKey) ([]*port.DexOrder, error) {
	var out []*port.DexOrder
	page := port.FirstPage(500)
	for {
		orders, next, err := s.store.ListDexOpenOrders(ctx, key, page)
		if err != nil {
			return nil, fmt.Errorf("list open orders: %w", err)
		}
		out = append(out, orders...)
		if next == "" {
			return out, nil
		}
		page = port.Page{After: next, Limit: page.Limit}
	}
}

// Books are found by the storage they read, so the API extension of a
// chain finds the books of that chain.
var attached sync.Map // storage -> *Service

// Attach makes svc the books of the storage store (what the GraphQL
// extension's Storage returns).
func Attach(store any, svc *Service) { attached.Store(store, svc) }

// Lookup returns the books attached to store, nil when there are none.
func Lookup(store any) *Service {
	if v, ok := attached.Load(store); ok {
		return v.(*Service)
	}
	return nil
}

func detach(svc *Service) {
	attached.Range(func(k, v any) bool {
		if v == svc {
			attached.Delete(k)
		}
		return true
	})
}
