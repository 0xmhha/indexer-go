package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type readerDiagnosticEvents struct {
	mu      sync.Mutex
	reads   []ReadObservation
	queries []QueryObservation
}

func (e *readerDiagnosticEvents) context(ctx context.Context) context.Context {
	ctx = ObserveReads(ctx, func(o ReadObservation) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.reads = append(e.reads, o)
	})
	return ObserveQueries(ctx, func(o QueryObservation) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.queries = append(e.queries, o)
	})
}

func (e *readerDiagnosticEvents) assertCanceled(t *testing.T, path string, sql bool) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	require.Len(t, e.reads, 1, "canceled reads never decode a block")
	require.Equal(t, "admission", e.reads[0].Stage)
	require.Equal(t, path, e.reads[0].Path)
	require.Equal(t, !sql, e.reads[0].Failed, "admission success is independent of SQL cancellation")
	stages := map[string]int{}
	for _, o := range e.queries {
		stages[o.Stage]++
	}
	want := map[string]int{"acquire": 1}
	if sql {
		want["query"] = 1
	} else {
		require.GreaterOrEqual(t, e.reads[0].BorrowProbes, 1)
	}
	require.Equal(t, want, stages)
}

func TestReservedQueryReaderDiagnosticsPostgres(t *testing.T) {
	t.Run("cancel queued admission returns active reference", func(t *testing.T) {
		ctx, stop := context.WithTimeout(t.Context(), 30*time.Second)
		defer stop()
		s := newTestStore(t)
		require.Equal(t, int32(4), s.pool.Config().MaxConns)
		require.NoError(t, s.SetBlock(ctx, &model.Block{Number: 1, Hash: common.Hash{1}}))
		lease, err := s.ReserveQueryReader(ctx)
		require.NoError(t, err)
		t.Cleanup(lease.Close)
		apiCtx := lease.Context(ctx)
		rows, err := s.q(apiCtx).Query(apiCtx, "SELECT 1")
		require.NoError(t, err)
		defer rows.Close()
		require.True(t, rows.Next())
		var held []*pgxpool.Conn
		defer func() {
			for _, c := range held {
				c.Release()
			}
		}()
		for i := 0; i < 3; i++ {
			c, err := s.pool.Acquire(ctx)
			require.NoError(t, err)
			held = append(held, c)
		}
		require.Equal(t, int32(4), s.pool.Stat().AcquiredConns())
		events := &readerDiagnosticEvents{}
		request, cancel := context.WithCancel(events.context(apiCtx))
		defer cancel()
		done := make(chan error, 1)
		canceledBefore := s.pool.Stat().CanceledAcquireCount()
		go func() { _, err := s.GetBlock(request, 1); done <- err }()
		// Wait for a real pool admission attempt, not a short timeout/sleep.
		require.Eventually(t, func() bool { return s.pool.Stat().CanceledAcquireCount() > canceledBefore }, 10*time.Second, time.Millisecond)
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		events.assertCanceled(t, "unavailable", false)
		reader := lease.(*queryReaderLease).reader
		reader.stateMu.Lock()
		active := reader.active
		reader.stateMu.Unlock()
		require.Equal(t, 1, active, "only the deliberately held rows remain active")
		rows.Close()
		rows.Close()
		_, err = s.GetBlock(apiCtx, 1)
		require.NoError(t, err, "cancellation returned the reservation gate")
		lease.Close()
		require.Equal(t, int32(3), s.pool.Stat().AcquiredConns())
	})

	t.Run("cancel SQL lock wait then reuse reserved slot", func(t *testing.T) {
		ctx, stop := context.WithTimeout(t.Context(), 30*time.Second)
		defer stop()
		s := newTestStore(t)
		require.Equal(t, int32(4), s.pool.Config().MaxConns)
		require.NoError(t, s.SetBlock(ctx, &model.Block{Number: 1, Hash: common.Hash{1}}))
		lease, err := s.ReserveQueryReader(ctx)
		require.NoError(t, err)
		t.Cleanup(lease.Close)
		apiCtx := lease.Context(ctx)
		reader := lease.(*queryReaderLease).reader
		pinned, release, err := reader.acquire(ctx)
		require.NoError(t, err)
		pid := pinned.Conn().PgConn().PID()
		release()
		control, err := s.pool.Acquire(ctx)
		require.NoError(t, err)
		defer control.Release()
		lock, err := control.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = lock.Rollback(ctx) }()
		_, err = lock.Exec(ctx, "LOCK TABLE blocks IN ACCESS EXCLUSIVE MODE")
		require.NoError(t, err)
		events := &readerDiagnosticEvents{}
		request, cancel := context.WithCancel(events.context(apiCtx))
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := s.GetBlock(request, 1); done <- err }()
		require.Eventually(t, func() bool {
			var blocked bool
			err := lock.QueryRow(ctx, "SELECT pg_backend_pid() = ANY(pg_blocking_pids($1::integer))", int32(pid)).Scan(&blocked)
			require.NoError(t, err)
			return blocked
		}, 10*time.Second, time.Millisecond, "cancel only after PostgreSQL confirms the SELECT is waiting on our lock")
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		events.assertCanceled(t, "pinned", true)
		require.NoError(t, lock.Rollback(ctx))
		_, err = s.GetBlock(apiCtx, 1)
		require.NoError(t, err, "the next read recovers within the original four-connection pool")
		require.LessOrEqual(t, s.pool.Stat().TotalConns(), int32(4))
		lease.Close()
		require.Equal(t, int32(1), s.pool.Stat().AcquiredConns(), "only the control connection remains")
	})
}
