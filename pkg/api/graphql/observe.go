package graphql

import (
	"context"
	"time"
)

// RequestObservation contains only a phase name and wall time, never a query,
// arguments or results. Execution includes resolver DB work: phases are nested
// with storage diagnostics and must not be added to those durations.
type RequestObservation struct {
	Stage   string
	Elapsed time.Duration
}

type requestObserverKey struct{}

// ObserveRequest opts a trusted caller into request-local diagnostics. HTTP
// headers do not enable this. The callback must be quick and concurrency-safe.
func ObserveRequest(ctx context.Context, observe func(RequestObservation)) context.Context {
	return context.WithValue(ctx, requestObserverKey{}, observe)
}

func requestPhase(ctx context.Context, stage string) func() {
	if observe, _ := ctx.Value(requestObserverKey{}).(func(RequestObservation)); observe != nil {
		start := time.Now()
		return func() { observe(RequestObservation{Stage: stage, Elapsed: time.Since(start)}) }
	}
	return nil // no clocks or execution extensions on unobserved requests
}

func finishRequestPhase(finish func()) {
	if finish != nil {
		finish()
	}
}
