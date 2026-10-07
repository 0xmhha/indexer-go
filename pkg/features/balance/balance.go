// Package balance is the balance.native feature: it keeps the native
// balance history of every account touched by a transaction.
package balance

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// Name is the feature name.
const Name = "balance.native"

type balanceFeature struct{}

func (balanceFeature) Name() string       { return Name }
func (balanceFeature) Requires() []string { return nil }
func (balanceFeature) DefaultOn() bool    { return true }

func (balanceFeature) Register(r feature.Registrar) error {
	d := r.Deps()
	w, ok := d.Storage.(port.HistoricalWriter)
	if !ok {
		return fmt.Errorf("storage does not support balance history")
	}
	rd, ok := d.Storage.(port.HistoricalReader)
	if !ok {
		return fmt.Errorf("storage does not support balance history")
	}
	rc, ok := d.Storage.(port.BalanceRecordChecker)
	if !ok {
		return fmt.Errorf("storage does not support balance history")
	}
	if d.BalanceAt == nil {
		return fmt.Errorf("no node balance reader")
	}
	logger := d.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	r.OnBlock(&handler{
		w: w, r: rd, records: rc,
		accounting: chains.AccountingOf(d.Profile),
		balanceAt:  d.BalanceAt, blocks: d.Blocks(), logger: logger,
	})
	return nil
}

func init() { feature.Register(balanceFeature{}) }

type handler struct {
	w          port.HistoricalWriter
	r          port.HistoricalReader
	records    port.BalanceRecordChecker
	accounting chains.NativeAccounting
	balanceAt  func(ctx context.Context, addr common.Address, block *big.Int) (*big.Int, error)
	blocks     chains.AccountingEnv
	logger     *zap.Logger
}

// HandleBlock applies the native balance changes of the block, as the chain
// profile's accounting rules derive them (who pays gas, where the tip and
// base fee go, which value moves are visible). An account seen for the
// first time starts from the node's balance before the block. If a change
// would make a balance negative, the indexed balance has diverged from the
// chain: the account is reset to the node's balance after the block.
// Balance tracking is best-effort: storage failures are logged.
func (h *handler) HandleBlock(ctx context.Context, b *feature.Block) error {
	n := b.Model.Number
	deltas, err := h.accounting.NativeDeltas(ctx, b.Model, b.Receipts, h.blocks)
	if err != nil {
		return fmt.Errorf("native balance changes of block %d: %w", n, err)
	}

	initialized := map[common.Address]bool{}
	diverged := map[common.Address]bool{}
	for _, d := range mergeDeltas(deltas) {
		if d.Delta.Sign() == 0 || diverged[d.Address] {
			continue
		}
		if !initialized[d.Address] {
			if err := h.ensureInitialized(ctx, d.Address, n); err != nil {
				h.logger.Warn("Failed to initialize balance",
					zap.String("address", d.Address.Hex()), zap.Uint64("block", n), zap.Error(err))
			}
			initialized[d.Address] = true
		}
		err := h.w.UpdateBalance(ctx, d.Address, n, d.Delta, d.TxHash)
		switch {
		case errors.Is(err, port.ErrNegativeBalance):
			diverged[d.Address] = true
		case err != nil:
			h.logger.Warn("Failed to update balance",
				zap.Uint64("block", n), zap.String("tx", d.TxHash.Hex()), zap.String("reason", d.Reason),
				zap.String("address", d.Address.Hex()), zap.String("delta", d.Delta.String()), zap.Error(err))
		}
	}
	for addr := range diverged {
		h.resync(ctx, addr, n)
	}

	if n == 0 {
		if err := h.initGenesisMiner(ctx, b.Model.Miner); err != nil {
			h.logger.Warn("Failed to initialize genesis balances", zap.Error(err))
		}
	}
	return nil
}

// mergeDeltas sums the changes of each account per transaction (gas and
// value of the same transaction become one history entry), keeping the
// order in which accounts first appear.
func mergeDeltas(deltas []chains.BalanceDelta) []chains.BalanceDelta {
	type key struct {
		addr common.Address
		tx   common.Hash
	}
	index := map[key]int{}
	var out []chains.BalanceDelta
	for _, d := range deltas {
		k := key{d.Address, d.TxHash}
		if i, ok := index[k]; ok {
			out[i].Delta = new(big.Int).Add(out[i].Delta, d.Delta)
			out[i].Reason += "+" + d.Reason
			continue
		}
		index[k] = len(out)
		d.Delta = new(big.Int).Set(d.Delta)
		out = append(out, d)
	}
	return out
}

// resync resets a diverged account to the node's balance after block n.
func (h *handler) resync(ctx context.Context, addr common.Address, n uint64) {
	balance, err := h.balanceAt(ctx, addr, new(big.Int).SetUint64(n))
	if err != nil {
		h.logger.Warn("Indexed balance diverged and the node balance is unavailable",
			zap.String("address", addr.Hex()), zap.Uint64("block", n), zap.Error(err))
		return
	}
	h.logger.Warn("Indexed balance diverged from the chain; reset to the node balance",
		zap.String("address", addr.Hex()), zap.Uint64("block", n), zap.String("balance", balance.String()))
	if err := h.w.SetBalance(ctx, addr, n, balance); err != nil {
		h.logger.Warn("Failed to reset balance", zap.String("address", addr.Hex()), zap.Error(err))
	}
}

// ensureInitialized seeds the balance of an account seen for the first time
// with the node's balance before this block.
func (h *handler) ensureInitialized(ctx context.Context, addr common.Address, block uint64) error {
	// The stored record only: GetAddressBalance would look the account up
	// in genesis and start it from its genesis balance, missing every
	// change before this block.
	recorded, err := h.records.HasBalanceRecord(ctx, addr)
	if err != nil {
		return fmt.Errorf("failed to check address balance: %w", err)
	}
	if recorded {
		return nil
	}

	at := big.NewInt(0)
	if block > 0 {
		at = new(big.Int).SetUint64(block - 1)
	}
	balance, err := h.balanceAt(ctx, addr, at)
	if err != nil {
		h.logger.Warn("Failed to fetch initial balance from RPC, starting from 0",
			zap.String("address", addr.Hex()), zap.Uint64("block", block), zap.Error(err))
		balance = big.NewInt(0)
	}
	return h.w.SetBalance(ctx, addr, block, balance)
}

// initGenesisMiner records the genesis balance of block 0's miner, which
// typically holds a genesis allocation without sending a transaction.
func (h *handler) initGenesisMiner(ctx context.Context, miner common.Address) error {
	current, err := h.r.GetAddressBalance(ctx, miner, 0)
	if err != nil {
		return fmt.Errorf("failed to check miner balance: %w", err)
	}
	if current.Sign() != 0 {
		return nil
	}
	history, _, err := h.r.GetBalanceHistory(ctx, miner, 0, 0, port.FirstPage(1))
	if err != nil {
		return fmt.Errorf("failed to check miner balance history: %w", err)
	}
	if len(history) > 0 {
		return nil
	}
	balance, err := h.balanceAt(ctx, miner, big.NewInt(0))
	if err != nil {
		return err
	}
	if balance.Sign() > 0 {
		if err := h.w.SetBalance(ctx, miner, 0, balance); err != nil {
			return fmt.Errorf("failed to set genesis miner balance: %w", err)
		}
	}
	return nil
}
