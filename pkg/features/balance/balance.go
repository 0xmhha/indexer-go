// Package balance is the balance.native feature: it keeps the native
// balance history of every account touched by a transaction.
package balance

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/feature"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// Name is the feature name.
const Name = "balance.native"

type balanceFeature struct{}

func (balanceFeature) Name() string       { return Name }
func (balanceFeature) Requires() []string { return nil }
func (balanceFeature) DefaultOn() bool    { return true }

func (balanceFeature) Register(r feature.Registrar) error {
	d := r.Deps()
	w, ok := d.Storage.(storagepkg.HistoricalWriter)
	if !ok {
		return fmt.Errorf("storage does not support balance history")
	}
	rd, ok := d.Storage.(storagepkg.HistoricalReader)
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
	r.OnBlock(&handler{storage: d.Storage, w: w, r: rd, balanceAt: d.BalanceAt, logger: logger})
	return nil
}

func init() { feature.Register(balanceFeature{}) }

type handler struct {
	storage   storagepkg.Storage
	w         storagepkg.HistoricalWriter
	r         storagepkg.HistoricalReader
	balanceAt func(ctx context.Context, addr common.Address, block *big.Int) (*big.Int, error)
	logger    *zap.Logger
}

// HandleBlock applies the balance changes of the block's transactions. The
// sender pays the value; the gas (gas used times the receipt's effective gas
// price) is paid by the fee payer of a fee delegation transaction, otherwise
// by the sender. Balance tracking is best-effort: failures are logged.
func (h *handler) HandleBlock(ctx context.Context, b *feature.Block) error {
	n := b.Model.Number
	apply := func(addr common.Address, delta *big.Int, txHash common.Hash, what string) {
		if err := h.ensureInitialized(ctx, addr, n); err != nil {
			h.logger.Warn("Failed to initialize "+what+" balance",
				zap.String("address", addr.Hex()), zap.Uint64("block", n), zap.Error(err))
		}
		if err := h.w.UpdateBalance(ctx, addr, n, delta, txHash); err != nil {
			h.logger.Warn("Failed to update "+what+" balance",
				zap.Uint64("block", n), zap.String("tx", txHash.Hex()),
				zap.String("address", addr.Hex()), zap.String("delta", delta.String()), zap.Error(err))
		}
	}

	for _, p := range b.Transactions() {
		tx, receipt := p.Tx, p.Receipt
		from := tx.From
		if from == (common.Address{}) {
			continue // sender unknown
		}

		gasPrice := receipt.EffectiveGasPrice
		if gasPrice == nil {
			gasPrice = tx.GasPrice
		}
		gasCost := new(big.Int)
		if gasPrice != nil {
			gasCost.Mul(new(big.Int).SetUint64(receipt.GasUsed), gasPrice)
		}
		value := tx.Value
		if value == nil {
			value = new(big.Int)
		}

		payer := from
		if feePayer, ok := feature.DelegatedFeePayer(ctx, h.storage, tx); ok {
			payer = feePayer
		}
		if payer == from {
			apply(from, new(big.Int).Neg(new(big.Int).Add(value, gasCost)), tx.Hash, "sender")
		} else {
			if value.Sign() > 0 {
				apply(from, new(big.Int).Neg(value), tx.Hash, "sender")
			}
			apply(payer, new(big.Int).Neg(gasCost), tx.Hash, "fee payer")
		}

		// The receiver gets the value (not the gas); for a contract creation
		// the receiver is the new contract.
		to := tx.To
		if to == nil && receipt.ContractAddress != nil {
			to = receipt.ContractAddress
		}
		if to != nil && value.Sign() > 0 {
			apply(*to, value, tx.Hash, "receiver")
		}
	}

	if n == 0 {
		if err := h.initGenesisMiner(ctx, b.Model.Miner); err != nil {
			h.logger.Warn("Failed to initialize genesis balances", zap.Error(err))
		}
	}
	return nil
}

// ensureInitialized seeds the balance of an account seen for the first time
// with the node's balance before this block.
func (h *handler) ensureInitialized(ctx context.Context, addr common.Address, block uint64) error {
	current, err := h.r.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		return fmt.Errorf("failed to check address balance: %w", err)
	}
	if current.Sign() != 0 {
		return nil
	}
	history, err := h.r.GetBalanceHistory(ctx, addr, 0, block, 1, 0)
	if err != nil {
		return fmt.Errorf("failed to check balance history: %w", err)
	}
	if len(history) > 0 {
		return nil // initialized; the balance may legitimately be zero
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
	history, err := h.r.GetBalanceHistory(ctx, miner, 0, 0, 1, 0)
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
