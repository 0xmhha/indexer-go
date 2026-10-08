// Package api serves the DEX records over GraphQL (refactoring plan R5-1):
// markets, trades (per market and per trader), liquidity changes and
// perpetual orders, read a page at a time with cursors, the dexTrade
// subscription, and the order books of dex.orderbook (R5-2). It is a
// GraphQL extension, linked in with the features.
package api

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
)

func init() {
	graphql.RegisterExtension("dex", register)
	graphql.RegisterSubscription("dexTrade", graphql.SubscriptionSpec{
		EventType: dex.EventTypeTrade,
		Filter:    tradeFilter,
		Payload:   tradePayload,
	})
}

type reader = port.DexReader

// Types. Integers beyond 32 bits are strings (BigInt), addresses and hashes
// hex.
var (
	marketType = gql.NewObject(gql.ObjectConfig{
		Name:        "DexMarket",
		Description: "A pool, pair or perpetual market registered by its venue's factory or engine, with its latest state",
		Fields: gql.Fields{
			"address":         {Type: gql.NewNonNull(graphql.AddressType), Description: "Pool or pair address; the order manager for a perpetual market"},
			"marketId":        {Type: gql.NewNonNull(graphql.BigIntType), Description: "Perpetual market id; 0 for pools and pairs"},
			"venue":           {Type: gql.NewNonNull(gql.String), Description: "uniswap_v2, uniswap_v3 or perp_orderbook"},
			"creator":         {Type: gql.NewNonNull(graphql.AddressType)},
			"base":            {Type: gql.NewNonNull(graphql.AddressType), Description: "token0, or the perpetual base token"},
			"quote":           {Type: gql.NewNonNull(graphql.AddressType), Description: "token1, or the perpetual quote token"},
			"fee":             {Type: gql.Int, Description: "Uniswap V3 fee in hundredths of a basis point"},
			"tickSpacing":     {Type: gql.Int},
			"createdBlock":    {Type: gql.NewNonNull(graphql.BigIntType)},
			"createdTx":       {Type: gql.NewNonNull(graphql.HashType)},
			"reserve0":        {Type: graphql.BigIntType},
			"reserve1":        {Type: graphql.BigIntType},
			"sqrtPriceX96":    {Type: graphql.BigIntType},
			"tick":            {Type: gql.Int},
			"liquidity":       {Type: graphql.BigIntType},
			"updatedBlock":    {Type: gql.NewNonNull(graphql.BigIntType)},
			"createdLogIndex": {Type: gql.NewNonNull(gql.Int)},
		},
	})
	tradeType = gql.NewObject(gql.ObjectConfig{
		Name:        "DexTrade",
		Description: "A swap of a pool or pair, or a fill of a perpetual market",
		Fields: gql.Fields{
			"market":          {Type: gql.NewNonNull(graphql.AddressType)},
			"marketId":        {Type: gql.NewNonNull(graphql.BigIntType)},
			"venue":           {Type: gql.NewNonNull(gql.String)},
			"blockNumber":     {Type: gql.NewNonNull(graphql.BigIntType)},
			"transactionHash": {Type: gql.NewNonNull(graphql.HashType)},
			"logIndex":        {Type: gql.NewNonNull(gql.Int)},
			"timestamp":       {Type: gql.NewNonNull(graphql.BigIntType)},
			"side":            {Type: gql.NewNonNull(gql.String), Description: "The taker's side: buy (base out of the market) or sell"},
			"baseAmount":      {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
			"quoteAmount":     {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
			"price":           {Type: gql.NewNonNull(graphql.BigIntType), Description: "Quote per base in raw units, times 1e18"},
			"taker":           {Type: gql.NewNonNull(graphql.AddressType)},
			"sender":          {Type: graphql.AddressType},
			"maker":           {Type: graphql.AddressType},
			"takerOrder":      {Type: graphql.HashType},
			"makerOrder":      {Type: graphql.HashType},
			"sqrtPriceX96":    {Type: graphql.BigIntType},
			"tick":            {Type: gql.Int},
		},
	})
	liquidityType = gql.NewObject(gql.ObjectConfig{
		Name: "DexLiquidityChange",
		Fields: gql.Fields{
			"market":          {Type: gql.NewNonNull(graphql.AddressType)},
			"venue":           {Type: gql.NewNonNull(gql.String)},
			"blockNumber":     {Type: gql.NewNonNull(graphql.BigIntType)},
			"transactionHash": {Type: gql.NewNonNull(graphql.HashType)},
			"logIndex":        {Type: gql.NewNonNull(gql.Int)},
			"timestamp":       {Type: gql.NewNonNull(graphql.BigIntType)},
			"kind":            {Type: gql.NewNonNull(gql.String), Description: "add or remove"},
			"owner":           {Type: gql.NewNonNull(graphql.AddressType)},
			"amount0":         {Type: gql.NewNonNull(graphql.BigIntType)},
			"amount1":         {Type: gql.NewNonNull(graphql.BigIntType)},
			"tickLower":       {Type: gql.Int},
			"tickUpper":       {Type: gql.Int},
			"liquidity":       {Type: graphql.BigIntType},
		},
	})
	orderType = gql.NewObject(gql.ObjectConfig{
		Name: "DexOrder",
		Fields: gql.Fields{
			"manager":      {Type: gql.NewNonNull(graphql.AddressType)},
			"marketId":     {Type: gql.NewNonNull(graphql.BigIntType)},
			"id":           {Type: gql.NewNonNull(graphql.HashType)},
			"trader":       {Type: gql.NewNonNull(graphql.AddressType)},
			"side":         {Type: gql.NewNonNull(gql.String)},
			"type":         {Type: gql.NewNonNull(gql.Int)},
			"size":         {Type: gql.NewNonNull(graphql.BigIntType)},
			"price":        {Type: gql.NewNonNull(graphql.BigIntType), Description: "Times 1e18"},
			"filled":       {Type: gql.NewNonNull(graphql.BigIntType)},
			"status":       {Type: gql.NewNonNull(gql.String), Description: "pending (a trigger order not triggered yet), open, partially_filled, filled, cancelled or expired"},
			"createdBlock": {Type: gql.NewNonNull(graphql.BigIntType)},
			"createdTx":    {Type: gql.NewNonNull(graphql.HashType)},
			"updatedBlock": {Type: gql.NewNonNull(graphql.BigIntType)},
		},
	})
)

// connection is a page of nodes with its pageInfo (no total: the stores
// keep no counts of these lists).
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
	marketConnection    = connection("DexMarketConnection", marketType)
	tradeConnection     = connection("DexTradeConnection", tradeType)
	liquidityConnection = connection("DexLiquidityConnection", liquidityType)
	orderConnection     = connection("DexOrderConnection", orderType)
)

func register(e *graphql.Extension) {
	r, _ := e.Storage().(reader)
	page := &gql.ArgumentConfig{Type: graphql.PaginationInputType()}
	market := gql.FieldConfigArgument{
		"market":     {Type: gql.NewNonNull(gql.String), Description: "Pool, pair or order manager address"},
		"marketId":   {Type: gql.String, Description: "Perpetual market id (default 0)"},
		"pagination": page,
	}
	e.AddQuery("dexMarkets", &gql.Field{
		Type: gql.NewNonNull(marketConnection), Description: "DEX markets in registration order",
		Args: gql.FieldConfigArgument{"pagination": page},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			pg := graphql.Page(p, 0)
			items, next, err := r.ListDexMarkets(p.Context, pg)
			return pageOf(items, pg, next, marketMap), err
		},
	})
	e.AddQuery("dexMarket", &gql.Field{
		Type: marketType, Description: "A DEX market, null when none is registered",
		Args: gql.FieldConfigArgument{"address": {Type: gql.NewNonNull(gql.String)}, "marketId": {Type: gql.String}},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			key, err := marketKey(p.Args["address"], p.Args["marketId"])
			if err != nil {
				return nil, err
			}
			m, err := r.GetDexMarket(p.Context, key)
			if errors.Is(err, port.ErrNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return marketMap(m), nil
		},
	})
	e.AddQuery("dexTrades", &gql.Field{
		Type: gql.NewNonNull(tradeConnection), Description: "A market's trades, newest first",
		Args: market,
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			key, err := marketKey(p.Args["market"], p.Args["marketId"])
			if err != nil {
				return nil, err
			}
			pg := graphql.Page(p, 0)
			items, next, err := r.ListDexTrades(p.Context, key, pg)
			return pageOf(items, pg, next, TradeMap), err
		},
	})
	e.AddQuery("dexTradesByTrader", &gql.Field{
		Type: gql.NewNonNull(tradeConnection), Description: "The trades an address took or made, newest first",
		Args: gql.FieldConfigArgument{"trader": {Type: gql.NewNonNull(gql.String)}, "pagination": page},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			trader, err := address(p.Args["trader"])
			if err != nil {
				return nil, err
			}
			pg := graphql.Page(p, 0)
			items, next, err := r.ListDexTradesByTrader(p.Context, trader, pg)
			return pageOf(items, pg, next, TradeMap), err
		},
	})
	e.AddQuery("dexLiquidityChanges", &gql.Field{
		Type: gql.NewNonNull(liquidityConnection), Description: "A pool's or pair's liquidity changes, newest first",
		Args: market,
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			key, err := marketKey(p.Args["market"], p.Args["marketId"])
			if err != nil {
				return nil, err
			}
			pg := graphql.Page(p, 0)
			items, next, err := r.ListDexLiquidity(p.Context, key, pg)
			return pageOf(items, pg, next, liquidityMap), err
		},
	})
	e.AddQuery("dexOrders", &gql.Field{
		Type: gql.NewNonNull(orderConnection), Description: "A perpetual market's orders, newest first",
		Args: market,
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			key, err := marketKey(p.Args["market"], p.Args["marketId"])
			if err != nil {
				return nil, err
			}
			pg := graphql.Page(p, 0)
			items, next, err := r.ListDexOrders(p.Context, key, pg)
			return pageOf(items, pg, next, orderMap), err
		},
	})
	e.AddQuery("dexOrder", &gql.Field{
		Type: orderType, Description: "An order of a perpetual order manager, null when unknown",
		Args: gql.FieldConfigArgument{"manager": {Type: gql.NewNonNull(gql.String)}, "id": {Type: gql.NewNonNull(gql.String)}},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			if r == nil {
				return nil, errUnsupported
			}
			manager, err := address(p.Args["manager"])
			if err != nil {
				return nil, err
			}
			id, _ := p.Args["id"].(string)
			o, err := r.GetDexOrder(p.Context, manager, common.HexToHash(id))
			if errors.Is(err, port.ErrNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return orderMap(o), nil
		},
	})
	e.AddSubscription("dexTrade", &gql.Field{
		Type:        gql.NewNonNull(tradeType),
		Description: "Trades as their blocks are indexed; filter by market addresses",
		Args:        gql.FieldConfigArgument{"markets": {Type: gql.NewList(gql.NewNonNull(gql.String))}},
	})
	registerOrderBook(e)
}

var errUnsupported = errors.New("the storage does not keep DEX records")

func address(v interface{}) (common.Address, error) {
	s, _ := v.(string)
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
	if s, ok := id.(string); ok && s != "" {
		if key.ID, err = strconv.ParseUint(s, 10, 64); err != nil {
			return port.DexMarketKey{}, fmt.Errorf("invalid marketId %q", s)
		}
	}
	return key, nil
}

func pageOf[T any](items []T, pg port.Page, next string, node func(T) map[string]interface{}) map[string]interface{} {
	nodes := make([]map[string]interface{}, len(items))
	for i, it := range items {
		nodes[i] = node(it)
	}
	return map[string]interface{}{"nodes": nodes, "pageInfo": graphql.CursorPageInfo(pg, next)}
}

func u64(v uint64) string { return strconv.FormatUint(v, 10) }

// optional returns nil for a nil big integer, its decimal string otherwise.
func optional[T interface{ String() string }](v T, isNil bool) interface{} {
	if isNil {
		return nil
	}
	return v.String()
}

func optionalAddress(a common.Address) interface{} {
	if a == (common.Address{}) {
		return nil
	}
	return a.Hex()
}

func optionalHash(h common.Hash) interface{} {
	if h == (common.Hash{}) {
		return nil
	}
	return h.Hex()
}

func marketMap(m *port.DexMarket) map[string]interface{} {
	out := map[string]interface{}{
		"address": m.Key.Address.Hex(), "marketId": u64(m.Key.ID), "venue": string(m.Venue), "creator": m.Creator.Hex(),
		"base": m.Base.Hex(), "quote": m.Quote.Hex(), "createdBlock": u64(m.CreatedBlock), "createdTx": m.CreatedTx.Hex(),
		"createdLogIndex": int(m.CreatedLogIndex), "updatedBlock": u64(m.UpdatedBlock),
		"reserve0": optional(m.Reserve0, m.Reserve0 == nil), "reserve1": optional(m.Reserve1, m.Reserve1 == nil),
		"sqrtPriceX96": optional(m.SqrtPriceX96, m.SqrtPriceX96 == nil), "liquidity": optional(m.Liquidity, m.Liquidity == nil),
	}
	if m.Venue == port.DexUniswapV3 {
		out["fee"], out["tickSpacing"], out["tick"] = int(m.Fee), int(m.TickSpacing), int(m.Tick)
	}
	return out
}

// TradeMap is the GraphQL value of a trade (queries and the dexTrade
// subscription).
func TradeMap(t *port.DexTrade) map[string]interface{} {
	out := map[string]interface{}{
		"market": t.Market.Address.Hex(), "marketId": u64(t.Market.ID), "venue": string(t.Venue),
		"blockNumber": u64(t.BlockNumber), "transactionHash": t.TxHash.Hex(), "logIndex": int(t.LogIndex),
		"timestamp": u64(t.Timestamp), "side": string(t.Side),
		"baseAmount": t.BaseAmount.String(), "quoteAmount": t.QuoteAmount.String(), "price": t.Price.String(),
		"taker": t.Taker.Hex(), "sender": optionalAddress(t.Sender), "maker": optionalAddress(t.Maker),
		"takerOrder": optionalHash(t.TakerOrder), "makerOrder": optionalHash(t.MakerOrder),
		"sqrtPriceX96": optional(t.SqrtPriceX96, t.SqrtPriceX96 == nil),
	}
	if t.Venue == port.DexUniswapV3 {
		out["tick"] = int(t.Tick)
	}
	return out
}

func liquidityMap(l *port.DexLiquidity) map[string]interface{} {
	out := map[string]interface{}{
		"market": l.Market.Address.Hex(), "venue": string(l.Venue), "blockNumber": u64(l.BlockNumber),
		"transactionHash": l.TxHash.Hex(), "logIndex": int(l.LogIndex), "timestamp": u64(l.Timestamp),
		"kind": string(l.Kind), "owner": l.Owner.Hex(), "amount0": l.Amount0.String(), "amount1": l.Amount1.String(),
		"liquidity": optional(l.Liquidity, l.Liquidity == nil),
	}
	if l.Venue == port.DexUniswapV3 {
		out["tickLower"], out["tickUpper"] = int(l.TickLower), int(l.TickUpper)
	}
	return out
}

func orderMap(o *port.DexOrder) map[string]interface{} {
	return map[string]interface{}{
		"manager": o.Market.Address.Hex(), "marketId": u64(o.Market.ID), "id": o.ID.Hex(), "trader": o.Trader.Hex(),
		"side": string(o.Side), "type": int(o.Type), "size": o.Size.String(), "price": o.Price.String(),
		"filled": o.Filled.String(), "status": string(o.Status), "createdBlock": u64(o.CreatedBlock),
		"createdTx": o.CreatedTx.Hex(), "updatedBlock": u64(o.UpdatedBlock),
	}
}

// tradeFilter reads the markets argument of a dexTrade subscription.
func tradeFilter(variables map[string]interface{}) (*events.Filter, error) {
	raw, ok := variables["markets"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	f := events.NewFilter()
	for _, v := range raw {
		a, err := address(v)
		if err != nil {
			return nil, err
		}
		f.Addresses = append(f.Addresses, a)
	}
	return f, nil
}

func tradePayload(ev events.Event) (interface{}, bool) {
	t, ok := ev.(*dex.TradeEvent)
	if !ok {
		return nil, false
	}
	return TradeMap(&t.Trade), true
}
