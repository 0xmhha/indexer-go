package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func TestExecutionRewindReadAdmissionFence(t *testing.T) {
	s, cfg, p := publicationFixture(t, 7)
	_, err := s.CommitExecution(t.Context(), p, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	// Identify only this Store's pool processes; unrelated host databases and
	// projects must not influence the lock-wait assertion.
	var pids []int32
	var releaseAll []func()
	for range 4 {
		conn, err := s.pool.Acquire(t.Context())
		require.NoError(t, err)
		pids = append(pids, int32(conn.Conn().PgConn().PID()))
		releaseAll = append(releaseAll, conn.Release)
	}
	for _, release := range releaseAll {
		release()
	}
	lease, err := s.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	defer lease.Close()
	ctx, cancel := context.WithTimeout(lease.Context(t.Context()), 10*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	var workers sync.WaitGroup
	defer func() { cancel(); releaseOnce(); workers.Wait() }()
	readDone := make(chan error, 1)
	workers.Go(func() {
		readDone <- s.ReadSnapshot(ctx, func(view context.Context) error {
			if _, err := s.GetBlock(view, 1); err != nil {
				return err
			}
			close(entered)
			select {
			case <-release:
				return nil
			case <-view.Done():
				return view.Err()
			}
		})
	})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	rewindDone := make(chan error, 1)
	workers.Go(func() { _, err := s.BeginExecutionRewind(ctx, cfg.Anchor, cfg.AnchorRoot); rewindDone <- err })
	monitor, err := s.pool.Acquire(ctx)
	require.NoError(t, err)
	defer monitor.Release()
	require.Eventually(t, func() bool {
		var waiting bool
		err := monitor.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=ANY($1::integer[]) AND locktype='advisory' AND mode='ExclusiveLock' AND NOT granted)", pids).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, time.Millisecond)
	calls := 0
	part := firstExecutionPart(port.ExecutionAccounts, cfg)
	part.Ref.Height = 2
	part.Parent = p.Ref.Hash
	part.Ref.Hash = execwire.Hash{3}
	err = s.WriteExecutionPart(ctx, part, func(context.Context) error { calls++; return nil })
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
	require.Zero(t, calls)
	err = s.ReadSnapshot(ctx, func(context.Context) error { calls++; return nil })
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
	require.Zero(t, calls)
	releaseOnce()
	select {
	case err := <-readDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-rewindDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err = s.ResumeExecutionRewind(ctx, nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
}
