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

// indexWithFeatures runs one session over [from, to] with the given feature
// overrides; a backfill, if needed, runs while the app starts.
func indexWithFeatures(t *testing.T, srv *testchain.Server, dir string, from, to uint64, features map[string]bool) {
	t.Helper()
	app, err := startAppFeatures(t, srv, dir, features)
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, from, to))
}

// TestBackfillMatchesFromScratch indexes the reference scenario with one
// feature off, then restarts with it on: the backfill must leave exactly the
// storage of a database that had the feature from the start.
func TestBackfillMatchesFromScratch(t *testing.T) {
	golden := dumpScenarioIndex(t)
	for name := range featureKeys {
		t.Run(name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			head := sc.Chain.Head()

			indexWithFeatures(t, srv, dir, 0, head, map[string]bool{name: false})
			indexWithFeatures(t, srv, dir, head, head, nil) // enables it: backfill 0..head

			diff := testchain.DiffKeyspace(golden, dumpDir(t, dir), 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
		})
	}
}

// TestBackfillAfterDisable turns a feature off in the middle of the chain and
// back on: only the blocks indexed while it was off are backfilled.
func TestBackfillAfterDisable(t *testing.T) {
	golden := dumpScenarioIndex(t)
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	head := sc.Chain.Head()
	mid := head / 2

	indexWithFeatures(t, srv, dir, 0, mid, nil)
	indexWithFeatures(t, srv, dir, mid+1, head, map[string]bool{"token.transfers": false, "balance.native": false})
	indexWithFeatures(t, srv, dir, head, head, nil)

	diff := testchain.DiffKeyspace(golden, dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

// TestBackfillResumesAfterCrash stops a backfill before one block commits;
// the next start resumes after the last committed block.
func TestBackfillResumesAfterCrash(t *testing.T) {
	golden := dumpScenarioIndex(t)
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	head := sc.Chain.Head()

	indexWithFeatures(t, srv, dir, 0, head, map[string]bool{"balance.native": false})

	errCrash := errors.New("injected crash")
	backfillCommitHook = func(h uint64) error {
		if h == head/2 {
			return errCrash
		}
		return nil
	}
	_, err := startAppFeatures(t, srv, dir, nil)
	backfillCommitHook = nil
	require.ErrorIs(t, err, errCrash)

	indexWithFeatures(t, srv, dir, head, head, nil)
	diff := testchain.DiffKeyspace(golden, dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}
