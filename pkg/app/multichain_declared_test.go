package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/features/records"
	"github.com/0xmhha/indexer-go/pkg/multichain"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// chainRecords is a chain entry's features section declaring one records
// table over one contract.
func chainRecords(t *testing.T, address, table string) map[string]config.FeatureConfig {
	t.Helper()
	var holder config.Config
	on := true
	spec := declared.Spec{
		Sources: []declared.Source{{Name: "settlement", Address: address, Events: []string{testchain.PaymentSettledSignature}}},
		Tables:  []declared.Table{{Name: table, Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"merchant", "orderId"}}}},
	}
	require.NoError(t, holder.SetFeatureSettings(records.Name, spec))
	fc := holder.Features[records.Name]
	fc.Enabled = &on
	return map[string]config.FeatureConfig{records.Name: fc}
}

// TestMultiChainDeclaredMode: indexer.mode declared runs in multichain
// mode with each chain's own records declaration (chains[].features). Each
// chain's database holds only its own declared data, and each chain's API
// serves GraphQL extensions only, without JSON-RPC or REST, as the
// single-chain declared server does.
func TestMultiChainDeclaredMode(t *testing.T) {
	a, b := longReceipts(120), longReceipts(120)
	aSrv, bSrv := testchain.NewServer(a.Chain), testchain.NewServer(b.Chain)
	t.Cleanup(aSrv.Close)
	t.Cleanup(bSrv.Close)

	ea, eb := chainEntry("a", aSrv.URL()), chainEntry("b", bSrv.URL())
	ea.Features = chainRecords(t, a.Settlement.Hex(), "receipts")
	eb.Features = chainRecords(t, b.Other.Hex(), "other_receipts")
	root := filepath.Join(t.TempDir(), "db")
	cfg := multiChainConfig(t, root, ea, eb)
	declaredMode(cfg)
	require.NoError(t, cfg.Validate(), "declared mode is allowed in multichain mode")

	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(runCtx) }()
	for id, sc := range map[string]*testchain.ReceiptsScenario{"a": a, "b": b} {
		require.Eventually(t, func() bool {
			ci, err := app.multichainManager.GetChain(id)
			if err != nil {
				return false
			}
			h, ok := ci.IndexedHeight(ctx)
			return ok && h >= sc.Chain.Head()
		}, time.Minute, 20*time.Millisecond, "chain %s indexed", id)
	}

	tables := map[string]string{"a": "receipts", "b": "other_receipts"}
	contracts := map[string]string{"a": strings.ToLower(a.Settlement.Hex()), "b": strings.ToLower(b.Other.Hex())}
	router := app.apiServer.Router()
	for id, table := range tables {
		store, _, ok := app.multichainManager.ChainStore(id)
		require.True(t, ok)
		rs := store.(port.RecordReader)
		got, _, err := rs.ListRecords(ctx, table, port.FirstPage(100))
		require.NoError(t, err)
		require.NotEmpty(t, got, "chain %s holds its table", id)
		for _, r := range got {
			assert.Equal(t, contracts[id], strings.ToLower(r.Address.Hex()), "chain %s records only its contract", id)
		}
		for other, otherTable := range tables {
			if other != id {
				none, _, err := rs.ListRecords(ctx, otherTable, port.FirstPage(10))
				require.NoError(t, err)
				assert.Empty(t, none, "chain %s holds no table of chain %s", id, other)
			}
		}

		body := serve(t, router, http.MethodPost, "/chains/"+id+"/graphql", `{"query":"{ records(table: \"`+table+`\") { nodes { blockNumber } } }"}`)
		assert.Contains(t, body, "blockNumber", "chain %s serves its records", id)
		assert.NotContains(t, body, "errors")
		body = serve(t, router, http.MethodPost, "/chains/"+id+"/graphql", `{"query":"{ block(number: \"1\") { hash } }"}`)
		assert.Contains(t, body, "errors", "chain %s serves no explorer queries", id)
		for _, path := range []string{"/chains/" + id + "/rpc", "/chains/" + id + "/v1/blocks"} {
			rec := httptest.NewRecorder()
			method := http.MethodPost
			if strings.Contains(path, "/v1/") {
				method = http.MethodGet
			}
			router.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
			assert.Equal(t, http.StatusNotFound, rec.Code, "%s is not served", path)
		}
	}

	stop()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	app.Shutdown()
	for id := range tables {
		requireOnlyDeclaredData(t, chainDBPath(root, id))
	}
}

// TestChainFeatures: a chain's section replaces the shared one for that
// chain, keeps the shared enabled when it gives none, and leaves the shared
// sections unchanged.
func TestChainFeatures(t *testing.T) {
	on, off := true, false
	shared := map[string]config.FeatureConfig{"a": {Enabled: &on}, "b": {Enabled: &off}}
	got := chainFeatures(shared, map[string]config.FeatureConfig{"b": {}, "c": {Enabled: &on}})
	require.Len(t, got, 3)
	assert.True(t, *got["a"].Enabled)
	assert.False(t, *got["b"].Enabled, "no enabled in the chain's section: the shared one")
	assert.True(t, *got["c"].Enabled)
	got["a"] = config.FeatureConfig{Enabled: &off}
	assert.True(t, *shared["a"].Enabled, "the shared sections are not changed")
}

// TestChainFeaturesFromYAML: chains[].features is read from the
// configuration file like the top-level features, and each chain's App
// gets its own records declaration.
func TestChainFeaturesFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
database:
  path: `+filepath.Join(t.TempDir(), "db")+`
indexer:
  mode: declared
features:
  records:
    enabled: true
    sources: [{name: s, address: "0x00000000000000000000000000000000000000aa", events: ["Ping(uint256 n)"]}]
    tables: [{name: pings, source: s, event: Ping}]
multichain:
  enabled: true
  chains:
    - id: a
      rpc_endpoint: http://127.0.0.1:1
      chain_id: 1
      enabled: true
    - id: b
      rpc_endpoint: http://127.0.0.1:2
      chain_id: 2
      enabled: true
      features:
        records:
          sources: [{name: s, address: "0x00000000000000000000000000000000000000bb", events: ["Ping(uint256 n)"]}]
          tables: [{name: pings_b, source: s, event: Ping}]
`), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	app := &App{config: cfg}
	for id, want := range map[string]string{"a": "0x00000000000000000000000000000000000000aa", "b": "0x00000000000000000000000000000000000000bb"} {
		cc := cfg.MultiChain.Chains[0]
		if id == "b" {
			cc = cfg.MultiChain.Chains[1]
		}
		chainCfg := app.chainAppConfig(&multichain.ChainConfig{ID: cc.ID, RPCEndpoint: cc.RPCEndpoint, ChainID: cc.ChainID})
		var spec records.Spec
		require.NoError(t, chainCfg.FeatureSettings(records.Name, &spec))
		require.Len(t, spec.Sources, 1)
		assert.Equal(t, want, spec.Sources[0].Address, "chain %s", id)
		assert.True(t, chainCfg.FeatureOverrides()[records.Name], "chain %s keeps records enabled", id)
	}

}
