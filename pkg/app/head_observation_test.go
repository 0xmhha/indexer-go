package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHeadObservationProductionHealthIsFresh(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL())
	require.NoError(t, err)
	forward := httputil.NewSingleHostReverseProxy(u)
	var heads atomic.Int64
	var fail atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var call struct{ Method string }
		if json.Unmarshal(raw, &call) == nil && call.Method == "eth_blockNumber" {
			heads.Add(1)
			if fail.Load() {
				http.Error(w, "node offline", http.StatusServiceUnavailable)
				return
			}
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	app := newPreparedApp(t, proxy.URL, filepath.Join(t.TempDir(), "db"), true, 0)
	t.Cleanup(app.Shutdown)
	idx := &chainIndexer{app: app}
	ci := multichain.NewChainInstance(&multichain.ChainConfig{ID: "head-test"}, nil, zap.NewNop())
	ci.Indexer = idx
	head, err := idx.NodeHeight(t.Context())
	require.NoError(t, err)
	before := heads.Load()
	observed, latency, err := idx.ObserveNodeHeight(t.Context(), time.Hour)
	require.NoError(t, err)
	require.Equal(t, head, observed)
	require.Positive(t, latency)
	require.Equal(t, before, heads.Load(), "block metrics reuse the production fetcher's observation")
	sc.Chain.AddBlock()
	health := ci.HealthCheck(t.Context())
	require.Equal(t, head+1, health.LatestHeight)
	require.Equal(t, before+1, heads.Load(), "explicit health is a fresh RPC")
	fail.Store(true)
	health = ci.HealthCheck(t.Context())
	require.False(t, health.IsHealthy)
	require.NotEmpty(t, health.LastError)
	_, _, err = idx.ObserveNodeHeight(t.Context(), time.Hour)
	require.Error(t, err, "new failure cannot be hidden by the previous cached success")
}
