package model

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// EncodingVersion is the first byte of every encoded block, transaction and
// receipt. Decoding rejects other versions, so a stored database written by
// an incompatible build is detected instead of misread.
const EncodingVersion byte = 1

// ErrEncodingVersion means stored data was written with another encoding.
var ErrEncodingVersion = errors.New("model: unsupported encoding version")

// ErrUnknownExtension means an extension has no registered codec: it cannot
// be stored, or stored data names an extension this build does not know.
var ErrUnknownExtension = errors.New("model: extension has no registered codec")

// The records below are the RLP layout of the encoding. Optional values are
// encoded as lists of zero or one element so that nil and zero stay distinct.
// Fields may only be appended, never reordered or removed.

type extRecord struct {
	Name string
	Data []byte
}

type logRecord struct {
	Address     common.Address
	Topics      []common.Hash
	Data        []byte
	BlockNumber uint64
	BlockHash   common.Hash
	TxHash      common.Hash
	TxIndex     uint64
	Index       uint64
	Removed     bool
}

type receiptRecord struct {
	Type              uint8
	Status            uint64
	CumulativeGasUsed uint64
	GasUsed           uint64
	EffectiveGasPrice []*big.Int
	BlobGasUsed       uint64
	BlobGasPrice      []*big.Int
	ContractAddress   []common.Address
	Bloom             []byte
	Logs              []logRecord
	TxHash            common.Hash
	TxIndex           uint64
	BlockHash         common.Hash
	BlockNumber       uint64
	Ext               []extRecord
}

type accessRecord struct {
	Address     common.Address
	StorageKeys []common.Hash
}

type authRecord struct {
	ChainID []*big.Int
	Address common.Address
	Nonce   uint64
	V       uint8
	R, S    []*big.Int
}

type txRecord struct {
	Hash        common.Hash
	Type        uint8
	ChainID     []*big.Int
	Nonce       uint64
	From        common.Address
	To          []common.Address
	Value       []*big.Int
	Gas         uint64
	GasPrice    []*big.Int
	GasTipCap   []*big.Int
	GasFeeCap   []*big.Int
	Input       []byte
	AccessList  []accessRecord
	AuthList    []authRecord
	BlobHashes  []common.Hash
	BlobFeeCap  []*big.Int
	V, R, S     []*big.Int
	Raw         []byte
	BlockHash   common.Hash
	BlockNumber uint64
	Index       uint64
	Opaque      bool
	Ext         []extRecord
}

type blockRecord struct {
	Hash             common.Hash
	ParentHash       common.Hash
	UncleHash        common.Hash
	Miner            common.Address
	StateRoot        common.Hash
	TxRoot           common.Hash
	ReceiptRoot      common.Hash
	Bloom            []byte
	Difficulty       []*big.Int
	Number           uint64
	GasLimit         uint64
	GasUsed          uint64
	Time             uint64
	Extra            []byte
	MixDigest        common.Hash
	Nonce            uint64
	BaseFee          []*big.Int
	WithdrawalsRoot  []common.Hash
	BlobGasUsed      []uint64
	ExcessBlobGas    []uint64
	ParentBeaconRoot []common.Hash
	RequestsHash     []common.Hash
	Size             uint64
	Transactions     []txRecord
	Ext              []extRecord
	Uncles           []common.Hash
	Withdrawals      [][]Withdrawal // optional list
}

// EncodeBlock encodes b with its transactions.
func EncodeBlock(b *Block) ([]byte, error) {
	r, err := blockToRecord(b)
	if err != nil {
		return nil, err
	}
	return encode(r)
}

// DecodeBlock decodes a block written by EncodeBlock.
func DecodeBlock(data []byte) (*Block, error) {
	var r blockRecord
	if err := decode(data, &r); err != nil {
		return nil, err
	}
	return blockFromRecord(&r)
}

// EncodeTransaction encodes tx.
func EncodeTransaction(tx *Transaction) ([]byte, error) {
	r, err := txToRecord(tx)
	if err != nil {
		return nil, err
	}
	return encode(r)
}

// DecodeTransaction decodes a transaction written by EncodeTransaction.
func DecodeTransaction(data []byte) (*Transaction, error) {
	var r txRecord
	if err := decode(data, &r); err != nil {
		return nil, err
	}
	return txFromRecord(&r)
}

// EncodeReceipt encodes r with its logs.
func EncodeReceipt(r *Receipt) ([]byte, error) {
	rec, err := receiptToRecord(r)
	if err != nil {
		return nil, err
	}
	return encode(rec)
}

// DecodeReceipt decodes a receipt written by EncodeReceipt.
func DecodeReceipt(data []byte) (*Receipt, error) {
	var r receiptRecord
	if err := decode(data, &r); err != nil {
		return nil, err
	}
	return receiptFromRecord(&r)
}

func encode(v any) ([]byte, error) {
	payload, err := rlp.EncodeToBytes(v)
	if err != nil {
		return nil, fmt.Errorf("model: encode: %w", err)
	}
	return append([]byte{EncodingVersion}, payload...), nil
}

func decode(data []byte, v any) error {
	if len(data) == 0 || data[0] != EncodingVersion {
		got := "empty"
		if len(data) > 0 {
			got = fmt.Sprint(data[0])
		}
		return fmt.Errorf("%w: %s, want %d", ErrEncodingVersion, got, EncodingVersion)
	}
	if err := rlp.DecodeBytes(data[1:], v); err != nil {
		return fmt.Errorf("model: decode: %w", err)
	}
	return nil
}

// --- optional values ---

func optBig(x *big.Int) []*big.Int {
	if x == nil {
		return nil
	}
	return []*big.Int{x}
}

func opt[T any](p *T) []T {
	if p == nil {
		return nil
	}
	return []T{*p}
}

func unoptBig(s []*big.Int) (*big.Int, error) {
	switch len(s) {
	case 0:
		return nil, nil
	case 1:
		return s[0], nil
	}
	return nil, fmt.Errorf("model: optional value has %d elements", len(s))
}

func unopt[T any](s []T) (*T, error) {
	switch len(s) {
	case 0:
		return nil, nil
	case 1:
		v := s[0]
		return &v, nil
	}
	return nil, fmt.Errorf("model: optional value has %d elements", len(s))
}

// --- extensions ---

func encodeExt(e Extensions) ([]extRecord, error) {
	if len(e) == 0 {
		return nil, nil
	}
	out := make([]extRecord, 0, len(e))
	for k, v := range e {
		stored, ok := lookupExtKey(k.name)
		if !ok || stored != k {
			return nil, fmt.Errorf("%w: %s", ErrUnknownExtension, k.name)
		}
		data, err := stored.codec.Encode(v)
		if err != nil {
			return nil, fmt.Errorf("model: encode extension %s: %w", k.name, err)
		}
		out = append(out, extRecord{Name: k.name, Data: data})
	}
	// Map order is random; sort so equal values encode to equal bytes.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func decodeExt(rs []extRecord) (Extensions, error) {
	var e Extensions
	for _, r := range rs {
		k, ok := lookupExtKey(r.Name)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownExtension, r.Name)
		}
		v, err := k.codec.Decode(r.Data)
		if err != nil {
			return nil, fmt.Errorf("model: decode extension %s: %w", r.Name, err)
		}
		e.Set(k, v)
	}
	return e, nil
}

// --- conversions ---

func logToRecord(l *Log) logRecord {
	return logRecord{
		Address: l.Address, Topics: l.Topics, Data: l.Data,
		BlockNumber: l.BlockNumber, BlockHash: l.BlockHash, TxHash: l.TxHash,
		TxIndex: uint64(l.TxIndex), Index: uint64(l.Index), Removed: l.Removed,
	}
}

func logFromRecord(r *logRecord) *Log {
	return &Log{
		Address: r.Address, Topics: r.Topics, Data: r.Data,
		BlockNumber: r.BlockNumber, BlockHash: r.BlockHash, TxHash: r.TxHash,
		TxIndex: uint(r.TxIndex), Index: uint(r.Index), Removed: r.Removed,
	}
}

func receiptToRecord(r *Receipt) (*receiptRecord, error) {
	ext, err := encodeExt(r.Ext)
	if err != nil {
		return nil, err
	}
	rec := &receiptRecord{
		Type: r.Type, Status: r.Status, CumulativeGasUsed: r.CumulativeGasUsed, GasUsed: r.GasUsed,
		EffectiveGasPrice: optBig(r.EffectiveGasPrice), BlobGasUsed: r.BlobGasUsed,
		BlobGasPrice: optBig(r.BlobGasPrice), ContractAddress: opt(r.ContractAddress),
		Bloom: r.Bloom, TxHash: r.TxHash, TxIndex: uint64(r.TxIndex),
		BlockHash: r.BlockHash, BlockNumber: r.BlockNumber, Ext: ext,
	}
	for _, l := range r.Logs {
		rec.Logs = append(rec.Logs, logToRecord(l))
	}
	return rec, nil
}

func receiptFromRecord(rec *receiptRecord) (*Receipt, error) {
	r := &Receipt{
		Type: rec.Type, Status: rec.Status, CumulativeGasUsed: rec.CumulativeGasUsed, GasUsed: rec.GasUsed,
		BlobGasUsed: rec.BlobGasUsed, Bloom: rec.Bloom, TxHash: rec.TxHash, TxIndex: uint(rec.TxIndex),
		BlockHash: rec.BlockHash, BlockNumber: rec.BlockNumber,
	}
	var err error
	if r.EffectiveGasPrice, err = unoptBig(rec.EffectiveGasPrice); err != nil {
		return nil, err
	}
	if r.BlobGasPrice, err = unoptBig(rec.BlobGasPrice); err != nil {
		return nil, err
	}
	if r.ContractAddress, err = unopt(rec.ContractAddress); err != nil {
		return nil, err
	}
	for i := range rec.Logs {
		r.Logs = append(r.Logs, logFromRecord(&rec.Logs[i]))
	}
	if r.Ext, err = decodeExt(rec.Ext); err != nil {
		return nil, err
	}
	return r, nil
}

func txToRecord(tx *Transaction) (*txRecord, error) {
	ext, err := encodeExt(tx.Ext)
	if err != nil {
		return nil, err
	}
	r := &txRecord{
		Hash: tx.Hash, Type: tx.Type, ChainID: optBig(tx.ChainID), Nonce: tx.Nonce,
		From: tx.From, To: opt(tx.To), Value: optBig(tx.Value), Gas: tx.Gas,
		GasPrice: optBig(tx.GasPrice), GasTipCap: optBig(tx.GasTipCap), GasFeeCap: optBig(tx.GasFeeCap),
		Input: tx.Input, BlobHashes: tx.BlobHashes, BlobFeeCap: optBig(tx.BlobFeeCap),
		V: optBig(tx.Signature.V), R: optBig(tx.Signature.R), S: optBig(tx.Signature.S),
		Raw: tx.Raw, BlockHash: tx.BlockHash, BlockNumber: tx.BlockNumber, Index: uint64(tx.Index),
		Opaque: tx.Opaque, Ext: ext,
	}
	for _, a := range tx.AccessList {
		r.AccessList = append(r.AccessList, accessRecord{Address: a.Address, StorageKeys: a.StorageKeys})
	}
	for _, a := range tx.AuthList {
		r.AuthList = append(r.AuthList, authRecord{
			ChainID: optBig(a.ChainID), Address: a.Address, Nonce: a.Nonce, V: a.V, R: optBig(a.R), S: optBig(a.S),
		})
	}
	return r, nil
}

func txFromRecord(r *txRecord) (*Transaction, error) {
	tx := &Transaction{
		Hash: r.Hash, Type: r.Type, Nonce: r.Nonce, From: r.From, Gas: r.Gas,
		Input: r.Input, BlobHashes: r.BlobHashes, Raw: r.Raw,
		BlockHash: r.BlockHash, BlockNumber: r.BlockNumber, Index: uint(r.Index), Opaque: r.Opaque,
	}
	var err error
	for _, f := range []struct {
		dst **big.Int
		src []*big.Int
	}{
		{&tx.ChainID, r.ChainID}, {&tx.Value, r.Value}, {&tx.GasPrice, r.GasPrice},
		{&tx.GasTipCap, r.GasTipCap}, {&tx.GasFeeCap, r.GasFeeCap}, {&tx.BlobFeeCap, r.BlobFeeCap},
		{&tx.Signature.V, r.V}, {&tx.Signature.R, r.R}, {&tx.Signature.S, r.S},
	} {
		if *f.dst, err = unoptBig(f.src); err != nil {
			return nil, err
		}
	}
	if tx.To, err = unopt(r.To); err != nil {
		return nil, err
	}
	for _, a := range r.AccessList {
		tx.AccessList = append(tx.AccessList, AccessTuple{Address: a.Address, StorageKeys: a.StorageKeys})
	}
	for _, a := range r.AuthList {
		auth := SetCodeAuthorization{Address: a.Address, Nonce: a.Nonce, V: a.V}
		if auth.ChainID, err = unoptBig(a.ChainID); err != nil {
			return nil, err
		}
		if auth.R, err = unoptBig(a.R); err != nil {
			return nil, err
		}
		if auth.S, err = unoptBig(a.S); err != nil {
			return nil, err
		}
		tx.AuthList = append(tx.AuthList, auth)
	}
	if tx.Ext, err = decodeExt(r.Ext); err != nil {
		return nil, err
	}
	return tx, nil
}

func blockToRecord(b *Block) (*blockRecord, error) {
	ext, err := encodeExt(b.Ext)
	if err != nil {
		return nil, err
	}
	r := &blockRecord{
		Hash: b.Hash, ParentHash: b.ParentHash, UncleHash: b.UncleHash, Miner: b.Miner,
		StateRoot: b.StateRoot, TxRoot: b.TxRoot, ReceiptRoot: b.ReceiptRoot, Bloom: b.Bloom,
		Difficulty: optBig(b.Difficulty), Number: b.Number, GasLimit: b.GasLimit, GasUsed: b.GasUsed,
		Time: b.Time, Extra: b.Extra, MixDigest: b.MixDigest, Nonce: b.Nonce, BaseFee: optBig(b.BaseFee),
		WithdrawalsRoot: opt(b.WithdrawalsRoot), BlobGasUsed: opt(b.BlobGasUsed),
		ExcessBlobGas: opt(b.ExcessBlobGas), ParentBeaconRoot: opt(b.ParentBeaconRoot),
		RequestsHash: opt(b.RequestsHash), Size: b.Size, Ext: ext, Uncles: b.Uncles,
	}
	if b.Withdrawals != nil {
		r.Withdrawals = [][]Withdrawal{b.Withdrawals}
	}
	for i, tx := range b.Transactions {
		tr, err := txToRecord(tx)
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
		r.Transactions = append(r.Transactions, *tr)
	}
	return r, nil
}

func blockFromRecord(r *blockRecord) (*Block, error) {
	b := &Block{
		Hash: r.Hash, ParentHash: r.ParentHash, UncleHash: r.UncleHash, Miner: r.Miner,
		StateRoot: r.StateRoot, TxRoot: r.TxRoot, ReceiptRoot: r.ReceiptRoot, Bloom: r.Bloom,
		Number: r.Number, GasLimit: r.GasLimit, GasUsed: r.GasUsed, Time: r.Time, Extra: r.Extra,
		MixDigest: r.MixDigest, Nonce: r.Nonce, Size: r.Size, Uncles: r.Uncles,
	}
	ws, err := unopt(r.Withdrawals)
	if err != nil {
		return nil, err
	}
	if ws != nil {
		b.Withdrawals = *ws
		if b.Withdrawals == nil {
			b.Withdrawals = []Withdrawal{}
		}
	}
	if b.Difficulty, err = unoptBig(r.Difficulty); err != nil {
		return nil, err
	}
	if b.BaseFee, err = unoptBig(r.BaseFee); err != nil {
		return nil, err
	}
	if b.WithdrawalsRoot, err = unopt(r.WithdrawalsRoot); err != nil {
		return nil, err
	}
	if b.BlobGasUsed, err = unopt(r.BlobGasUsed); err != nil {
		return nil, err
	}
	if b.ExcessBlobGas, err = unopt(r.ExcessBlobGas); err != nil {
		return nil, err
	}
	if b.ParentBeaconRoot, err = unopt(r.ParentBeaconRoot); err != nil {
		return nil, err
	}
	if b.RequestsHash, err = unopt(r.RequestsHash); err != nil {
		return nil, err
	}
	for i := range r.Transactions {
		tx, err := txFromRecord(&r.Transactions[i])
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
		b.Transactions = append(b.Transactions, tx)
	}
	if b.Ext, err = decodeExt(r.Ext); err != nil {
		return nil, err
	}
	return b, nil
}
