package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type preparedRPCRequest struct {
	ID     json.RawMessage
	Method string
	Params []json.RawMessage
}

func preparedRPCGate(t *testing.T, upstreamURL string, gate func(preparedRPCRequest) (int, string)) *httptest.Server {
	t.Helper()
	upstream, err := url.Parse(upstreamURL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request preparedRPCRequest
		_ = json.Unmarshal(raw, &request)
		code, message := gate(request)
		if code != 0 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": code, "message": message}})
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}
func TestPreparedTokenStateFailureRollsBackAndRetries(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	var missing atomic.Bool
	gateway := preparedRPCGate(t, srv.URL(), func(r preparedRPCRequest) (int, string) {
		if missing.Load() && (r.Method == "eth_call" || r.Method == "eth_getCode") && len(r.Params) > 1 {
			var selector struct{ BlockHash common.Hash }
			_ = json.Unmarshal(r.Params[1], &selector)
			if selector.BlockHash == sc.Chain.Block(2).Block.Hash() {
				return -32000, "missing trie node: historical state is not available"
			}
		}
		return 0, ""
	})
	app := newPreparedApp(t, gateway.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) {
		off := false
		cfg.Features["balance.native"] = config.FeatureConfig{Enabled: &off}
	})
	t.Cleanup(app.Shutdown)
	ctx := context.Background()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, 1))
	before, err := app.storage.(port.Outbox).ReadOutbox(ctx, 0, 0)
	require.NoError(t, err)
	missing.Store(true)
	require.ErrorContains(t, app.fetcher.FetchBlock(ctx, 2), "missing trie node")
	requireFailedStateBlockAbsent(t, app, sc.Chain, 2)
	_, err = app.storage.GetTokenMetadata(ctx, sc.ERC20)
	require.ErrorIs(t, err, port.ErrNotFound)
	after, err := app.storage.(port.Outbox).ReadOutbox(ctx, 0, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
	missing.Store(false)
	require.NoError(t, app.fetcher.FetchBlock(ctx, 2))
	md, err := app.storage.GetTokenMetadata(ctx, sc.ERC20)
	require.NoError(t, err)
	require.Equal(t, "TT", md.Symbol)
	require.Equal(t, uint64(2), md.DetectedAt)
}
func TestPreparedUnsupportedHashRPCDoesNotUseZeroFallback(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	var unsupported atomic.Bool
	unsupported.Store(true)
	gateway := preparedRPCGate(t, srv.URL(), func(r preparedRPCRequest) (int, string) {
		if unsupported.Load() && r.Method == "eth_getBalance" && len(r.Params) > 1 && len(r.Params[1]) > 0 && r.Params[1][0] == '{' {
			return -32602, "hash selector unsupported"
		}
		return 0, ""
	})
	app := newPreparedApp(t, gateway.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) {
		require.NoError(t, cfg.SetFeatureSettings("balance.native", map[string]any{"allow_pruned_state": true}))
		off := false
		cfg.Features["token.metadata"] = config.FeatureConfig{Enabled: &off}
	})
	t.Cleanup(app.Shutdown)
	ctx := context.Background()
	require.ErrorContains(t, app.fetcher.FetchBlock(ctx, 0), "hash selector unsupported")
	requireFailedStateBlockAbsent(t, app, sc.Chain, 0)
	unsupported.Store(false)
	require.NoError(t, app.fetcher.FetchBlock(ctx, 0))
}
