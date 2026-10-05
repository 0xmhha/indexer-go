package evm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Decoding of consensus (RLP) encodings, used for history archives (era1).
// The JSON path checks hashes and senders against what the node reported;
// here there is nothing to compare with, so equality with the JSON path is
// established by tests against the same chain.

var _ chains.BinaryProfile = (*Profile)(nil)

// ErrUnsupportedBinary means a binary block holds something the profile
// cannot decode (an unknown transaction type).
var ErrUnsupportedBinary = errors.New("evm: unsupported in binary decoding")

// BinaryTxDecoder decodes the canonical encoding (type byte followed by the
// payload) of a chain-specific transaction type, recovering its sender.
type BinaryTxDecoder func(enc []byte) (*model.Transaction, error)

// EffectiveGasPriceFunc computes a receipt's effective gas price. r already
// carries its logs and block location.
type EffectiveGasPriceFunc func(b *model.Block, tx *model.Transaction, r *model.Receipt) *big.Int

// WithBinaryTxDecoder registers a binary decoder for a chain-specific
// transaction type.
func WithBinaryTxDecoder(typ uint8, d BinaryTxDecoder) Option {
	return func(p *Profile) { p.binaryTxDecoders[typ] = d }
}

// WithEffectiveGasPrice replaces the rule that derives effective gas prices
// for binary receipts (default: LondonEffectiveGasPrice).
func WithEffectiveGasPrice(f EffectiveGasPriceFunc) Option {
	return func(p *Profile) { p.effectiveGasPrice = f }
}

// LondonEffectiveGasPrice is the Ethereum rule: without a base fee the fee
// cap (the gas price of legacy transactions), otherwise the base fee plus the
// tip, capped by the fee cap.
func LondonEffectiveGasPrice(b *model.Block, tx *model.Transaction, _ *model.Receipt) *big.Int {
	if b.BaseFee == nil {
		return new(big.Int).Set(tx.GasFeeCap)
	}
	return MinBig(new(big.Int).Add(tx.GasTipCap, b.BaseFee), tx.GasFeeCap)
}

// MinBig returns a copy of the smaller of a and b.
func MinBig(a, b *big.Int) *big.Int {
	if a.Cmp(b) <= 0 {
		return new(big.Int).Set(a)
	}
	return new(big.Int).Set(b)
}

// DecodeBlockRLP implements chains.BinaryProfile.
func (p *Profile) DecodeBlockRLP(header, body []byte) (*model.Block, error) {
	var head types.Header
	if err := rlp.DecodeBytes(header, &head); err != nil {
		return nil, fmt.Errorf("evm: decode header RLP: %w", err)
	}
	content, rest, err := rlp.SplitList(body)
	if err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("evm: block %d: body is not an RLP list", head.Number)
	}
	parts, err := splitItems(content)
	if err != nil || len(parts) < 2 || len(parts) > 3 {
		return nil, fmt.Errorf("evm: block %d: body has %d parts: %v", head.Number, len(parts), err)
	}

	b := headerToModel(&head)
	b.Hash = p.headerHash(&head)
	b.Size = uint64(rlp.ListSize(uint64(len(header) + len(content))))

	var uncles []*types.Header
	if err := rlp.DecodeBytes(parts[1], &uncles); err != nil {
		return nil, fmt.Errorf("evm: block %d uncles: %w", b.Number, err)
	}
	b.Uncles = make([]common.Hash, len(uncles))
	for i, u := range uncles {
		b.Uncles[i] = p.headerHash(u)
	}
	if len(parts) == 3 {
		var ws []*types.Withdrawal
		if err := rlp.DecodeBytes(parts[2], &ws); err != nil {
			return nil, fmt.Errorf("evm: block %d withdrawals: %w", b.Number, err)
		}
		b.Withdrawals = make([]model.Withdrawal, 0, len(ws))
		for _, w := range ws {
			b.Withdrawals = append(b.Withdrawals, model.Withdrawal{Index: w.Index, Validator: w.Validator, Address: w.Address, Amount: w.Amount})
		}
	}

	txList, _, err := rlp.SplitList(parts[0])
	if err != nil {
		return nil, fmt.Errorf("evm: block %d transactions: %w", b.Number, err)
	}
	encs, err := txEncodings(txList)
	if err != nil {
		return nil, fmt.Errorf("evm: block %d transactions: %w", b.Number, err)
	}
	txs := make([]*model.Transaction, len(encs))
	err = parallelFor(len(encs), func(i int) error {
		tx, err := p.decodeTxBinary(encs[i])
		if err != nil {
			return fmt.Errorf("tx %d: %w", i, err)
		}
		tx.BlockHash, tx.BlockNumber, tx.Index = b.Hash, b.Number, uint(i)
		txs[i] = tx
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("evm: block %d %w", b.Number, err)
	}
	b.Transactions = txs
	return b, nil
}

// splitItems returns the raw RLP items of a list's content.
func splitItems(content []byte) ([][]byte, error) {
	var out [][]byte
	for len(content) > 0 {
		_, _, rest, err := rlp.Split(content)
		if err != nil {
			return nil, err
		}
		out = append(out, content[:len(content)-len(rest)])
		content = rest
	}
	return out, nil
}

// txEncodings returns the canonical encoding of each transaction in a body's
// transaction list: legacy transactions are RLP lists, typed ones are byte
// strings holding type || payload.
func txEncodings(list []byte) ([][]byte, error) {
	items, err := splitItems(list)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(items))
	for i, item := range items {
		kind, val, _, err := rlp.Split(item)
		if err != nil {
			return nil, err
		}
		switch kind {
		case rlp.List:
			out[i] = item
		case rlp.String:
			if len(val) == 0 {
				return nil, fmt.Errorf("tx %d: empty typed transaction", i)
			}
			out[i] = val
		default:
			return nil, fmt.Errorf("tx %d: unexpected RLP byte", i)
		}
	}
	return out, nil
}

func (p *Profile) decodeTxBinary(enc []byte) (*model.Transaction, error) {
	if enc[0] < 0x7f {
		if d, ok := p.binaryTxDecoders[enc[0]]; ok {
			return d(enc)
		}
	}
	var tx types.Transaction
	if err := tx.UnmarshalBinary(enc); err != nil {
		if errors.Is(err, types.ErrTxTypeNotSupported) {
			return nil, fmt.Errorf("%w: transaction type %#x", ErrUnsupportedBinary, enc[0])
		}
		return nil, err
	}
	return FromGethTx(&tx)
}

// receiptRLP is the consensus encoding of a receipt.
type receiptRLP struct {
	PostStateOrStatus []byte
	CumulativeGasUsed uint64
	Bloom             types.Bloom
	Logs              []*types.Log
}

// DeriveReceipts implements chains.BinaryProfile.
func (p *Profile) DeriveReceipts(b *model.Block, receipts []byte) ([]*model.Receipt, error) {
	content, rest, err := rlp.SplitList(receipts)
	if err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("evm: block %d: receipts are not an RLP list", b.Number)
	}
	items, err := splitItems(content)
	if err != nil {
		return nil, fmt.Errorf("evm: block %d receipts: %w", b.Number, err)
	}
	if len(items) != len(b.Transactions) {
		return nil, fmt.Errorf("evm: block %d has %d transactions and %d receipts", b.Number, len(b.Transactions), len(items))
	}

	out := make([]*model.Receipt, len(items))
	var logIndex uint
	for i, item := range items {
		tx := b.Transactions[i]
		kind, val, _, err := rlp.Split(item)
		if err != nil {
			return nil, fmt.Errorf("evm: block %d receipt %d: %w", b.Number, i, err)
		}
		enc := item
		if kind == rlp.String { // typed: type || rlp(receipt)
			if len(val) == 0 || val[0] != tx.Type {
				return nil, fmt.Errorf("evm: block %d receipt %d: type does not match transaction type %#x", b.Number, i, tx.Type)
			}
			enc = val[1:]
		}
		var dec receiptRLP
		if err := rlp.DecodeBytes(enc, &dec); err != nil {
			return nil, fmt.Errorf("evm: block %d receipt %d: %w", b.Number, i, err)
		}

		r := &model.Receipt{
			Type:              tx.Type,
			CumulativeGasUsed: dec.CumulativeGasUsed,
			GasUsed:           dec.CumulativeGasUsed,
			Bloom:             dec.Bloom.Bytes(),
			TxHash:            tx.Hash,
			TxIndex:           uint(i),
			BlockHash:         b.Hash,
			BlockNumber:       b.Number,
		}
		if len(dec.PostStateOrStatus) == 1 && dec.PostStateOrStatus[0] == 1 {
			r.Status = types.ReceiptStatusSuccessful
		}
		if i > 0 {
			r.GasUsed -= out[i-1].CumulativeGasUsed
		}
		if tx.To == nil {
			addr := crypto.CreateAddress(tx.From, tx.Nonce)
			r.ContractAddress = &addr
		}
		r.Logs = make([]*model.Log, 0, len(dec.Logs))
		for _, l := range dec.Logs {
			r.Logs = append(r.Logs, &model.Log{
				Address: l.Address, Topics: l.Topics, Data: l.Data,
				BlockNumber: b.Number, BlockHash: b.Hash, TxHash: tx.Hash,
				TxIndex: uint(i), Index: logIndex,
			})
			logIndex++
		}
		r.EffectiveGasPrice = p.effectiveGasPrice(b, tx, r)
		if tx.Type == types.BlobTxType {
			// The blob gas used follows from the transaction. The blob gas
			// price is not supported: it depends on the chain's blob
			// schedule (update fraction per fork), which block data does not
			// carry, so it is left nil.
			r.BlobGasUsed = uint64(len(tx.BlobHashes)) * params.BlobTxBlobGasPerBlob
		}
		out[i] = r
	}
	return out, nil
}
