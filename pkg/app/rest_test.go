package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// frontendDocumentNamed returns indexer-frontend's GraphQL document of an
// operation.
func frontendDocumentNamed(t *testing.T, operation string) string {
	t.Helper()
	raw, err := os.ReadFile(frontendDocuments)
	require.NoError(t, err)
	var docs []frontendDocument
	require.NoError(t, json.Unmarshal(raw, &docs))
	name := regexp.MustCompile(`\bquery\s+` + operation + `\b`)
	for _, d := range docs {
		if name.MatchString(d.Document) {
			return d.Document
		}
	}
	t.Fatalf("indexer-frontend has no operation %s", operation)
	return ""
}

// withoutEndCursor removes pageInfo.endCursor, which the REST responses add
// to the frontend's field sets.
func withoutEndCursor(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if k == "pageInfo" {
				if pi, ok := e.(map[string]any); ok {
					delete(pi, "endCursor")
				}
			}
			withoutEndCursor(e)
		}
	case []any:
		for _, e := range x {
			withoutEndCursor(e)
		}
	}
	return v
}

func asJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// TestRESTMatchesFrontendQueries (refactoring plan R4-4): every REST path
// answers what indexer-frontend's GraphQL query of the same data answers,
// for an indexed chain.
func TestRESTMatchesFrontendQueries(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	app := startAppMode(t, srv, dir, atomicMode)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	cfg := api.DefaultConfig()
	cfg.EnableRateLimit = false
	server, err := api.NewServer(cfg, zap.NewNop(), app.storage)
	require.NoError(t, err)
	httpSrv := httptest.NewServer(server.Router())
	defer httpSrv.Close()
	gql, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)

	// An address with transactions.
	res := gql.ExecuteQuery(`{ transactions(pagination: {limit: 1}) { nodes { from } } }`, nil)
	require.Empty(t, res.Errors)
	nodes := res.Data.(map[string]any)["transactions"].(map[string]any)["nodes"].([]any)
	require.NotEmpty(t, nodes)
	addr := nodes[0].(map[string]any)["from"].(string)

	cases := []struct {
		path      string
		operation string
		variables map[string]any
	}{
		{"/v1/blocks?limit=5&offset=1", "GetBlocks", map[string]any{"limit": 5, "offset": 1}},
		{"/v1/blocks?numberFrom=2&numberTo=4", "GetBlocks", map[string]any{"numberFrom": "2", "numberTo": "4"}},
		{"/v1/transactions?limit=20", "GetTransactions", map[string]any{"limit": 20}},
		{"/v1/transactions?from=" + addr, "GetTransactions", map[string]any{"from": addr}},
		{"/v1/addresses/" + addr + "/transactions?limit=10", "GetTransactionsByAddress", map[string]any{"address": addr, "limit": 10}},
		{"/v1/addresses/" + addr + "/balance", "GetAddressBalance", map[string]any{"address": addr}},
		{"/v1/addresses/" + addr + "/overview", "GetAddressOverview", map[string]any{"address": addr}},
		{"/v1/addresses/" + addr + "/tokens", "GetTokenBalances", map[string]any{"address": addr}},
		{"/v1/stats/miners?limit=3", "GetTopMiners", map[string]any{"limit": 3}},
		{"/v1/stats/network?fromTime=0&toTime=4000000000", "GetNetworkMetrics", map[string]any{"fromTime": "0", "toTime": "4000000000"}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			want := gql.ExecuteQuery(frontendDocumentNamed(t, tc.operation), tc.variables)
			require.Empty(t, want.Errors)

			resp, err := http.Get(httpSrv.URL + tc.path)
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
			var got map[string]any
			require.NoError(t, json.Unmarshal(body, &got))
			assert.Nil(t, got["errors"])
			assert.Equal(t, asJSON(t, want.Data), withoutEndCursor(got["data"]))
			assert.NotContains(t, string(body), `"nodes":[]`, "the scenario has data for every path")

			// A client holding the response revalidates it without a body.
			req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+tc.path, nil)
			req.Header.Set("If-None-Match", resp.Header.Get("ETag"))
			again, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			_ = again.Body.Close()
			assert.Equal(t, http.StatusNotModified, again.StatusCode)
		})
	}

	// An address's transactions continue from the cursor of the previous
	// page.
	path := httpSrv.URL + "/v1/addresses/" + addr + "/transactions?limit=1"
	first := restGet(t, path)["transactionsByAddress"].(map[string]any)
	cursor, _ := first["pageInfo"].(map[string]any)["endCursor"].(string)
	require.NotEmpty(t, cursor)
	next := restGet(t, path+"&after="+url.QueryEscape(cursor))["transactionsByAddress"].(map[string]any)
	byOffset := restGet(t, path+"&offset=1")["transactionsByAddress"].(map[string]any)
	assert.Equal(t, byOffset["nodes"], next["nodes"])
}

func restGet(t *testing.T, target string) map[string]any {
	t.Helper()
	resp, err := http.Get(target)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Data   map[string]any `json:"data"`
		Errors []any          `json:"errors"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Empty(t, out.Errors)
	require.NotNil(t, out.Data)
	return out.Data
}
