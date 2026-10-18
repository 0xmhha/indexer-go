package postgres

import (
	"context"
	"runtime/trace"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryObservation reports pool acquisition separately from SQL execution
// (through row consumption). It contains no SQL, arguments or row data.
type QueryObservation struct {
	Stage   string // "acquire" or "query"
	Elapsed time.Duration
}

type observerKey struct{}
type acquireStartKey struct{}
type queryStartKey struct{}
type acquireRegionKey struct{}
type queryRegionKey struct{}

// ObserveQueries opts a context into synchronous, request-local diagnostics.
// The callback must be quick and safe for concurrent calls. Other contexts
// record no clocks and invoke no callbacks. This never issues SQL or RPC.
func ObserveQueries(ctx context.Context, observe func(QueryObservation)) context.Context {
	return context.WithValue(ctx, observerKey{}, observe)
}

type queryObserver struct{}

func observationStart(ctx context.Context, key any) context.Context {
	if f, _ := ctx.Value(observerKey{}).(func(QueryObservation)); f != nil {
		return context.WithValue(ctx, key, time.Now())
	}
	return ctx
}
func observationEnd(ctx context.Context, key any, stage string) {
	if start, ok := ctx.Value(key).(time.Time); ok {
		if f, _ := ctx.Value(observerKey{}).(func(QueryObservation)); f != nil {
			f(QueryObservation{Stage: stage, Elapsed: time.Since(start)})
		}
	}
}
func (queryObserver) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	if trace.IsEnabled() {
		var task *trace.Task
		ctx, task = trace.NewTask(ctx, "postgres.acquire")
		ctx = context.WithValue(ctx, acquireRegionKey{}, task)
	}
	return observationStart(ctx, acquireStartKey{})
}
func (queryObserver) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	observationEnd(ctx, acquireStartKey{}, "acquire")
	if task, ok := ctx.Value(acquireRegionKey{}).(*trace.Task); ok {
		task.End()
	}
}
func (queryObserver) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if trace.IsEnabled() {
		var task *trace.Task
		ctx, task = trace.NewTask(ctx, "postgres.query")
		ctx = context.WithValue(ctx, queryRegionKey{}, task)
	}
	return observationStart(ctx, queryStartKey{})
}
func (queryObserver) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	observationEnd(ctx, queryStartKey{}, "query")
	if task, ok := ctx.Value(queryRegionKey{}).(*trace.Task); ok {
		task.End()
	}
}
