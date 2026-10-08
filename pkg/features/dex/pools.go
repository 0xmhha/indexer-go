// Package dex is the DEX indexing features (refactoring plan R5-1):
// dex.pools registers markets from the factories and engines the
// configuration names and keeps their state, liquidity changes and
// perpetual orders; dex.trades records every trade of a registered market
// and publishes it.
//
// Three venue kinds are read: Uniswap V3 pools (factory PoolCreated), Uniswap
// V2 pairs (factory PairCreated) and perpetual order book markets (engine
// MarketCreated, order manager orders and fills). Only events of registered
// markets count: anyone can deploy a contract emitting the same events.
//
//	features:
//	  dex.pools:
//	    enabled: true
//	    venues:
//	      - type: uniswap_v3
//	        factory: "0x..."
//	      - type: uniswap_v2
//	        factory: "0x..."
//	      - type: perp_orderbook
//	        engine: "0x..."         # emits MarketCreated
//	        order_manager: "0x..."  # emits the order events
//	  dex.trades:
//	    enabled: true
package dex

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// Feature names.
const (
	PoolsName  = "dex.pools"
	TradesName = "dex.trades"
)

// Venue is one exchange the configuration names.
type Venue struct {
	Type         string `yaml:"type"`
	Factory      string `yaml:"factory"`
	Engine       string `yaml:"engine"`
	OrderManager string `yaml:"order_manager"`
}

// Settings are the settings of dex.pools.
type Settings struct {
	Venues []Venue `yaml:"venues"`
}

// venues are the contracts whose registration events count.
type venues struct {
	v2, v3 map[common.Address]bool           // factories
	perp   map[common.Address]common.Address // engine -> order manager
	orders map[common.Address]bool           // order managers
}

func parseAddress(field, s string) (common.Address, error) {
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("%s %q is not an address", field, s)
	}
	return common.HexToAddress(s), nil
}

func (st Settings) venues() (*venues, error) {
	v := &venues{v2: map[common.Address]bool{}, v3: map[common.Address]bool{},
		perp: map[common.Address]common.Address{}, orders: map[common.Address]bool{}}
	for i, ven := range st.Venues {
		var err error
		switch port.DexVenue(ven.Type) {
		case port.DexUniswapV2, port.DexUniswapV3:
			var f common.Address
			if f, err = parseAddress("factory", ven.Factory); err == nil {
				if port.DexVenue(ven.Type) == port.DexUniswapV2 {
					v.v2[f] = true
				} else {
					v.v3[f] = true
				}
			}
		case port.DexPerpOrderBook:
			var engine, manager common.Address
			if engine, err = parseAddress("engine", ven.Engine); err == nil {
				if manager, err = parseAddress("order_manager", ven.OrderManager); err == nil {
					v.perp[engine], v.orders[manager] = manager, true
				}
			}
		default:
			err = fmt.Errorf("type %q is not one of %s, %s, %s", ven.Type, port.DexUniswapV2, port.DexUniswapV3, port.DexPerpOrderBook)
		}
		if err != nil {
			return nil, fmt.Errorf("features.%s.venues[%d]: %w", PoolsName, i, err)
		}
	}
	if len(st.Venues) == 0 {
		return nil, fmt.Errorf("features.%s.venues is empty: name the factories and engines to index", PoolsName)
	}
	return v, nil
}

type poolsFeature struct{}

func (poolsFeature) Name() string       { return PoolsName }
func (poolsFeature) Requires() []string { return nil }

// dex.pools is order-dependent: market state and orders take the latest
// event, so processing an older block after a newer one would restore old
// state.

func (poolsFeature) Register(r feature.Registrar) error {
	deps := r.Deps()
	store, ok := deps.Storage.(dexStore)
	if !ok {
		return fmt.Errorf("storage does not support DEX indexing")
	}
	var st Settings
	if err := deps.DecodeSettings(PoolsName, &st); err != nil {
		return err
	}
	v, err := st.venues()
	if err != nil {
		return err
	}
	r.OnBlock(&pools{store: store, venues: v})
	return nil
}

func init() { feature.Register(poolsFeature{}) }

type dexStore interface {
	port.DexReader
	port.DexWriter
}

type pools struct {
	store  dexStore
	venues *venues
}

// blockState holds the markets and orders a block touches, written once
// each at the end of the block in the order first touched.
type blockState struct {
	store   dexStore
	markets map[port.DexMarketKey]*port.DexMarket
	orders  map[[2]common.Hash]*port.DexOrder
	mOrder  []port.DexMarketKey
	oOrder  [][2]common.Hash
}

func (s *blockState) market(ctx context.Context, key port.DexMarketKey) (*port.DexMarket, error) {
	if m, ok := s.markets[key]; ok {
		return m, nil
	}
	m, err := s.store.GetDexMarket(ctx, key)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.markets[key] = m
	s.mOrder = append(s.mOrder, key)
	return m, nil
}

func (s *blockState) putMarket(m *port.DexMarket) {
	if _, ok := s.markets[m.Key]; !ok {
		s.mOrder = append(s.mOrder, m.Key)
	}
	s.markets[m.Key] = m
}

func orderKey(manager common.Address, id common.Hash) [2]common.Hash {
	return [2]common.Hash{common.BytesToHash(manager.Bytes()), id}
}

func (s *blockState) order(ctx context.Context, manager common.Address, id common.Hash) (*port.DexOrder, error) {
	k := orderKey(manager, id)
	if o, ok := s.orders[k]; ok {
		return o, nil
	}
	o, err := s.store.GetDexOrder(ctx, manager, id)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.orders[k] = o
	s.oOrder = append(s.oOrder, k)
	return o, nil
}

func (s *blockState) putOrder(o *port.DexOrder) {
	k := orderKey(o.Market.Address, o.ID)
	if _, ok := s.orders[k]; !ok {
		s.oOrder = append(s.oOrder, k)
	}
	s.orders[k] = o
}

func (s *blockState) save(ctx context.Context) error {
	for _, k := range s.mOrder {
		if err := s.store.SaveDexMarket(ctx, s.markets[k]); err != nil {
			return fmt.Errorf("save DEX market %s/%d: %w", k.Address.Hex(), k.ID, err)
		}
	}
	for _, k := range s.oOrder {
		o := s.orders[k]
		if err := s.store.SaveDexOrder(ctx, o); err != nil {
			return fmt.Errorf("save DEX order %s: %w", o.ID.Hex(), err)
		}
	}
	return nil
}

// HandleBlock implements feature.BlockHandler.
func (p *pools) HandleBlock(ctx context.Context, b *feature.Block) error {
	st := &blockState{store: p.store, markets: map[port.DexMarketKey]*port.DexMarket{}, orders: map[[2]common.Hash]*port.DexOrder{}}
	for _, receipt := range b.Receipts {
		for _, log := range receipt.Logs {
			if log == nil || len(log.Topics) == 0 {
				continue
			}
			if err := p.handleLog(ctx, st, b, event{log}); err != nil {
				return fmt.Errorf("DEX log %d of %s: %w", log.Index, log.TxHash.Hex(), err)
			}
		}
	}
	return st.save(ctx)
}

func (p *pools) handleLog(ctx context.Context, st *blockState, b *feature.Block, e event) error {
	switch {
	case p.venues.v3[e.Address] && e.is(TopicV3PoolCreated, 4, 2):
		return p.register(ctx, st, e, &port.DexMarket{
			Key: port.DexMarketKey{Address: e.address(1)}, Venue: port.DexUniswapV3,
			Base: e.topicAddress(1), Quote: e.topicAddress(2),
			Fee: uint32(e.topicUint(3).Uint64()), TickSpacing: int32Of(e.int(0)),
		})
	case p.venues.v2[e.Address] && e.is(TopicV2PairCreated, 3, 2):
		return p.register(ctx, st, e, &port.DexMarket{
			Key: port.DexMarketKey{Address: e.address(0)}, Venue: port.DexUniswapV2,
			Base: e.topicAddress(1), Quote: e.topicAddress(2),
		})
	case e.is(TopicPerpMarketCreated, 4, 1):
		manager, ok := p.venues.perp[e.Address]
		if !ok {
			return nil
		}
		return p.register(ctx, st, e, &port.DexMarket{
			Key: port.DexMarketKey{Address: manager, ID: e.topicUint(1).Uint64()}, Venue: port.DexPerpOrderBook,
			Base: e.topicAddress(2), Quote: e.topicAddress(3),
		})
	}
	if p.venues.orders[e.Address] {
		return p.handleOrder(ctx, st, b, e)
	}
	return p.handlePoolLog(ctx, st, b, e)
}

// register records a market created by e. A market already registered by
// the same log (the block processed again) keeps its state.
func (p *pools) register(ctx context.Context, st *blockState, e event, m *port.DexMarket) error {
	m.Creator, m.CreatedBlock, m.CreatedTx, m.CreatedLogIndex = e.Address, e.BlockNumber, e.TxHash, e.Index
	m.UpdatedBlock = e.BlockNumber
	old, err := st.market(ctx, m.Key)
	if err != nil {
		return err
	}
	if old != nil && old.CreatedBlock == m.CreatedBlock && old.CreatedLogIndex == m.CreatedLogIndex {
		return nil
	}
	st.putMarket(m)
	return nil
}

// handlePoolLog updates a registered pool or pair: its state, and its
// liquidity changes.
func (p *pools) handlePoolLog(ctx context.Context, st *blockState, b *feature.Block, e event) error {
	topic := e.Topics[0]
	switch topic {
	case TopicV3Initialize, TopicV3Swap, TopicV3Mint, TopicV3Burn, TopicV2Sync, TopicV2Mint, TopicV2Burn:
	default:
		return nil
	}
	m, err := st.market(ctx, port.DexMarketKey{Address: e.Address})
	if err != nil || m == nil {
		return err
	}
	change := &port.DexLiquidity{
		Market: m.Key, Venue: m.Venue, BlockNumber: e.BlockNumber, TxHash: e.TxHash, LogIndex: e.Index, Timestamp: b.Model.Time,
	}
	switch {
	case m.Venue == port.DexUniswapV3 && e.is(TopicV3Initialize, 1, 2):
		m.SqrtPriceX96, m.Tick, m.Liquidity = e.uint(0), int32Of(e.int(1)), new(big.Int)
	case m.Venue == port.DexUniswapV3 && e.is(TopicV3Swap, 3, 5):
		m.SqrtPriceX96, m.Liquidity, m.Tick = e.uint(2), e.uint(3), int32Of(e.int(4))
	case m.Venue == port.DexUniswapV3 && e.is(TopicV3Mint, 4, 4):
		change.Kind, change.Owner = port.DexAddLiquidity, e.topicAddress(1)
		change.TickLower, change.TickUpper = int32Of(e.topicInt(2)), int32Of(e.topicInt(3))
		change.Liquidity, change.Amount0, change.Amount1 = e.uint(1), e.uint(2), e.uint(3)
		return p.saveLiquidity(ctx, change)
	case m.Venue == port.DexUniswapV3 && e.is(TopicV3Burn, 4, 3):
		change.Kind, change.Owner = port.DexRemoveLiquidity, e.topicAddress(1)
		change.TickLower, change.TickUpper = int32Of(e.topicInt(2)), int32Of(e.topicInt(3))
		change.Liquidity, change.Amount0, change.Amount1 = e.uint(0), e.uint(1), e.uint(2)
		if change.Liquidity.Sign() == 0 {
			return nil // a fee update ("poke"), no liquidity moved
		}
		return p.saveLiquidity(ctx, change)
	case m.Venue == port.DexUniswapV2 && e.is(TopicV2Sync, 1, 2):
		m.Reserve0, m.Reserve1 = e.uint(0), e.uint(1)
	case m.Venue == port.DexUniswapV2 && e.is(TopicV2Mint, 2, 2):
		change.Kind, change.Owner = port.DexAddLiquidity, e.topicAddress(1)
		change.Amount0, change.Amount1 = e.uint(0), e.uint(1)
		return p.saveLiquidity(ctx, change)
	case m.Venue == port.DexUniswapV2 && e.is(TopicV2Burn, 3, 2):
		change.Kind, change.Owner = port.DexRemoveLiquidity, e.topicAddress(2)
		change.Amount0, change.Amount1 = e.uint(0), e.uint(1)
		return p.saveLiquidity(ctx, change)
	default:
		return nil
	}
	m.UpdatedBlock = e.BlockNumber
	return nil
}

func (p *pools) saveLiquidity(ctx context.Context, change *port.DexLiquidity) error {
	if err := p.store.SaveDexLiquidity(ctx, change); err != nil {
		return fmt.Errorf("save DEX liquidity change: %w", err)
	}
	return nil
}

// sideOf maps the order manager's PositionSide (0 long, 1 short).
func sideOf(v *big.Int) port.DexSide {
	if v.Sign() == 0 {
		return port.DexBuy
	}
	return port.DexSell
}

// handleOrder keeps the orders of an order manager.
func (p *pools) handleOrder(ctx context.Context, st *blockState, b *feature.Block, e event) error {
	if e.is(TopicPerpOrderCreated, 4, 4) {
		st.putOrder(&port.DexOrder{
			Market: port.DexMarketKey{Address: e.Address, ID: e.topicUint(3).Uint64()},
			ID:     e.Topics[1], Trader: e.topicAddress(2), Side: sideOf(e.uint(0)), Type: uint8(e.uint(1).Uint64()),
			Size: e.uint(2), Price: e.uint(3), Filled: new(big.Int), Status: port.DexOrderOpen,
			CreatedBlock: e.BlockNumber, CreatedTx: e.TxHash, CreatedLogIndex: e.Index, UpdatedBlock: e.BlockNumber,
		})
		return nil
	}
	var change func(o *port.DexOrder)
	switch {
	case e.is(TopicPerpOrderPartiallyFill, 2, 4):
		total, remaining := e.uint(1), e.uint(2)
		change = func(o *port.DexOrder) {
			o.Filled, o.Status = total, port.DexOrderPartiallyFilled
			if remaining.Sign() == 0 {
				o.Status = port.DexOrderFilled
			}
		}
	case e.is(TopicPerpOrderCancelled, 3, 0):
		change = func(o *port.DexOrder) { o.Status = port.DexOrderCancelled }
	case e.is(TopicPerpOrderExpired, 3, 1):
		change = func(o *port.DexOrder) { o.Status = port.DexOrderExpired }
	case e.is(TopicPerpOrderModified, 3, 2):
		size, price := e.uint(0), e.uint(1)
		change = func(o *port.DexOrder) { o.Size, o.Price = size, price }
	default:
		return nil
	}
	o, err := st.order(ctx, e.Address, e.Topics[1])
	if err != nil || o == nil {
		return err // an order created before indexing started is unknown
	}
	change(o)
	o.UpdatedBlock = e.BlockNumber
	return nil
}
