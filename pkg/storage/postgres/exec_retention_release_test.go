package postgres

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExecutionRetentionReleaseRequiresExactCompletedReceipt(t *testing.T) {
	s, r := retentionFixture(t)
	p, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
	require.NoError(t, err)
	pd, _ := retention.Digest(p)
	receipt := retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}
	calls := 0
	apply := func(context.Context) error { calls++; return nil }
	require.Error(t, s.WithExecutionRetentionRelease(t.Context(), p, receipt, apply))
	require.Zero(t, calls)
	require.NoError(t, s.CompleteExecutionRetention(t.Context(), p, receipt, func(context.Context) error { return nil }))
	require.NoError(t, s.WithExecutionRetentionRelease(t.Context(), p, receipt, apply), "exact completed E3 must hold EX while releasing files")
	require.Equal(t, 1, calls)
	wrong := receipt
	wrong.Bytes++
	require.Error(t, s.WithExecutionRetentionRelease(t.Context(), p, wrong, apply))
	require.Equal(t, 1, calls)
}

func TestExecutionRetentionReleaseExcludesReorgAndJoinsCancellation(t *testing.T) {
	s, r := retentionFixture(t)
	p, err := s.BeginExecutionRetention(t.Context(), r, func(context.Context) error { return nil })
	require.NoError(t, err)
	pd, _ := retention.Digest(p)
	receipt := retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}
	require.NoError(t, s.CompleteExecutionRetention(t.Context(), p, receipt, func(context.Context) error { return nil }))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, joined := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.WithExecutionRetentionRelease(ctx, p, receipt, func(bound context.Context) error { close(entered); <-bound.Done(); close(joined); return bound.Err() })
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("release scope did not start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("release did not acquire EX")
	}
	blocked, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	_, err = s.BeginExecutionReorgCheck(blocked)
	stop()
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	select {
	case <-joined:
	default:
		t.Fatal("release callback was not joined")
	}
	_, err = s.BeginExecutionReorgCheck(t.Context())
	require.NoError(t, err, "canceled release must return its EX lease")
}
