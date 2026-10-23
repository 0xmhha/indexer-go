package postgres

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func executionCoreFixture(t *testing.T) (*Store, port.ExecutionConfig, *model.Block, []*model.Receipt) {
	t.Helper()
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	b := &model.Block{Number: 1, Hash: common.Hash{2}, ParentHash: common.Hash(cfg.Anchor.Hash), Time: 1, Transactions: []*model.Transaction{{Hash: common.Hash{7}, From: common.Address{1}, Value: big.NewInt(1), BlockNumber: 1, BlockHash: common.Hash{2}, Index: 0}}}
	r := &model.Receipt{BlockNumber: 1, BlockHash: b.Hash, TxHash: b.Transactions[0].Hash, TxIndex: 0, Status: 1, Logs: []*model.Log{{BlockNumber: 1, BlockHash: b.Hash, TxHash: b.Transactions[0].Hash, TxIndex: 0, Index: 0, Address: common.Address{3}, Data: []byte{4}}}}
	return s, cfg, b, []*model.Receipt{r}
}
func TestExecutionCoreWritersOwnOnlyTheirTables(t *testing.T) {
	s, cfg, b, rs := executionCoreFixture(t)
	want := map[string]int{"blocks": 1, "transactions": 0, "receipts": 0, "logs": 0}
	for _, w := range []port.ExecutionWriter{port.ExecutionLogs, port.ExecutionReceipts, port.ExecutionTransactions, port.ExecutionBlocks} {
		p := firstExecutionPart(w, cfg)
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionCore(ctx, w, b, rs) }))
		want[string(w)]++
		for table, n := range want {
			var got int
			require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&got))
			require.Equal(t, n, got, "writer %s changed unexpected table %s", w, table)
		}
	}
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, cfg.Anchor, c.Visible)
	count, err := s.GetTransactionCount(t.Context())
	require.NoError(t, err)
	require.Zero(t, count, "part durability does not contribute to public count")
	var h int64
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT undo_height FROM transactions WHERE hash=$1", b.Transactions[0].Hash[:]).Scan(&h))
	require.Equal(t, int64(1), h)
	var undo int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM undo_log").Scan(&undo))
	require.Zero(t, undo)
}
func TestExecutionCoreScopeAndImmutableCollision(t *testing.T) {
	s, cfg, b, rs := executionCoreFixture(t)
	require.ErrorIs(t, s.ApplyExecutionCore(t.Context(), port.ExecutionBlocks, b, rs), port.ErrClosed)
	// A preexisting transaction of another block must not move ahead of
	// visibility just because a later part reuses its hash.
	old := *b
	old.Number = 0
	old.Hash = common.Hash{9}
	old.ParentHash = common.Hash{}
	require.NoError(t, s.SetBlock(t.Context(), &old))
	p := firstExecutionPart(port.ExecutionTransactions, cfg)
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionCore(ctx, p.Writer, b, rs) }), port.ErrExecutionConflict)
	_, loc, err := s.GetTransaction(t.Context(), b.Transactions[0].Hash)
	require.NoError(t, err)
	require.Equal(t, uint64(0), loc.BlockHeight)
	cur, err := s.ExecutionCursor(t.Context(), p.Writer)
	require.NoError(t, err)
	require.Equal(t, cfg.Anchor, cur.Ref)
}
func TestExecutionCoreRejectsMismatchedModels(t *testing.T) {
	s, cfg, b, rs := executionCoreFixture(t)
	p := firstExecutionPart(port.ExecutionReceipts, cfg)
	rs[0].BlockHash[0]++
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionCore(ctx, p.Writer, b, rs) }), port.ErrExecutionConflict)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM receipts").Scan(&count))
	require.Zero(t, count)
}
