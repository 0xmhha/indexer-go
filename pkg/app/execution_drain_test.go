package app

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/stretchr/testify/require"
)

type drainIngressProbe struct {
	*runtimeIngress
	point   ingress.DrainPoint
	resumed bool
}

func (p *drainIngressProbe) Drain(context.Context) (ingress.DrainPoint, error) { return p.point, nil }
func (p *drainIngressProbe) Resume(context.Context) error                      { p.resumed = true; return nil }

func TestExecutionAppDrainLeavesReceiverAndOwnedRunAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	p := &drainIngressProbe{runtimeIngress: newRuntimeIngress()}
	ready := make(chan struct{})
	close(ready)
	r := &executionRuntime{recover: func(context.Context) error { return nil }, relay: func() {}, ready: ready, ingress: p, serve: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	a := &App{executionFeed: r}
	api, ok := any(a).(interface {
		DrainExecution(context.Context) (ingress.DrainPoint, error)
		ResumeExecution(context.Context) error
	})
	require.True(t, ok, "App must forward optional drain without closing its SQL/receiver owner")
	run := make(chan error, 1)
	go func() { run <- r.Run(ctx) }()
	defer r.Stop()
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	point, err := api.DrainExecution(ctx)
	require.NoError(t, err)
	require.Equal(t, p.point, point)
	require.NoError(t, api.ResumeExecution(ctx))
	require.True(t, p.resumed)
	require.False(t, p.isStopped())
	select {
	case err := <-run:
		t.Fatalf("drain stopped receiver: %v", err)
	default:
	}
	r.Stop()
	require.Error(t, <-run)
}
