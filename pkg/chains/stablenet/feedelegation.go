package stablenet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chains"
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

// FeeDelegation is the fee-payer part of a fee delegation transaction.
type FeeDelegation struct {
	FeePayer   common.Address // the account that paid the gas
	V, R, S    *big.Int       // fee payer signature
	SenderHash common.Hash    // EIP-1559 hash of the sender's inner transaction
	// Invalid is set when the fee payer signature does not recover to the
	// declared fee payer, or no fee payer is declared. go-stablenet v1.0.0
	// checked the signature only in the transaction pool, so blocks of that
	// era can hold such transactions; consensus then charged the declared
	// fee payer, or the sender when none was declared (FeePayer says which).
	Invalid bool
}

// invalidFeePayers counts decoded transactions with Invalid set.
var invalidFeePayers atomic.Uint64

// InvalidFeePayerSignatures returns how many fee delegation transactions
// with an invalid fee payer signature were decoded by this process.
func InvalidFeePayerSignatures() uint64 { return invalidFeePayers.Load() }

var feeDelegationKey = model.NewExtKey("stablenet.fee_delegation")

// feeDelegationRecord is the stored form of FeeDelegation. Fields may only be
// appended.
type feeDelegationRecord struct {
	FeePayer   common.Address
	V, R, S    *big.Int
	SenderHash common.Hash
	Invalid    bool `rlp:"optional"`
}

func init() {
	chains.RegisterFeeDelegation(chains.FeeDelegationScheme{
		Name:  ID,
		Types: []uint8{FeeDelegationTxType},
		Of: func(tx *model.Transaction) (*chains.FeeDelegation, bool) {
			fd, ok := FeeDelegationOf(tx)
			if !ok {
				return nil, false
			}
			return &chains.FeeDelegation{Payer: fd.FeePayer, V: fd.V, R: fd.R, S: fd.S}, true
		},
	})
	model.RegisterExtCodec(feeDelegationKey, model.ExtCodec{
		Encode: func(v any) ([]byte, error) {
			fd, ok := v.(*FeeDelegation)
			if !ok {
				return nil, fmt.Errorf("stablenet: fee delegation extension holds %T", v)
			}
			return rlp.EncodeToBytes(&feeDelegationRecord{fd.FeePayer, fd.V, fd.R, fd.S, fd.SenderHash, fd.Invalid})
		},
		Decode: func(data []byte) (any, error) {
			var r feeDelegationRecord
			if err := rlp.DecodeBytes(data, &r); err != nil {
				return nil, err
			}
			return &FeeDelegation{FeePayer: r.FeePayer, V: r.V, R: r.R, S: r.S, SenderHash: r.SenderHash, Invalid: r.Invalid}, nil
		},
	})
}

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
	}
	// feePayer and fv/fr/fs may be absent: a transaction without a fee
	// payer could be included before go-stablenet v1.1.0 (see Invalid).
	return ""
}

func bigOrZero(b *hexutil.Big) *big.Int {
	if b == nil {
		return new(big.Int)
	}
	return (*big.Int)(b)
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

	inner := &types.DynamicFeeTx{
		ChainID:    (*big.Int)(j.ChainID),
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
	m, err := feeDelegationTx(inner, j.FeePayer, bigOrZero(j.FV), bigOrZero(j.FR), bigOrZero(j.FS))
	if err != nil {
		return nil, err
	}
	if m.Hash != j.Hash {
		return nil, fmt.Errorf("%w: fee delegation tx computed %s reported %s", evm.ErrHashMismatch, m.Hash.Hex(), j.Hash.Hex())
	}
	if j.From != nil && *j.From != m.From {
		return nil, fmt.Errorf("%w: fee delegation tx %s recovered %s reported %s", evm.ErrSenderMismatch, m.Hash.Hex(), m.From.Hex(), j.From.Hex())
	}
	return m, nil
}

// feeDelegationTx builds the model of a fee delegation transaction from its
// fields: it computes the canonical encoding and hash, recovers the sender
// and checks that the fee payer signature recovers to the declared payer.
// A failed check (or no declared payer) marks the transaction Invalid
// instead of rejecting it: go-stablenet v1.0.0 included such transactions,
// charging the declared payer, or the sender when none was declared.
func feeDelegationTx(inner *types.DynamicFeeTx, feePayer *common.Address, fv, fr, fs *big.Int) (*model.Transaction, error) {
	senderTx := types.NewTx(inner)
	senderFields := []any{
		inner.ChainID, inner.Nonce, inner.GasTipCap, inner.GasFeeCap, inner.Gas,
		toField(inner.To), inner.Value, inner.Data, inner.AccessList,
		inner.V, inner.R, inner.S,
	}
	payload, err := rlp.EncodeToBytes([]any{senderFields, toField(feePayer), fv, fr, fs})
	if err != nil {
		return nil, fmt.Errorf("stablenet: encode fee delegation tx: %w", err)
	}
	encoding := append([]byte{FeeDelegationTxType}, payload...)
	hash := crypto.Keccak256Hash(encoding)

	from, err := types.Sender(types.NewLondonSigner(inner.ChainID), senderTx)
	if err != nil {
		return nil, fmt.Errorf("stablenet: recover sender of %s: %w", hash.Hex(), err)
	}

	fd := &FeeDelegation{V: fv, R: fr, S: fs, SenderHash: senderTx.Hash()}
	if feePayer == nil {
		fd.FeePayer, fd.Invalid = from, true
	} else {
		fd.FeePayer = *feePayer
		payerPayload, err := rlp.EncodeToBytes([]any{senderFields, *feePayer})
		if err != nil {
			return nil, fmt.Errorf("stablenet: encode fee payer sighash: %w", err)
		}
		payerHash := crypto.Keccak256Hash(append([]byte{FeeDelegationTxType}, payerPayload...))
		payer, err := recoverAddress(payerHash, fv, fr, fs)
		fd.Invalid = err != nil || payer != *feePayer
	}
	if fd.Invalid {
		invalidFeePayers.Add(1)
	}

	m := &model.Transaction{
		Hash:      hash,
		Type:      FeeDelegationTxType,
		ChainID:   inner.ChainID,
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
	m.Ext.Set(feeDelegationKey, fd)
	return m, nil
}

// toField encodes a missing address (recipient, fee payer) as the empty
// string, as go-stablenet's rlp:"nil" fields do.
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
