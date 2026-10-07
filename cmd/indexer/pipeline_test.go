package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
)

// TestIngestPathsStoreTheSameData indexes one chain through the live loop,
// gap recovery and block-by-block fetches: every path goes through the same
// pipeline, so the databases are identical (refactoring plan R2-2, D6).
func TestIngestPathsStoreTheSameData(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	head := sc.Chain.Head()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	index := func(name string, run func(app *App)) string {
		dir := filepath.Join(t.TempDir(), name)
		app := startApp(t, srv, dir)
		run(app)
		app.Shutdown()
		return dir
	}
	live := index("live", func(app *App) {
		loopCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- app.fetcher.Run(loopCtx) }()
		require.Eventually(t, func() bool {
			h, err := app.storage.GetLatestHeight(ctx)
			return err == nil && h == head
		}, time.Minute, 10*time.Millisecond)
		stop()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("live loop: %v", err)
		}
	})
	gaps := index("gaps", func(app *App) {
		require.NoError(t, app.fetcher.FetchRange(ctx, 0, 2))
		require.NoError(t, app.fetcher.FetchRangeConcurrent(ctx, 3, head))
	})
	single := index("single", func(app *App) {
		for n := uint64(0); n <= head; n++ {
			require.NoError(t, app.fetcher.FetchBlock(ctx, n))
		}
	})

	want := dumpDir(t, live)
	for name, dir := range map[string]string{"gap recovery": gaps, "block by block": single} {
		diff := testchain.DiffKeyspace(want, dumpDir(t, dir), 0)
		require.Empty(t, diff, "%s: %s", name, testchain.SummarizeDiff(diff))
	}
}
