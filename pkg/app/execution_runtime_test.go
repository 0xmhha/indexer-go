package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func TestExecutionRuntimeRecoversBeforeRelayAndServeAndJoinsStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var phase atomic.Int32
	entered := make(chan struct{})
	finished := make(chan error, 1)
	r := &executionRuntime{recover: func(context.Context) error { phase.Store(1); return nil }, relay: func() { phase.CompareAndSwap(1, 2) }, serve: func(ctx context.Context) error {
		if phase.Load() != 2 {
			return errors.New("serve preceded recovery/relay")
		}
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}
	go func() { finished <- r.Run(ctx) }()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("runtime failed before serving: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	r.Stop()
	require.ErrorIs(t, <-finished, context.Canceled)
	require.ErrorIs(t, r.Run(ctx), port.ErrClosed)
}
func TestExecutionRuntimeRecoveryFailureAndStopBeforeRun(t *testing.T) {
	want := errors.New("recovery failed")
	var started atomic.Int32
	r := &executionRuntime{recover: func(context.Context) error { return want }, relay: func() { started.Add(1) }, serve: func(context.Context) error { started.Add(1); return nil }}
	require.ErrorIs(t, r.Run(t.Context()), want)
	require.Zero(t, started.Load())
	r.Stop()
	r = &executionRuntime{recover: func(context.Context) error { started.Add(1); return nil }, relay: func() { started.Add(1) }, serve: func(context.Context) error { started.Add(1); return nil }}
	r.Stop()
	require.ErrorIs(t, r.Run(t.Context()), port.ErrClosed)
	require.Zero(t, started.Load())
}
