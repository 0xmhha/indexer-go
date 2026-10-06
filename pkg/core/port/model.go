package port

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// ModelReader reads blocks, transactions and receipts as the chain-neutral
// model, exactly as the chain profile decoded them (canonical hashes,
// chain-specific transaction types and extensions).
type ModelReader interface {
	GetModelBlock(ctx context.Context, height uint64) (*model.Block, error)
	GetModelBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error)
	// GetModelBlocks returns the stored blocks in [start, end]; missing
	// heights are skipped.
	GetModelBlocks(ctx context.Context, start, end uint64) ([]*model.Block, error)
	GetModelTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *TxLocation, error)
	GetModelReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error)
}

// ModelWriter stores blocks and receipts given as the chain-neutral model.
// Every key is derived from the model's hashes, so a block or transaction is
// found under the hash its chain reports.
type ModelWriter interface {
	// SetModelBlock stores the block, its hash index and every transaction
	// with its location.
	SetModelBlock(ctx context.Context, b *model.Block) error
	// SetModelReceipt stores one receipt.
	SetModelReceipt(ctx context.Context, r *model.Receipt) error
}
