package main

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

type rollbacker interface {
	RollbackTo(ctx context.Context, to uint64) error
}

// TestRollbackRestoresEarlierState indexes the reference scenario, rolls the
// database back to height j and requires exactly the storage of a database
// that indexed only blocks 0..j. Indexing the rest again must then give the
// golden: in-memory counters were reset with the data.
func TestRollbackRestoresEarlierState(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	golden := dumpScenarioIndex(t)

	for _, j := range []uint64{0, 3, head / 2, head - 1} {
		partial := filepath.Join(t.TempDir(), "partial")
		runSession(t, srv, partial, 0, j)

		dir := filepath.Join(t.TempDir(), "db")
		app := startApp(t, srv, dir)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
		require.NoError(t, app.storage.(rollbacker).RollbackTo(ctx, j))
		latest, err := app.storage.GetLatestHeight(ctx)
		require.NoError(t, err)
		require.Equal(t, j, latest)
		cancel()
		app.Shutdown()

		diff := testchain.DiffKeyspace(dumpDir(t, partial), dumpDir(t, dir), 0)
		require.Empty(t, diff, "rollback to %d: %v", j, testchain.SummarizeDiff(diff))

		runSession(t, srv, dir, j+1, head)
		diff = testchain.DiffKeyspace(golden, dumpDir(t, dir), 0)
		require.Empty(t, diff, "re-index after rollback to %d: %v", j, testchain.SummarizeDiff(diff))
	}
}

// TestRollbackWithoutUndoFails refuses to roll back below the undo window.
func TestRollbackWithoutUndoFails(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	app := startApp(t, srv, dir)
	defer app.Shutdown()
	ctx := context.Background()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, 5))
	require.NoError(t, app.storage.(interface {
		DropUndo(context.Context, uint64) error
	}).DropUndo(ctx, 3))
	err := app.storage.(rollbacker).RollbackTo(ctx, 1)
	require.ErrorIs(t, err, storage.ErrNoUndo)
	latest, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(5), latest, "nothing was rolled back")
}

// reorgChain replaces the blocks above keep of the scenario's chain with n
// new blocks that move different amounts between other accounts, so hashes,
// transactions and balances all change.
func reorgChain(sc *testchain.Scenario, keep uint64, n int) {
	sc.Chain.Reorg(keep)
	a, d := sc.Accounts[0], sc.Accounts[3]
	gp := big.NewInt(1_000_000_000)
	for i := 0; i < n; i++ {
		sc.Chain.AddBlockWithExtra([]byte("fork"),
			testchain.TxSpec{From: d, Tx: &types.LegacyTx{To: &a.Address, Value: big.NewInt(int64(1000 + i)), Gas: 21000, GasPrice: gp}})
	}
}

// TestLiveLoopRollsBackReorg indexes the reference chain with the live loop,
// replaces its last blocks with a competing branch and keeps the loop
// running: it must detect the reorganization, roll back to the fork point and
// index the new branch, ending with exactly the storage of a database that
// indexed the new chain from scratch.
func TestLiveLoopRollsBackReorg(t *testing.T) {
	for _, mode := range []ingestMode{atomicMode, clientMode} {
		t.Run(mode.name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			head := sc.Chain.Head()
			dir := filepath.Join(t.TempDir(), "db")

			app := startAppMode(t, srv, dir, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))

			reorgChain(sc, head-3, 5) // drop 3 blocks, add 5
			newHead := sc.Chain.Head()

			loopCtx, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- app.fetcher.Run(loopCtx) }()
			require.Eventually(t, func() bool {
				h, err := app.storage.GetLatestHeight(ctx)
				if err != nil || h != newHead {
					return false
				}
				b, err := app.storage.GetBlock(ctx, newHead)
				return err == nil && b.Extra() != nil
			}, time.Minute, 50*time.Millisecond)
			stop()
			<-done
			count, depth := app.fetcher.Reorgs()
			app.Shutdown()
			require.Equal(t, uint64(1), count, "one reorganization")
			require.Equal(t, uint64(3), depth, "three blocks rolled back")

			fresh := filepath.Join(t.TempDir(), "fresh")
			runSessionMode(t, srv, fresh, 0, newHead, mode)
			diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
		})
	}
}

// TestReorgBeyondUndoStops makes the fork point lose its undo record: the
// live loop must stop with ErrReorgTooDeep instead of indexing on top.
func TestReorgBeyondUndoStops(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	require.NoError(t, app.storage.(interface {
		DropUndo(context.Context, uint64) error
	}).DropUndo(ctx, head-1))

	reorgChain(sc, head-3, 5)
	err := app.fetcher.Run(ctx)
	require.ErrorIs(t, err, fetch.ErrReorgTooDeep)
	latest, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, head, latest, "nothing was rolled back or indexed on top")
}

// TestRollbackLargeBlocks covers blocks large enough that their undo records
// are read on several cores.
func TestRollbackLargeBlocks(t *testing.T) {
	sc := testchain.BuildLoad(6, 300, 0)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	j := head - 3

	partial := filepath.Join(t.TempDir(), "partial")
	runSession(t, srv, partial, 0, j)

	dir := filepath.Join(t.TempDir(), "db")
	app := startApp(t, srv, dir)
	ctx := context.Background()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	require.NoError(t, app.storage.(rollbacker).RollbackTo(ctx, j))
	app.Shutdown()

	diff := testchain.DiffKeyspace(dumpDir(t, partial), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}
