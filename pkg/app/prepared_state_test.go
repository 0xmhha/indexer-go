package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newPreparedApp(t testing.TB, endpoint, dir string, on bool, from uint64, options ...func(*config.Config)) *App {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.API.Enabled = false
	cfg.Indexer.Workers = 2
	cfg.Indexer.StatePrefetch = on
	cfg.Indexer.StateWorkers = 4
	cfg.Indexer.StartHeight = from
	setTestDatabase(t, cfg, dir)
	enableTestChainFeatures(cfg)
	for _, option := range options {
		option(cfg)
	}
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	return app
}

// Compare the production opt-in path with the ordered path, including a
// recent start where accounts already have non-genesis balances. Both storage
// drivers run this test. Compare stored identities, all features and outbox.
func TestPreparedStateMatchesOrderedIndexing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func() *testchain.Chain
		from  uint64
	}{
		{"evm", func() *testchain.Chain { return testchain.BuildDefault().Chain }, 0},
		{"recent", func() *testchain.Chain { return testchain.BuildDefault().Chain }, 1},
		{"stablenet", func() *testchain.Chain { return testchain.BuildStableNet().Chain }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := tc.build()
			srv := testchain.NewServer(chain)
			defer srv.Close()
			end := chain.Head()
			var dirs []string
			for _, on := range []bool{false, true} {
				dir := filepath.Join(t.TempDir(), "db")
				dirs = append(dirs, dir)
				app := newPreparedApp(t, srv.URL(), dir, on, tc.from)
				require.NoError(t, app.fetcher.FetchRange(context.Background(), tc.from, end))
				for _, addr := range chain.Accounts() {
					balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), addr, end)
					if tracked {
						require.Equal(t, 0, balance.Cmp(chain.BalanceAt(addr, end)), "%s", addr)
					}
				}
				stats := app.fetcher.GetPipelineMetrics()
				require.Zero(t, stats.InFlight)
				require.Zero(t, stats.StateBytes)
				if on {
					require.Positive(t, stats.PeakStateBytes)
					require.LessOrEqual(t, stats.PeakInFlight, int64(4))
				}
				// Reprocessing must skip preparation even if the state is now pruned.
				if on {
					srv.PruneStateBefore(end + 1)
					require.NoError(t, app.fetcher.FetchBlock(context.Background(), end))
					srv.PruneStateBefore(0)
				}
				app.Shutdown()
			}
			diff := testchain.DiffKeyspace(dumpDir(t, dirs[0]), dumpDir(t, dirs[1]), 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
		})
	}
}

func TestPreparedStateFailureRollsBackAndRetries(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	app := newPreparedApp(t, srv.URL(), filepath.Join(t.TempDir(), "db"), true, 0)
	defer app.Shutdown()
	ctx := context.Background()
	require.NoError(t, app.fetcher.FetchBlock(ctx, 0))
	before, err := app.storage.(port.Outbox).ReadOutbox(ctx, 0, 0)
	require.NoError(t, err)
	srv.PruneStateBefore(1)
	require.ErrorContains(t, app.fetcher.FetchBlock(ctx, 1), "missing trie node")
	requireFailedStateBlockAbsent(t, app, sc.Chain, 1)
	latest, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Zero(t, latest)
	after, err := app.storage.(port.Outbox).ReadOutbox(ctx, 0, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
	srv.PruneStateBefore(0)
	require.NoError(t, app.fetcher.FetchBlock(ctx, 1))
	for _, addr := range sc.Chain.Accounts() {
		balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), addr, 1)
		if tracked {
			require.Equal(t, 0, balance.Cmp(sc.Chain.BalanceAt(addr, 1)))
		}
	}
}

// State acquired before entering the writer remains usable after the node
// prunes it. No latest substitution or second initial-balance RPC is needed.
func TestPreparedStateSurvivesPruningWhileQueued(t *testing.T) {
	sc := testchain.BuildDefault()
	sc.Chain.SetHead(1)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	app := newPreparedApp(t, srv.URL(), filepath.Join(t.TempDir(), "db"), true, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { cancel(); unblock(); app.Shutdown() }()
	entered := make(chan struct{})
	app.fetcher.SetBeforeCommitHook(func(n uint64) error {
		if n != 0 {
			return nil
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() { done <- app.fetcher.FetchRange(ctx, 0, 1) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("writer did not reach commit")
	}
	require.Eventually(t, func() bool { return app.fetcher.GetPipelineMetrics().Prepare.Count == 2 }, 5*time.Second, time.Millisecond)
	calls := srv.Calls()["eth_getBalance"]
	srv.PruneStateBefore(1)
	unblock()
	require.NoError(t, <-done)
	require.Equal(t, calls, srv.Calls()["eth_getBalance"], "writer consumes prepared balances after pruning")
	for _, addr := range sc.Chain.Accounts() {
		balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), addr, 1)
		if tracked {
			require.Equal(t, 0, balance.Cmp(sc.Chain.BalanceAt(addr, 1)))
		}
	}
}

func TestPreparedStateChainConfiguration(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Indexer.StatePrefetch = true
	cfg.Indexer.StateWorkers = 3
	cfg.Indexer.StateBatchSize = 16
	a := &App{config: cfg}
	cc := &multichain.ChainConfig{ID: "test", RPCEndpoint: "http://localhost"}
	require.True(t, a.chainAppConfig(cc).Indexer.StatePrefetch)
	require.Equal(t, 3, a.chainAppConfig(cc).Indexer.StateWorkers)
	require.Equal(t, 16, a.chainAppConfig(cc).Indexer.StateBatchSize)
	single := 1
	cc.StateBatchSize = &single
	require.Equal(t, 1, a.chainAppConfig(cc).Indexer.StateBatchSize)
	require.Equal(t, 16, cfg.Indexer.StateBatchSize, "batch overrides must not change other chains")
	off := false
	cc.StatePrefetch = &off
	require.False(t, a.chainAppConfig(cc).Indexer.StatePrefetch)
	require.True(t, cfg.Indexer.StatePrefetch, "a per-chain override must not change other chains")
}
