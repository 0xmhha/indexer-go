package port

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// BlockReader reads blocks, transactions and receipts as the chain-neutral
// model (pkg/core/model), exactly as the chain profile decoded them
// (canonical hashes, chain-specific transaction types and extensions).
type BlockReader interface {
	GetBlock(ctx context.Context, height uint64) (*model.Block, error)
	GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error)
	// GetBlocks returns the stored blocks in [start, end]; missing
	// heights are skipped.
	GetBlocks(ctx context.Context, start, end uint64) ([]*model.Block, error)
	GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *TxLocation, error)
	GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error)
}

// BlockWriter stores blocks and receipts. Every key is derived from the
// model's hashes, so a block or transaction is found under the hash its
// chain reports.
type BlockWriter interface {
	// SetBlock stores the block, its hash index, its time (a store that
	// also implements HistoricalReader finds the block by time) and every
	// transaction with its location.
	SetBlock(ctx context.Context, b *model.Block) error
	// SetReceipt stores one receipt.
	SetReceipt(ctx context.Context, r *model.Receipt) error
}
