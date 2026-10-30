package app

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type maintenanceIngressProbe struct {
	*drainIngressProbe
	calls int
}

func (p *maintenanceIngressProbe) Maintain(context.Context, ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	p.calls++
	return ingress.MaintenanceResult{Phase: "complete", Resumed: true}, nil
}
func (p *maintenanceIngressProbe) RecoverMaintenance(ctx context.Context, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	return p.Maintain(ctx, o)
}
func TestExecutionAppMaintenanceDelegatesWithoutClosingReceiver(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	p := &maintenanceIngressProbe{drainIngressProbe: &drainIngressProbe{runtimeIngress: newRuntimeIngress()}}
	ready := make(chan struct{})
	close(ready)
	r := &executionRuntime{recover: func(context.Context) error { return nil }, relay: func() {}, ready: ready, ingress: p, serve: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	a := &App{executionFeed: r}
	api, ok := any(a).(interface {
		MaintainExecution(context.Context, ingress.MaintenanceOptions) (ingress.MaintenanceResult, error)
		RecoverExecutionMaintenance(context.Context, ingress.MaintenanceOptions) (ingress.MaintenanceResult, error)
	})
	require.True(t, ok, "App must expose explicit optional maintenance and recovery")
	_, err := api.MaintainExecution(ctx, ingress.MaintenanceOptions{})
	require.Error(t, err)
	require.Zero(t, p.calls)
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	defer r.Stop()
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	result, err := api.MaintainExecution(ctx, ingress.MaintenanceOptions{})
	require.NoError(t, err)
	require.True(t, result.Resumed)
	_, err = api.RecoverExecutionMaintenance(ctx, ingress.MaintenanceOptions{})
	require.NoError(t, err)
	require.Equal(t, 2, p.calls)
	require.False(t, p.isStopped())
	select {
	case err := <-done:
		t.Fatalf("maintenance ended receiver: %v", err)
	default:
	}
	r.Stop()
	require.Error(t, <-done)
}
