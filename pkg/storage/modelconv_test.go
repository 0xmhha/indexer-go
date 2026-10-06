package storage

import (
	"context"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// The tests here build go-ethereum values; these helpers convert them to
// the model the storage stores.

func modelBlock(b *types.Block) *model.Block {
	m, err := gethconv.BlockFromGeth(b)
	if err != nil {
		panic(err)
	}
	return m
}

func modelReceipt(r *types.Receipt) *model.Receipt {
	if r == nil {
		return nil
	}
	return gethconv.ReceiptFromGeth(r)
}

func modelLog(l *types.Log) *model.Log { return gethconv.LogFromGeth(l) }

func modelLogs(ls []*types.Log) []*model.Log { return gethconv.LogsFromGeth(ls) }

func modelTx(tx *types.Transaction) *model.Transaction {
	if tx == nil {
		return nil
	}
	m, err := gethconv.TxFromGeth(tx)
	if err != nil {
		panic(err)
	}
	return m
}

// setTx stores a transaction at a location the way a block write does, for
// tests that read transactions without building whole blocks.
func setTx(s any, ctx context.Context, tx *types.Transaction, loc *port.TxLocation) error {
	return s.(*PebbleStorage).setModelTransaction(ctx, modelTx(tx), loc)
}
