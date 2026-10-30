package execution

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"testing"
)

type pendingControllerStore struct {
	*controllerStoreProbe
	checked bool
	observe func()
}

func (s *pendingControllerStore) BeginExecutionReorgCheck(context.Context) (port.ExecutionReorgCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observe != nil {
		s.observe()
	}
	s.checked = true
	s.status.ReorgPending = true
	return port.ExecutionReorgCheck{Generation: s.status.Generation, From: s.status.Visible, Root: s.status.Root}, nil
}
func (s *pendingControllerStore) ExecutionReorgCheckStatus(context.Context) (port.ExecutionReorgCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return port.ExecutionReorgCheck{Generation: s.status.Generation, From: s.status.Visible, Root: s.status.Root}, nil
}
func (s *pendingControllerStore) BeginExecutionRewind(ctx context.Context, target execwire.Ref, root execwire.Hash) (port.ExecutionRewind, error) {
	m, err := s.controllerStoreProbe.BeginExecutionRewind(ctx, target, root)
	s.mu.Lock()
	s.status.ReorgPending = false
	s.mu.Unlock()
	return m, err
}
func TestControllerReorgCheckDurablyFencesAndJoinsBeforeTargetSearch(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	store := &pendingControllerStore{controllerStoreProbe: newControllerStore()}
	b := &controlledBackend{block: make(chan struct{}), entered: make(chan port.ExecutionWriter, 32)}
	sawActive := false
	store.observe = func() { sawActive = b.active.Load() > 0 }
	c, err := NewController(ctx, Limits{MaxBlocks: 1, MaxBytes: 100}, store, b, func(context.Context) error {
		require.Zero(t, b.active.Load())
		store.recover()
		close(b.block)
		return nil
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	old, err := c.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	select {
	case <-b.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, c.CheckReorg(ctx))
	require.True(t, store.checked, "durable fence must precede caller's potentially long RPC target search")
	require.True(t, sawActive, "fence is persisted before joining old queues")
	require.Zero(t, b.active.Load())
	require.Error(t, old.Wait(ctx))
	status, err := store.ExecutionStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), status.Generation)
	require.True(t, status.ReorgPending)
	_, err = c.Submit(ctx, schedulerWork(1))
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
	require.NoError(t, c.Rewind(ctx, schedulerWork(0).result.Ref, execwire.Hash{9}))
	next := schedulerWork(1)
	next.result.Ref.Generation = 2
	future, err := c.Submit(ctx, next)
	require.NoError(t, err)
	require.NoError(t, future.Wait(ctx))
}
func TestControllerReorgCheckRestartRecoversBeforeWorkers(t *testing.T) {
	store := &pendingControllerStore{controllerStoreProbe: newControllerStore()}
	store.status.ReorgPending = true
	b := &controlledBackend{}
	calls := 0
	c, err := NewController(schedulerCtx(t), Limits{MaxBlocks: 1, MaxBytes: 100}, store, b, func(context.Context) error {
		calls++
		require.Zero(t, b.active.Load())
		store.mu.Lock()
		store.status.ReorgPending = false
		store.mu.Unlock()
		return nil
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	require.Equal(t, 1, calls)
}
