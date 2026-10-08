package storage

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.DexReader = (*PebbleStorage)(nil)
	_ port.DexWriter = (*PebbleStorage)(nil)
)

// DEX keys (refactoring plan R5-1). A market key is its address and id
// (28 bytes), a log position its block and log index (12 bytes), all
// big-endian so keys sort numerically.
//
//	/dex/market/<market>                       market (JSON)
//	/dex/markets/<created position>            -> <market>
//	/dex/trade/<market><position>              trade (JSON)
//	/dex/trader/<address><position>            -> <market> (taker and maker)
//	/dex/block/<position>                      -> <market> (trades by block)
//	/dex/liquidity/<market><position>          liquidity change (JSON)
//	/dex/order/<manager><order id>             order (JSON)
//	/dex/orders/<market><created position>     -> <order id>
//	/dex/open/<market><created position>       -> <order id> (resting orders)
//	/dex/tick/<market><tick + 2^31>            tick (JSON)
const (
	prefixDexMarket    = "/dex/market/"
	prefixDexMarkets   = "/dex/markets/"
	prefixDexTrade     = "/dex/trade/"
	prefixDexTrader    = "/dex/trader/"
	prefixDexBlock     = "/dex/block/"
	prefixDexLiquidity = "/dex/liquidity/"
	prefixDexOrder     = "/dex/order/"
	prefixDexOrders    = "/dex/orders/"
	prefixDexOpen      = "/dex/open/"
	prefixDexTick      = "/dex/tick/"
)

func init() {
	RegisterKeyspace("dex", ChainData, prefixDexMarket, prefixDexMarkets, prefixDexTrade, prefixDexTrader, prefixDexBlock,
		prefixDexLiquidity, prefixDexOrder, prefixDexOrders, prefixDexOpen, prefixDexTick)
}

const dexMarketKeyLen = common.AddressLength + 8

func dexMarketBytes(k port.DexMarketKey) []byte {
	return binary.BigEndian.AppendUint64(append([]byte{}, k.Address.Bytes()...), k.ID)
}

func dexMarketFrom(b []byte) port.DexMarketKey {
	return port.DexMarketKey{
		Address: common.BytesToAddress(b[:common.AddressLength]),
		ID:      binary.BigEndian.Uint64(b[common.AddressLength:dexMarketKeyLen]),
	}
}

func dexPosition(block uint64, logIndex uint) []byte {
	return binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint64(nil, block), uint32(logIndex))
}

func dexKey(prefix string, parts ...[]byte) []byte {
	k := []byte(prefix)
	for _, p := range parts {
		k = append(k, p...)
	}
	return k
}

// getDexJSON reads a JSON record, port.ErrNotFound when the key is absent.
func getDexJSON[T any](ctx context.Context, s *PebbleStorage, key []byte) (*T, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}
	value, closer, err := s.kv(ctx).Get(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	v := new(T)
	if err := json.Unmarshal(value, v); err != nil {
		return nil, fmt.Errorf("decode %q: %w", key, err)
	}
	return v, nil
}

// putDex writes the entries of one record.
func (s *PebbleStorage) putDex(ctx context.Context, entries ...[2][]byte) error {
	return s.writeDex(ctx, entries, nil)
}

// writeDex writes the entries of one record and deletes the keys of del.
func (s *PebbleStorage) writeDex(ctx context.Context, entries [][2][]byte, del [][]byte) error {
	if s.closed.Load() {
		return port.ErrClosed
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	batch := s.newBatch(ctx)
	defer func() { _ = batch.Close() }()
	for _, e := range entries {
		if err := batch.Set(e[0], e[1], nil); err != nil {
			return err
		}
	}
	for _, k := range del {
		if err := batch.Delete(k, nil); err != nil {
			return err
		}
	}
	return s.commitBatch(ctx, batch, pebble.Sync)
}

// dexPage reads one page of the JSON records under prefix, newest first or
// in key order.
func dexPage[T any](ctx context.Context, s *PebbleStorage, prefix []byte, newestFirst bool, page port.Page) ([]*T, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), newestFirst, page, limit, nil)
	if err != nil {
		return nil, "", err
	}
	out := make([]*T, 0, len(entries))
	for _, e := range entries {
		v := new(T)
		if err := json.Unmarshal(e.Value, v); err != nil {
			return nil, "", fmt.Errorf("decode %q: %w", e.Key, err)
		}
		out = append(out, v)
	}
	return out, next, nil
}

// GetDexMarket implements port.DexReader.
func (s *PebbleStorage) GetDexMarket(ctx context.Context, key port.DexMarketKey) (*port.DexMarket, error) {
	return getDexJSON[port.DexMarket](ctx, s, dexKey(prefixDexMarket, dexMarketBytes(key)))
}

// ListDexMarkets implements port.DexReader.
func (s *PebbleStorage) ListDexMarkets(ctx context.Context, page port.Page) ([]*port.DexMarket, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	prefix := []byte(prefixDexMarkets)
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	isRef := func(_, value []byte) bool { return len(value) == dexMarketKeyLen }
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, limit, isRef)
	if err != nil {
		return nil, "", err
	}
	out := make([]*port.DexMarket, 0, len(entries))
	for _, e := range entries {
		m, err := s.GetDexMarket(ctx, dexMarketFrom(e.Value))
		if err != nil {
			return nil, "", err
		}
		out = append(out, m)
	}
	return out, next, nil
}

// ListDexTrades implements port.DexReader.
func (s *PebbleStorage) ListDexTrades(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexTrade, string, error) {
	return dexPage[port.DexTrade](ctx, s, dexKey(prefixDexTrade, dexMarketBytes(market)), true, page)
}

// ListDexTradesByTrader implements port.DexReader.
func (s *PebbleStorage) ListDexTradesByTrader(ctx context.Context, trader common.Address, page port.Page) ([]*port.DexTrade, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	prefix := dexKey(prefixDexTrader, trader.Bytes())
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	isRef := func(_, value []byte) bool { return len(value) == dexMarketKeyLen }
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), true, page, limit, isRef)
	if err != nil {
		return nil, "", err
	}
	out := make([]*port.DexTrade, 0, len(entries))
	for _, e := range entries {
		position := e.Key[len(prefix):]
		t, err := getDexJSON[port.DexTrade](ctx, s, dexKey(prefixDexTrade, e.Value, position))
		if err != nil {
			return nil, "", err
		}
		out = append(out, t)
	}
	return out, next, nil
}

// ListDexTradesInBlock implements port.DexReader.
func (s *PebbleStorage) ListDexTradesInBlock(ctx context.Context, block uint64) ([]*port.DexTrade, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}
	prefix := dexKey(prefixDexBlock, be64(block))
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()
	var out []*port.DexTrade
	for iter.First(); iter.Valid(); iter.Next() {
		position := iter.Key()[len(prefixDexBlock):]
		t, err := getDexJSON[port.DexTrade](ctx, s, dexKey(prefixDexTrade, iter.Value(), position))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, iter.Error()
}

// ListDexLiquidity implements port.DexReader.
func (s *PebbleStorage) ListDexLiquidity(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexLiquidity, string, error) {
	return dexPage[port.DexLiquidity](ctx, s, dexKey(prefixDexLiquidity, dexMarketBytes(market)), true, page)
}

// GetDexOrder implements port.DexReader.
func (s *PebbleStorage) GetDexOrder(ctx context.Context, manager common.Address, id common.Hash) (*port.DexOrder, error) {
	return getDexJSON[port.DexOrder](ctx, s, dexKey(prefixDexOrder, manager.Bytes(), id.Bytes()))
}

// ListDexOrders implements port.DexReader.
func (s *PebbleStorage) ListDexOrders(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexOrder, string, error) {
	return s.dexOrderPage(ctx, prefixDexOrders, market, true, page)
}

// ListDexOpenOrders implements port.DexReader.
func (s *PebbleStorage) ListDexOpenOrders(ctx context.Context, market port.DexMarketKey, page port.Page) ([]*port.DexOrder, string, error) {
	return s.dexOrderPage(ctx, prefixDexOpen, market, false, page)
}

// dexOrderPage reads one page of a market's order list under prefix.
func (s *PebbleStorage) dexOrderPage(ctx context.Context, prefix string, market port.DexMarketKey, newestFirst bool, page port.Page) ([]*port.DexOrder, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	list := dexKey(prefix, dexMarketBytes(market))
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	isRef := func(_, value []byte) bool { return len(value) == common.HashLength }
	entries, next, err := s.scanPage(ctx, list, prefixUpperBound(list), newestFirst, page, limit, isRef)
	if err != nil {
		return nil, "", err
	}
	out := make([]*port.DexOrder, 0, len(entries))
	for _, e := range entries {
		o, err := s.GetDexOrder(ctx, market.Address, common.BytesToHash(e.Value))
		if err != nil {
			return nil, "", err
		}
		out = append(out, o)
	}
	return out, next, nil
}

// dexTickBytes encodes a tick so ticks sort numerically.
func dexTickBytes(tick int32) []byte {
	return binary.BigEndian.AppendUint32(nil, uint32(int64(tick)+1<<31))
}

// GetDexTick implements port.DexReader.
func (s *PebbleStorage) GetDexTick(ctx context.Context, market port.DexMarketKey, tick int32) (*port.DexTick, error) {
	return getDexJSON[port.DexTick](ctx, s, dexKey(prefixDexTick, dexMarketBytes(market), dexTickBytes(tick)))
}

// ListDexTicks implements port.DexReader.
func (s *PebbleStorage) ListDexTicks(ctx context.Context, market port.DexMarketKey) ([]*port.DexTick, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}
	prefix := dexKey(prefixDexTick, dexMarketBytes(market))
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()
	var out []*port.DexTick
	for iter.First(); iter.Valid(); iter.Next() {
		t := new(port.DexTick)
		if err := json.Unmarshal(iter.Value(), t); err != nil {
			return nil, fmt.Errorf("decode %q: %w", iter.Key(), err)
		}
		out = append(out, t)
	}
	return out, iter.Error()
}

// SaveDexMarket implements port.DexWriter.
func (s *PebbleStorage) SaveDexMarket(ctx context.Context, m *port.DexMarket) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	market := dexMarketBytes(m.Key)
	return s.putDex(ctx,
		[2][]byte{dexKey(prefixDexMarket, market), data},
		[2][]byte{dexKey(prefixDexMarkets, dexPosition(m.CreatedBlock, m.CreatedLogIndex), market), market})
}

// SaveDexTrade implements port.DexWriter.
func (s *PebbleStorage) SaveDexTrade(ctx context.Context, t *port.DexTrade) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	market, position := dexMarketBytes(t.Market), dexPosition(t.BlockNumber, t.LogIndex)
	entries := [][2][]byte{{dexKey(prefixDexTrade, market, position), data}, {dexKey(prefixDexBlock, position), market}}
	for _, a := range dexTraders(t) {
		entries = append(entries, [2][]byte{dexKey(prefixDexTrader, a.Bytes(), position), market})
	}
	return s.putDex(ctx, entries...)
}

// dexTraders returns the addresses a trade is listed under: its taker and
// maker, once each, without the zero address.
func dexTraders(t *port.DexTrade) []common.Address {
	var out []common.Address
	for _, a := range []common.Address{t.Taker, t.Maker} {
		if a != (common.Address{}) && (len(out) == 0 || out[0] != a) {
			out = append(out, a)
		}
	}
	return out
}

// SaveDexLiquidity implements port.DexWriter.
func (s *PebbleStorage) SaveDexLiquidity(ctx context.Context, l *port.DexLiquidity) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return s.putDex(ctx, [2][]byte{dexKey(prefixDexLiquidity, dexMarketBytes(l.Market), dexPosition(l.BlockNumber, l.LogIndex)), data})
}

// SaveDexOrder implements port.DexWriter.
func (s *PebbleStorage) SaveDexOrder(ctx context.Context, o *port.DexOrder) error {
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	market, position := dexMarketBytes(o.Market), dexPosition(o.CreatedBlock, o.CreatedLogIndex)
	entries := [][2][]byte{
		{dexKey(prefixDexOrder, o.Market.Address.Bytes(), o.ID.Bytes()), data},
		{dexKey(prefixDexOrders, market, position, o.ID.Bytes()), o.ID.Bytes()},
	}
	open := dexKey(prefixDexOpen, market, position, o.ID.Bytes())
	if o.Status.Resting() {
		return s.writeDex(ctx, append(entries, [2][]byte{open, o.ID.Bytes()}), nil)
	}
	return s.writeDex(ctx, entries, [][]byte{open})
}

// SaveDexTick implements port.DexWriter.
func (s *PebbleStorage) SaveDexTick(ctx context.Context, t *port.DexTick) error {
	key := dexKey(prefixDexTick, dexMarketBytes(t.Market), dexTickBytes(t.Tick))
	if t.LiquidityGross == nil || t.LiquidityGross.Sign() == 0 {
		return s.writeDex(ctx, nil, [][]byte{key})
	}
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return s.putDex(ctx, [2][]byte{key, data})
}
