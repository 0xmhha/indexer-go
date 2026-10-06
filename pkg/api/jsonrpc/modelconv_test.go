package jsonrpc

import (
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// The test doubles here keep go-ethereum values; these helpers convert what
// they return to the model the storage ports use.

func modelBlockOf(b *types.Block, err error) (*model.Block, error) {
	if err != nil || b == nil {
		return nil, err
	}
	return gethconv.BlockFromGeth(b)
}

func modelBlocksOf(bs []*types.Block, err error) ([]*model.Block, error) {
	if err != nil {
		return nil, err
	}
	out := make([]*model.Block, 0, len(bs))
	for _, b := range bs {
		m, err := modelBlockOf(b, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func modelTxOf(tx *types.Transaction, loc *port.TxLocation, err error) (*model.Transaction, *port.TxLocation, error) {
	if err != nil || tx == nil {
		return nil, loc, err
	}
	m, err := gethconv.TxFromGeth(tx)
	return m, loc, err
}

func modelTxsOf(txs []*types.Transaction, locs []*port.TxLocation, err error) ([]*model.Transaction, []*port.TxLocation, error) {
	out := make([]*model.Transaction, len(txs))
	for i, tx := range txs {
		if tx != nil {
			out[i], _ = gethconv.TxFromGeth(tx)
		}
	}
	return out, locs, err
}

func modelReceiptOf(r *types.Receipt, err error) (*model.Receipt, error) {
	if err != nil || r == nil {
		return nil, err
	}
	return gethconv.ReceiptFromGeth(r), nil
}

func modelReceiptsOf(rs []*types.Receipt, err error) ([]*model.Receipt, error) {
	out := make([]*model.Receipt, len(rs))
	for i, r := range rs {
		if r != nil {
			out[i] = gethconv.ReceiptFromGeth(r)
		}
	}
	return out, err
}

func modelLogsOf(ls []*types.Log, err error) ([]*model.Log, error) {
	if err != nil {
		return nil, err
	}
	return gethconv.LogsFromGeth(ls), nil
}

func modelTx(tx *types.Transaction) *model.Transaction {
	if tx == nil {
		return nil
	}
	m, _ := gethconv.TxFromGeth(tx)
	return m
}

func modelReceipt(r *types.Receipt) *model.Receipt {
	if r == nil {
		return nil
	}
	return gethconv.ReceiptFromGeth(r)
}
