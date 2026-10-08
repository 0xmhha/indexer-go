package port

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// DEX indexing (refactoring plan R5-1): markets registered by a venue's
// factory or engine, their trades, liquidity changes and, for order book
// venues, orders. Three venue kinds share these records.

// DexVenue is the kind of exchange a market belongs to.
type DexVenue string

const (
	// DexUniswapV2 is a constant-product pair (Uniswap V2 events).
	DexUniswapV2 DexVenue = "uniswap_v2"
	// DexUniswapV3 is a concentrated-liquidity pool (Uniswap V3 events).
	DexUniswapV3 DexVenue = "uniswap_v3"
	// DexPerpOrderBook is a perpetual futures market whose orders are
	// matched by an operator and settled on chain (OrderManager events).
	DexPerpOrderBook DexVenue = "perp_orderbook"
)

// DexMarketKey identifies a market: the contract its trades come from and,
// for a venue that keeps several markets in one contract (the perpetual
// order manager), the market id; 0 for a pool or pair.
type DexMarketKey struct {
	Address common.Address `json:"address"`
	ID      uint64         `json:"id"`
}

// DexMarket is a market registered by its venue's factory (pools, pairs) or
// engine (perpetual markets), with its latest state.
type DexMarket struct {
	Key   DexMarketKey `json:"key"`
	Venue DexVenue     `json:"venue"`
	// Creator is the factory or engine whose event registered the market.
	Creator common.Address `json:"creator"`
	// Base and Quote are token0 and token1 of a pool or pair, and the base
	// and quote (collateral) tokens of a perpetual market.
	Base  common.Address `json:"base"`
	Quote common.Address `json:"quote"`
	// Fee is a Uniswap V3 pool's fee in hundredths of a basis point.
	Fee uint32 `json:"fee,omitempty"`
	// TickSpacing is a Uniswap V3 pool's tick spacing.
	TickSpacing int32 `json:"tickSpacing,omitempty"`

	CreatedBlock    uint64      `json:"createdBlock"`
	CreatedTx       common.Hash `json:"createdTx"`
	CreatedLogIndex uint        `json:"createdLogIndex"`

	// State from the latest event that set it (nil until one did): the
	// reserves of a V2 pair (Sync), the price, tick and in-range liquidity
	// of a V3 pool (Initialize, Swap, Mint, Burn).
	Reserve0     *big.Int `json:"reserve0,omitempty"`
	Reserve1     *big.Int `json:"reserve1,omitempty"`
	SqrtPriceX96 *big.Int `json:"sqrtPriceX96,omitempty"`
	Tick         int32    `json:"tick,omitempty"`
	Liquidity    *big.Int `json:"liquidity,omitempty"`
	UpdatedBlock uint64   `json:"updatedBlock"`
}

// DexSide is the taker's side of a trade, or an order's side.
type DexSide string

const (
	// DexBuy takes base out of the market (a perpetual long).
	DexBuy DexSide = "buy"
	// DexSell puts base into the market (a perpetual short).
	DexSell DexSide = "sell"
)

// DexPriceScale is the scale of DexTrade.Price: quote per base, times 1e18.
var DexPriceScale = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// DexTrade is one trade: a swap of a pool or pair, or a fill of a
// perpetual market. It is identified by the log that recorded it.
type DexTrade struct {
	Market      DexMarketKey `json:"market"`
	Venue       DexVenue     `json:"venue"`
	BlockNumber uint64       `json:"blockNumber"`
	TxHash      common.Hash  `json:"txHash"`
	LogIndex    uint         `json:"logIndex"`
	Timestamp   uint64       `json:"timestamp"` // block time, Unix seconds

	// Side is the taker's side.
	Side DexSide `json:"side"`
	// BaseAmount and QuoteAmount are what changed hands, in raw token units
	// (no decimals applied); a perpetual fill's quote amount is size times
	// price.
	BaseAmount  *big.Int `json:"baseAmount"`
	QuoteAmount *big.Int `json:"quoteAmount"`
	// Price is quote per base in raw units, times DexPriceScale: the fill
	// price of a perpetual market, quote amount over base amount for a pool
	// or pair.
	Price *big.Int `json:"price"`

	// Taker received the base or quote: a swap's recipient, a perpetual
	// fill's taker trader. Sender is a swap's caller (often a router).
	// Maker is a perpetual fill's maker trader (zero for swaps and fills
	// without a matched maker order).
	Taker  common.Address `json:"taker"`
	Sender common.Address `json:"sender,omitempty"`
	Maker  common.Address `json:"maker,omitempty"`

	// Perpetual fills: the orders filled (zero when none).
	TakerOrder common.Hash `json:"takerOrder,omitempty"`
	MakerOrder common.Hash `json:"makerOrder,omitempty"`

	// Uniswap V3 swaps: the pool's price and tick after the swap.
	SqrtPriceX96 *big.Int `json:"sqrtPriceX96,omitempty"`
	Tick         int32    `json:"tick,omitempty"`
}

// DexLiquidityKind says whether liquidity was added or removed.
type DexLiquidityKind string

const (
	DexAddLiquidity    DexLiquidityKind = "add"
	DexRemoveLiquidity DexLiquidityKind = "remove"
)

// DexLiquidity is liquidity added to or removed from a pool or pair (Mint,
// Burn).
type DexLiquidity struct {
	Market      DexMarketKey     `json:"market"`
	Venue       DexVenue         `json:"venue"`
	BlockNumber uint64           `json:"blockNumber"`
	TxHash      common.Hash      `json:"txHash"`
	LogIndex    uint             `json:"logIndex"`
	Timestamp   uint64           `json:"timestamp"`
	Kind        DexLiquidityKind `json:"kind"`
	// Owner is the position owner (V3) or the caller (V2 Mint, Burn).
	Owner   common.Address `json:"owner"`
	Amount0 *big.Int       `json:"amount0"`
	Amount1 *big.Int       `json:"amount1"`
	// Uniswap V3: the position's range and the liquidity added or removed.
	TickLower int32    `json:"tickLower,omitempty"`
	TickUpper int32    `json:"tickUpper,omitempty"`
	Liquidity *big.Int `json:"liquidity,omitempty"`
}

// DexOrderStatus is the state of a perpetual market order.
type DexOrderStatus string

const (
	// DexOrderPending is a trigger order (stop, take profit) waiting for its
	// trigger price; it is not in the book until triggered.
	DexOrderPending         DexOrderStatus = "pending"
	DexOrderOpen            DexOrderStatus = "open"
	DexOrderPartiallyFilled DexOrderStatus = "partially_filled"
	DexOrderFilled          DexOrderStatus = "filled"
	DexOrderCancelled       DexOrderStatus = "cancelled"
	DexOrderExpired         DexOrderStatus = "expired"
)

// Resting reports whether an order with this status is in the book (open
// or partially filled).
func (s DexOrderStatus) Resting() bool { return s == DexOrderOpen || s == DexOrderPartiallyFilled }

// DexOrder is an order of a perpetual market.
type DexOrder struct {
	Market DexMarketKey   `json:"market"`
	ID     common.Hash    `json:"id"`
	Trader common.Address `json:"trader"`
	Side   DexSide        `json:"side"`
	// Type is the order manager's order type (0 market, 1 limit, 2 stop,
	// 3 stop limit, 4 take profit, 5 take profit limit).
	Type   uint8          `json:"type"`
	Size   *big.Int       `json:"size"`
	Price  *big.Int       `json:"price"`
	Filled *big.Int       `json:"filled"`
	Status DexOrderStatus `json:"status"`

	CreatedBlock    uint64      `json:"createdBlock"`
	CreatedTx       common.Hash `json:"createdTx"`
	CreatedLogIndex uint        `json:"createdLogIndex"`
	UpdatedBlock    uint64      `json:"updatedBlock"`
}

// DexTick is an initialized tick of a Uniswap V3 pool: the liquidity of the
// positions with a bound at the tick (gross) and the liquidity added when
// the price crosses the tick upwards (net; removed when it crosses
// downwards), as the pool keeps them (refactoring plan R5-2).
type DexTick struct {
	Market         DexMarketKey `json:"market"`
	Tick           int32        `json:"tick"`
	LiquidityGross *big.Int     `json:"liquidityGross"`
	LiquidityNet   *big.Int     `json:"liquidityNet"`
}

// DexReader reads the DEX records.
type DexReader interface {
	// GetDexMarket returns a market, ErrNotFound when none is registered
	// under key.
	GetDexMarket(ctx context.Context, key DexMarketKey) (*DexMarket, error)
	// ListDexMarkets returns one page of the markets in registration order
	// (block, log index) and the cursor of the next page.
	ListDexMarkets(ctx context.Context, page Page) ([]*DexMarket, string, error)
	// ListDexTrades returns one page of a market's trades, newest first (by
	// block and log index, descending).
	ListDexTrades(ctx context.Context, market DexMarketKey, page Page) ([]*DexTrade, string, error)
	// ListDexTradesByTrader returns one page of the trades an address took
	// or made, newest first.
	ListDexTradesByTrader(ctx context.Context, trader common.Address, page Page) ([]*DexTrade, string, error)
	// ListDexLiquidity returns one page of a market's liquidity changes,
	// newest first.
	ListDexLiquidity(ctx context.Context, market DexMarketKey, page Page) ([]*DexLiquidity, string, error)
	// GetDexOrder returns an order of the order manager at manager,
	// ErrNotFound when none was created under id.
	GetDexOrder(ctx context.Context, manager common.Address, id common.Hash) (*DexOrder, error)
	// ListDexOrders returns one page of a market's orders, newest first (by
	// creation block and log index, descending).
	ListDexOrders(ctx context.Context, market DexMarketKey, page Page) ([]*DexOrder, string, error)
	// ListDexOpenOrders returns one page of a market's resting orders (open
	// or partially filled), oldest first (by creation block and log index).
	ListDexOpenOrders(ctx context.Context, market DexMarketKey, page Page) ([]*DexOrder, string, error)
	// GetDexTick returns an initialized tick of a pool, ErrNotFound when the
	// tick has no liquidity.
	GetDexTick(ctx context.Context, market DexMarketKey, tick int32) (*DexTick, error)
	// ListDexTicks returns every initialized tick of a pool, in tick order.
	ListDexTicks(ctx context.Context, market DexMarketKey) ([]*DexTick, error)
}

// DexWriter writes the DEX records. Writing a market or an order again
// replaces it; a trade or liquidity change is identified by its log (block,
// log index) and writing it again replaces it. A tick is identified by its
// pool and index; writing one with zero gross liquidity removes it.
type DexWriter interface {
	SaveDexMarket(ctx context.Context, market *DexMarket) error
	SaveDexTrade(ctx context.Context, trade *DexTrade) error
	SaveDexLiquidity(ctx context.Context, change *DexLiquidity) error
	SaveDexOrder(ctx context.Context, order *DexOrder) error
	SaveDexTick(ctx context.Context, tick *DexTick) error
}
