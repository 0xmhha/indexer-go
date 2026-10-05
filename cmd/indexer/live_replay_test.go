package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
)

// TestLiveRecordReplay records a running StableNet node while indexing it
// and indexes again from the recording without the node: the two databases
// must be identical and the replay must need no unrecorded call.
//
// Runs only with INDEXER_LIVE_RPC.
func TestLiveRecordReplay(t *testing.T) {
	endpoint := os.Getenv("INDEXER_LIVE_RPC")
	if endpoint == "" {
		t.Skip("INDEXER_LIVE_RPC not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ec, err := ethclient.DialContext(ctx, endpoint)
	require.NoError(t, err)
	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)
	ec.Close()
	head = liveHead(t, head)

	run := func(endpoint, recordDir, dir string) (*App, time.Duration) {
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = endpoint
		cfg.RPC.RecordDir = recordDir
		cfg.RPC.Timeout = 5 * time.Second
		cfg.Database.Path = dir
		cfg.API.Enabled = false
		app, err := NewApp(cfg, zap.NewNop(), false, "")
		require.NoError(t, err)
		start := time.Now()
		require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
		return app, time.Since(start)
	}

	archive := filepath.Join(t.TempDir(), "archive")
	recorded := filepath.Join(t.TempDir(), "recorded")
	app, nodeTime := run(endpoint, archive, recorded)
	app.Shutdown()

	replayed := filepath.Join(t.TempDir(), "replayed")
	app, replayTime := run("replay://"+archive, "", replayed)
	unrecorded := app.rpcReplay.Unrecorded()
	app.Shutdown()

	t.Logf("blocks 0..%d: node (recording) %v, replay %v", head, nodeTime.Round(time.Millisecond), replayTime.Round(time.Millisecond))
	require.Empty(t, unrecorded)
	diff := testchain.DiffKeyspace(dumpDir(t, recorded), dumpDir(t, replayed), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}
