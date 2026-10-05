package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
)

// indexVia indexes [0, head] through endpoint (a node URL or replay:///dir),
// recording into recordDir when it is set.
func indexVia(t *testing.T, endpoint, recordDir, dir string, head uint64) *App {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.RecordDir = recordDir
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = dir
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	return app
}

// TestRecordThenReplay records the reference scenario while indexing it,
// stops the node and indexes again from the recording: both databases must
// equal the golden, and the replay must need no call that was not recorded.
func TestRecordThenReplay(t *testing.T) {
	golden := dumpScenarioIndex(t)
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	head := sc.Chain.Head()
	archive := filepath.Join(t.TempDir(), "archive")

	recorded := filepath.Join(t.TempDir(), "recorded")
	indexVia(t, srv.URL(), archive, recorded, head).Shutdown()
	srv.Close() // no node from here on

	replayed := filepath.Join(t.TempDir(), "replayed")
	app := indexVia(t, "replay://"+archive, "", replayed, head)
	unrecorded := app.rpcReplay.Unrecorded()
	app.Shutdown()

	require.Empty(t, unrecorded, "the replay needed calls that were not recorded")
	for name, dir := range map[string]string{"recorded": recorded, "replayed": replayed} {
		diff := testchain.DiffKeyspace(golden, dumpDir(t, dir), 0)
		require.Empty(t, diff, "%s: %v", name, testchain.SummarizeDiff(diff))
	}
}
