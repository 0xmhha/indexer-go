package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.DexReader = (*Store)(nil)
	_ port.DexWriter = (*Store)(nil)
)

// GetDexMarket implements port.DexReader.
func (s *Store) GetDexMarket(ctx context.Context, key port.DexMarketKey) (*port.DexMarket, error) {
	return getJSON[port.DexMarket](ctx, s.q(ctx),
		"SELECT data FROM dex_markets WHERE address = $1 AND market_id = $2", key.Address.Bytes(), i64(key.ID))
}

// ListDexMarkets implements port.DexReader.
func (s *Store) ListDexMarkets(ctx context.Context, page port.Page) ([]*port.DexMarket, string, error) {
	return listQuery[*port.DexMarket]{
		list: "dex-markets",
		sql:  "SELECT data FROM dex_markets WHERE true",
		keys: []keyCol{{"created_block", kindInt, false}, {"created_log_index", kindInt, false}},
		scan: scanJSON[port.DexMarket],
		keyOf: func(m *port.DexMarket) []string {
			return []string{u64s(m.CreatedBlock), u64s(uint64(m.CreatedLogIndex))}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// dexLogKeys sort a list of records identified by their log, newest first.
var dexLogKeys = []keyCol{{"block_number", kindInt, true}, {"log_index", kindInt, true}}

func dexMarketList(name string, key port.DexMarketKey) string {
	return name + ":" + key.Address.Hex() + ":" + u64s(key.ID)
}

// ListDexTrades implements port.DexReader.
func (s *Store) ListDexTrades(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexTrade, string, error) {
	return listQuery[*port.DexTrade]{
		list:  dexMarketList("dex-trades", market),
		sql:   "SELECT data FROM dex_trades WHERE address = $1 AND market_id = $2",
		args:  []any{market.Address.Bytes(), i64(market.ID)},
		keys:  dexLogKeys,
		scan:  scanJSON[port.DexTrade],
		keyOf: func(t *port.DexTrade) []string { return []string{u64s(t.BlockNumber), u64s(uint64(t.LogIndex))} },
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// ListDexTradesByTrader implements port.DexReader.
func (s *Store) ListDexTradesByTrader(ctx context.Context, trader common.Address, page port.Page) ([]*port.DexTrade, string, error) {
	return listQuery[*port.DexTrade]{
		list:  "dex-trader:" + trader.Hex(),
		sql:   "SELECT data FROM dex_trades WHERE (taker = $1 OR maker = $1)",
		args:  []any{trader.Bytes()},
		keys:  dexLogKeys,
		scan:  scanJSON[port.DexTrade],
		keyOf: func(t *port.DexTrade) []string { return []string{u64s(t.BlockNumber), u64s(uint64(t.LogIndex))} },
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// ListDexLiquidity implements port.DexReader.
func (s *Store) ListDexLiquidity(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexLiquidity, string, error) {
	return listQuery[*port.DexLiquidity]{
		list:  dexMarketList("dex-liquidity", market),
		sql:   "SELECT data FROM dex_liquidity WHERE address = $1 AND market_id = $2",
		args:  []any{market.Address.Bytes(), i64(market.ID)},
		keys:  dexLogKeys,
		scan:  scanJSON[port.DexLiquidity],
		keyOf: func(l *port.DexLiquidity) []string { return []string{u64s(l.BlockNumber), u64s(uint64(l.LogIndex))} },
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// GetDexOrder implements port.DexReader.
func (s *Store) GetDexOrder(ctx context.Context, manager common.Address, id common.Hash) (*port.DexOrder, error) {
	return getJSON[port.DexOrder](ctx, s.q(ctx),
		"SELECT data FROM dex_orders WHERE manager = $1 AND order_id = $2", manager.Bytes(), id.Bytes())
}

// ListDexOrders implements port.DexReader.
func (s *Store) ListDexOrders(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexOrder, string, error) {
	return listQuery[*port.DexOrder]{
		list: dexMarketList("dex-orders", market),
		sql:  "SELECT data FROM dex_orders WHERE manager = $1 AND market_id = $2",
		args: []any{market.Address.Bytes(), i64(market.ID)},
		keys: []keyCol{{"created_block", kindInt, true}, {"created_log_index", kindInt, true}},
		scan: scanJSON[port.DexOrder],
		keyOf: func(o *port.DexOrder) []string {
			return []string{u64s(o.CreatedBlock), u64s(uint64(o.CreatedLogIndex))}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// ListDexOpenOrders implements port.DexReader.
func (s *Store) ListDexOpenOrders(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexOrder, string, error) {
	return listQuery[*port.DexOrder]{
		list: dexMarketList("dex-open-orders", market),
		sql: `SELECT o.data FROM dex_open_orders p JOIN dex_orders o USING (manager, order_id)
			WHERE p.manager = $1 AND p.market_id = $2`,
		args: []any{market.Address.Bytes(), i64(market.ID)},
		keys: []keyCol{{"p.created_block", kindInt, false}, {"p.created_log_index", kindInt, false}},
		scan: scanJSON[port.DexOrder],
		keyOf: func(o *port.DexOrder) []string {
			return []string{u64s(o.CreatedBlock), u64s(uint64(o.CreatedLogIndex))}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// GetDexTick implements port.DexReader.
func (s *Store) GetDexTick(ctx context.Context, market port.DexMarketKey, tick int32) (*port.DexTick, error) {
	return getJSON[port.DexTick](ctx, s.q(ctx),
		"SELECT data FROM dex_ticks WHERE address = $1 AND market_id = $2 AND tick = $3", market.Address.Bytes(), i64(market.ID), tick)
}

// ListDexTicks implements port.DexReader.
func (s *Store) ListDexTicks(ctx context.Context, market port.DexMarketKey) ([]*port.DexTick, error) {
	return queryJSON[port.DexTick](ctx, s.q(ctx),
		"SELECT data FROM dex_ticks WHERE address = $1 AND market_id = $2 ORDER BY tick", market.Address.Bytes(), i64(market.ID))
}

// saveDex writes a record as JSON with the given statement, whose last
// argument is the data.
func (s *Store) saveDex(ctx context.Context, what string, record any, sql string, args ...any) error {
	if err := s.write(); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", what, err)
	}
	_, err = s.q(ctx).Exec(ctx, sql, append(args, data)...)
	return err
}

// SaveDexMarket implements port.DexWriter.
func (s *Store) SaveDexMarket(ctx context.Context, m *port.DexMarket) error {
	return s.saveDex(ctx, "dex market", m, `INSERT INTO dex_markets (address, market_id, created_block, created_log_index, data)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (address, market_id) DO UPDATE SET created_block = EXCLUDED.created_block,
			created_log_index = EXCLUDED.created_log_index, data = EXCLUDED.data`,
		m.Key.Address.Bytes(), i64(m.Key.ID), i64(m.CreatedBlock), int64(m.CreatedLogIndex))
}

// optionalAddress is NULL for the zero address.
func optionalAddress(a common.Address) any {
	if a == (common.Address{}) {
		return nil
	}
	return a.Bytes()
}

// SaveDexTrade implements port.DexWriter.
func (s *Store) SaveDexTrade(ctx context.Context, t *port.DexTrade) error {
	return s.saveDex(ctx, "dex trade", t, `INSERT INTO dex_trades (block_number, log_index, address, market_id, taker, maker, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (block_number, log_index) DO UPDATE SET address = EXCLUDED.address, market_id = EXCLUDED.market_id,
			taker = EXCLUDED.taker, maker = EXCLUDED.maker, data = EXCLUDED.data`,
		i64(t.BlockNumber), int64(t.LogIndex), t.Market.Address.Bytes(), i64(t.Market.ID),
		optionalAddress(t.Taker), optionalAddress(t.Maker))
}

// SaveDexLiquidity implements port.DexWriter.
func (s *Store) SaveDexLiquidity(ctx context.Context, l *port.DexLiquidity) error {
	return s.saveDex(ctx, "dex liquidity change", l, `INSERT INTO dex_liquidity (block_number, log_index, address, market_id, data)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (block_number, log_index) DO UPDATE SET address = EXCLUDED.address, market_id = EXCLUDED.market_id,
			data = EXCLUDED.data`,
		i64(l.BlockNumber), int64(l.LogIndex), l.Market.Address.Bytes(), i64(l.Market.ID))
}

// SaveDexOrder implements port.DexWriter.
func (s *Store) SaveDexOrder(ctx context.Context, o *port.DexOrder) error {
	err := s.saveDex(ctx, "dex order", o, `INSERT INTO dex_orders (manager, order_id, market_id, created_block, created_log_index, data)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (manager, order_id) DO UPDATE SET market_id = EXCLUDED.market_id,
			created_block = EXCLUDED.created_block, created_log_index = EXCLUDED.created_log_index, data = EXCLUDED.data`,
		o.Market.Address.Bytes(), o.ID.Bytes(), i64(o.Market.ID), i64(o.CreatedBlock), int64(o.CreatedLogIndex))
	if err != nil {
		return err
	}
	if !o.Status.Resting() {
		_, err = s.q(ctx).Exec(ctx, "DELETE FROM dex_open_orders WHERE manager = $1 AND order_id = $2", o.Market.Address.Bytes(), o.ID.Bytes())
		return err
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO dex_open_orders (manager, order_id, market_id, created_block, created_log_index)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (manager, order_id) DO UPDATE SET market_id = EXCLUDED.market_id,
			created_block = EXCLUDED.created_block, created_log_index = EXCLUDED.created_log_index`,
		o.Market.Address.Bytes(), o.ID.Bytes(), i64(o.Market.ID), i64(o.CreatedBlock), int64(o.CreatedLogIndex))
	return err
}

// SaveDexTick implements port.DexWriter.
func (s *Store) SaveDexTick(ctx context.Context, t *port.DexTick) error {
	if t.LiquidityGross == nil || t.LiquidityGross.Sign() == 0 {
		if err := s.write(); err != nil {
			return err
		}
		_, err := s.q(ctx).Exec(ctx, "DELETE FROM dex_ticks WHERE address = $1 AND market_id = $2 AND tick = $3",
			t.Market.Address.Bytes(), i64(t.Market.ID), t.Tick)
		return err
	}
	return s.saveDex(ctx, "dex tick", t, `INSERT INTO dex_ticks (address, market_id, tick, data) VALUES ($1, $2, $3, $4)
		ON CONFLICT (address, market_id, tick) DO UPDATE SET data = EXCLUDED.data`,
		t.Market.Address.Bytes(), i64(t.Market.ID), t.Tick)
}
