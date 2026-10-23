package execution

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type controllerStoreProbe struct {
	port.ExecutionStore
	port.ExecutionRewindStore
	mu     sync.Mutex
	status port.ExecutionControl
	marker port.ExecutionRewind
	begun  func()
}

func newControllerStore() *controllerStoreProbe {
	return &controllerStoreProbe{status: port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Anchor: schedulerWork(0).result.Ref}, Generation: 1, Visible: schedulerWork(0).result.Ref, BaselineReady: true}}
}
func (s *controllerStoreProbe) ExecutionStatus(context.Context) (port.ExecutionControl, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, nil
}
func (s *controllerStoreProbe) BeginExecutionRewind(_ context.Context, target execwire.Ref, root execwire.Hash) (port.ExecutionRewind, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.begun != nil {
		s.begun()
	}
	s.marker = port.ExecutionRewind{From: s.status.Visible, Target: target, Root: root}
	s.marker.Target.Generation = s.status.Generation + 1
	s.status.Generation++
	s.status.Rewinding = true
	s.status.Visible.Generation = s.status.Generation
	return s.marker, nil
}
func (s *controllerStoreProbe) recover() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Visible = s.marker.Target
	s.status.Rewinding = false
}
func TestControllerDurableFenceThenJoinOldQueuesBeforeRecover(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	store := newControllerStore()
	b := &controlledBackend{block: make(chan struct{}), entered: make(chan port.ExecutionWriter, 32)}
	sawActive := false
	store.begun = func() { sawActive = b.active.Load() > 0 }
	recovered := 0
	c, err := NewController(ctx, Limits{MaxBlocks: 1, MaxBytes: 100}, store, b, func(context.Context) error {
		recovered++
		require.Zero(t, b.active.Load())
		close(b.block)
		store.recover()
		return nil
	})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	old, err := c.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	select {
	case <-b.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, c.Rewind(ctx, schedulerWork(0).result.Ref, execwire.Hash{9}))
	require.True(t, sawActive)
	require.Equal(t, 1, recovered)
	require.Error(t, old.Wait(ctx))
	next := schedulerWork(1)
	next.result.Ref.Generation = 2
	f, err := c.Submit(ctx, next)
	require.NoError(t, err)
	require.NoError(t, f.Wait(ctx))
	require.NoError(t, c.Close())
	require.Zero(t, b.active.Load())
}
func TestControllerFailedRecoveryRemainsBlockedAndCloseJoinsRecovery(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	store := newControllerStore()
	entered := make(chan struct{})
	b := &controlledBackend{}
	c, err := NewController(ctx, Limits{MaxBlocks: 1, MaxBytes: 100}, store, b, func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() })
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- c.Rewind(ctx, schedulerWork(0).result.Ref, execwire.Hash{9}) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err = c.Submit(ctx, schedulerWork(1))
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
	require.NoError(t, c.Close())
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err = c.Submit(ctx, schedulerWork(1))
	require.ErrorIs(t, err, port.ErrClosed)
}
func TestControllerRecoversMarkerBeforeCreatingAnyWorker(t *testing.T) {
	ctx := schedulerCtx(t)
	store := newControllerStore()
	store.status.Rewinding = true
	store.status.Generation = 2
	store.marker.Target = schedulerWork(0).result.Ref
	store.marker.Target.Generation = 2
	b := &controlledBackend{}
	calls := 0
	c, err := NewController(ctx, Limits{MaxBlocks: 1, MaxBytes: 100}, store, b, func(context.Context) error { calls++; require.Zero(t, b.active.Load()); store.recover(); return nil })
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, 1, calls)
	failure := errors.New("unrecoverable marker")
	store.status.Rewinding = true
	_, err = NewController(ctx, Limits{MaxBlocks: 1, MaxBytes: 100}, store, b, func(context.Context) error { return failure })
	require.ErrorIs(t, err, failure)
}
