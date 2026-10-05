package stablenet

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// feeDelegationRLP is the payload of a fee delegation transaction's
// canonical encoding (after the type byte).
type feeDelegationRLP struct {
	Sender struct {
		ChainID    *big.Int
		Nonce      uint64
		GasTipCap  *big.Int
		GasFeeCap  *big.Int
		Gas        uint64
		To         *common.Address `rlp:"nil"`
		Value      *big.Int
		Data       []byte
		AccessList types.AccessList
		V, R, S    *big.Int
	}
	FeePayer   common.Address
	FV, FR, FS *big.Int
}

// DecodeFeeDelegationTxBinary decodes the canonical encoding of a type 0x16
// transaction (0x16 || rlp(...)), recovering its sender and checking its fee
// payer signature.
func DecodeFeeDelegationTxBinary(enc []byte) (*model.Transaction, error) {
	if len(enc) == 0 || enc[0] != FeeDelegationTxType {
		return nil, fmt.Errorf("stablenet: not a fee delegation transaction")
	}
	var d feeDelegationRLP
	if err := rlp.DecodeBytes(enc[1:], &d); err != nil {
		return nil, fmt.Errorf("stablenet: decode fee delegation tx: %w", err)
	}
	s := d.Sender
	m, err := feeDelegationTx(&types.DynamicFeeTx{
		ChainID: s.ChainID, Nonce: s.Nonce, GasTipCap: s.GasTipCap, GasFeeCap: s.GasFeeCap,
		Gas: s.Gas, To: s.To, Value: s.Value, Data: s.Data, AccessList: s.AccessList,
		V: s.V, R: s.R, S: s.S,
	}, d.FeePayer, d.FV, d.FR, d.FS)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(m.Raw, enc) {
		// Re-encoding must give the same bytes, or the hash would differ.
		return nil, fmt.Errorf("%w: fee delegation tx %s is not canonically encoded", evm.ErrHashMismatch, m.Hash.Hex())
	}
	return m, nil
}
