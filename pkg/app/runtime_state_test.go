package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

func requireFailedStateBlockAbsent(t *testing.T, app *App, chain *testchain.Chain, number uint64) {
	t.Helper()
	ctx := context.Background()
	_, err := app.storage.GetBlock(ctx, number)
	require.ErrorIs(t, err, port.ErrNotFound)
	for _, tx := range chain.Block(number).Block.Transactions() {
		_, _, err := app.storage.GetTransaction(ctx, tx.Hash())
		require.ErrorIs(t, err, port.ErrNotFound, "no partial transaction")
		_, err = app.storage.GetReceipt(ctx, tx.Hash())
		require.ErrorIs(t, err, port.ErrNotFound, "no partial receipt")
	}
}

// State can disappear after startup's probe. Failed blocks must leave neither
// derived balances nor the cursor/outbox committed, and the same block can be
// retried when the node can answer again. Run on both storage drivers.
func TestRuntimeBalanceStateLossAbortsBlock(t *testing.T) {
	for _, tc := range []struct {
		name         string
		block, prune uint64
		corrupt      bool
		message      string
	}{
		{"genesis", 0, 1, false, "initialize genesis balances"},
		{"new account", 1, 1, false, "initialize balance"},
		{"diverged account", 2, 3, true, "read balance of diverged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			t.Cleanup(srv.Close)
			app, err := startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), false, func(cfg *config.Config) {
				off := false
				cfg.Features["token.metadata"] = config.FeatureConfig{Enabled: &off}
			})
			require.NoError(t, err)
			t.Cleanup(app.Shutdown)
			ctx := context.Background()
			if tc.block > 0 {
				require.NoError(t, app.fetcher.FetchRange(ctx, 0, tc.block-1))
			}
			if tc.corrupt {
				// Force a negative indexed delta to exercise end-of-block resync.
				require.NoError(t, app.storage.(port.HistoricalWriter).SetBalance(ctx, sc.Accounts[0].Address, tc.block-1, big.NewInt(0)))
			}
			reader := app.storage.(port.HistoricalReader)
			before, _, err := reader.GetBalanceHistory(ctx, sc.Accounts[0].Address, 0, tc.block, port.FirstPage(100))
			require.NoError(t, err)
			outbox := app.storage.(port.Outbox)
			eventsBefore, err := outbox.ReadOutbox(ctx, 0, 0)
			require.NoError(t, err)
			srv.PruneStateBefore(tc.prune)
			err = app.fetcher.FetchRange(ctx, tc.block, tc.block)
			require.ErrorContains(t, err, tc.message)
			require.ErrorContains(t, err, "missing trie node")
			requireFailedStateBlockAbsent(t, app, sc.Chain, tc.block)
			latest, err := app.storage.GetLatestHeight(ctx)
			if tc.block == 0 {
				require.ErrorIs(t, err, port.ErrNotFound)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.block-1, latest)
			}
			after, _, err := reader.GetBalanceHistory(ctx, sc.Accounts[0].Address, 0, tc.block, port.FirstPage(100))
			require.NoError(t, err)
			require.Equal(t, before, after, "no partial balance changes")
			eventsAfter, err := outbox.ReadOutbox(ctx, 0, 0)
			require.NoError(t, err)
			require.Equal(t, eventsBefore, eventsAfter, "no committed events from the failed block")

			srv.PruneStateBefore(0)
			require.NoError(t, app.fetcher.FetchRange(ctx, tc.block, tc.block))
			latest, err = app.storage.GetLatestHeight(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.block, latest, "retry commits the same height")
			for _, addr := range sc.Chain.Accounts() {
				balance, tracked := indexedBalanceAt(t, reader, addr, tc.block)
				if tracked {
					require.Equal(t, 0, balance.Cmp(sc.Chain.BalanceAt(addr, tc.block)), "correct balance after retry for %s", addr.Hex())
				}
			}
		})
	}
}

// Creation-block state can disappear after startup or between metadata reads.
// Test the actual feature setting, transaction rollback and explicit opt-in.
func TestRuntimeTokenStateLossAbortsBlock(t *testing.T) {
	for _, allow := range []bool{false, true} {
		name := "strict"
		if allow {
			name = "explicit fallback"
		}
		t.Run(name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			t.Cleanup(srv.Close)
			upstream, err := url.Parse(srv.URL())
			require.NoError(t, err)
			proxy := httputil.NewSingleHostReverseProxy(upstream)
			var missing atomic.Bool
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				_ = r.Body.Close()
				r.Body = io.NopCloser(bytes.NewReader(raw))
				var request struct {
					ID     json.RawMessage   `json:"id"`
					Method string            `json:"method"`
					Params []json.RawMessage `json:"params"`
				}
				_ = json.Unmarshal(raw, &request)
				if missing.Load() && (request.Method == "eth_getCode" || request.Method == "eth_call") && len(request.Params) > 1 && string(request.Params[1]) == `"0x2"` {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID,
						"error": map[string]any{"code": -32000, "message": "missing trie node: historical state is not available"}})
					return
				}
				proxy.ServeHTTP(w, r)
			}))
			t.Cleanup(gateway.Close)
			cfg := config.NewConfig()
			cfg.RPC.Endpoint = gateway.URL
			setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
			cfg.API.Enabled = false
			enableTestChainFeatures(cfg)
			off := false
			cfg.Features["balance.native"] = config.FeatureConfig{Enabled: &off}
			require.NoError(t, cfg.SetFeatureSettings("token.metadata", map[string]any{"allow_pruned_state": allow}))
			app, err := NewApp(cfg, zap.NewNop(), false, "")
			require.NoError(t, err)
			t.Cleanup(app.Shutdown)
			ctx := context.Background()
			require.NoError(t, app.fetcher.FetchRange(ctx, 0, 1))
			outbox := app.storage.(port.Outbox)
			before, err := outbox.ReadOutbox(ctx, 0, 0)
			require.NoError(t, err)
			missing.Store(true)
			err = app.fetcher.FetchRange(ctx, 2, 2)
			if !allow {
				require.ErrorContains(t, err, "missing trie node")
				requireFailedStateBlockAbsent(t, app, sc.Chain, 2)
				_, err = app.storage.GetTokenMetadata(ctx, sc.ERC20)
				require.ErrorIs(t, err, port.ErrNotFound)
				latest, readErr := app.storage.GetLatestHeight(ctx)
				require.NoError(t, readErr)
				require.Equal(t, uint64(1), latest)
				after, readErr := outbox.ReadOutbox(ctx, 0, 0)
				require.NoError(t, readErr)
				require.Equal(t, before, after)
				missing.Store(false)
				err = app.fetcher.FetchRange(ctx, 2, 2)
			}
			require.NoError(t, err)
			metadata, err := app.storage.GetTokenMetadata(ctx, sc.ERC20)
			require.NoError(t, err)
			require.Equal(t, "TT", metadata.Symbol)
			require.Equal(t, uint64(2), metadata.DetectedAt)
			latest, err := app.storage.GetLatestHeight(ctx)
			require.NoError(t, err)
			require.Equal(t, uint64(2), latest)
		})
	}
}
