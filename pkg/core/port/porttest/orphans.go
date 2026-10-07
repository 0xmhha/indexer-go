package porttest

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// testOrphans checks Rollbacker and OrphanReader: RollbackTo(to) removes the
// indexed blocks above to from the Reader ports and moves the cursor back to
// to; the removed blocks, with their receipts, stay readable as orphans by
// hash, height and transaction, under a reorganization record that lists
// them newest first. A rollback that reaches a block without undo changes
// nothing and wraps port.ErrNoUndo.
func testOrphans(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	c := newChain(5)

	t.Run("EmptyStore", func(t *testing.T) {
		s := open[orphanStore](t, newStore)
		reorgs, _, err := s.GetReorgs(ctx, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, reorgs)
		_, err = s.GetReorg(ctx, 1)
		assert.ErrorIs(t, err, port.ErrNotFound)
		_, err = s.GetOrphanedBlock(ctx, c.Blocks[1].Hash)
		assert.ErrorIs(t, err, port.ErrNotFound)
		obs, err := s.GetOrphanedBlocksAt(ctx, 1)
		require.NoError(t, err)
		assert.Empty(t, obs)
		obs, err = s.GetOrphanedTransaction(ctx, c.Blocks[1].Transactions[0].Hash)
		require.NoError(t, err)
		assert.Empty(t, obs)
	})

	t.Run("RollbackRemovesBlocks", func(t *testing.T) {
		s := open[orphanStore](t, newStore)
		orphanIndexChain(t, s, c)
		reorg, err := s.RollbackTo(ctx, 1, nil)
		require.NoError(t, err)
		require.NotNil(t, reorg)

		assert.Equal(t, uint64(1), reorg.Seq, "the first reorganization")
		assert.Equal(t, uint64(1), reorg.ForkNumber)
		assert.Equal(t, c.Blocks[1].Hash, reorg.ForkHash)
		assert.Equal(t, uint64(4), reorg.OldHead)
		assert.Equal(t, orphanRefs(c.Blocks[4], c.Blocks[3], c.Blocks[2]), reorg.Removed, "newest first")
		assert.NotZero(t, reorg.DetectedAt)
		require.Len(t, reorg.Blocks, 3, "the removed blocks are returned")
		for i, h := range []uint64{4, 3, 2} {
			orphanAssertBlock(t, c, c.Blocks[h], 1, reorg.Blocks[i])
		}

		head, err := s.GetLatestHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), head, "the cursor moves back to the fork")
		for _, b := range c.Blocks[2:] {
			_, err := s.GetBlock(ctx, b.Number)
			assert.ErrorIs(t, err, port.ErrNotFound, "block %d", b.Number)
			_, err = s.GetBlockByHash(ctx, b.Hash)
			assert.ErrorIs(t, err, port.ErrNotFound, "block %d by hash", b.Number)
			ok, err := s.HasBlock(ctx, b.Number)
			require.NoError(t, err)
			assert.False(t, ok)
			for _, tx := range b.Transactions {
				_, _, err := s.GetTransaction(ctx, tx.Hash)
				assert.ErrorIs(t, err, port.ErrNotFound, "tx %s", tx.Hash.Hex())
				_, err = s.GetReceipt(ctx, tx.Hash)
				assert.ErrorIs(t, err, port.ErrNotFound, "receipt %s", tx.Hash.Hex())
			}
		}
		for _, b := range c.Blocks[:2] {
			got, err := s.GetBlock(ctx, b.Number)
			require.NoError(t, err, "block %d below the fork stays", b.Number)
			assertBlock(t, b, got)
			for _, tx := range b.Transactions {
				r, err := s.GetReceipt(ctx, tx.Hash)
				require.NoError(t, err)
				assertReceipt(t, c.receipt(tx.Hash), r)
			}
		}
	})

	t.Run("OrphansReadable", func(t *testing.T) {
		s := open[orphanStore](t, newStore)
		orphanIndexChain(t, s, c)
		_, err := s.RollbackTo(ctx, 1, nil)
		require.NoError(t, err)

		for _, b := range c.Blocks[2:] {
			ob, err := s.GetOrphanedBlock(ctx, b.Hash)
			require.NoError(t, err, "block %d", b.Number)
			orphanAssertBlock(t, c, b, 1, ob)

			at, err := s.GetOrphanedBlocksAt(ctx, b.Number)
			require.NoError(t, err)
			require.Len(t, at, 1)
			orphanAssertBlock(t, c, b, 1, at[0])

			for _, tx := range b.Transactions {
				obs, err := s.GetOrphanedTransaction(ctx, tx.Hash)
				require.NoError(t, err)
				require.Len(t, obs, 1, "tx %s", tx.Hash.Hex())
				assert.Equal(t, b.Hash, obs[0].Block.Hash)
			}
		}
		at, err := s.GetOrphanedBlocksAt(ctx, 1)
		require.NoError(t, err)
		assert.Empty(t, at, "the fork block is not orphaned")
		obs, err := s.GetOrphanedTransaction(ctx, c.Blocks[1].Transactions[0].Hash)
		require.NoError(t, err)
		assert.Empty(t, obs, "a canonical transaction is not orphaned")

		rec, err := s.GetReorg(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), rec.Seq)
		assert.Equal(t, uint64(1), rec.ForkNumber)
		assert.Equal(t, c.Blocks[1].Hash, rec.ForkHash)
		assert.Equal(t, uint64(4), rec.OldHead)
		assert.Equal(t, orphanRefs(c.Blocks[4], c.Blocks[3], c.Blocks[2]), rec.Removed)
		assert.NotZero(t, rec.DetectedAt)
		_, err = s.GetReorg(ctx, 2)
		assert.ErrorIs(t, err, port.ErrNotFound)
	})

	t.Run("RepeatedReorgs", func(t *testing.T) {
		s := open[orphanStore](t, newStore)
		orphanIndexChain(t, s, c)
		first, err := s.RollbackTo(ctx, 2, nil)
		require.NoError(t, err)
		require.NotNil(t, first)

		// The new branch has another block 3 with the same transactions.
		fork, forkReceipts := orphanFork(c, 3)
		orphanIndex(t, s, fork, forkReceipts, true)
		got, err := s.GetBlock(ctx, 3)
		require.NoError(t, err)
		assert.Equal(t, fork.Hash, got.Hash, "the new branch is canonical")

		second, err := s.RollbackTo(ctx, 2, nil)
		require.NoError(t, err)
		require.NotNil(t, second)
		assert.Equal(t, first.Seq+1, second.Seq, "sequence numbers increase")
		assert.Equal(t, uint64(3), second.OldHead)
		assert.Equal(t, orphanRefs(fork), second.Removed)

		reorgs, _, err := s.GetReorgs(ctx, port.FirstPage(10))
		require.NoError(t, err)
		assert.Equal(t, []uint64{second.Seq, first.Seq}, orphanSeqs(reorgs), "newest first")

		at, err := s.GetOrphanedBlocksAt(ctx, 3)
		require.NoError(t, err)
		assert.ElementsMatch(t,
			[]orphanKey{{c.Blocks[3].Hash, first.Seq}, {fork.Hash, second.Seq}}, orphanKeys(at),
			"both blocks orphaned at height 3")
		tx := c.Blocks[3].Transactions[0].Hash
		obs, err := s.GetOrphanedTransaction(ctx, tx)
		require.NoError(t, err)
		assert.ElementsMatch(t,
			[]orphanKey{{c.Blocks[3].Hash, first.Seq}, {fork.Hash, second.Seq}}, orphanKeys(obs),
			"a transaction can be orphaned more than once")
		ob, err := s.GetOrphanedBlock(ctx, fork.Hash)
		require.NoError(t, err)
		orphanAssertBlockWith(t, fork, forkReceipts, second.Seq, ob)

		ob, err = s.GetOrphanedBlock(ctx, c.Blocks[4].Hash)
		require.NoError(t, err, "orphans survive later rollbacks")
		assert.Equal(t, first.Seq, ob.ReorgSeq)
		_, _, err = s.GetTransaction(ctx, tx)
		assert.ErrorIs(t, err, port.ErrNotFound)
	})

	t.Run("ReorgsPaging", func(t *testing.T) {
		s := open[orphanStore](t, newStore)
		orphanIndexChain(t, s, c)
		var want []*port.Reorg
		for _, to := range []uint64{3, 2, 1} {
			r, err := s.RollbackTo(ctx, to, nil)
			require.NoError(t, err)
			require.NotNil(t, r)
			want = append([]*port.Reorg{r}, want...) // newest first
		}
		reorgs := func(page port.Page) ([]*port.Reorg, string, error) { return s.GetReorgs(ctx, page) }
		checkPaging(t, want, func(r *port.Reorg) uint64 { return r.Seq }, reorgs)

		all, next, err := s.GetReorgs(ctx, port.Page{})
		require.NoError(t, err)
		assert.Equal(t, orphanSeqs(want), orphanSeqs(all), "limit 0 lists every record")
		assert.Empty(t, next)
	})

	t.Run("RollbackToHeadIsNoop", func(t *testing.T) {
		s := open[orphanStore](t, newStore)
		orphanIndexChain(t, s, c)
		for _, to := range []uint64{c.head(), c.head() + 5} {
			reorg, err := s.RollbackTo(ctx, to, nil)
			require.NoError(t, err, "to %d", to)
			if reorg != nil {
				assert.Empty(t, reorg.Removed, "to %d", to)
			}
		}
		head, err := s.GetLatestHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, c.head(), head)
		_, err = s.GetBlock(ctx, c.head())
		assert.NoError(t, err)
		reorgs, _, err := s.GetReorgs(ctx, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, reorgs, "nothing was rolled back")
	})

	t.Run("BlockWithoutUndo", func(t *testing.T) {
		s := open[orphanStore](t, newStore)
		for _, b := range c.Blocks {
			orphanIndex(t, s, b, orphanReceipts(c, b), b.Number != 3)
		}
		reorg, err := s.RollbackTo(ctx, 1, nil)
		assert.ErrorIs(t, err, port.ErrNoUndo, "block 3 has no undo")
		assert.Nil(t, reorg)
		orphanAssertUnchanged(t, s, c)

		reorg, err = s.RollbackTo(ctx, 3, nil)
		require.NoError(t, err, "block 4 above it can still be rolled back")
		assert.Equal(t, orphanRefs(c.Blocks[4]), reorg.Removed)
	})

	t.Run("PastUndoWindow", func(t *testing.T) {
		// The undo window is the store's choice; a rollback deeper than the
		// blocks it kept undo for must fail as a whole.
		s := open[orphanStore](t, newStore)
		long := &chain{}
		for h := uint64(0); h < 200; h++ {
			b := &model.Block{Hash: fixtureHash("long", h), Number: h, Time: baseTime + 12*h, GasLimit: 30_000_000, Miner: minerX}
			if h > 0 {
				b.ParentHash = fixtureHash("long", h-1)
			}
			long.Blocks = append(long.Blocks, b)
			orphanIndex(t, s, b, nil, true)
		}
		reorg, err := s.RollbackTo(ctx, 0, nil)
		if err == nil {
			t.Logf("store keeps undo for at least %d blocks", long.head())
			require.NotNil(t, reorg)
			assert.Len(t, reorg.Removed, int(long.head()))
			return
		}
		assert.ErrorIs(t, err, port.ErrNoUndo)
		assert.Nil(t, reorg)
		orphanAssertUnchanged(t, s, long)
	})
}

// orphanIndex writes block b the way ingest does: one block transaction
// with the block, its receipts and the cursor, recording undo when undo is
// set.
func orphanIndex(t *testing.T, s orphanStore, b *model.Block, receipts []*model.Receipt, undo bool) {
	t.Helper()
	txCtx, tx, err := s.BeginBlock(context.Background())
	require.NoError(t, err)
	if undo {
		tx.SetHeight(b.Number)
	}
	require.NoError(t, s.SetBlock(txCtx, b))
	for _, r := range receipts {
		require.NoError(t, s.SetReceipt(txCtx, r))
	}
	require.NoError(t, s.SetLatestHeight(txCtx, b.Number))
	require.NoError(t, tx.Commit())
}

// orphanIndexChain writes every block of c with undo.
func orphanIndexChain(t *testing.T, s orphanStore, c *chain) {
	t.Helper()
	for _, b := range c.Blocks {
		orphanIndex(t, s, b, orphanReceipts(c, b), true)
	}
}

// orphanReceipts returns the fixture receipts of b in transaction order.
func orphanReceipts(c *chain, b *model.Block) []*model.Receipt {
	var out []*model.Receipt
	for _, tx := range b.Transactions {
		out = append(out, c.receipt(tx.Hash))
	}
	return out
}

// orphanFork returns another block at height h of c, with a different hash
// and the same transactions, and its receipts. The fixture is not changed.
func orphanFork(c *chain, h uint64) (*model.Block, []*model.Receipt) {
	orig := c.Blocks[h]
	b := *orig
	b.Hash = fixtureHash("fork", h)
	b.Extra = []byte("fork")
	b.Transactions = nil
	var receipts []*model.Receipt
	for _, tx := range orig.Transactions {
		cp := *tx
		cp.BlockHash = b.Hash
		b.Transactions = append(b.Transactions, &cp)
		r := *c.receipt(tx.Hash)
		r.BlockHash = b.Hash
		r.Logs = nil
		for _, l := range c.receipt(tx.Hash).Logs {
			lc := *l
			lc.BlockHash = b.Hash
			r.Logs = append(r.Logs, &lc)
		}
		if r.Logs == nil {
			r.Logs = []*model.Log{}
		}
		receipts = append(receipts, &r)
	}
	return &b, receipts
}

func orphanRefs(blocks ...*model.Block) []port.BlockRef {
	out := make([]port.BlockRef, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, port.BlockRef{Number: b.Number, Hash: b.Hash})
	}
	return out
}

func orphanSeqs(reorgs []*port.Reorg) []uint64 {
	out := make([]uint64, 0, len(reorgs))
	for _, r := range reorgs {
		out = append(out, r.Seq)
	}
	return out
}

// orphanKey identifies an orphaned block and the reorganization that
// removed it.
type orphanKey struct {
	Hash common.Hash
	Seq  uint64
}

func orphanKeys(obs []*port.OrphanedBlock) []orphanKey {
	out := make([]orphanKey, 0, len(obs))
	for _, ob := range obs {
		out = append(out, orphanKey{ob.Block.Hash, ob.ReorgSeq})
	}
	return out
}

// orphanAssertBlock checks that ob is fixture block want with its receipts,
// orphaned by reorganization seq.
func orphanAssertBlock(t *testing.T, c *chain, want *model.Block, seq uint64, ob *port.OrphanedBlock) {
	t.Helper()
	orphanAssertBlockWith(t, want, orphanReceipts(c, want), seq, ob)
}

func orphanAssertBlockWith(t *testing.T, want *model.Block, receipts []*model.Receipt, seq uint64, ob *port.OrphanedBlock) {
	t.Helper()
	require.NotNil(t, ob)
	assertBlock(t, want, ob.Block)
	assert.Equal(t, seq, ob.ReorgSeq)
	require.Len(t, ob.Receipts, len(receipts), "receipts of block %d", want.Number)
	for i, r := range receipts {
		assertReceipt(t, r, ob.Receipts[i])
	}
}

// orphanAssertUnchanged checks that every block of c is still indexed, the
// cursor is at its head and no reorganization was recorded.
func orphanAssertUnchanged(t *testing.T, s orphanStore, c *chain) {
	t.Helper()
	ctx := context.Background()
	head, err := s.GetLatestHeight(ctx)
	require.NoError(t, err)
	assert.Equal(t, c.head(), head, "the cursor stays")
	for _, b := range c.Blocks {
		got, err := s.GetBlock(ctx, b.Number)
		require.NoError(t, err, "block %d stays", b.Number)
		assert.Equal(t, b.Hash, got.Hash)
		for _, tx := range b.Transactions {
			_, err := s.GetReceipt(ctx, tx.Hash)
			assert.NoError(t, err, "receipt %s stays", tx.Hash.Hex())
		}
	}
	reorgs, _, err := s.GetReorgs(ctx, port.FirstPage(10))
	require.NoError(t, err)
	assert.Empty(t, reorgs, "no reorganization recorded")
	at, err := s.GetOrphanedBlocksAt(ctx, c.head())
	require.NoError(t, err)
	assert.Empty(t, at, "nothing orphaned")
}
