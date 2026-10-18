package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func feedAppConfig(t *testing.T, chains ...*testchain.Chain) (*config.Config, *testchain.Chain, *testchain.Server) {
	t.Helper()
	c := testchain.NewChain(5042002, nil)
	if len(chains) > 0 {
		c = chains[0]
	} else {
		addFeedBlocks(c, 2)
	}
	peer, e := testchain.NewEthPeerServer(c)
	require.NoError(t, e)
	t.Cleanup(peer.Close)
	rpc := testchain.NewServer(c)
	t.Cleanup(rpc.Close)
	cfg := config.NewConfig()
	cfg.API.Enabled = false
	cfg.RPC.Endpoint = rpc.URL()
	cfg.RPC.RateLimit = 5
	cfg.RPC.CoalesceReads = true
	cfg.Indexer.StartHeight = 1
	cfg.Indexer.ChunkSize = 1
	cfg.Indexer.Workers = 1
	cfg.Indexer.PollInterval = 50 * time.Millisecond
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	if cfg.Database.Driver == config.DriverPostgres {
		cfg.Database.Postgres.MaxConns = 1
	}
	off := false
	cfg.Features = map[string]config.FeatureConfig{"balance.native": {Enabled: &off}, "token.metadata": {Enabled: &off}}
	key := filepath.Join(t.TempDir(), "peer.key")
	require.NoError(t, crypto.SaveECDSA(key, testchain.NewAccount(55).Key))
	require.NoError(t, os.Chmod(key, 0600))
	root := t.TempDir()
	cfg.WindowStorage = window.Layout{Volumes: map[string]window.VolumeSpec{"external": {Root: root, ID: "test-app", AllowDirectory: true, MaxBytes: 8 << 20}}, Windows: map[string]window.WindowConfig{"arc": {Volume: "external", Path: filepath.Join(root, "raw"), MaxBytes: 6 << 20, SegmentBytes: 64 << 10, MaxPayloadBytes: 32 << 10, MaxSegmentRecords: 8}}}
	cfg.Source.Feed = &feed.Config{Module: "arc.eth68", Window: "arc", InitializeVolume: true, CreateWindow: true, Consumer: "test-projection", ChainID: "5042002", GenesisHash: c.Block(0).Block.Hash().Hex(), AnchorHash: c.Block(0).Block.Hash().Hex(), NetworkID: 5042002, ForkHash: fmt.Sprintf("%x", peer.Fork), KeyFile: key, Peers: []string{peer.Node().URLv4()}, ControlInterval: time.Second}
	return cfg, c, rpc
}
func addFeedBlocks(c *testchain.Chain, n int) {
	for i := 0; i < n; i++ {
		c.AddBlock()
	}
	c.SetHead(uint64(c.Head()))
}
func waitFeedProjection(t *testing.T, a *App, c *testchain.Chain, n uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		b, e := a.storage.GetBlock(context.Background(), n)
		return e == nil && b.Hash == c.Block(n).Block.Hash()
	}, 20*time.Second, 20*time.Millisecond)
}
func runFeedApp(t *testing.T, a *App) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() { cancel(); a.Shutdown() })
	return cancel, done
}
func TestManagedFeedAppCaptureProjectionRestart(t *testing.T) {
	cfg, c, rpc := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	waitFeedProjection(t, a, c, 2)
	state, e := a.managedFeed.store.State(t.Context())
	require.NoError(t, e)
	id, e := a.managedFeed.store.CanonicalAt(t.Context(), state.Generation, 2)
	require.NoError(t, e)
	require.Eventually(t, func() bool {
		receipt, e := a.managedFeed.store.ProjectionAck(t.Context(), cfg.Source.Feed.Consumer, state.Generation, id)
		return e == nil && receipt.Identity == id
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	a.Shutdown()
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	addFeedBlocks(c, 1)
	waitFeedProjection(t, a, c, 3)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	a.Shutdown()
	calls := rpc.Calls()
	require.Zero(t, calls["eth_getBlockReceipts"])
	require.Zero(t, calls["eth_getBalance"])
	require.Zero(t, calls["eth_getCode"])
	require.Zero(t, calls["eth_call"])
}
func TestManagedFeedAppReorgToMissingBootstrap(t *testing.T) {
	cfg, c, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	waitFeedProjection(t, a, c, 2)
	c.Reorg(0)
	c.AddBlockWithExtra([]byte("replacement"))
	c.AddBlock()
	c.SetHead(2)
	waitFeedProjection(t, a, c, 2)
	state, e := a.managedFeed.store.State(t.Context())
	require.NoError(t, e)
	require.EqualValues(t, 2, state.Generation)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
func TestManagedFeedAppShutdownJoinsBeforeDBClose(t *testing.T) {
	cfg, c, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	_, done := runFeedApp(t, a)
	waitFeedProjection(t, a, c, 1)
	a.Shutdown()
	require.ErrorIs(t, <-done, context.Canceled)
	rawopts, e := cfg.WindowStorage.OptionsFor("arc", model.FeedChain{ID: "5042002", GenesisHash: c.Block(0).Block.Hash()})
	require.NoError(t, e)
	raw, e := window.Open(rawopts)
	require.NoError(t, e)
	require.NoError(t, raw.Close())
}
func TestManagedFeedPreflightBeforeIO(t *testing.T) {
	cfg, _, rpc := feedAppConfig(t)
	cases := map[string]func(*config.Config){
		"worker": func(c *config.Config) { c.Source.Feed.Projections = []string{"unknown"} },
		"duplicate-workers": func(c *config.Config) {
			c.Source.Feed.Projections = []string{"execution.summary.v1", "execution.summary.v1"}
		},
		"unsupported": func(c *config.Config) { c.Source.Feed.Module = "bsc.eth68" },
		"chain":       func(c *config.Config) { c.Source.Feed.ChainID = "97" },
		"state":       func(c *config.Config) { on := true; c.Features["balance.native"] = config.FeatureConfig{Enabled: &on} },
		"rate":        func(c *config.Config) { c.RPC.RateLimit = 6 },
		"height":      func(c *config.Config) { c.Indexer.StartHeight = 0 },
		"no-volume":   func(c *config.Config) { c.WindowStorage = window.Layout{} },
		"authority":   func(c *config.Config) { c.Source.Feed.Collect = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cp := *cfg
			f := *cfg.Source.Feed
			cp.Source.Feed = &f
			cp.Features = chainFeatures(cfg.Features, nil)
			cp.Database.Path = filepath.Join(t.TempDir(), "absent")
			mutate(&cp)
			_, e := NewApp(&cp, zap.NewNop(), false, "")
			require.Error(t, e)
			_, e = os.Stat(cp.Database.Path)
			require.ErrorIs(t, e, os.ErrNotExist)
		})
	}
	require.Empty(t, rpc.Calls())
	b, e := yaml.Marshal(cfg.Source.Feed)
	require.NoError(t, e)
	var f feed.Config
	require.Error(t, yaml.Unmarshal(append(b, []byte("typo: true\n")...), &f))
}

func TestManagedFeedFourChainIsolation(t *testing.T) {
	cfg, arc, rpc := feedAppConfig(t)
	cfg.API.Enabled = true
	selected := cfg.Source.Feed
	cfg.Source = config.SourceConfig{}
	cfg.MultiChain.Enabled = true
	cfg.MultiChain.AutoRestart = false
	cfg.MultiChain.HealthCheckInterval = time.Hour
	cfg.MultiChain.Chains = []config.ChainConfig{{ID: "arc", Name: "Arc", Enabled: true, ChainID: 5042002, RPCEndpoint: rpc.URL(), StartHeight: 1, Feed: selected}}
	chains := map[string]*testchain.Chain{"arc": arc}
	for _, tc := range []struct {
		id string
		c  *testchain.Chain
	}{{"stablenet", testchain.BuildStableNet().Chain}, {"bsc", testchain.NewChain(97, nil)}, {"sepolia", testchain.NewChain(11155111, nil)}} {
		if tc.c.Head() == 0 {
			addFeedBlocks(tc.c, 2)
		}
		srv := testchain.NewServer(tc.c)
		t.Cleanup(srv.Close)
		chains[tc.id] = tc.c
		cfg.MultiChain.Chains = append(cfg.MultiChain.Chains, config.ChainConfig{ID: tc.id, Name: tc.id, Enabled: true, ChainID: tc.c.ChainID().Uint64(), RPCEndpoint: srv.URL(), StartHeight: 1})
	}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	for id, c := range chains {
		require.Eventually(t, func() bool {
			idx, ok := a.multichainManager.ChainIndexer(id)
			if !ok {
				return false
			}
			h, e := idx.IndexedHeight(context.Background())
			return e == nil && h >= c.Head()
		}, 30*time.Second, 20*time.Millisecond, id)
	}
	for id := range chains {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", "/chains/"+id+"/indexing/status", nil))
		require.Equal(t, 200, w.Code)
		var status indexingstatus.Status
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
		if id == "arc" {
			require.Equal(t, "captured_facts", status.Mode)
		} else {
			require.Equal(t, "unreported", status.Mode)
		}
	}
	rpc.FailMethod("eth_getBlockByNumber", 10000)
	for id, c := range chains {
		if id != "arc" {
			addFeedBlocks(c, 1)
		}
	}
	for id, c := range chains {
		if id == "arc" {
			continue
		}
		require.Eventually(t, func() bool {
			idx, ok := a.multichainManager.ChainIndexer(id)
			if !ok {
				return false
			}
			h, e := idx.IndexedHeight(context.Background())
			return e == nil && h >= c.Head()
		}, 20*time.Second, 20*time.Millisecond, id)
	}
	rpc.FailMethod("eth_getBlockByNumber", 0)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	a.Shutdown()
}

func TestManagedFeedMissingRawIsNeverRecreated(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	a.Shutdown()
	path := cfg.WindowStorage.Windows["arc"].Path
	require.NoError(t, os.Rename(path, path+"-removed"))
	_, e = NewApp(cfg, zap.NewNop(), false, "")
	require.Error(t, e)
	_, e = os.Stat(path)
	require.ErrorIs(t, e, os.ErrNotExist, "even retained create_window=true cannot recreate a recorded window")
}

func TestManagedFeedJoinAndStop(t *testing.T) {
	// No network/fsync fixture: repeat the cancel/join ordering independently.
	entered := make(chan struct{}, 6)
	release := make(chan struct{})
	returned := make(chan struct{}, 6)
	task := func(ctx context.Context) error {
		entered <- struct{}{}
		<-ctx.Done()
		<-release
		returned <- struct{}{}
		return ctx.Err()
	}
	m := &managedFeed{program: feed.Program{Run: func(ctx context.Context, _ port.CaptureDatabase, _ capture.SessionConfig) error { return task(ctx) }}, projection: task, extraTasks: []func(context.Context) error{task, task, task, task}}
	runDone := make(chan error, 1)
	go func() { runDone <- m.Run(context.Background()) }()
	<-entered
	<-entered
	<-entered
	<-entered
	<-entered
	<-entered
	stopped := make(chan struct{})
	go func() { m.Stop(); close(stopped) }()
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	cancel()
	select {
	case <-stopped:
		t.Fatal("Stop returned before workers joined")
	default:
	}
	close(release)
	require.ErrorIs(t, <-runDone, context.Canceled)
	<-stopped
	<-returned
	<-returned
	<-returned
	<-returned
	<-returned
	<-returned
	require.Error(t, m.Run(context.Background()), "a stopped app cannot launch a second worker set")
}

func TestManagedFeedRestartFromBootstrapCursor(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	// Undo to a bootstrap which has no captured/stored block leaves its height
	// cursor. It must be accepted when the next epoch/restart reconciles undo.
	require.NoError(t, a.fetcher.Exec(t.Context(), "bootstrap-cursor", func(ctx context.Context) error { return a.storage.SetLatestHeight(ctx, 0) }))
	a.Shutdown()
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	a.Shutdown()
}

func TestManagedFeedConfigurationRoundTrip(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	encoded, e := yaml.Marshal(cfg)
	require.NoError(t, e)
	decoded := config.NewConfig()
	require.NoError(t, yaml.Unmarshal(encoded, decoded))
	require.Equal(t, cfg.Source.Feed, decoded.Source.Feed)
	require.NoError(t, preflightFeeds(decoded, false))
	cfg.MultiChain.Enabled = true
	cfg.MultiChain.Chains = []config.ChainConfig{{ID: "arc", Name: "Arc", Enabled: true, ChainID: 5042002, RPCEndpoint: cfg.RPC.Endpoint, StartHeight: 1, Feed: cfg.Source.Feed}}
	cfg.Source = config.SourceConfig{}
	encoded, e = yaml.Marshal(cfg)
	require.NoError(t, e)
	decoded = config.NewConfig()
	require.NoError(t, yaml.Unmarshal(encoded, decoded))
	require.NoError(t, preflightFeeds(decoded, false))
	require.Equal(t, cfg.MultiChain.Chains[0].Feed, decoded.MultiChain.Chains[0].Feed)
	// Two independently enabled entries cannot accidentally share one window.
	cc := decoded.MultiChain.Chains[0]
	cc.ID = "arc-copy"
	decoded.MultiChain.Chains = append(decoded.MultiChain.Chains, cc)
	require.ErrorContains(t, preflightFeeds(decoded, false), "reservation is shared")
}

func TestManagedFeedBackfillOutsideCoverageRejected(t *testing.T) {
	cfg, c, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	waitFeedProjection(t, a, c, 2)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	// Simulate a newly enabled feature whose historical progress is absent.
	require.NoError(t, a.storage.(port.FeatureStateStore).SetFeatureState(t.Context(), "address.index", port.FeatureState{Active: true, Gap: &port.BlockRange{From: 0, To: 2}}))
	a.Shutdown()
	second, e := NewApp(cfg, zap.NewNop(), false, "")
	if second != nil {
		defer second.Shutdown()
	}
	require.ErrorIs(t, e, facts.ErrCoverage)
}
