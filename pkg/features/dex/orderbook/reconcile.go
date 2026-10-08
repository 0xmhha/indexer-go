package orderbook

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var reconciliations = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "indexer",
	Subsystem: "dex_orderbook",
	Name:      "reconciliations_total",
	Help:      "Order book comparisons with the chain, by venue and result (in_sync, mismatch, error).",
}, []string{"venue", "result"})

// Contract functions read for reconciliation.
const (
	sigGetReserves = "getReserves()"
	sigSlot0       = "slot0()"
	sigLiquidity   = "liquidity()"
	sigTicks       = "ticks(int24)"
	sigTickBitmap  = "tickBitmap(int16)"
	sigGetOrder    = "getOrder(bytes32)"
)

// CallData encodes a call of a contract function whose arguments are all
// one word (integers, two's complement when negative).
func CallData(signature string, args ...*big.Int) []byte {
	out := append([]byte{}, crypto.Keccak256([]byte(signature))[:4]...)
	for _, a := range args {
		out = append(out, Word(a)...)
	}
	return out
}

// Word encodes an integer as a 32-byte word, two's complement when negative.
func Word(v *big.Int) []byte {
	if v.Sign() < 0 {
		v = new(big.Int).Add(v, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return common.LeftPadBytes(v.Bytes(), 32)
}

// signedWord decodes a two's complement word.
func signedWord(b []byte) *big.Int {
	v := new(big.Int).SetBytes(b)
	if b[0]&0x80 != 0 {
		v.Sub(v, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return v
}

func (s *Service) reconcileLoop() {
	defer s.wg.Done()
	t := time.NewTicker(s.cfg.reconcile)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			_ = s.Reconcile(s.ctx)
		}
	}
}

// Reconcile reloads every book and compares it with the chain at the book's
// height, one market at a time; it returns the results by market.
func (s *Service) Reconcile(ctx context.Context) map[port.DexMarketKey]*Reconciliation {
	out := map[port.DexMarketKey]*Reconciliation{}
	for _, a := range s.actorList() {
		var r *Reconciliation
		err := a.do(ctx, func() {
			a.reload()
			if b := a.book.Load(); b != nil {
				r = s.compare(ctx, b)
				a.recon.Store(r)
			}
		})
		if err != nil {
			break
		}
		if r != nil {
			out[a.key] = r
		}
	}
	return out
}

// compare compares a book with its contract's state at the book's height.
func (s *Service) compare(ctx context.Context, b *Book) *Reconciliation {
	m := b.Market
	r := &Reconciliation{Block: b.Height, At: time.Now()}
	result := "in_sync"
	switch {
	case s.caller == nil:
		r.Err = "no node to compare with"
	case b.Height == 0:
		r.Err = "blocks were indexed while the book was read"
	default:
		c := &chainReader{ctx: ctx, caller: s.caller, block: new(big.Int).SetUint64(b.Height)}
		var err error
		switch m.Venue {
		case port.DexUniswapV2:
			r.Mismatches, err = c.compareV2(b)
		case port.DexUniswapV3:
			r.Mismatches, err = c.compareV3(b)
		case port.DexPerpOrderBook:
			r.Mismatches, err = c.compareOrders(b)
		}
		if err != nil {
			r.Err = err.Error()
		}
	}
	switch {
	case r.Err != "":
		result = "error"
		s.logger.Debug("Order book not compared", zap.String("market", m.Key.Address.Hex()), zap.Uint64("id", m.Key.ID), zap.String("reason", r.Err))
	case len(r.Mismatches) > 0:
		result = "mismatch"
		s.logger.Warn("Order book differs from the chain", zap.String("market", m.Key.Address.Hex()), zap.Uint64("id", m.Key.ID),
			zap.String("venue", string(m.Venue)), zap.Uint64("block", b.Height), zap.Strings("mismatches", r.Mismatches))
	}
	reconciliations.WithLabelValues(string(m.Venue), result).Inc()
	return r
}

// chainReader calls a market's contract at one block.
type chainReader struct {
	ctx    context.Context
	caller interface {
		CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber interface{}) ([]byte, error)
	}
	block *big.Int
}

// call returns the first n words a function returns.
func (c *chainReader) call(to common.Address, n int, signature string, args ...*big.Int) ([][]byte, error) {
	out, err := c.caller.CallContract(c.ctx, ethereum.CallMsg{To: &to, Data: CallData(signature, args...)}, c.block)
	if err != nil {
		return nil, fmt.Errorf("%s at %s: %w", signature, to.Hex(), err)
	}
	if len(out) < 32*n {
		return nil, fmt.Errorf("%s at %s returned %d bytes, want %d", signature, to.Hex(), len(out), 32*n)
	}
	words := make([][]byte, n)
	for i := range words {
		words[i] = out[32*i : 32*(i+1)]
	}
	return words, nil
}

// differ records a mismatch when the indexed and chain values differ (nil
// indexed values count as zero).
func differ(out *[]string, what string, indexed, chain *big.Int) {
	if indexed == nil {
		indexed = new(big.Int)
	}
	if indexed.Cmp(chain) != 0 {
		*out = append(*out, fmt.Sprintf("%s: indexed %s, chain %s", what, indexed, chain))
	}
}

func (c *chainReader) compareV2(b *Book) ([]string, error) {
	w, err := c.call(b.Market.Key.Address, 2, sigGetReserves)
	if err != nil {
		return nil, err
	}
	var out []string
	differ(&out, "reserve0", b.Market.Reserve0, new(big.Int).SetBytes(w[0]))
	differ(&out, "reserve1", b.Market.Reserve1, new(big.Int).SetBytes(w[1]))
	return out, nil
}

// compareV3 compares the price, tick and in-range liquidity, every indexed
// tick's liquidity, and the pool's tick bitmap in the words that hold an
// indexed tick or the current tick: a tick initialized on chain but not
// indexed (or the reverse) in those words is a mismatch.
func (c *chainReader) compareV3(b *Book) ([]string, error) {
	m := b.Market
	pool := m.Key.Address
	var out []string
	w, err := c.call(pool, 2, sigSlot0)
	if err != nil {
		return nil, err
	}
	differ(&out, "sqrtPriceX96", m.SqrtPriceX96, new(big.Int).SetBytes(w[0]))
	differ(&out, "tick", big.NewInt(int64(m.Tick)), signedWord(w[1]))
	if w, err = c.call(pool, 1, sigLiquidity); err != nil {
		return nil, err
	}
	differ(&out, "liquidity", m.Liquidity, new(big.Int).SetBytes(w[0]))

	indexed := map[int32]bool{}
	for _, t := range b.Ticks {
		indexed[t.Tick] = true
		w, err := c.call(pool, 2, sigTicks, big.NewInt(int64(t.Tick)))
		if err != nil {
			return nil, err
		}
		differ(&out, fmt.Sprintf("tick %d liquidityGross", t.Tick), t.LiquidityGross, new(big.Int).SetBytes(w[0]))
		differ(&out, fmt.Sprintf("tick %d liquidityNet", t.Tick), t.LiquidityNet, signedWord(w[1]))
	}
	spacing := int64(m.TickSpacing)
	if spacing <= 0 {
		return out, nil
	}
	// The bitmap word of a tick (TickBitmap.position of the compressed tick).
	position := func(tick int64) (word int64, bit int64) {
		compressed := tick / spacing
		if tick < 0 && tick%spacing != 0 {
			compressed--
		}
		return compressed >> 8, compressed & 0xff
	}
	words := map[int64]bool{}
	cur, _ := position(int64(m.Tick))
	words[cur] = true
	for t := range indexed {
		wp, _ := position(int64(t))
		words[wp] = true
	}
	positions := make([]int64, 0, len(words))
	for wp := range words {
		positions = append(positions, wp)
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })
	for _, wp := range positions {
		w, err := c.call(pool, 1, sigTickBitmap, big.NewInt(wp))
		if err != nil {
			return nil, err
		}
		bitmap := new(big.Int).SetBytes(w[0])
		for bit := 0; bit < 256; bit++ {
			tick := (wp*256 + int64(bit)) * spacing
			if tick < MinTick || tick > MaxTick {
				continue
			}
			onChain := bitmap.Bit(bit) == 1
			if onChain && !indexed[int32(tick)] {
				out = append(out, fmt.Sprintf("tick %d: initialized on chain, not indexed", tick))
			} else if !onChain && indexed[int32(tick)] {
				out = append(out, fmt.Sprintf("tick %d: indexed, not initialized on chain", tick))
			}
		}
	}
	return out, nil
}

// Order manager OrderStatus values of resting orders, and the words of
// getOrder's Order (orderId, trader, marketId, side, orderType, status,
// timeInForce, reduceOnly, size, filledSize, price, ...).
const (
	chainOrderOpen            = 1
	chainOrderPartiallyFilled = 2
	orderWords                = 15
	orderWordStatus           = 5
	orderWordSize             = 8
	orderWordFilled           = 9
	orderWordPrice            = 10
)

// compareOrders compares every resting order of the book with the order
// manager's record of it: it must rest there too, with the same size,
// filled size and price.
func (c *chainReader) compareOrders(b *Book) ([]string, error) {
	var out []string
	for _, o := range b.Orders {
		w, err := c.call(b.Market.Key.Address, orderWords, sigGetOrder, new(big.Int).SetBytes(o.ID.Bytes()))
		if err != nil {
			return nil, err
		}
		id := o.ID.Hex()
		if status := new(big.Int).SetBytes(w[orderWordStatus]).Uint64(); status != chainOrderOpen && status != chainOrderPartiallyFilled {
			out = append(out, fmt.Sprintf("order %s: resting in the book, status %d on chain", id, status))
			continue
		}
		differ(&out, "order "+id+" size", o.Size, new(big.Int).SetBytes(w[orderWordSize]))
		differ(&out, "order "+id+" filled", o.Filled, new(big.Int).SetBytes(w[orderWordFilled]))
		differ(&out, "order "+id+" price", o.Price, new(big.Int).SetBytes(w[orderWordPrice]))
	}
	return out, nil
}
