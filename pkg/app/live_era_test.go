package app

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
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/source/era"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestLiveEraSource indexes a running StableNet node twice: once only from
// the node, once reading the blocks held by era1 archives (exported from the
// same chain with `gstable export-history`) from the files and the following
// blocks from the node. The databases must be identical.
//
// Runs only with INDEXER_LIVE_RPC and INDEXER_LIVE_ERA_DIR.
func TestLiveEraSource(t *testing.T) {
	endpoint, eraDir := os.Getenv("INDEXER_LIVE_RPC"), os.Getenv("INDEXER_LIVE_ERA_DIR")
	if endpoint == "" || eraDir == "" {
		t.Skip("INDEXER_LIVE_RPC or INDEXER_LIVE_ERA_DIR not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	ec, err := ethclient.DialContext(ctx, endpoint)
	require.NoError(t, err)
	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)
	ec.Close()

	es, err := era.OpenDir(eraDir, stablenet.New())
	require.NoError(t, err)
	first, last := es.Range()
	require.NoError(t, es.Close())
	require.Zero(t, first, "the test indexes from genesis")
	target := min(last+500, head) // continue past the archive through the node

	run := func(eraDir, dir string) time.Duration {
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = endpoint
		cfg.RPC.Timeout = 5 * time.Second
		cfg.Source.EraDir = eraDir
		cfg.Database.Path = dir
		cfg.API.Enabled = false
		app, err := NewApp(cfg, zap.NewNop(), false, "")
		require.NoError(t, err)
		defer app.Shutdown()
		start := time.Now()
		require.NoError(t, app.fetcher.FetchRange(ctx, 0, target))
		return time.Since(start)
	}

	viaNode := filepath.Join(t.TempDir(), "node")
	nodeTime := run("", viaNode)
	viaEra := filepath.Join(t.TempDir(), "era")
	eraTime := run(eraDir, viaEra)

	t.Logf("blocks 0..%d (era1 holds 0..%d): node only %v, era1 then node %v",
		target, last, nodeTime.Round(time.Millisecond), eraTime.Round(time.Millisecond))
	diff := testchain.DiffKeyspace(dumpDir(t, viaNode), dumpDir(t, viaEra), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}
