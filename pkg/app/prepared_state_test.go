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
			for _, mode := range []string{"ordered", "prepared", "queued", "coalesced"} {
				on := mode != "ordered"
				dir := filepath.Join(t.TempDir(), "db")
				dirs = append(dirs, dir)
				app := newPreparedApp(t, srv.URL(), dir, on, tc.from, func(cfg *config.Config) {
					cfg.Indexer.AcquisitionQueue = mode == "queued" || mode == "coalesced"
					cfg.RPC.CoalesceReads = mode == "coalesced"
				})
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
					if mode == "prepared" {
						require.LessOrEqual(t, stats.PeakInFlight, int64(4))
					}
					if mode == "queued" || mode == "coalesced" {
						require.NotNil(t, stats.AcquiredHeight)
						require.Equal(t, end, *stats.CommittedHeight)
					}
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
			diff = testchain.DiffKeyspace(dumpDir(t, dirs[0]), dumpDir(t, dirs[2]), 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
			diff = testchain.DiffKeyspace(dumpDir(t, dirs[0]), dumpDir(t, dirs[3]), 0)
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
	app := newPreparedApp(t, srv.URL(), filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) { cfg.Indexer.AcquisitionQueue = true; cfg.RPC.CoalesceReads = true })
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
	on := true
	cc.CoalesceReads = &on
	require.True(t, a.chainAppConfig(cc).RPC.CoalesceReads)
	require.False(t, cfg.RPC.CoalesceReads)
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

func TestAcquisitionQueueChainConfiguration(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Indexer.StatePrefetch = true
	cfg.Indexer.AcquisitionQueue = false
	cfg.RPC.RateLimit = 5
	on, blocks, bytes := true, 256, int64(256<<20)
	a := &App{config: cfg}
	cc := &multichain.ChainConfig{ID: "arc-testnet", RPCEndpoint: "http://localhost", AcquisitionQueue: &on, QueueBlocks: &blocks, QueueMaxBytes: &bytes}
	arc := a.chainAppConfig(cc)
	require.True(t, arc.Indexer.AcquisitionQueue)
	require.Equal(t, blocks, arc.Indexer.QueueBlocks)
	require.Equal(t, bytes, arc.Indexer.QueueMaxBytes)
	require.Equal(t, float64(5), float64(arc.RPC.RateLimit))
	require.False(t, cfg.Indexer.AcquisitionQueue)
	other := a.chainAppConfig(&multichain.ChainConfig{ID: "other"})
	require.False(t, other.Indexer.AcquisitionQueue)
	require.Zero(t, other.Indexer.QueueBlocks)
	cfg.Indexer.StatePrefetch = false
	cfg.Indexer.AcquisitionQueue = true
	require.ErrorContains(t, validateConfig(cfg), "requires state_prefetch")
}

func TestAcquisitionQueueReorgDiscardsPreparedFork(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	dir := filepath.Join(t.TempDir(), "db")
	app := newPreparedApp(t, srv.URL(), dir, true, 0, func(cfg *config.Config) {
		cfg.Indexer.AcquisitionQueue = true
		cfg.Indexer.PollInterval = time.Millisecond
		cfg.RPC.CoalesceReads = true
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	app.fetcher.SetBeforeCommitHook(func(n uint64) error {
		if n == head-2 {
			enterOnce.Do(func() {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
			})
		}
		return ctx.Err()
	})
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(ctx) }()
	joined := false
	defer func() {
		cancel()
		unblock()
		if !joined {
			<-done
		}
		app.Shutdown()
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("commit did not start")
	}
	require.Eventually(t, func() bool {
		height := app.fetcher.GetPipelineMetrics().AcquiredHeight
		return height != nil && *height == head
	}, 5*time.Second, time.Millisecond)
	reorgChain(sc, head-3, 5)
	newHead := sc.Chain.Head()
	unblock()
	require.Eventually(t, func() bool {
		n, err := app.storage.GetLatestHeight(ctx)
		if err != nil || n != newHead {
			return false
		}
		b, err := app.storage.GetBlock(ctx, newHead)
		return err == nil && b.Hash == sc.Chain.Block(newHead).Block.Hash()
	}, 10*time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	joined = true
	count, depth := app.fetcher.Reorgs()
	require.Equal(t, uint64(1), count)
	require.GreaterOrEqual(t, depth, uint64(1))
	require.LessOrEqual(t, depth, uint64(3), "all committed old-fork blocks are rolled back")
	app.Shutdown()
	fresh := filepath.Join(t.TempDir(), "fresh")
	reference := newPreparedApp(t, srv.URL(), fresh, true, 0)
	require.NoError(t, reference.fetcher.FetchRange(t.Context(), 0, newHead))
	reference.Shutdown()
	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

func TestAcquisitionQueueReorgBelowConfiguredStart(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	app := newPreparedApp(t, srv.URL(), filepath.Join(t.TempDir(), "db"), true, head, func(cfg *config.Config) {
		cfg.Indexer.AcquisitionQueue = true
		cfg.Indexer.PollInterval = time.Millisecond
	})
	defer app.Shutdown()
	require.NoError(t, app.fetcher.FetchRange(t.Context(), 0, head))
	reorgChain(sc, head-3, 5)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(ctx) }()
	defer func() { cancel(); <-done }()
	require.Eventually(t, func() bool {
		n, err := app.storage.GetLatestHeight(ctx)
		return err == nil && n == sc.Chain.Head()
	}, 8*time.Second, time.Millisecond)
	for n := head - 2; n <= sc.Chain.Head(); n++ {
		b, err := app.storage.GetBlock(t.Context(), n)
		require.NoError(t, err, "reorg recovery must not skip below start_height")
		require.Equal(t, sc.Chain.Block(n).Block.Hash(), b.Hash)
	}
}
