// Package gethconv converts between go-ethereum types and the chain-neutral
// model (pkg/core/model).
package gethconv

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// It is a temporary bridge for code that still works on go-ethereum types:
// storage methods that take or return them, the legacy RPC client and feature
// processors not yet moved to the model. It goes away once ingest and the
// APIs use the model directly (chain profile design, CP-4 and CP-5).
//
// Converting to go-ethereum types is lossy for what go-ethereum cannot
// represent: a StableNet fee delegation transaction is returned as its inner
// EIP-1559 transaction, a block hash is recomputed with the Ethereum rule, and
// uncle headers are not stored (only their hashes).

// BlockFromGeth converts a block given to a legacy write method.
func BlockFromGeth(b *types.Block) (*model.Block, error) {
	h := b.Header()
	m := &model.Block{
		Hash:             b.Hash(),
		ParentHash:       h.ParentHash,
		UncleHash:        h.UncleHash,
		Miner:            h.Coinbase,
		StateRoot:        h.Root,
		TxRoot:           h.TxHash,
		ReceiptRoot:      h.ReceiptHash,
		Bloom:            h.Bloom.Bytes(),
		Difficulty:       h.Difficulty,
		Number:           h.Number.Uint64(),
		GasLimit:         h.GasLimit,
		GasUsed:          h.GasUsed,
		Time:             h.Time,
		Extra:            h.Extra,
		MixDigest:        h.MixDigest,
		Nonce:            h.Nonce.Uint64(),
		BaseFee:          h.BaseFee,
		WithdrawalsRoot:  h.WithdrawalsHash,
		BlobGasUsed:      h.BlobGasUsed,
		ExcessBlobGas:    h.ExcessBlobGas,
		ParentBeaconRoot: h.ParentBeaconRoot,
		RequestsHash:     h.RequestsHash,
		Size:             b.Size(),
	}
	for i, tx := range b.Transactions() {
		mt, err := TxFromGeth(tx)
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
		mt.BlockHash, mt.BlockNumber, mt.Index = m.Hash, m.Number, uint(i)
		m.Transactions = append(m.Transactions, mt)
	}
	for _, u := range b.Uncles() {
		m.Uncles = append(m.Uncles, u.Hash())
	}
	if ws := b.Withdrawals(); ws != nil {
		m.Withdrawals = make([]model.Withdrawal, 0, len(ws))
		for _, w := range ws {
			m.Withdrawals = append(m.Withdrawals, model.Withdrawal{Index: w.Index, Validator: w.Validator, Address: w.Address, Amount: w.Amount})
		}
	}
	return m, nil
}

// TxFromGeth converts a transaction given to a legacy write method. The sender
// is recovered when the signature allows it; otherwise it is left zero, as
// the legacy encoding never stored it.
func TxFromGeth(tx *types.Transaction) (*model.Transaction, error) {
	if m, err := evm.FromGethTx(tx); err == nil {
		return m, nil
	}
	return evm.ConvertGethTx(tx)
}

func ReceiptFromGeth(r *types.Receipt) *model.Receipt {
	return evm.ReceiptToModel(r)
}

// BlockToGeth converts a stored block for a legacy read method.
func BlockToGeth(m *model.Block) (*types.Block, error) {
	h := &types.Header{
		ParentHash:       m.ParentHash,
		UncleHash:        m.UncleHash,
		Coinbase:         m.Miner,
		Root:             m.StateRoot,
		TxHash:           m.TxRoot,
		ReceiptHash:      m.ReceiptRoot,
		Bloom:            types.BytesToBloom(m.Bloom),
		Difficulty:       m.Difficulty,
		Number:           new(big.Int).SetUint64(m.Number),
		GasLimit:         m.GasLimit,
		GasUsed:          m.GasUsed,
		Time:             m.Time,
		Extra:            m.Extra,
		MixDigest:        m.MixDigest,
		Nonce:            types.EncodeNonce(m.Nonce),
		BaseFee:          m.BaseFee,
		WithdrawalsHash:  m.WithdrawalsRoot,
		BlobGasUsed:      m.BlobGasUsed,
		ExcessBlobGas:    m.ExcessBlobGas,
		ParentBeaconRoot: m.ParentBeaconRoot,
		RequestsHash:     m.RequestsHash,
	}
	if h.Difficulty == nil {
		h.Difficulty = new(big.Int)
	}
	body := types.Body{Transactions: make([]*types.Transaction, 0, len(m.Transactions))}
	for i, mt := range m.Transactions {
		tx, err := TxToGeth(mt)
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
		body.Transactions = append(body.Transactions, tx)
	}
	if m.Withdrawals != nil {
		body.Withdrawals = make([]*types.Withdrawal, 0, len(m.Withdrawals))
		for _, w := range m.Withdrawals {
			body.Withdrawals = append(body.Withdrawals, &types.Withdrawal{Index: w.Index, Validator: w.Validator, Address: w.Address, Amount: w.Amount})
		}
	}
	return types.NewBlockWithHeader(h).WithBody(body), nil
}

// TxToGeth converts a stored transaction for a legacy read method. Types
// go-ethereum cannot decode (StableNet fee delegation, opaque types) are
// rebuilt as the closest standard transaction from their common fields.
func TxToGeth(m *model.Transaction) (*types.Transaction, error) {
	if len(m.Raw) > 0 {
		var tx types.Transaction
		if err := tx.UnmarshalBinary(m.Raw); err == nil {
			return &tx, nil
		}
	}
	if m.GasTipCap != nil && m.GasFeeCap != nil && m.ChainID != nil {
		al := make(types.AccessList, 0, len(m.AccessList))
		for _, a := range m.AccessList {
			al = append(al, types.AccessTuple{Address: a.Address, StorageKeys: a.StorageKeys})
		}
		return types.NewTx(&types.DynamicFeeTx{
			ChainID: m.ChainID, Nonce: m.Nonce, GasTipCap: m.GasTipCap, GasFeeCap: m.GasFeeCap,
			Gas: m.Gas, To: m.To, Value: m.Value, Data: m.Input, AccessList: al,
			V: m.Signature.V, R: m.Signature.R, S: m.Signature.S,
		}), nil
	}
	return types.NewTx(&types.LegacyTx{
		Nonce: m.Nonce, GasPrice: m.GasPrice, Gas: m.Gas, To: m.To, Value: m.Value, Data: m.Input,
		V: m.Signature.V, R: m.Signature.R, S: m.Signature.S,
	}), nil
}

// ReceiptToGeth converts a stored receipt for a legacy read method.
func ReceiptToGeth(m *model.Receipt) *types.Receipt {
	r := &types.Receipt{
		Type:              m.Type,
		Status:            m.Status,
		CumulativeGasUsed: m.CumulativeGasUsed,
		Bloom:             types.BytesToBloom(m.Bloom),
		TxHash:            m.TxHash,
		GasUsed:           m.GasUsed,
		EffectiveGasPrice: m.EffectiveGasPrice,
		BlobGasUsed:       m.BlobGasUsed,
		BlobGasPrice:      m.BlobGasPrice,
		BlockHash:         m.BlockHash,
		BlockNumber:       new(big.Int).SetUint64(m.BlockNumber),
		TransactionIndex:  m.TxIndex,
		Logs:              make([]*types.Log, 0, len(m.Logs)),
	}
	if m.ContractAddress != nil {
		r.ContractAddress = *m.ContractAddress
	}
	for _, l := range m.Logs {
		r.Logs = append(r.Logs, &types.Log{
			Address: l.Address, Topics: l.Topics, Data: l.Data,
			BlockNumber: l.BlockNumber, BlockHash: l.BlockHash, TxHash: l.TxHash,
			TxIndex: l.TxIndex, Index: l.Index, Removed: l.Removed,
		})
	}
	return r
}

// ReceiptsFromGeth converts a block's receipts.
func ReceiptsFromGeth(rs types.Receipts) []*model.Receipt {
	out := make([]*model.Receipt, len(rs))
	for i, r := range rs {
		out[i] = ReceiptFromGeth(r)
	}
	return out
}

// ReceiptsToGeth converts a block's receipts.
func ReceiptsToGeth(rs []*model.Receipt) types.Receipts {
	out := make(types.Receipts, len(rs))
	for i, r := range rs {
		out[i] = ReceiptToGeth(r)
	}
	return out
}

// AuthToGeth converts an EIP-7702 authorization, for example to recover its
// authority.
func AuthToGeth(a model.SetCodeAuthorization) types.SetCodeAuthorization {
	out := types.SetCodeAuthorization{Address: a.Address, Nonce: a.Nonce, V: a.V}
	if a.ChainID != nil {
		out.ChainID.SetFromBig(a.ChainID)
	}
	if a.R != nil {
		out.R.SetFromBig(a.R)
	}
	if a.S != nil {
		out.S.SetFromBig(a.S)
	}
	return out
}
