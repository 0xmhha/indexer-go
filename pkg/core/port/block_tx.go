package port

import (
	"context"
	"errors"
)

// ErrBlockTxDone is returned when a finished block transaction is used.
var ErrBlockTxDone = errors.New("storage: block transaction already finished")

// BlockTransactor is implemented by storages that can index one block
// atomically. The fetcher uses it to make all writes for a block, including
// the cursor, become durable together or not at all.
type BlockTransactor interface {
	// BeginBlock opens a block transaction; storage calls made with the
	// returned context belong to it.
	BeginBlock(ctx context.Context) (context.Context, BlockTx, error)
}

// BlockTx is an open block transaction.
type BlockTx interface {
	// SetHeight makes Commit record undo for the block at height, so a
	// reorganization can roll it back.
	SetHeight(height uint64)
	// Commit makes every write of the block durable together.
	Commit() error
	// Rollback discards the writes; it does nothing after Commit.
	Rollback()
}
