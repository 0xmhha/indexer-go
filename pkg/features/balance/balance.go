// Package balance is the balance.native feature: it keeps the native
// balance history of every account touched by a transaction.
package balance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
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
//
// Storage errors and node reads the node did not answer (connection,
// timeout) fail the block, so it is retried and every run stores the same
// balances (docs/SDK.md, determinism rules). A node that answers it keeps
// no state of the block asked for (not an archive node) cannot give the
// balance: a new account then starts from 0 and a diverged account keeps
// its balance, both logged; correct balances from such heights need an
// archive node.
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
				return fmt.Errorf("initialize balance of %s at block %d: %w", d.Address.Hex(), n, err)
			}
			initialized[d.Address] = true
		}
		err := h.w.UpdateBalance(ctx, d.Address, n, d.Delta, d.TxHash)
		switch {
		case errors.Is(err, port.ErrNegativeBalance):
			diverged[d.Address] = true
		case err != nil:
			return fmt.Errorf("update balance of %s by %s (%s, tx %s): %w", d.Address.Hex(), d.Delta, d.Reason, d.TxHash.Hex(), err)
		}
	}
	for _, addr := range sortedAddresses(diverged) {
		if err := h.resync(ctx, addr, n); err != nil {
			return err
		}
	}

	if n == 0 {
		if err := h.initGenesisMiner(ctx, b.Model.Miner); err != nil {
			return fmt.Errorf("initialize genesis balances: %w", err)
		}
	}
	return nil
}

// sortedAddresses returns the keys of set in order, so resets happen in the
// same order every run.
func sortedAddresses(set map[common.Address]bool) []common.Address {
	out := make([]common.Address, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out
}

// unanswered reports whether a node read failed without the node answering
// (connection, timeout, HTTP error), as opposed to a JSON-RPC error the
// node returned, such as missing state of an old block.
func unanswered(err error) bool {
	var answered rpc.Error
	return err != nil && !errors.As(err, &answered)
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
// A node that answers it has no such balance leaves the account as it is.
func (h *handler) resync(ctx context.Context, addr common.Address, n uint64) error {
	balance, err := h.balanceAt(ctx, addr, new(big.Int).SetUint64(n))
	if unanswered(err) {
		return fmt.Errorf("read balance of diverged %s at block %d: %w", addr.Hex(), n, err)
	}
	if err != nil {
		h.logger.Warn("Indexed balance diverged and the node keeps no balance of the block",
			zap.String("address", addr.Hex()), zap.Uint64("block", n), zap.Error(err))
		return nil
	}
	h.logger.Warn("Indexed balance diverged from the chain; reset to the node balance",
		zap.String("address", addr.Hex()), zap.Uint64("block", n), zap.String("balance", balance.String()))
	if err := h.w.SetBalance(ctx, addr, n, balance); err != nil {
		return fmt.Errorf("reset balance of %s at block %d: %w", addr.Hex(), n, err)
	}
	return nil
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
	if unanswered(err) {
		return err
	}
	if err != nil {
		// The node answered that it keeps no state of the previous block
		// (not an archive node): nothing else knows the balance, and
		// failing the block would stop indexing for good.
		h.logger.Warn("The node keeps no balance of the previous block; starting from 0",
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
	if unanswered(err) {
		return err
	}
	if err != nil {
		h.logger.Warn("The node keeps no genesis state; the genesis miner's balance is not recorded",
			zap.String("address", miner.Hex()), zap.Error(err))
		return nil
	}
	if balance.Sign() > 0 {
		if err := h.w.SetBalance(ctx, miner, 0, balance); err != nil {
			return fmt.Errorf("failed to set genesis miner balance: %w", err)
		}
	}
	return nil
}
