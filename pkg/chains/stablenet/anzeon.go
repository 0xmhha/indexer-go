package stablenet

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Anzeon fee rule (go-stablenet core/types Receipts.DeriveFields): the tip a
// transaction pays is the gas tip agreed by governance and carried in the
// WBFT extra data, except for transactions executed by an authorized
// account, which pay their own tip. Such transactions end with an
// AuthorizedTxExecuted event of the AccountManager system contract.

var (
	// AccountManagerAddress is the AccountManager system contract.
	AccountManagerAddress = common.HexToAddress("0x0000000000000000000000000000000000B00003")
	// AuthorizedTxExecutedTopic is the AuthorizedTxExecuted event signature.
	AuthorizedTxExecutedTopic = common.HexToHash("0x40e728a89c7f5b192cf1c1b747fb64d51d81c7a2b3ed4607b94d3a1e6a3e0373")
)

// extraGasTip is the position of the gas tip in the WBFT extra-data list.
const extraGasTip = 8

// HeaderGasTip returns the gas tip in a WBFT header's extra data, or nil if
// the extra data is not a WBFT extra or carries no tip. An empty value is
// no tip, not a zero tip (go-stablenet decodes the field with rlp:"nil"),
// so the transaction's own tip applies.
func HeaderGasTip(extra []byte) *big.Int {
	var elems []rlp.RawValue
	if err := rlp.DecodeBytes(extra, &elems); err != nil || len(elems) != extraFields {
		return nil
	}
	if raw := elems[extraGasTip]; len(raw) == 1 && raw[0] == 0x80 {
		return nil
	}
	tip := new(big.Int)
	if err := rlp.DecodeBytes(elems[extraGasTip], tip); err != nil {
		return nil
	}
	return tip
}

// EffectiveGasPrice derives a receipt's effective gas price with the Anzeon
// rule. Without a header gas tip it is the London rule.
func EffectiveGasPrice(b *model.Block, tx *model.Transaction, r *model.Receipt) *big.Int {
	if b.BaseFee == nil {
		return new(big.Int).Set(tx.GasPrice)
	}
	tip := tx.GasTipCap
	if headerTip := HeaderGasTip(b.Extra); headerTip != nil && !authorizedTx(r) {
		tip = headerTip
	}
	return evm.MinBig(new(big.Int).Add(tip, b.BaseFee), tx.GasFeeCap)
}

// authorizedTx reports whether the receipt's last log is AuthorizedTxExecuted.
func authorizedTx(r *model.Receipt) bool {
	if len(r.Logs) == 0 {
		return false
	}
	last := r.Logs[len(r.Logs)-1]
	return last.Address == AccountManagerAddress && len(last.Topics) > 0 && last.Topics[0] == AuthorizedTxExecutedTopic
}
