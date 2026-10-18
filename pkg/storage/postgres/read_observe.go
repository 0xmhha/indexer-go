package postgres

import (
	"context"
	"time"
)

// ReadObservation describes API admission or block decoding without SQL or data.
// BorrowProbes counts pool admission attempts, never SQL retries. Admission
// includes connection selection/reconnection; it is not pure queue wait.
type ReadObservation struct {
	Stage        string
	Elapsed      time.Duration
	Path         string // pinned, borrowed, or unavailable (admission only)
	BorrowProbes int
	Failed       bool
}

type readObserverKey struct{}

// ObserveReads opts a context into additional read diagnostics. It changes no
// connection, transaction or retry policy. Callbacks must be concurrency-safe.
func ObserveReads(ctx context.Context, observe func(ReadObservation)) context.Context {
	return context.WithValue(ctx, readObserverKey{}, observe)
}

func readObserver(ctx context.Context) func(ReadObservation) {
	f, _ := ctx.Value(readObserverKey{}).(func(ReadObservation))
	return f
}

func observeDecode(ctx context.Context) func() {
	if observe := readObserver(ctx); observe != nil {
		start := time.Now()
		return func() { observe(ReadObservation{Stage: "block_decode", Elapsed: time.Since(start)}) }
	}
	return nil
}
