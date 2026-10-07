package porttest

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type outboxStore interface {
	orphanStore
	port.Outbox
}

// testOutbox checks Outbox: entries appended in block transactions are
// numbered 1, 2, ... in commit order without gaps; a rolled-back
// transaction uses no numbers; entries exist only after the transaction
// commits; a block rollback keeps them, and entries the undo hook appends
// commit with the rollback; pruning deletes old entries without restarting
// the numbering; cursors are kept per name.
func testOutbox(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NumbersInCommitOrder", func(t *testing.T) {
		s := open[outboxStore](t, newStore)
		last, err := s.LastOutboxSeq(ctx)
		require.NoError(t, err)
		assert.Zero(t, last)
		entries, err := s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Empty(t, entries)

		outboxAppend(t, s, true, "a", "b")
		outboxAppend(t, s, false, "lost") // rolled back
		outboxAppend(t, s, true, "c")
		outboxAppend(t, s, true)

		entries, err = s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, []string{"1:a", "2:b", "3:c"}, outboxStrings(entries), "a rolled-back transaction uses no numbers")
		last, err = s.LastOutboxSeq(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(3), last)

		entries, err = s.ReadOutbox(ctx, 1, 1)
		require.NoError(t, err)
		assert.Equal(t, []string{"2:b"}, outboxStrings(entries), "after and limit")
		entries, err = s.ReadOutbox(ctx, 3, 10)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("VisibleAfterCommit", func(t *testing.T) {
		s := open[outboxStore](t, newStore)
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.AppendOutbox(txCtx, []port.OutboxEntry{{Type: "t", Data: []byte("x")}}))
		require.NoError(t, s.AppendOutbox(txCtx, []port.OutboxEntry{{Type: "t", Data: []byte("y")}}))
		entries, err := s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Empty(t, entries, "nothing is visible before commit")
		require.NoError(t, tx.Commit())
		entries, err = s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, []string{"1:x", "2:y"}, outboxStrings(entries), "appends in one transaction continue each other")
	})

	t.Run("RequiresBlockTransaction", func(t *testing.T) {
		s := open[outboxStore](t, newStore)
		assert.Error(t, s.AppendOutbox(ctx, []port.OutboxEntry{{Type: "t"}}))
	})

	t.Run("RollbackKeepsEntries", func(t *testing.T) {
		s := open[outboxStore](t, newStore)
		c := newChain(4)
		for _, b := range c.Blocks {
			txCtx, tx, err := s.BeginBlock(ctx)
			require.NoError(t, err)
			tx.SetHeight(b.Number)
			require.NoError(t, s.SetBlock(txCtx, b))
			for _, r := range orphanReceipts(c, b) {
				require.NoError(t, s.SetReceipt(txCtx, r))
			}
			require.NoError(t, s.SetLatestHeight(txCtx, b.Number))
			require.NoError(t, s.AppendOutbox(txCtx, []port.OutboxEntry{{Type: "block", Data: []byte(fmt.Sprint(b.Number))}}))
			require.NoError(t, tx.Commit())
		}

		var calls []string
		_, err := s.RollbackTo(ctx, 1, func(txCtx context.Context, r *port.Reorg, b *port.OrphanedBlock, first bool) error {
			calls = append(calls, fmt.Sprintf("%d/%v", b.Block.Number, first))
			return s.AppendOutbox(txCtx, []port.OutboxEntry{{Type: "removed", Data: []byte(fmt.Sprint(b.Block.Number))}})
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"3/true", "2/false"}, calls, "the hook runs per block, newest first")
		entries, err := s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, []string{"1:0", "2:1", "3:2", "4:3", "5:3", "6:2"}, outboxStrings(entries),
			"the rolled-back blocks' entries stay; the hook's entries follow")

		// A failing hook aborts its block's rollback with its entries.
		_, err = s.RollbackTo(ctx, 0, func(context.Context, *port.Reorg, *port.OrphanedBlock, bool) error {
			return fmt.Errorf("boom")
		})
		require.Error(t, err)
		head, err := s.GetLatestHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), head)
		last, err := s.LastOutboxSeq(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(6), last)
	})

	t.Run("PruneKeepsNumbering", func(t *testing.T) {
		s := open[outboxStore](t, newStore)
		outboxAppend(t, s, true, "a", "b", "c")
		require.NoError(t, s.PruneOutbox(ctx, 3))
		entries, err := s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, []string{"3:c"}, outboxStrings(entries))
		require.NoError(t, s.PruneOutbox(ctx, 4))
		entries, err = s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Empty(t, entries)
		outboxAppend(t, s, true, "d")
		entries, err = s.ReadOutbox(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, []string{"4:d"}, outboxStrings(entries), "numbering continues after pruning everything")
	})

	t.Run("Cursors", func(t *testing.T) {
		s := open[outboxStore](t, newStore)
		seq, err := s.OutboxCursor(ctx, "relay")
		require.NoError(t, err)
		assert.Zero(t, seq)
		require.NoError(t, s.SetOutboxCursor(ctx, "relay", 7))
		require.NoError(t, s.SetOutboxCursor(ctx, "other", 2))
		seq, err = s.OutboxCursor(ctx, "relay")
		require.NoError(t, err)
		assert.Equal(t, uint64(7), seq)
		seq, err = s.OutboxCursor(ctx, "other")
		require.NoError(t, err)
		assert.Equal(t, uint64(2), seq)
	})
}

// outboxAppend appends one entry per value (type "t") in one block
// transaction and commits it, or rolls it back when commit is false.
func outboxAppend(t *testing.T, s outboxStore, commit bool, values ...string) {
	t.Helper()
	txCtx, tx, err := s.BeginBlock(context.Background())
	require.NoError(t, err)
	for _, v := range values {
		require.NoError(t, s.AppendOutbox(txCtx, []port.OutboxEntry{{Type: "t", Data: []byte(v)}}))
	}
	if commit {
		require.NoError(t, tx.Commit())
	} else {
		tx.Rollback()
	}
}

func outboxStrings(entries []port.OutboxEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%d:%s", e.Seq, e.Data))
	}
	return out
}
