package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Use this unchanged benchmark in both checkouts. Time includes chain startup,
// ingest, delivery to the metrics subscriber and cancellation of the live run.
// Empty blocks isolate head-observation overhead from native-state RPC volume.
func BenchmarkHeadObservationQuota(b *testing.B) {
	b.StopTimer()
	chain := testchain.NewChain(testchain.DefaultChainID, nil)
	for range 40 {
		chain.AddBlock()
	}
	srv := testchain.NewServer(chain)
	defer srv.Close()
	u, err := url.Parse(srv.URL())
	require.NoError(b, err)
	forward := httputil.NewSingleHostReverseProxy(u)
	var httpCalls, headCalls atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		httpCalls.Add(1)
		var call struct{ Method string }
		if json.Unmarshal(raw, &call) == nil && call.Method == "eth_blockNumber" {
			headCalls.Add(1)
		}
		forward.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	b.ReportAllocs()
	var totalHTTP, totalHead int64
	for range b.N {
		b.StopTimer()
		cfg := config.NewConfig()
		cfg.RPC.Endpoint, cfg.RPC.Timeout, cfg.RPC.RateLimit = "", 15*time.Second, 5
		setTestDatabase(b, cfg, filepath.Join(b.TempDir(), "db"))
		cfg.API.Enabled, cfg.Notifications.Enabled = false, false
		cfg.Indexer.Workers, cfg.Indexer.ChunkSize, cfg.Indexer.PollInterval = 2, 10, 2*time.Second
		cfg.Indexer.StatePrefetch, cfg.Indexer.StateWorkers = true, 4
		cfg.MultiChain = config.MultiChainConfig{Enabled: true, HealthCheckInterval: 30 * time.Second,
			MaxUnhealthyDuration: 5 * time.Minute, AutoRestartDelay: 10 * time.Second,
			Chains: []config.ChainConfig{{ID: "head-bench", Name: "head-bench", ChainID: testchain.DefaultChainID,
				RPCEndpoint: proxy.URL, AdapterType: "evm", Enabled: true, Workers: 2, BatchSize: 10}}}
		app, err := NewApp(cfg, zap.NewNop(), false, "")
		require.NoError(b, err)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		var stopOnce, closeOnce sync.Once
		var runErr error
		stop := func() {
			stopOnce.Do(func() {
				cancel()
				select {
				case runErr = <-done:
				case <-time.After(30 * time.Second):
					runErr = fmt.Errorf("benchmark shutdown timed out")
				}
			})
		}
		cleanup := func() { stop(); closeOnce.Do(app.Shutdown) }
		beforeHTTP, beforeHead := httpCalls.Load(), headCalls.Load()
		b.StartTimer()
		go func() { done <- app.Run(ctx) }()
		b.Cleanup(cleanup)
		require.Eventually(b, func() bool {
			ci, err := app.multichainManager.GetChain("head-bench")
			if err != nil {
				return false
			}
			h, ok := ci.IndexedHeight(ctx)
			return ok && h == chain.Head() && ci.GetMetrics().BlocksIndexed == chain.Head()+1
		}, 2*time.Minute, time.Millisecond)
		stop()
		b.StopTimer()
		require.True(b, runErr == nil || errors.Is(runErr, context.Canceled), "%v", runErr)
		totalHTTP += httpCalls.Load() - beforeHTTP
		totalHead += headCalls.Load() - beforeHead
		cleanup()
	}
	b.ReportMetric(float64(chain.Head()+1), "blocks/op")
	b.ReportMetric(float64(totalHTTP)/float64(b.N), "HTTPs/op")
	b.ReportMetric(float64(totalHead)/float64(b.N), "heads/op")
}
