package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func TestExecutionPartConcurrentReadersAndRewindFence(t *testing.T) {
	s, cfg := executionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, s.InitExecution(ctx, cfg))
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	require.NotNil(t, lease)
	defer lease.Close()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	var once sync.Once
	var wg sync.WaitGroup
	t.Cleanup(func() { once.Do(func() { close(release) }); wg.Wait() })
	for _, writer := range []port.ExecutionWriter{port.ExecutionAccounts, port.ExecutionStorage} {
		wg.Go(func() {
			p := firstExecutionPart(writer, cfg)
			results <- s.WriteExecutionPart(ctx, p, func(ctx context.Context) error {
				entered <- struct{}{}
				select {
				case <-release:
					return s.Put(ctx, []byte("part/"+writer), []byte("ready"))
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		})
	}
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("independent writers did not enter concurrently", ctx.Err())
		}
	}
	c, err := s.ExecutionStatus(lease.Context(ctx))
	require.NoError(t, err, "reserved API read proceeds while two parts hold connections")
	require.Equal(t, cfg.Anchor, c.Visible)
	require.LessOrEqual(t, s.pool.Stat().TotalConns(), int32(4))
	var exclusive bool
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock("+executionLockSQL+")").Scan(&exclusive))
	require.False(t, exclusive, "rewind cannot cross active part transactions")
	once.Do(func() { close(release) })
	for range 2 {
		require.NoError(t, <-results)
	}
	wg.Wait()

	tx, err := s.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")")
	require.NoError(t, err)
	p := firstExecutionPart(port.ExecutionAccounts, cfg)
	p.Parent = p.Ref.Hash
	p.Ref.Height++
	p.Ref.Hash[0]++
	err = s.WriteExecutionPart(ctx, p, func(context.Context) error { t.Error("part crossed rewind fence"); return nil })
	require.ErrorIs(t, err, port.ErrExecutionRewinding, "try-lock returns retryable error without waiting for rewind")
}
