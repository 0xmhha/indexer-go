package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
)

// TestLiveHeadLatency follows a running node with the live loop for 30
// seconds and measures, per block, the time from the block first appearing
// on the node (sampled every 5ms) to the indexer's cursor reaching it. With
// the default poll interval the p95 must stay within 100ms.
//
// Runs only with INDEXER_LIVE_RPC and INDEXER_LIVE_LATENCY=1, on a node that
// produces blocks; results depend on the machine's load.
func TestLiveHeadLatency(t *testing.T) {
	endpoint := os.Getenv("INDEXER_LIVE_RPC")
	if endpoint == "" || os.Getenv("INDEXER_LIVE_LATENCY") == "" {
		t.Skip("INDEXER_LIVE_RPC and INDEXER_LIVE_LATENCY=1 not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ec, err := ethclient.DialContext(ctx, endpoint)
	require.NoError(t, err)
	defer ec.Close()
	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)

	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = filepath.Join(t.TempDir(), "db")
	cfg.API.Enabled = false
	cfg.Indexer.StartHeight = head
	cfg.Indexer.Finality = os.Getenv("INDEXER_FINALITY") // empty: head
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)

	var mu sync.Mutex
	seen := map[uint64]time.Time{}    // first time the node reported a head
	indexed := map[uint64]time.Time{} // first time the cursor reached a height
	watch := func(read func() (uint64, error), into map[uint64]time.Time, every time.Duration) {
		for ctx.Err() == nil {
			if h, err := read(); err == nil {
				mu.Lock()
				if _, ok := into[h]; !ok {
					into[h] = time.Now()
				}
				mu.Unlock()
			}
			time.Sleep(every)
		}
	}
	go watch(func() (uint64, error) { return ec.BlockNumber(ctx) }, seen, 5*time.Millisecond)
	go watch(func() (uint64, error) { return app.storage.GetLatestHeight(ctx) }, indexed, time.Millisecond)

	runCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	_ = app.fetcher.Run(runCtx)
	stop()
	cancel()
	app.Shutdown()

	mu.Lock()
	defer mu.Unlock()
	var lat []time.Duration
	for h, s := range seen {
		if h <= head+1 {
			continue // started before the loop
		}
		var at time.Time
		for ih, it := range indexed {
			if ih >= h && (at.IsZero() || it.Before(at)) {
				at = it
			}
		}
		if !at.IsZero() {
			lat = append(lat, at.Sub(s))
		}
	}
	require.NotEmpty(t, lat, "the node produced no blocks")
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p95 := lat[len(lat)*95/100]
	t.Logf("finality %q poll %v: %d blocks, p50 %v p95 %v max %v", cfg.Indexer.Finality, cfg.Indexer.PollInterval, len(lat),
		lat[len(lat)/2].Round(time.Millisecond), p95.Round(time.Millisecond), lat[len(lat)-1].Round(time.Millisecond))
	if cfg.Indexer.Finality == "" || cfg.Indexer.Finality == "head" {
		// Other policies wait for confirmation by design and are only measured.
		require.LessOrEqual(t, p95, 100*time.Millisecond)
	}
}
