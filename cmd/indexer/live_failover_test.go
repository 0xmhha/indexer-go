package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
)

// TestLiveFailover indexes a live node configured as the fallback of a
// primary endpoint that refuses connections: start-up and indexing go
// through the fallback (refactoring plan R2-1). Runs only with
// INDEXER_LIVE_RPC.
func TestLiveFailover(t *testing.T) {
	endpoint := os.Getenv("INDEXER_LIVE_RPC")
	if endpoint == "" {
		t.Skip("INDEXER_LIVE_RPC not set")
	}
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = "http://127.0.0.1:1" // nothing listens here
	cfg.RPC.FallbackEndpoints = []string{endpoint}
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = filepath.Join(t.TempDir(), "db")
	cfg.API.Enabled = false
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, 100))
	h, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(100), h)
	st := app.client.Pool().Status()
	require.False(t, st[0].Available)
	t.Logf("primary: %d requests, %d failures; fallback: %d requests", st[0].Requests, st[0].Failures, st[1].Requests)
}
