package port

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// BlockRef identifies a block.
type BlockRef struct {
	Number uint64
	Hash   common.Hash
}

// Reorg records one rollback caused by a chain reorganization.
type Reorg struct {
	Seq        uint64 // 1, 2, ... in the order they happened
	ForkNumber uint64 // newest block both branches share
	ForkHash   common.Hash
	OldHead    uint64     // indexed height before the rollback
	Removed    []BlockRef // rolled-back blocks, newest first
	DetectedAt uint64     // unix seconds

	// Blocks holds the rolled-back blocks, newest first, as returned by
	// RollbackTo. It is not stored in the record (see GetOrphanedBlock).
	Blocks []*OrphanedBlock `rlp:"-"`
}

// OrphanedBlock is a rolled-back block with its receipts (in transaction
// order) and the reorganization that removed it.
type OrphanedBlock struct {
	Block    *model.Block
	Receipts []*model.Receipt
	ReorgSeq uint64
}

// OrphanReader reads reorganization records and orphaned blocks.
type OrphanReader interface {
	// GetReorgs returns reorganization records, newest first.
	GetReorgs(ctx context.Context, limit, offset int) ([]*Reorg, error)
	// GetReorg returns record seq, or ErrNotFound.
	GetReorg(ctx context.Context, seq uint64) (*Reorg, error)
	// GetOrphanedBlock returns an orphaned block by hash, or ErrNotFound.
	GetOrphanedBlock(ctx context.Context, hash common.Hash) (*OrphanedBlock, error)
	// GetOrphanedBlocksAt returns the blocks orphaned at a height.
	GetOrphanedBlocksAt(ctx context.Context, height uint64) ([]*OrphanedBlock, error)
	// GetOrphanedTransaction returns every orphaned block that held the
	// transaction (a transaction can be orphaned more than once).
	GetOrphanedTransaction(ctx context.Context, txHash common.Hash) ([]*OrphanedBlock, error)
}

// ErrNoUndo means a block cannot be rolled back: its undo record was pruned,
// never written, or the block used an operation that cannot be undone.
var ErrNoUndo = errors.New("storage: no undo record for block")

// Rollbacker rolls indexed blocks back after a chain reorganization.
type Rollbacker interface {
	// RollbackTo undoes the indexed blocks above height to, newest first,
	// archives them as orphans and returns the reorganization record. If a
	// block in the range cannot be undone, nothing changes and the error
	// wraps ErrNoUndo.
	RollbackTo(ctx context.Context, to uint64) (*Reorg, error)
}
