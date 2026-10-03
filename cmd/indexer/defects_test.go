package main

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
)

// knownDefects lists data-integrity defects (docs/analysis/refactoring-plan.md
// section 4.2) that the tests below currently reproduce. While an id is listed
// its test asserts the defect is still observable, so a silent behaviour change
// is noticed. The PR that fixes a defect removes its id, and from then on the
// same test requires correct behaviour.
var knownDefects = map[string]string{
	"D3":  "reprocessing an indexed block is not idempotent (balance deltas, address index, tx count) (fix: R0-4)",
	"D10": "gap recovery rewinds the cursor and reprocesses already indexed blocks (fix: R0-4)",
}

// checkDefect asserts correct behaviour (ok == true) unless id is a known
// defect, in which case it asserts the defect is still reproduced.
func checkDefect(t *testing.T, id string, ok bool, detail ...string) {
	t.Helper()
	if why, known := knownDefects[id]; known {
		require.False(t, ok, "%s no longer reproduces (%s): remove it from knownDefects", id, why)
		t.Logf("%s reproduced: %s", id, why)
		for _, d := range detail {
			t.Logf("  %s", d)
		}
		return
	}
	require.True(t, ok, "%s regression: %v", id, detail)
}

// TestRestartPreservesIndex indexes the scenario in three sessions with clean
// shutdowns in between. The result must equal a single uninterrupted run.
func TestRestartPreservesIndex(t *testing.T) {
	want := dumpScenarioIndex(t)

	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")

	head := sc.Chain.Head()
	runSession(t, srv, dir, 0, 3)
	runSession(t, srv, dir, 4, 12)
	runSession(t, srv, dir, 13, head)

	diff := testchain.DiffKeyspace(want, dumpDir(t, dir), 0)
	checkDefect(t, "D1", len(diff) == 0, testchain.SummarizeDiff(diff)...)
}

// TestReprocessingIsIdempotent indexes everything, then processes blocks
// 15..head again in the same session (so D1's restart effect is excluded).
// This is what happens after a crash between a block's data writes and its
// cursor write: the block is fetched and processed once more. The stored
// result must not change.
func TestReprocessingIsIdempotent(t *testing.T) {
	want := dumpScenarioIndex(t)

	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	head := sc.Chain.Head()

	app := startApp(t, srv, dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	require.NoError(t, app.fetcher.FetchRange(ctx, 15, head))
	app.Shutdown()

	diff := testchain.DiffKeyspace(want, dumpDir(t, dir), 0)
	checkDefect(t, "D3", len(diff) == 0, testchain.SummarizeDiff(diff)...)
}

// TestGapRecoveryDoesNotReprocess leaves blocks 6..9 unindexed, then performs
// the startup sequence of RunWithGapRecovery (detect gaps, fill them, resume
// from the cursor). Each block must be fetched exactly once in total.
func TestGapRecoveryDoesNotReprocess(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	head := sc.Chain.Head()

	runSession(t, srv, dir, 0, 5)
	runSession(t, srv, dir, 10, head) // blocks 6..9 are now a gap

	app := startApp(t, srv, dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Same steps as Fetcher.RunWithGapRecovery before it enters the live
	// loop (which never returns, so it is not called here).
	latest, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	gaps, err := app.fetcher.DetectGaps(ctx, 0, latest)
	require.NoError(t, err)
	require.NotEmpty(t, gaps, "test setup must leave a gap")
	require.NoError(t, app.fetcher.FillGaps(ctx, gaps))
	if next := app.fetcher.GetNextHeight(ctx); next <= head {
		require.NoError(t, app.fetcher.FetchRange(ctx, next, head))
	}
	app.Shutdown()

	var repeated []string
	loads := srv.BlockLoads()
	heights := make([]uint64, 0, len(loads))
	for h := range loads {
		heights = append(heights, h)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	for _, h := range heights {
		if loads[h] > 1 {
			repeated = append(repeated, blockLoadLine(h, loads[h]))
		}
	}
	for h := uint64(0); h <= head; h++ {
		require.NotZero(t, loads[h], "block %d was never fetched", h)
	}
	checkDefect(t, "D10", len(repeated) == 0, repeated...)
}

func blockLoadLine(h uint64, n int) string {
	return "block " + itoa(h) + " fetched " + itoa(uint64(n)) + " times"
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
