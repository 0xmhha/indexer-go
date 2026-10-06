package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// indexOrphanTestChain stores blocks 0..n, each with one transaction whose
// receipt has one log, the way the fetcher does (one block transaction with
// a height, so undo is recorded).
func indexOrphanTestChain(t *testing.T, s *PebbleStorage, n uint64, tag byte) []*model.Block {
	t.Helper()
	ctx := context.Background()
	var blocks []*model.Block
	parent := common.Hash{}
	for h := uint64(0); h <= n; h++ {
		hash := common.Hash{tag, byte(h), 1}
		txHash := common.Hash{tag, byte(h), 2}
		to := common.Address{9}
		b := &model.Block{
			Hash: hash, ParentHash: parent, Number: h, Difficulty: big.NewInt(0), BaseFee: big.NewInt(1),
			Transactions: []*model.Transaction{{
				Hash: txHash, To: &to, Value: big.NewInt(int64(h)), Gas: 21000,
				GasPrice: big.NewInt(1), GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(1),
				BlockHash: hash, BlockNumber: h,
			}},
		}
		r := &model.Receipt{
			Status: 1, CumulativeGasUsed: 21000, GasUsed: 21000, EffectiveGasPrice: big.NewInt(1),
			TxHash: txHash, BlockHash: hash, BlockNumber: h, Bloom: make([]byte, 256),
			Logs: []*model.Log{{Address: to, Topics: []common.Hash{{7}}, Data: []byte{byte(h)}, BlockNumber: h, BlockHash: hash, TxHash: txHash}},
		}
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		tx.SetHeight(h)
		require.NoError(t, s.SetModelBlock(txCtx, b))
		require.NoError(t, s.SetModelReceipt(txCtx, r))
		require.NoError(t, s.SetLatestHeight(txCtx, h))
		require.NoError(t, tx.Commit())
		blocks = append(blocks, b)
		parent = hash
	}
	return blocks
}

func TestRollbackArchivesOrphans(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	blocks := indexOrphanTestChain(t, s, 5, 'a')

	r, err := s.RollbackTo(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, uint64(1), r.Seq)
	require.Equal(t, uint64(2), r.ForkNumber)
	require.Equal(t, blocks[2].Hash, r.ForkHash)
	require.Equal(t, uint64(5), r.OldHead)
	require.Equal(t, []BlockRef{{Number: 5, Hash: blocks[5].Hash}, {Number: 4, Hash: blocks[4].Hash}, {Number: 3, Hash: blocks[3].Hash}}, r.Removed)
	require.Len(t, r.Blocks, 3)

	latest, err := s.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), latest)
	for h := uint64(3); h <= 5; h++ {
		_, err := s.GetModelBlock(ctx, h)
		require.ErrorIs(t, err, ErrNotFound, "block %d left the canonical index", h)

		ob, err := s.GetOrphanedBlock(ctx, blocks[h].Hash)
		require.NoError(t, err)
		require.Equal(t, uint64(1), ob.ReorgSeq)
		require.Equal(t, blocks[h].Hash, ob.Block.Hash)
		require.Equal(t, blocks[h].Transactions[0].Hash, ob.Block.Transactions[0].Hash)
		require.Len(t, ob.Receipts, 1)
		require.Equal(t, []byte{byte(h)}, ob.Receipts[0].Logs[0].Data)

		at, err := s.GetOrphanedBlocksAt(ctx, h)
		require.NoError(t, err)
		require.Len(t, at, 1)
		byTx, err := s.GetOrphanedTransaction(ctx, blocks[h].Transactions[0].Hash)
		require.NoError(t, err)
		require.Len(t, byTx, 1)
		require.Equal(t, blocks[h].Hash, byTx[0].Block.Hash)
	}
	at, err := s.GetOrphanedBlocksAt(ctx, 2)
	require.NoError(t, err)
	require.Empty(t, at)

	// A second reorganization on the new branch gets the next record, and
	// the first one's orphans stay.
	indexAgain := func(from, to uint64) {
		for h := from; h <= to; h++ {
			txCtx, tx, err := s.BeginBlock(ctx)
			require.NoError(t, err)
			tx.SetHeight(h)
			b := &model.Block{Hash: common.Hash{'b', byte(h)}, Number: h, Difficulty: big.NewInt(0)}
			require.NoError(t, s.SetModelBlock(txCtx, b))
			require.NoError(t, s.SetLatestHeight(txCtx, h))
			require.NoError(t, tx.Commit())
		}
	}
	indexAgain(3, 4)
	r2, err := s.RollbackTo(ctx, 3)
	require.NoError(t, err)
	require.Equal(t, uint64(2), r2.Seq)
	at, err = s.GetOrphanedBlocksAt(ctx, 4)
	require.NoError(t, err)
	require.Len(t, at, 2, "both branches' block 4 are kept")

	reorgs, err := s.GetReorgs(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, reorgs, 2)
	require.Equal(t, uint64(2), reorgs[0].Seq, "newest first")
	reorgs, err = s.GetReorgs(ctx, 1, 1)
	require.NoError(t, err)
	require.Len(t, reorgs, 1)
	require.Equal(t, uint64(1), reorgs[0].Seq)
	got, err := s.GetReorg(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, r.Removed, got.Removed)
	_, err = s.GetReorg(ctx, 3)
	require.ErrorIs(t, err, ErrNotFound)
}

// TestRollbackArchivesAtomically makes archiving fail in the middle of a
// rollback: every block is either still canonical or orphaned, never both
// and never neither.
func TestRollbackArchivesAtomically(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	blocks := indexOrphanTestChain(t, s, 5, 'a')
	require.NoError(t, s.db.Delete(ReceiptKey(blocks[4].Transactions[0].Hash), pebble.Sync))

	_, err := s.RollbackTo(ctx, 2)
	require.Error(t, err)
	for h := uint64(3); h <= 5; h++ {
		_, canonErr := s.GetModelBlock(ctx, h)
		_, orphanErr := s.GetOrphanedBlock(ctx, blocks[h].Hash)
		require.NotEqual(t, canonErr == nil, orphanErr == nil, "block %d: canonical %v, orphan %v", h, canonErr, orphanErr)
	}
	_, err = s.GetOrphanedBlock(ctx, blocks[5].Hash)
	require.NoError(t, err, "block 5 rolled back before the failure")
	latest, err := s.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), latest)
}

// TestOrphanRetentionPrunesOldReorgs keeps the newest two reorganization
// records: older records are deleted with the blocks they archived, except
// a block a later reorganization archived again.
func TestOrphanRetentionPrunesOldReorgs(t *testing.T) {
	s := newTestPebble(t)
	s.SetOrphanRetention(2)
	ctx := context.Background()
	chain := indexOrphanTestChain(t, s, 5, 'a')
	a5 := chain[5]

	// indexAt stores b at height 5 on top of block 4, with its transaction.
	indexAt := func(b *model.Block) {
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		tx.SetHeight(5)
		require.NoError(t, s.SetModelBlock(txCtx, b))
		for _, x := range b.Transactions {
			require.NoError(t, s.SetModelReceipt(txCtx, &model.Receipt{
				Status: 1, CumulativeGasUsed: 21000, GasUsed: 21000, EffectiveGasPrice: big.NewInt(1),
				TxHash: x.Hash, BlockHash: b.Hash, BlockNumber: 5, Bloom: make([]byte, 256),
			}))
		}
		require.NoError(t, s.SetLatestHeight(txCtx, 5))
		require.NoError(t, tx.Commit())
	}
	branch := func(tag byte) *model.Block {
		to := common.Address{9}
		hash := common.Hash{tag, 5, 1}
		return &model.Block{
			Hash: hash, ParentHash: chain[4].Hash, Number: 5, Difficulty: big.NewInt(0), BaseFee: big.NewInt(1),
			Transactions: []*model.Transaction{{
				Hash: common.Hash{tag, 5, 2}, To: &to, Value: big.NewInt(1), Gas: 21000,
				GasPrice: big.NewInt(1), GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(1),
				BlockHash: hash, BlockNumber: 5,
			}},
		}
	}
	rollback := func(seq uint64) {
		r, err := s.RollbackTo(ctx, 4)
		require.NoError(t, err)
		require.Equal(t, seq, r.Seq)
	}

	rollback(1) // removes a5
	b5 := branch('b')
	indexAt(b5)
	rollback(2) // removes b5
	indexAt(a5)
	rollback(3) // removes a5 again; record 1 is pruned

	_, err := s.GetReorg(ctx, 1)
	require.ErrorIs(t, err, ErrNotFound)
	ob, err := s.GetOrphanedBlock(ctx, a5.Hash)
	require.NoError(t, err, "a5 was archived again by record 3")
	require.Equal(t, uint64(3), ob.ReorgSeq)
	byTx, err := s.GetOrphanedTransaction(ctx, a5.Transactions[0].Hash)
	require.NoError(t, err)
	require.Len(t, byTx, 1)

	c5 := branch('c')
	indexAt(c5)
	rollback(4) // removes c5; record 2 and b5 are pruned

	_, err = s.GetOrphanedBlock(ctx, b5.Hash)
	require.ErrorIs(t, err, ErrNotFound)
	byTx, err = s.GetOrphanedTransaction(ctx, b5.Transactions[0].Hash)
	require.NoError(t, err)
	require.Empty(t, byTx)
	at, err := s.GetOrphanedBlocksAt(ctx, 5)
	require.NoError(t, err)
	var hashes []common.Hash
	for _, o := range at {
		hashes = append(hashes, o.Block.Hash)
	}
	require.ElementsMatch(t, []common.Hash{a5.Hash, c5.Hash}, hashes)

	reorgs, err := s.GetReorgs(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, reorgs, 2)
	require.Equal(t, uint64(4), reorgs[0].Seq)
	require.Equal(t, uint64(3), reorgs[1].Seq)
}
