package execution

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"testing"
	"time"
)

func TestReorgGateUsesExistingControllerForEarlyFenceBeforeProposalTransport(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	store := &pendingControllerStore{controllerStoreProbe: newControllerStore()}
	b := &controlledBackend{block: make(chan struct{}), entered: make(chan port.ExecutionWriter, 32)}
	c, err := NewController(ctx, Limits{MaxBlocks: 1, MaxBytes: 100}, store, b, func(context.Context) error { return nil })
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	gate := NewReorgGate(store)
	release := gate.Bind(c)
	defer release()
	future, err := c.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	select {
	case <-b.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, gate.Fence(ctx, execwire.Ref{Generation: 1, Height: 3, Hash: execwire.Hash{42}}))
	status, err := store.ExecutionStatus(ctx)
	require.NoError(t, err)
	require.True(t, status.ReorgPending, "known branch change must fence reads even when proposal validation/transport races")
	require.Equal(t, uint64(1), status.Generation)
	require.Zero(t, b.active.Load())
	require.Error(t, future.Wait(ctx))
	release()
	require.NoError(t, gate.Fence(ctx, execwire.Ref{Generation: 1, Height: 3, Hash: execwire.Hash{43}}), "existing durable fence survives session release")
}
func TestReorgGateUnboundWaitIsCancelledAndInvalidGenerationCannotFence(t *testing.T) {
	store := &pendingControllerStore{controllerStoreProbe: newControllerStore()}
	gate := NewReorgGate(store)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, gate.Fence(ctx, execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}), context.Canceled)
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.ErrorIs(t, gate.Fence(ctx, execwire.Ref{Generation: 2, Hash: execwire.Hash{1}}), port.ErrExecutionStale)
	require.False(t, store.checked)
}
