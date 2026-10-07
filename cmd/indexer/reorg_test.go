package main

import (
	"context"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/fetch"
)

type rollbacker interface {
	RollbackTo(ctx context.Context, to uint64) (*port.Reorg, error)
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
		_, err := app.storage.(rollbacker).RollbackTo(ctx, j)
		require.NoError(t, err)
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
	_, err := app.storage.(rollbacker).RollbackTo(ctx, 1)
	require.ErrorIs(t, err, port.ErrNoUndo)
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
	for _, mode := range allModes {
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
				return err == nil && b.Extra != nil
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
	_, err := app.storage.(rollbacker).RollbackTo(ctx, j)
	require.NoError(t, err)
	app.Shutdown()

	diff := testchain.DiffKeyspace(dumpDir(t, partial), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

// TestRestartAfterReorgRecovers stops the indexer, reorganizes the chain
// while it is down and starts it again: startup recovery must roll back to
// the fork point before ingest, and the result must equal a fresh index of
// the new chain.
func TestRestartAfterReorgRecovers(t *testing.T) {
	for _, mode := range allModes {
		t.Run(mode.name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			head := sc.Chain.Head()
			dir := filepath.Join(t.TempDir(), "db")
			runSessionMode(t, srv, dir, 0, head, mode)

			reorgChain(sc, head-3, 5)
			newHead := sc.Chain.Head()

			app := startAppMode(t, srv, dir, mode) // runs startup recovery
			ctx := context.Background()
			latest, err := app.storage.GetLatestHeight(ctx)
			require.NoError(t, err)
			require.Equal(t, head-3, latest, "rolled back to the fork point before ingest")
			require.NoError(t, app.fetcher.FetchRange(ctx, latest+1, newHead))
			app.Shutdown()

			fresh := filepath.Join(t.TempDir(), "fresh")
			runSessionMode(t, srv, fresh, 0, newHead, mode)
			diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
		})
	}
}

// TestReorgEventsAndOrphans runs the live loop through a reorganization that
// removes blocks with transactions and logs. Subscribers must get the reorg
// event first, then every log of the removed blocks marked removed (newest
// first), then the new branch. The removed blocks must stay queryable as
// orphans with their transactions and receipts, while the canonical index
// equals a fresh index of the new chain.
func TestReorgEventsAndOrphans(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	keep := head / 2
	dir := filepath.Join(t.TempDir(), "db")

	app := startApp(t, srv, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))

	// What the removed blocks held, newest first.
	var removed []*testchain.Block
	var removedLogs []*types.Log
	for n := head; n > keep; n-- {
		b := sc.Chain.Block(n)
		removed = append(removed, b)
		for i := len(b.Receipts) - 1; i >= 0; i-- {
			for j := len(b.Receipts[i].Logs) - 1; j >= 0; j-- {
				removedLogs = append(removedLogs, b.Receipts[i].Logs[j])
			}
		}
	}
	require.NotEmpty(t, removedLogs, "the removed range must hold logs")

	sub := app.eventBus.Subscribe("reorg-test", []events.EventType{events.EventTypeReorg, events.EventTypeLog, events.EventTypeBlock}, nil, 100_000)
	// The new branch must outgrow the indexed head: the live loop looks for
	// new blocks above it and finds the fork through their parents.
	reorgChain(sc, keep, int(head-keep)+3)
	newHead := sc.Chain.Head()

	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(loopCtx) }()
	var got []events.Event
	defer func() {
		if t.Failed() {
			h, _ := app.storage.GetLatestHeight(context.Background())
			c, d := app.fetcher.Reorgs()
			t.Logf("events: %s; indexed %d of %d; reorgs %d/%d", describeEvents(&got), h, newHead, c, d)
		}
	}()
	require.Eventually(t, func() bool {
		for {
			select {
			case ev := <-sub.Channel:
				got = append(got, ev)
			default:
				if len(got) == 0 {
					return false
				}
				be, isBlock := got[len(got)-1].(*events.BlockEvent)
				return isBlock && be.Number == newHead
			}
		}
	}, 10*time.Second, 20*time.Millisecond)
	stop()
	<-done

	re, ok := got[0].(*events.ReorgEvent)
	require.True(t, ok, "first event is the reorg, got %T", got[0])
	require.Equal(t, uint64(1), re.Seq)
	require.Equal(t, keep, re.ForkNumber)
	require.Equal(t, sc.Chain.Block(keep).Block.Hash(), re.ForkHash)
	require.Equal(t, head, re.OldHead)
	require.Len(t, re.Removed, len(removed))
	for i, b := range removed {
		require.Equal(t, b.Block.Hash(), re.Removed[i].Hash)
	}
	for i, want := range removedLogs {
		le, ok := got[1+i].(*events.LogEvent)
		require.True(t, ok, "event %d is a removed log, got %T", 1+i, got[1+i])
		require.True(t, le.Log.Removed)
		require.Equal(t, want.TxHash, le.Log.TxHash)
		require.Equal(t, want.Index, le.Log.Index)
		require.Equal(t, want.BlockHash, le.Log.BlockHash)
	}
	be, ok := got[1+len(removedLogs)].(*events.BlockEvent)
	require.True(t, ok, "the new branch follows")
	require.Equal(t, keep+1, be.Number)
	for _, ev := range got[1+len(removedLogs):] {
		if le, ok := ev.(*events.LogEvent); ok {
			require.False(t, le.Log.Removed)
		}
	}

	orphans := app.storage.(port.OrphanReader)
	reorgs, _, err := orphans.GetReorgs(ctx, port.FirstPage(10))
	require.NoError(t, err)
	require.Len(t, reorgs, 1)
	for _, b := range removed {
		ob, err := orphans.GetOrphanedBlock(ctx, b.Block.Hash())
		require.NoError(t, err)
		require.Len(t, ob.Block.Transactions, len(b.Block.Transactions()))
		require.Len(t, ob.Receipts, len(b.Receipts))
		for i, tx := range b.Block.Transactions() {
			require.Equal(t, tx.Hash(), ob.Block.Transactions[i].Hash)
			require.Len(t, ob.Receipts[i].Logs, len(b.Receipts[i].Logs))
			byTx, err := orphans.GetOrphanedTransaction(ctx, tx.Hash())
			require.NoError(t, err)
			require.Len(t, byTx, 1)
		}
	}
	checkOrphanQueries(t, app, keep, removed)
	app.Shutdown()

	fresh := filepath.Join(t.TempDir(), "fresh")
	runSession(t, srv, fresh, 0, newHead)
	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

func describeEvents(got *[]events.Event) string {
	var b strings.Builder
	for _, ev := range *got {
		switch e := ev.(type) {
		case *events.BlockEvent:
			fmt.Fprintf(&b, "block %d; ", e.Number)
		case *events.LogEvent:
			fmt.Fprintf(&b, "log %d/%d removed=%v; ", e.Log.BlockNumber, e.Log.Index, e.Log.Removed)
		default:
			fmt.Fprintf(&b, "%T; ", ev)
		}
	}
	return b.String()
}

// checkOrphanQueries reads the reorganization and the removed blocks back
// through GraphQL, as an application correcting its data would.
func checkOrphanQueries(t *testing.T, app *App, keep uint64, removed []*testchain.Block) {
	t.Helper()
	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	query := func(q string) map[string]any {
		res := h.ExecuteQuery(q, nil)
		require.Empty(t, res.Errors, "%s: %v", q, res.Errors)
		return res.Data.(map[string]any)
	}

	reorgs := query(`{ reorgs { id forkNumber depth removedBlocks { number hash } } }`)["reorgs"].([]any)
	require.Len(t, reorgs, 1)
	rg := reorgs[0].(map[string]any)
	require.Equal(t, "1", rg["id"])
	require.Equal(t, fmt.Sprint(keep), rg["forkNumber"])
	require.Equal(t, len(removed), rg["depth"])

	var withTx *testchain.Block
	for _, b := range removed {
		if len(b.Block.Transactions()) > 0 {
			withTx = b
		}
	}
	require.NotNil(t, withTx, "a removed block holds transactions")
	hash := withTx.Block.Hash().Hex()
	ob := query(fmt.Sprintf(`{ orphanedBlock(hash: "%s") { block { hash number transactions { hash } } receipts { transactionHash logs { logIndex removed } } reorg { id } } }`, hash))["orphanedBlock"].(map[string]any)
	require.Equal(t, hash, ob["block"].(map[string]any)["hash"])
	require.Len(t, ob["block"].(map[string]any)["transactions"], len(withTx.Block.Transactions()))
	require.Len(t, ob["receipts"], len(withTx.Receipts))
	require.Equal(t, "1", ob["reorg"].(map[string]any)["id"])

	atHeight := query(fmt.Sprintf(`{ orphanedBlocks(number: "%d") { block { hash } } }`, withTx.Block.NumberU64()))["orphanedBlocks"].([]any)
	require.Len(t, atHeight, 1)

	txHash := withTx.Block.Transactions()[0].Hash().Hex()
	otx := query(fmt.Sprintf(`{ orphanedTransaction(hash: "%s") { transaction { hash } receipt { status } block { number hash } reorgId reincludedIn { number } } }`, txHash))["orphanedTransaction"].([]any)
	require.Len(t, otx, 1)
	o := otx[0].(map[string]any)
	require.Equal(t, txHash, o["transaction"].(map[string]any)["hash"])
	require.Equal(t, hash, o["block"].(map[string]any)["hash"])
	require.Equal(t, "1", o["reorgId"])
	require.Nil(t, o["reincludedIn"], "the new branch does not hold the transaction")

	missing := query(`{ orphanedBlock(hash: "0x0000000000000000000000000000000000000000000000000000000000000001") { block { hash } } }`)
	require.Nil(t, missing["orphanedBlock"])
}
