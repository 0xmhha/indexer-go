package stablenet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// FeeDelegationTxType is StableNet's fee delegation transaction type: an
// EIP-1559 transaction signed by its sender and wrapped with a fee payer that
// pays the gas and co-signs.
//
// Implemented from the observed protocol behaviour (go-stablenet v1.1.0):
//
//	encoding  = 0x16 || rlp([sender, feePayer, fv, fr, fs])
//	sender    = [chainId, nonce, tip, feeCap, gas, to, value, data, accessList, v, r, s]
//	hash      = keccak256(encoding)
//	from      = ecrecover(EIP-1559 sighash of the sender fields, v, r, s)
//	feePayer  = ecrecover(keccak256(0x16 || rlp([sender, feePayer])), fv, fr, fs)
const FeeDelegationTxType = 0x16

// ErrFeePayerMismatch means the fee payer signature does not recover to the
// declared fee payer.
var ErrFeePayerMismatch = errors.New("stablenet: fee payer signature does not match fee payer")

// FeeDelegation is the fee-payer part of a fee delegation transaction.
type FeeDelegation struct {
	FeePayer   common.Address
	V, R, S    *big.Int    // fee payer signature
	SenderHash common.Hash // EIP-1559 hash of the sender's inner transaction
}

var feeDelegationKey = model.NewExtKey("stablenet.fee_delegation")

// FeeDelegationOf returns the fee delegation data of tx, if it has any.
func FeeDelegationOf(tx *model.Transaction) (*FeeDelegation, bool) {
	fd, ok := tx.Ext.Get(feeDelegationKey).(*FeeDelegation)
	return fd, ok
}

// FeePayerOf returns the account that pays gas for tx: the fee payer of a fee
// delegation transaction, otherwise the sender.
func FeePayerOf(tx *model.Transaction) common.Address {
	if fd, ok := FeeDelegationOf(tx); ok {
		return fd.FeePayer
	}
	return tx.From
}

type feeDelegationJSON struct {
	Hash       common.Hash      `json:"hash"`
	From       *common.Address  `json:"from"`
	ChainID    *hexutil.Big     `json:"chainId"`
	Nonce      *hexutil.Uint64  `json:"nonce"`
	TipCap     *hexutil.Big     `json:"maxPriorityFeePerGas"`
	FeeCap     *hexutil.Big     `json:"maxFeePerGas"`
	Gas        *hexutil.Uint64  `json:"gas"`
	To         *common.Address  `json:"to"`
	Value      *hexutil.Big     `json:"value"`
	Input      *hexutil.Bytes   `json:"input"`
	AccessList types.AccessList `json:"accessList"`
	V          *hexutil.Big     `json:"v"`
	R          *hexutil.Big     `json:"r"`
	S          *hexutil.Big     `json:"s"`
	FeePayer   *common.Address  `json:"feePayer"`
	FV         *hexutil.Big     `json:"fv"`
	FR         *hexutil.Big     `json:"fr"`
	FS         *hexutil.Big     `json:"fs"`
}

func (j *feeDelegationJSON) missing() string {
	switch {
	case j.ChainID == nil:
		return "chainId"
	case j.Nonce == nil:
		return "nonce"
	case j.TipCap == nil:
		return "maxPriorityFeePerGas"
	case j.FeeCap == nil:
		return "maxFeePerGas"
	case j.Gas == nil:
		return "gas"
	case j.Value == nil:
		return "value"
	case j.Input == nil:
		return "input"
	case j.V == nil || j.R == nil || j.S == nil:
		return "v/r/s"
	case j.FeePayer == nil:
		return "feePayer"
	case j.FV == nil || j.FR == nil || j.FS == nil:
		return "fv/fr/fs"
	}
	return ""
}

// DecodeFeeDelegationTx decodes a type 0x16 transaction object from an RPC
// response, verifying its hash, sender and fee payer.
func DecodeFeeDelegationTx(raw json.RawMessage) (*model.Transaction, error) {
	var j feeDelegationJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("stablenet: decode fee delegation tx: %w", err)
	}
	if f := j.missing(); f != "" {
		return nil, fmt.Errorf("stablenet: fee delegation tx %s: missing field %q", j.Hash.Hex(), f)
	}

	chainID := (*big.Int)(j.ChainID)
	inner := &types.DynamicFeeTx{
		ChainID:    chainID,
		Nonce:      uint64(*j.Nonce),
		GasTipCap:  (*big.Int)(j.TipCap),
		GasFeeCap:  (*big.Int)(j.FeeCap),
		Gas:        uint64(*j.Gas),
		To:         j.To,
		Value:      (*big.Int)(j.Value),
		Data:       *j.Input,
		AccessList: j.AccessList,
		V:          (*big.Int)(j.V),
		R:          (*big.Int)(j.R),
		S:          (*big.Int)(j.S),
	}
	senderTx := types.NewTx(inner)

	senderFields := []any{
		inner.ChainID, inner.Nonce, inner.GasTipCap, inner.GasFeeCap, inner.Gas,
		toField(inner.To), inner.Value, inner.Data, inner.AccessList,
		inner.V, inner.R, inner.S,
	}
	payload, err := rlp.EncodeToBytes([]any{senderFields, *j.FeePayer, (*big.Int)(j.FV), (*big.Int)(j.FR), (*big.Int)(j.FS)})
	if err != nil {
		return nil, fmt.Errorf("stablenet: encode fee delegation tx: %w", err)
	}
	encoding := append([]byte{FeeDelegationTxType}, payload...)
	hash := crypto.Keccak256Hash(encoding)
	if hash != j.Hash {
		return nil, fmt.Errorf("%w: fee delegation tx computed %s reported %s", evm.ErrHashMismatch, hash.Hex(), j.Hash.Hex())
	}

	from, err := types.Sender(types.NewLondonSigner(chainID), senderTx)
	if err != nil {
		return nil, fmt.Errorf("stablenet: recover sender of %s: %w", hash.Hex(), err)
	}
	if j.From != nil && *j.From != from {
		return nil, fmt.Errorf("%w: fee delegation tx %s recovered %s reported %s", evm.ErrSenderMismatch, hash.Hex(), from.Hex(), j.From.Hex())
	}

	payerPayload, err := rlp.EncodeToBytes([]any{senderFields, *j.FeePayer})
	if err != nil {
		return nil, fmt.Errorf("stablenet: encode fee payer sighash: %w", err)
	}
	payerHash := crypto.Keccak256Hash(append([]byte{FeeDelegationTxType}, payerPayload...))
	payer, err := recoverAddress(payerHash, (*big.Int)(j.FV), (*big.Int)(j.FR), (*big.Int)(j.FS))
	if err != nil {
		return nil, fmt.Errorf("stablenet: recover fee payer of %s: %w", hash.Hex(), err)
	}
	if payer != *j.FeePayer {
		return nil, fmt.Errorf("%w: tx %s recovered %s declared %s", ErrFeePayerMismatch, hash.Hex(), payer.Hex(), j.FeePayer.Hex())
	}

	m := &model.Transaction{
		Hash:      hash,
		Type:      FeeDelegationTxType,
		ChainID:   chainID,
		Nonce:     inner.Nonce,
		From:      from,
		To:        inner.To,
		Value:     inner.Value,
		Gas:       inner.Gas,
		GasPrice:  inner.GasFeeCap, // fee-market convention, as for type 2
		GasTipCap: inner.GasTipCap,
		GasFeeCap: inner.GasFeeCap,
		Input:     inner.Data,
		Signature: model.Signature{V: inner.V, R: inner.R, S: inner.S},
		Raw:       encoding,
	}
	for _, t := range inner.AccessList {
		m.AccessList = append(m.AccessList, model.AccessTuple{Address: t.Address, StorageKeys: t.StorageKeys})
	}
	m.Ext.Set(feeDelegationKey, &FeeDelegation{
		FeePayer:   payer,
		V:          (*big.Int)(j.FV),
		R:          (*big.Int)(j.FR),
		S:          (*big.Int)(j.FS),
		SenderHash: senderTx.Hash(),
	})
	return m, nil
}

// toField encodes a missing recipient as the empty string, as RLP requires.
func toField(to *common.Address) any {
	if to == nil {
		return []byte{}
	}
	return *to
}

// recoverAddress recovers a signer from a typed-transaction signature whose
// v is the y-parity (0 or 1).
func recoverAddress(hash common.Hash, v, r, s *big.Int) (common.Address, error) {
	if v == nil || r == nil || s == nil || v.BitLen() > 8 {
		return common.Address{}, errors.New("invalid signature values")
	}
	vb := byte(v.Uint64())
	if !crypto.ValidateSignatureValues(vb, r, s, true) {
		return common.Address{}, errors.New("invalid signature values")
	}
	sig := make([]byte, crypto.SignatureLength)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:64])
	sig[64] = vb
	pub, err := crypto.SigToPub(hash[:], sig)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pub), nil
}
