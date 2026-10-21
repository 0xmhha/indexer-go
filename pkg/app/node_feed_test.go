package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/multichain"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/nodefeed"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newFeedApp(t *testing.T, endpoint, socket, dir string) *App {
	return newPreparedApp(t, endpoint, dir, true, 0, func(cfg *config.Config) { cfg.Indexer.AcquisitionQueue = true; cfg.Source.NodeFeedSocket = socket })
}
func TestNodeFeedStorageParityAndDurableACK(t *testing.T) {
	sc := testchain.BuildDefault()
	server := testchain.NewServer(sc.Chain)
	defer server.Close()
	feed, err := testchain.NewNodeFeedServer(sc.Chain)
	require.NoError(t, err)
	defer feed.Close()
	var current atomic.Pointer[App]
	var premature atomic.Bool
	feed.SetHook(func(r nodefeed.Request, response *nodefeed.Response) {
		if r.Kind != "ack" {
			return
		}
		app := current.Load()
		if app == nil {
			premature.Store(true)
			return
		}
		block, err := app.storage.GetBlock(context.Background(), r.Height)
		latest, latestErr := app.storage.GetLatestHeight(context.Background())
		if err != nil || latestErr != nil || latest < r.Height || block.Hash != r.Hash {
			premature.Store(true)
			response.Error = &nodefeed.FeedError{Code: "premature", Message: "ACK preceded durable cursor"}
		}
	})
	var dirs []string
	for _, on := range []bool{false, true} {
		dir := filepath.Join(t.TempDir(), "db")
		dirs = append(dirs, dir)
		socket := ""
		if on {
			socket = feed.Socket()
		}
		app := newFeedApp(t, server.URL(), socket, dir)
		current.Store(app)
		before := server.Calls()["eth_getBalance"]
		require.NoError(t, app.fetcher.FetchRange(t.Context(), 0, sc.Chain.Head()))
		if on {
			require.Equal(t, before, server.Calls()["eth_getBalance"], "native preparation uses local hash-bound state")
		}
		for _, addr := range sc.Chain.Accounts() {
			balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), addr, sc.Chain.Head())
			if tracked {
				require.Zero(t, balance.Cmp(sc.Chain.BalanceAt(addr, sc.Chain.Head())))
			}
		}
		app.Shutdown()
	}
	require.False(t, premature.Load())
	acks := 0
	for _, r := range feed.Requests() {
		if r.Kind == "ack" {
			acks++
		}
	}
	require.Positive(t, acks)
	diff := testchain.DiffKeyspace(dumpDir(t, dirs[0]), dumpDir(t, dirs[1]), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

func TestNodeFeedStateFailureDoesNotACK(t *testing.T) {
	sc := testchain.BuildDefault()
	server := testchain.NewServer(sc.Chain)
	defer server.Close()
	feed, err := testchain.NewNodeFeedServer(sc.Chain)
	require.NoError(t, err)
	defer feed.Close()
	app := newFeedApp(t, server.URL(), feed.Socket(), filepath.Join(t.TempDir(), "db"))
	defer app.Shutdown()
	require.NoError(t, app.fetcher.FetchBlock(t.Context(), 0))
	before, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	feed.SetHook(func(r nodefeed.Request, response *nodefeed.Response) {
		if r.Kind == "balance" {
			response.Error = &nodefeed.FeedError{Code: "state_unavailable", Message: "required parent state pruned"}
		}
	})
	require.ErrorContains(t, app.fetcher.FetchBlock(t.Context(), 1), "parent state pruned")
	requireFailedStateBlockAbsent(t, app, sc.Chain, 1)
	after, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
	for _, r := range feed.Requests() {
		if r.Kind == "ack" {
			require.Zero(t, r.Height)
		}
	}
	feed.SetHook(nil)
	require.NoError(t, app.fetcher.FetchBlock(t.Context(), 1))
}

func TestNodeFeedQueuedForkNeedsFreshCanonical(t *testing.T) {
	sc := testchain.BuildDefault()
	server := testchain.NewServer(sc.Chain)
	defer server.Close()
	feed, err := testchain.NewNodeFeedServer(sc.Chain)
	require.NoError(t, err)
	defer feed.Close()
	app := newFeedApp(t, server.URL(), feed.Socket(), filepath.Join(t.TempDir(), "db"))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	release, entered := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { cancel(); unblock(); app.Shutdown() }()
	app.fetcher.SetBeforeCommitHook(func(n uint64) error {
		if n == 0 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- app.fetcher.FetchRange(ctx, 0, 3) }()
	<-entered
	require.Eventually(t, func() bool {
		m := app.fetcher.GetPipelineMetrics()
		return m.AcquiredHeight != nil && *m.AcquiredHeight >= 2
	}, 5*time.Second, time.Millisecond)
	for _, r := range feed.Requests() {
		require.NotEqual(t, "ack", r.Kind)
	}
	reorgChain(sc, 0, 3)
	unblock()
	require.Error(t, <-done)
	latest, err := app.storage.GetLatestHeight(t.Context())
	require.NoError(t, err)
	require.Zero(t, latest)
	_, err = app.storage.GetBlock(t.Context(), 1)
	require.ErrorIs(t, err, port.ErrNotFound)
	app.fetcher.SetBeforeCommitHook(nil)
	require.NoError(t, app.fetcher.FetchRange(t.Context(), 1, sc.Chain.Head()))
}

func TestNodeFeedACKFailureKeepsDurableCommit(t *testing.T) {
	sc := testchain.BuildDefault()
	server := testchain.NewServer(sc.Chain)
	defer server.Close()
	feed, err := testchain.NewNodeFeedServer(sc.Chain)
	require.NoError(t, err)
	defer feed.Close()
	app := newFeedApp(t, server.URL(), feed.Socket(), filepath.Join(t.TempDir(), "db"))
	defer app.Shutdown()
	feed.SetHook(func(r nodefeed.Request, response *nodefeed.Response) {
		if r.Kind == "ack" {
			response.Error = &nodefeed.FeedError{Code: "lost_ack", Message: "simulated disconnect"}
		}
	})
	require.ErrorContains(t, app.fetcher.FetchBlock(t.Context(), 0), "durable but feed ACK failed")
	_, err = app.storage.GetBlock(t.Context(), 0)
	require.NoError(t, err)
	before, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	feed.SetHook(nil)
	require.NoError(t, app.fetcher.FetchBlock(t.Context(), 0))
	after, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// Opt-in cross-language run, using the actual Rust IPC server and a frozen
// independent balance oracle. Normal Go tests require no Rust toolchain.
func TestNodeFeedRustProducerInterop(t *testing.T) {
	binary := os.Getenv("INDEXER_NODE_FEED_FIXTURE_BINARY")
	if binary == "" {
		t.Skip("Rust fixture binary not configured")
	}
	sc := testchain.BuildDefault()
	server := testchain.NewServer(sc.Chain)
	defer server.Close()
	fixture, err := testchain.NewNodeFeedServer(sc.Chain)
	require.NoError(t, err)
	defer fixture.Close()
	raw, err := fixture.ExportNodeFeedFixture()
	require.NoError(t, err)
	dir, err := os.MkdirTemp("/tmp", "infr-")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "fixture.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	socket := filepath.Join(dir, "feed.sock")
	cmd := exec.Command(binary, socket, path)
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	require.Eventually(t, func() bool { _, err := os.Stat(socket); return err == nil }, 5*time.Second, time.Millisecond)
	app := newFeedApp(t, server.URL(), socket, filepath.Join(t.TempDir(), "db"))
	defer app.Shutdown()
	require.NoError(t, app.fetcher.FetchRange(t.Context(), 0, sc.Chain.Head()))
	for _, addr := range sc.Chain.Accounts() {
		balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), addr, sc.Chain.Head())
		if tracked {
			require.Zero(t, balance.Cmp(sc.Chain.BalanceAt(addr, sc.Chain.Head())))
		}
	}
	_, err = app.storage.GetBlock(t.Context(), sc.Chain.Head())
	require.False(t, errors.Is(err, port.ErrNotFound))
	require.NoError(t, err)
}

func TestNodeFeedPerChainSettings(t *testing.T) {
	cfg := config.NewConfig()
	cfg.RPC.RateLimit = 5
	cfg.Source.NodeFeedSocket = "/tmp/global.sock"
	app := &App{config: cfg}
	child := app.chainAppConfig(&multichain.ChainConfig{ID: "arc", RPCEndpoint: "http://localhost", NodeFeedSocket: "/tmp/arc.sock"})
	require.Equal(t, "/tmp/arc.sock", child.Source.NodeFeedSocket)
	require.Equal(t, float64(5), child.RPC.RateLimit)
	other := app.chainAppConfig(&multichain.ChainConfig{ID: "other", RPCEndpoint: "http://localhost"})
	require.Empty(t, other.Source.NodeFeedSocket)
	require.Equal(t, "/tmp/global.sock", cfg.Source.NodeFeedSocket)
}

func TestNodeFeedChecksEachRequiredStateProvider(t *testing.T) {
	sc := testchain.BuildDefault()
	rpcServer := testchain.NewServer(sc.Chain)
	defer rpcServer.Close()
	rpcServer.PruneStateBefore(1)
	for _, metadata := range []bool{false, true} {
		feed, err := testchain.NewNodeFeedServer(sc.Chain)
		require.NoError(t, err)
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = rpcServer.URL()
		cfg.API.Enabled = false
		cfg.Indexer.StartHeight = 1
		cfg.Indexer.Workers = 2
		cfg.Indexer.StatePrefetch = true
		cfg.Indexer.AcquisitionQueue = true
		cfg.Source.NodeFeedSocket = feed.Socket()
		setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
		enableTestChainFeatures(cfg)
		cfg.Features["token.metadata"] = config.FeatureConfig{Enabled: &metadata}
		app, err := NewApp(cfg, zap.NewNop(), false, "")
		if metadata {
			require.ErrorContains(t, err, "token.metadata")
		} else {
			require.NoError(t, err)
			require.NoError(t, app.fetcher.FetchBlock(t.Context(), 1))
			app.Shutdown()
		}
		feed.Close()
	}
}
