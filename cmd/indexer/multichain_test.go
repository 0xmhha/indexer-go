package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
)

// freeAPIPort returns a TCP port nothing listens on.
func freeAPIPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// multiChainConfig configures multichain mode over the given chains, with
// the API on a free local port.
func multiChainConfig(t *testing.T, root string, chains ...config.ChainConfig) *config.Config {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = ""
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = root
	cfg.Indexer.PollInterval = 10 * time.Millisecond
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableJSONRPC = true
	cfg.MultiChain = config.MultiChainConfig{
		Enabled:              true,
		Chains:               chains,
		HealthCheckInterval:  time.Second,
		MaxUnhealthyDuration: time.Minute,
		AutoRestartDelay:     time.Second,
	}
	enableTestChainFeatures(cfg)
	require.NoError(t, cfg.Validate())
	require.NoError(t, validateConfig(cfg))
	return cfg
}

func chainEntry(id, endpoint string) config.ChainConfig {
	return config.ChainConfig{
		ID:          id,
		Name:        id,
		RPCEndpoint: endpoint,
		ChainID:     testchain.DefaultChainID,
		AdapterType: "auto",
		Enabled:     true,
	}
}

// TestMultiChainIndexesEachChainIntoItsOwnDatabase runs two different
// chains at once in multichain mode (refactoring plan R2-8, defect D4).
// Each chain is indexed into its own database under chains/<id>, which
// equals indexing that chain alone, and each chain's API under
// /chains/<id>/ answers from that chain's data.
func TestMultiChainIndexesEachChainIntoItsOwnDatabase(t *testing.T) {
	evm := testchain.BuildDefault()
	snet := testchain.BuildStableNet()
	evmSrv := testchain.NewServer(evm.Chain)
	snetSrv := testchain.NewServer(snet.Chain)
	t.Cleanup(evmSrv.Close)
	t.Cleanup(snetSrv.Close)

	root := filepath.Join(t.TempDir(), "db")
	cfg := multiChainConfig(t, root, chainEntry("evm", evmSrv.URL()), chainEntry("stablenet", snetSrv.URL()))
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	require.Nil(t, app.storage, "multichain mode opens no shared database")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(runCtx) }()

	heads := map[string]uint64{"evm": evm.Chain.Head(), "stablenet": snet.Chain.Head()}
	for id, head := range heads {
		require.Eventually(t, func() bool {
			ci, err := app.multichainManager.GetChain(id)
			if err != nil {
				return false
			}
			h, ok := ci.IndexedHeight(ctx)
			return ok && h == head
		}, time.Minute, 20*time.Millisecond, "chain %s indexed to its head", id)
	}

	// Each chain's API serves that chain's blocks.
	router := app.apiServer.Router()
	hashes := map[string]string{}
	for id := range heads {
		store, _, ok := app.multichainManager.ChainStore(id)
		require.True(t, ok)
		b, err := store.GetBlock(ctx, 1)
		require.NoError(t, err)
		hashes[id] = strings.ToLower(b.Hash.Hex())
	}
	require.NotEqual(t, hashes["evm"], hashes["stablenet"])
	for id, hash := range hashes {
		body := serve(t, router, http.MethodPost, "/chains/"+id+"/rpc",
			`{"jsonrpc":"2.0","id":1,"method":"getBlock","params":{"number":1}}`)
		require.Contains(t, strings.ToLower(body), hash, "chain %s JSON-RPC", id)
		body = serve(t, router, http.MethodPost, "/chains/"+id+"/graphql",
			`{"query":"{ block(number: \"1\") { hash } }"}`)
		require.Contains(t, strings.ToLower(body), hash, "chain %s GraphQL", id)
		require.NotContains(t, body, "error")
	}
	require.Contains(t, serve(t, router, http.MethodGet, "/chains", ""), `"id":"stablenet"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/chains/other/rpc", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusNotFound, rec.Code, "no root API without a shared database")

	stop()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	app.Shutdown()

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "chains", entries[0].Name(), "the root holds only the chains' databases")

	for id, sc := range map[string]*testchain.Scenario{"evm": evm, "stablenet": &snet.Scenario} {
		alone := indexScenario(t, sc)
		diff := testchain.DiffKeyspace(dumpDir(t, alone), dumpDir(t, chainDBPath(root, id)), 0)
		require.Empty(t, diff, "chain %s: %s", id, testchain.SummarizeDiff(diff))
	}
}

// TestMultiChainRejectsWrongChainID: a chain whose node serves another
// chain id does not start, and the other chains keep running.
func TestMultiChainRejectsWrongChainID(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)

	wrong := chainEntry("wrong", srv.URL())
	wrong.ChainID = testchain.DefaultChainID + 1
	root := filepath.Join(t.TempDir(), "db")
	cfg := multiChainConfig(t, root, chainEntry("ok", srv.URL()), wrong)
	cfg.API.Enabled = false
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	go func() { _ = app.Run(ctx) }()

	require.Eventually(t, func() bool {
		ci, err := app.multichainManager.GetChain("ok")
		if err != nil {
			return false
		}
		h, ok := ci.IndexedHeight(ctx)
		return ok && h == sc.Chain.Head()
	}, time.Minute, 20*time.Millisecond)
	ci, err := app.multichainManager.GetChain("wrong")
	require.NoError(t, err)
	require.Contains(t, ci.HealthCheck(ctx).LastError, fmt.Sprintf("configured chain_id is %d", wrong.ChainID))
	_, _, ok := app.multichainManager.ChainStore("wrong")
	require.False(t, ok)
}

func TestMultiChainConfigValidation(t *testing.T) {
	for _, id := range []string{"../evm", "a/b", ".x", ""} {
		cfg := config.NewConfig()
		cfg.Database.Path = t.TempDir()
		cfg.MultiChain = config.MultiChainConfig{Enabled: true, Chains: []config.ChainConfig{chainEntry(id, "http://127.0.0.1:1")}}
		require.Error(t, cfg.Validate(), "id %q", id)
	}
	cfg := config.NewConfig()
	cfg.Database.Path = t.TempDir()
	cfg.MultiChain = config.MultiChainConfig{Enabled: true, Chains: []config.ChainConfig{
		chainEntry("a", "http://127.0.0.1:1"), chainEntry("a", "http://127.0.0.1:2"),
	}}
	require.ErrorContains(t, cfg.Validate(), "duplicate id")

	cfg.MultiChain.Chains[1].ID = "b"
	cfg.RPC.Endpoint = ""
	require.NoError(t, cfg.Validate(), "multichain mode needs no root rpc.endpoint")
	require.NoError(t, validateConfig(cfg))
	dbs, err := databases(cfg)
	require.NoError(t, err)
	require.Len(t, dbs, 2)
	require.Equal(t, chainDBPath(cfg.Database.Path, "a"), dbs[0].Path)
	require.Equal(t, chainDBPath(cfg.Database.Path, "b"), dbs[1].Path)

	// With PostgreSQL every chain has a schema of its own; ids that map to
	// the same schema are refused.
	cfg.Database.Driver = config.DriverPostgres
	cfg.Database.Postgres = config.PostgresConfig{DSN: "postgres://localhost/indexer", Schema: "idx"}
	dbs, err = databases(cfg)
	require.NoError(t, err)
	require.Equal(t, "idx_a", dbs[0].Postgres.Schema)
	require.Equal(t, "idx_b", dbs[1].Postgres.Schema)
	cfg.MultiChain.Chains[0].ID, cfg.MultiChain.Chains[1].ID = "x-1", "x.1"
	require.ErrorContains(t, validateConfig(cfg), "both map to PostgreSQL schema")
}

// serve sends one request to h and returns the response body.
func serve(t *testing.T, h http.Handler, method, path, body string) string {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code, "%s %s: %s", method, path, out)
	return string(out)
}
