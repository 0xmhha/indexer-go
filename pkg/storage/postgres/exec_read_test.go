package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestExecutionReadHidesUnpublishedCoreEveryLookup(t *testing.T) {
	s, cfg, b, rs := executionCoreFixture(t)
	for _, w := range port.ExecutionWriters()[:4] {
		p := firstExecutionPart(w, cfg)
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionCore(ctx, w, b, rs) }))
	}
	_, err := s.GetBlock(t.Context(), 1)
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.GetBlockByHash(t.Context(), b.Hash)
	require.ErrorIs(t, err, port.ErrNotFound)
	_, _, err = s.GetTransaction(t.Context(), b.Transactions[0].Hash)
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.GetReceipt(t.Context(), rs[0].TxHash)
	require.ErrorIs(t, err, port.ErrNotFound)
	logs, err := s.GetLogsByBlock(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, logs)
	blocks, err := s.GetBlocks(t.Context(), 0, 10)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.NoError(t, s.SetLatestHeight(t.Context(), 99))
	tip, _, err := s.GetLatestBlockWindow(t.Context(), 0, 10)
	require.NoError(t, err)
	require.Zero(t, tip, "raw metadata cannot advance public execution height")
	count, err := s.GetTransactionCount(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	has, err := s.HasBlock(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, has)
}
func TestExecutionReadSnapshotRetainsVisibleGenerationDuringPublication(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	err := s.ReadSnapshot(t.Context(), func(ctx context.Context) error {
		before, err := s.ExecutionStatus(ctx)
		require.NoError(t, err)
		require.Equal(t, cfg.Anchor, before.Visible)
		_, err = s.CommitExecution(t.Context(), p, func(context.Context) error { return nil }, func(context.Context) error { return nil })
		require.NoError(t, err)
		after, err := s.ExecutionStatus(ctx)
		require.NoError(t, err)
		require.Equal(t, before.Visible, after.Visible)
		_, err = s.GetBlock(ctx, p.Ref.Height)
		require.ErrorIs(t, err, port.ErrNotFound)
		return nil
	})
	require.NoError(t, err)
	after, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, p.Ref, after.Visible)
}
func TestExecutionReadSnapshotReservedSlotParallelCancelAndRewind(t *testing.T) {
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	lease, err := s.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	defer lease.Close()
	ctx, cancel := context.WithTimeout(lease.Context(t.Context()), 5*time.Second)
	defer cancel()
	var retained context.Context
	require.NoError(t, s.ReadSnapshot(ctx, func(view context.Context) error {
		retained = view
		var wg sync.WaitGroup
		errs := make(chan error, 20)
		for range 20 {
			wg.Go(func() { _, err := s.GetBlock(view, 0); errs <- err })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		return nil
	}))
	_, err = s.GetBlock(retained, 0)
	require.Error(t, err, "closed snapshot must not fall back to pool")
	require.Equal(t, int32(1), s.pool.Stat().AcquiredConns())
	_, err = s.pool.Exec(ctx, "UPDATE exec_control SET rewinding=true WHERE id")
	require.NoError(t, err)
	called := false
	err = s.ReadSnapshot(ctx, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
	require.False(t, called)
}

func TestExecutionReadSnapshotUsesReservedFourthSlotAndRejectsWrites(t *testing.T) {
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	lease, err := s.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	defer lease.Close()
	for range 3 {
		conn, err := s.pool.Acquire(t.Context())
		require.NoError(t, err)
		defer conn.Release()
	}
	ctx, cancel := context.WithTimeout(lease.Context(t.Context()), 5*time.Second)
	defer cancel()
	require.NoError(t, s.ReadSnapshot(ctx, func(view context.Context) error {
		_, err := s.GetBlock(view, 0)
		require.NoError(t, err)
		require.ErrorIs(t, s.Put(view, []byte("no-read-write"), []byte("x")), port.ErrReadOnly)
		_, _, err = s.BeginBlock(view)
		require.ErrorIs(t, err, port.ErrReadOnly)
		return nil
	}))
	require.Equal(t, int32(4), s.pool.Stat().AcquiredConns())
}
func TestExecutionReadSnapshotClosesAbandonedRowsAndRejectsRetainedContext(t *testing.T) {
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	var rows pgx.Rows
	var retained context.Context
	require.NoError(t, s.ReadSnapshot(t.Context(), func(view context.Context) error {
		retained = view
		var err error
		rows, err = s.q(view).Query(view, "SELECT generate_series(1,5)")
		return err
	}))
	require.False(t, rows.Next())
	rows.Close()
	_, err := s.GetBlock(retained, 0)
	require.ErrorIs(t, err, port.ErrClosed)
	require.Zero(t, s.pool.Stat().AcquiredConns())
}
func TestExecutionReadSnapshotCancellationJoinsActiveSQL(t *testing.T) {
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	lease, err := s.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	defer lease.Close()
	ctx, cancel := context.WithCancel(lease.Context(t.Context()))
	defer cancel()
	monitor, err := s.pool.Acquire(t.Context())
	require.NoError(t, err)
	defer monitor.Release()
	pid := s.queryReader.conn.Conn().PgConn().PID()
	done := make(chan error, 1)
	go func() {
		done <- s.ReadSnapshot(ctx, func(view context.Context) error {
			var n int
			return s.q(view).QueryRow(view, "SELECT 1 FROM pg_sleep(30)").Scan(&n)
		})
	}()
	require.Eventually(t, func() bool {
		var active bool
		err := monitor.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND state='active' AND query LIKE '%pg_sleep(30)%')", pid).Scan(&active)
		return err == nil && active
	}, 5*time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot cancellation did not join active SQL")
	}
	require.Equal(t, int32(2), s.pool.Stat().AcquiredConns(), "only reservation and monitor remain")
}
