package app

import (
	"bytes"
	"context"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Each load run owns its loop even when several runs share one parent test.
// Stop it before App.Shutdown closes its client, writer and storage.
func startLoadFetcher(t *testing.T, app *App, parent context.Context) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(ctx) }()
	return func() {
		t.Helper()
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(10 * time.Second):
			t.Fatal("load fetcher did not stop before closing its App")
		}
	}
}

func countLiveFetchers(t *testing.T) int {
	t.Helper()
	var trace bytes.Buffer
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(&trace, 2))
	return strings.Count(trace.String(), "github.com/0xmhha/indexer-go/pkg/fetch.(*Fetcher).Run(")
}

// A load round must leave no live loop behind while the parent test is still
// active. With the old shared-context startup, each round added one loop.
func TestLoadRoundsStopLiveFetchers(t *testing.T) {
	before := countLiveFetchers(t)
	for range 2 {
		runSLOLoad(t, 2, 0, 1, 2, 5*time.Millisecond, 64)
		require.NoError(t, t.Context().Err(), "the next round still needs its parent")
		require.Equal(t, before, countLiveFetchers(t), "a completed round must not retain its live fetcher")
	}
}
