package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/fetch"
)

// knownDefects lists, per ingest path, data-integrity defects
// (docs/analysis/refactoring-plan.md section 4.2) that the tests below
// currently reproduce. While an id is listed its test asserts the defect is
// still observable, so a silent behaviour change is noticed. The change that
// fixes a defect removes its id, and from then on the same test requires
// correct behaviour. The legacy path keeps its defects until it is deleted.
var knownDefects = map[string]map[string]string{
	legacyMode.name: {
		"D3":  "reprocessing an indexed block is not idempotent (balance deltas, address index, tx count)",
		"D10": "gap recovery rewinds the cursor and reprocesses already indexed blocks",
	},
	clientMode.name: {},
	atomicMode.name: {},
}

// checkDefect asserts correct behaviour (ok == true) unless id is a known
// defect of mode, in which case it asserts the defect is still reproduced.
func checkDefect(t *testing.T, mode ingestMode, id string, ok bool, detail ...string) {
	t.Helper()
	if why, known := knownDefects[mode.name][id]; known {
		require.False(t, ok, "%s/%s no longer reproduces (%s): remove it from knownDefects", mode.name, id, why)
		t.Logf("%s reproduced on %s path: %s", id, mode.name, why)
		for _, d := range detail {
			t.Logf("  %s", d)
		}
		return
	}
	require.True(t, ok, "%s/%s regression: %v", mode.name, id, detail)
}

// TestRestartPreservesIndex indexes the scenario in three sessions with clean
// shutdowns in between. The result must equal a single uninterrupted run.
func TestRestartPreservesIndex(t *testing.T) {
	want := dumpScenarioIndex(t)
	for _, mode := range allModes {
		t.Run(mode.name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")

			head := sc.Chain.Head()
			runSessionMode(t, srv, dir, 0, 3, mode)
			runSessionMode(t, srv, dir, 4, 12, mode)
			runSessionMode(t, srv, dir, 13, head, mode)

			diff := testchain.DiffKeyspace(want, dumpDir(t, dir), 0)
			checkDefect(t, mode, "D1", len(diff) == 0, testchain.SummarizeDiff(diff)...)
		})
	}
}

// TestReprocessingIsIdempotent indexes everything, then processes blocks
// 15..head again in the same session (so restart effects are excluded).
// Reprocessing happens after a crash between a block's data writes and its
// cursor write, and whenever blocks are fetched again for another reason. The
// stored result must not change.
func TestReprocessingIsIdempotent(t *testing.T) {
	want := dumpScenarioIndex(t)
	for _, mode := range allModes {
		t.Run(mode.name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			head := sc.Chain.Head()

			app := startAppMode(t, srv, dir, mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
			require.NoError(t, app.fetcher.FetchRange(ctx, 15, head))
			app.Shutdown()

			diff := testchain.DiffKeyspace(want, dumpDir(t, dir), 0)
			checkDefect(t, mode, "D3", len(diff) == 0, testchain.SummarizeDiff(diff)...)
		})
	}
}

// TestGapRecoveryDoesNotReprocess leaves blocks 6..9 unindexed, then performs
// the startup sequence of RunWithGapRecovery (detect gaps, fill them, resume
// from the cursor). Recovery must fetch only the missing blocks. Gaps are
// filled only when every enabled feature is order-independent (D12, see
// TestGapBelowIndexedBlocksStops), so the order-dependent ones are off.
func TestGapRecoveryDoesNotReprocess(t *testing.T) {
	for _, base := range allModes {
		mode := orderIndependentOnly(base)
		t.Run(base.name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			head := sc.Chain.Head()

			runSessionMode(t, srv, dir, 0, 5, mode)
			runSessionMode(t, srv, dir, 10, head, mode) // blocks 6..9 are now a gap

			app := startAppMode(t, srv, dir, mode)
			// Count gap recovery only, not the startup reorg check.
			srv.ResetBlockLoads()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			// Same steps as Fetcher.RunWithGapRecovery before it enters the
			// live loop (which never returns, so it is not called here).
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

			var extra []string
			loads := srv.BlockLoads()
			heights := make([]uint64, 0, len(loads))
			for h := range loads {
				heights = append(heights, h)
			}
			sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
			for _, h := range heights {
				if h < 6 || h > 9 || loads[h] > 1 {
					extra = append(extra, fmt.Sprintf("block %d fetched %d times during recovery", h, loads[h]))
				}
			}
			checkDefect(t, base, "D10", len(extra) == 0, extra...)

			// The result equals indexing every block in one run.
			if mode.atomic {
				want := dumpDir(t, indexScenarioMode(t, testchain.BuildDefault(), mode))
				diff := testchain.DiffKeyspace(want, dumpDir(t, dir), 0)
				require.Empty(t, diff, testchain.SummarizeDiff(diff))
			}
		})
	}
}

// TestGapBelowIndexedBlocksStops: blocks missing below indexed blocks cannot
// be indexed after them with order-dependent features enabled (balances,
// address sequences and latest-state records such as module installs would
// be computed out of order, defect D12). Gap recovery stops with
// ErrGapBelowIndexed and leaves the database as it was; the database must be
// reindexed.
func TestGapBelowIndexedBlocksStops(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	runSession(t, srv, dir, 0, 5)
	runSession(t, srv, dir, 10, sc.Chain.Head()) // blocks 6..9 are now a gap
	before := dumpDir(t, dir)

	app := startApp(t, srv, dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	latest, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	gaps, err := app.fetcher.DetectGaps(ctx, 0, latest)
	require.NoError(t, err)
	require.Equal(t, []fetch.GapRange{{Start: 6, End: 9}}, gaps)
	err = app.fetcher.FillGaps(ctx, gaps)
	require.ErrorIs(t, err, fetch.ErrGapBelowIndexed)
	require.ErrorContains(t, err, "reindex")
	app.Shutdown()

	diff := testchain.DiffKeyspace(before, dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

// TestCrashBeforeCommitRecovers aborts indexing right before a block's
// transaction commits, which on the atomic path is equivalent to a process
// crash at any point inside the block, restarts, and requires the final
// storage to equal a single uninterrupted run.
func TestCrashBeforeCommitRecovers(t *testing.T) {
	want := dumpScenarioIndex(t)
	for _, crashAt := range []uint64{0, 2, 6, 8, 13, 20} {
		t.Run(fmt.Sprintf("crash_at_%d", crashAt), func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			head := sc.Chain.Head()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			errCrash := errors.New("injected crash")
			app := startAppMode(t, srv, dir, atomicMode)
			app.fetcher.SetBeforeCommitHook(func(h uint64) error {
				if h == crashAt {
					return errCrash
				}
				return nil
			})
			require.ErrorIs(t, app.fetcher.FetchRange(ctx, 0, head), errCrash)
			app.Shutdown()

			app = startAppMode(t, srv, dir, atomicMode)
			next := app.fetcher.GetNextHeight(ctx)
			if crashAt > 0 {
				require.Equal(t, crashAt, next, "cursor must point at the crashed block")
			}
			require.NoError(t, app.fetcher.FetchRange(ctx, next, head))
			app.Shutdown()

			diff := testchain.DiffKeyspace(want, dumpDir(t, dir), 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
		})
	}
}
