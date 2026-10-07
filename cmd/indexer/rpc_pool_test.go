package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
)

// TestIndexingSurvivesEndpointLoss: with a fallback endpoint configured,
// stopping the primary node does not stop indexing (refactoring plan R2-1).
// The live loop keeps following the chain through the fallback, and the
// result equals indexing the chain in one go.
func TestIndexingSurvivesEndpointLoss(t *testing.T) {
	sc := testchain.BuildDefault()
	primary := testchain.NewServer(sc.Chain)
	fallback := testchain.NewServer(sc.Chain)
	t.Cleanup(primary.Close)
	t.Cleanup(fallback.Close)

	cfg := config.NewConfig()
	cfg.RPC.Endpoint = primary.URL()
	cfg.RPC.FallbackEndpoints = []string{fallback.URL()}
	cfg.RPC.Timeout = 2 * time.Second
	cfg.Database.Path = filepath.Join(t.TempDir(), "db")
	cfg.API.Enabled = false
	cfg.Indexer.PollInterval = 10 * time.Millisecond
	enableTestChainFeatures(cfg)
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	half := sc.Chain.Head() / 2
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, half))

	primary.Close()
	extendChain(sc, 3)
	head := sc.Chain.Head()

	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(loopCtx) }()
	require.Eventually(t, func() bool {
		h, err := app.storage.GetLatestHeight(ctx)
		return err == nil && h == head
	}, time.Minute, 20*time.Millisecond, "indexing continues through the fallback")
	stop()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("live loop: %v", err)
	}
	st := app.client.Pool().Status()
	require.False(t, st[0].Available, "the stopped primary is cooling down")
	require.NotZero(t, st[1].Requests, "the fallback answered")
	dir := cfg.Database.Path
	app.Shutdown()

	fresh := filepath.Join(t.TempDir(), "fresh")
	runSession(t, fallback, fresh, 0, head)
	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}
