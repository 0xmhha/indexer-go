package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// startAppFinality starts the app against srv with a finality policy.
func startAppFinality(t *testing.T, srv *testchain.Server, finality string, confirmations uint64) (*App, error) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = false
	cfg.Indexer.Finality = finality
	cfg.Indexer.Confirmations = confirmations
	enableTestChainFeatures(cfg)
	return NewApp(cfg, zap.NewNop(), false, "")
}

// runLive runs the live loop until stop is called.
func runLive(t *testing.T, app *App) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(ctx) }()
	return func() { cancel(); <-done }
}

func indexedHeight(t *testing.T, app *App) uint64 {
	t.Helper()
	h, err := app.storage.GetLatestHeight(context.Background())
	if err != nil {
		return 0
	}
	return h
}

// requireStaysAt waits until the index reaches height and then checks that
// it does not go further over several poll intervals.
func requireStaysAt(t *testing.T, app *App, height uint64) {
	t.Helper()
	require.Eventually(t, func() bool { return indexedHeight(t, app) == height }, 30*time.Second, 10*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, height, indexedHeight(t, app))
}

func TestFinalityConfirmations(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()

	app, err := startAppFinality(t, srv, "confirmations", 3)
	require.NoError(t, err)
	defer app.Shutdown()
	stop := runLive(t, app)
	defer stop()

	requireStaysAt(t, app, head-3)
	sc.Chain.AddBlock()
	sc.Chain.AddBlock()
	requireStaysAt(t, app, head-1)
}

func TestFinalityFinalized(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	sc.Chain.SetFinalized(head - 5)

	app, err := startAppFinality(t, srv, "finalized", 0)
	require.NoError(t, err)
	defer app.Shutdown()
	stop := runLive(t, app)
	defer stop()

	requireStaysAt(t, app, head-5)
	sc.Chain.SetFinalized(head - 1)
	requireStaysAt(t, app, head-1)
}

// TestFinalityFinalizedNotYet: a node that has not finalized a block yet
// (go-stablenet after a restart or while syncing) must not stop the
// indexer: it waits, then follows once the node reports one.
func TestFinalityFinalizedNotYet(t *testing.T) {
	for name, disable := range map[string]func(*testchain.Chain){
		"null":  (*testchain.Chain).DisableFinalizedTag,
		"error": (*testchain.Chain).FailFinalizedTag, // go-stablenet
	} {
		t.Run(name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			head := sc.Chain.Head()
			disable(sc.Chain)

			app, err := startAppFinality(t, srv, "finalized", 0)
			require.NoError(t, err)
			defer app.Shutdown()
			stop := runLive(t, app)
			defer stop()

			time.Sleep(300 * time.Millisecond)
			require.Zero(t, indexedHeight(t, app), "nothing is indexed without a finalized block")
			sc.Chain.SetFinalized(head - 2)
			requireStaysAt(t, app, head-2)
		})
	}
}
