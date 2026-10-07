package storage

import (
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// txGasPrice returns the gas price as go-ethereum reports it, which the
// statistics have always used: the fee cap (equal to the gas price of
// legacy and access-list transactions, whose fee cap is their gas price).
func txGasPrice(tx *model.Transaction) *big.Int {
	if tx.GasFeeCap != nil {
		return tx.GasFeeCap
	}
	return tx.GasPrice
}

// receiptGasPrice returns the price a transaction paid per gas: the
// receipt's effective gas price, or the transaction's gas price when the
// receipt has none.
func receiptGasPrice(r *model.Receipt, tx *model.Transaction) *big.Int {
	if r != nil && r.EffectiveGasPrice != nil {
		return r.EffectiveGasPrice
	}
	return txGasPrice(tx)
}

