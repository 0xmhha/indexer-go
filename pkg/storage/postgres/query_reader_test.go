package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestReservedQueryReaderCapacityCancelAndRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newTestStore(t)
	require.NoError(t, s.SetBlock(ctx, &model.Block{Number: 1, Hash: common.Hash{1}}))
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	require.NotNil(t, lease)
	defer lease.Close()
	second, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	defer second.Close()
	require.Equal(t, int32(1), s.pool.Stat().AcquiredConns(), "leases share one existing connection")
	apiCtx := lease.Context(ctx)
	for i := 0; i < 3; i++ {
		conn, err := s.pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
	}
	require.Equal(t, int32(4), s.pool.Stat().AcquiredConns())
	waiting, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	defer stop()
	queued := make(chan error, 1)
	go func() {
		conn, err := s.pool.Acquire(waiting)
		if conn != nil {
			conn.Release()
		}
		queued <- err
	}()
	block, err := s.GetBlock(apiCtx, 1)
	require.NoError(t, err)
	require.Equal(t, common.Hash{1}, block.Hash)
	require.ErrorIs(t, <-queued, context.DeadlineExceeded, "background cannot consume the API slot")
	rows, err := s.q(apiCtx).Query(apiCtx, "SELECT 1")
	require.NoError(t, err)
	defer rows.Close()
	blocked, stopRead := context.WithTimeout(apiCtx, 20*time.Millisecond)
	_, err = s.GetBlock(blocked, 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	stopRead()
	rows.Close()
	rows.Close()
	_, err = s.GetBlock(apiCtx, 1)
	require.NoError(t, err, "row close returns the read gate exactly once")
	var echoed string
	attack := "'; DROP TABLE blocks; --"
	require.NoError(t, s.q(apiCtx).QueryRow(apiCtx, "SELECT $1::text", attack).Scan(&echoed))
	require.Equal(t, attack, echoed)
	// Simulate a disconnected pinned connection; the next read reacquires
	// within the same four-connection pool, without a second pool or fallback.
	reader := lease.(*queryReaderLease).reader
	connection, releaseConnection, err := reader.acquire(ctx)
	require.NoError(t, err)
	require.NoError(t, connection.Conn().Close(ctx))
	releaseConnection()
	_, err = s.GetBlock(apiCtx, 1)
	require.NoError(t, err)
	require.LessOrEqual(t, s.pool.Stat().TotalConns(), int32(4))
	lease.Close()
	_, err = s.GetBlock(second.Context(ctx), 1)
	require.NoError(t, err, "one server closing keeps the other lease alive")
	second.Close()
	require.Equal(t, int32(3), s.pool.Stat().AcquiredConns(), "final close restores the fourth background slot")
	fourth, err := s.pool.Acquire(ctx)
	require.NoError(t, err)
	fourth.Release()
}

func TestReservedQueryReaderKeepsBoundStore(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	defer lease.Close()
	bound, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, s.SetBlock(bound, &model.Block{Number: 2, Hash: common.Hash{2}}))
	got, err := s.GetBlock(lease.Context(bound), 2)
	require.NoError(t, err)
	require.Equal(t, common.Hash{2}, got.Hash, "bound transaction takes precedence over reserved reads")
	_, err = s.GetBlock(lease.Context(ctx), 2)
	require.ErrorIs(t, err, port.ErrNotFound, "API never sees staged projection")
	other := newTestStore(t)
	require.NoError(t, other.SetBlock(ctx, &model.Block{Number: 2, Hash: common.Hash{9}}))
	got, err = other.GetBlock(lease.Context(bound), 2)
	require.NoError(t, err)
	require.Equal(t, common.Hash{9}, got.Hash, "a context never selects another Store's reservation or transaction")
}

func TestReservedQueryReaderLifecycle(t *testing.T) {
	ctx := context.Background()
	for _, budget := range []int32{1, 2} {
		s, err := Open(ctx, Options{DSN: testDSN(t), Schema: newTestSchema(t), MaxConns: budget})
		require.NoError(t, err)
		lease, err := s.ReserveQueryReader(ctx)
		require.NoError(t, err)
		require.Nil(t, lease)
		require.NoError(t, s.Close())
	}
	s := newTestStore(t)
	ro := openTestStore(t, s.schema, true)
	lease, err := ro.ReserveQueryReader(ctx)
	require.NoError(t, err)
	require.Nil(t, lease)
	lease, err = s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	lease.Close()
	lease.Close()
	_, err = s.GetBlock(lease.Context(ctx), 0)
	require.ErrorIs(t, err, port.ErrClosed)
	_, err = s.ReserveQueryReader(ctx)
	require.ErrorIs(t, err, port.ErrClosed)
}

func TestReservedQueryReaderBorrowsSpareAndCloseWaits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := newTestStore(t)
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	defer lease.Close()
	apiCtx := lease.Context(ctx)
	paths := make(map[string]int)
	probes := 0
	apiCtx = ObserveReads(apiCtx, func(o ReadObservation) {
		if o.Stage == "admission" {
			paths[o.Path]++
			probes += o.BorrowProbes
		}
	})
	// Warm a background connection without consuming the pinned slot.
	spare, err := s.pool.Acquire(ctx)
	require.NoError(t, err)
	spare.Release()
	pinned, err := s.q(apiCtx).Query(apiCtx, "SELECT pg_backend_pid()")
	require.NoError(t, err)
	defer pinned.Close()
	require.True(t, pinned.Next())
	var first uint32
	require.NoError(t, pinned.Scan(&first))
	borrowed, err := s.q(apiCtx).Query(apiCtx, "SELECT pg_backend_pid()")
	require.NoError(t, err)
	defer borrowed.Close()
	require.True(t, borrowed.Next())
	var second uint32
	require.NoError(t, borrowed.Scan(&second))
	require.NotEqual(t, first, second, "idle capacity keeps API requests parallel")
	require.Equal(t, map[string]int{"pinned": 1, "borrowed": 1}, paths)
	require.GreaterOrEqual(t, probes, 1)
	closing := make(chan struct{})
	go func() { lease.Close(); close(closing) }()
	select {
	case <-closing:
		t.Fatal("closed with active rows")
	case <-time.After(10 * time.Millisecond):
	}
	pinned.Close()
	select {
	case <-closing:
		t.Fatal("closed with borrowed rows")
	case <-time.After(10 * time.Millisecond):
	}
	borrowed.Close()
	select {
	case <-closing:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Zero(t, s.pool.Stat().AcquiredConns())
	_, err = s.GetBlock(apiCtx, 0)
	require.ErrorIs(t, err, port.ErrClosed)
}
