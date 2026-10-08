package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/constants"
)

func cost(t *testing.T, query string, variables map[string]interface{}) (int, int) {
	t.Helper()
	doc, err := parser.Parse(parser.ParseParams{Source: query})
	require.NoError(t, err)
	return QueryCost(doc, variables)
}

func TestQueryCost(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		variables  map[string]interface{}
		depth, cpx int
	}{
		{"scalar", `{ latestHeight }`, nil, 1, 1},
		{"page", `{ blocks(pagination: {limit: 100}) { totalCount nodes { number hash } } }`, nil, 3, 1 + 100*(1+1+2)},
		{"page from a variable", `query($p: PaginationInput) { blocks(pagination: $p) { nodes { number } } }`,
			map[string]interface{}{"p": map[string]interface{}{"limit": float64(50)}}, 3, 1 + 50*2},
		{"limit argument from a variable", `query($n: Int) { a: logs(limit: $n) { data } }`,
			map[string]interface{}{"n": float64(20)}, 2, 1 + 20},
		{"page size is capped", `{ blocks(pagination: {limit: 99999999999999999999}) { number } }`, nil, 2, 1 + maxListMultiplier},
		{"nested pages multiply", `{ blocks(pagination: {limit: 100}) { nodes { transactions(pagination: {limit: 100}) { nodes { hash } } } } }`,
			nil, 5, 1 + 100*(1+1+100*(1+1))},
		{"aliases add up", `{ a: latestHeight b: latestHeight c: latestHeight }`, nil, 1, 3},
		{"fragments", `{ block(number: 1) { ...F ... on Block { hash } } } fragment F on Block { number parent { number } }`,
			nil, 3, 1 + 1 + 2 + 1},
		{"fragment cycle", `{ block(number: 1) { ...A } } fragment A on Block { number ...B } fragment B on Block { hash ...A }`,
			nil, 2, 3},
		{"costliest operation", `query A { latestHeight } query B { block(number: 1) { number hash } }`, nil, 2, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, c := cost(t, tc.query, tc.variables)
			assert.Equal(t, tc.depth, d, "depth")
			assert.Equal(t, tc.cpx, c, "complexity")
		})
	}

	// Saturates instead of overflowing.
	q := `{ a` + strings.Repeat(`(pagination: {limit: 1000}) { a`, 8) + strings.Repeat(` }`, 8) + ` }`
	_, c := cost(t, q, nil)
	assert.Positive(t, c)

	// The default bounds admit the introspection query of GraphQL tools.
	d, c := cost(t, testutil.IntrospectionQuery, nil)
	assert.LessOrEqual(t, d, constants.DefaultGraphQLMaxDepth)
	assert.LessOrEqual(t, c, constants.DefaultGraphQLMaxComplexity)
}

// limitResponse posts query to a handler with limits and returns the
// decoded response.
func limitResponse(t *testing.T, h *Handler, req *http.Request) map[string]interface{} {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

func errorCode(out map[string]interface{}) string {
	errs, _ := out["errors"].([]interface{})
	if len(errs) == 0 {
		return ""
	}
	e, _ := errs[0].(map[string]interface{})
	ext, _ := e["extensions"].(map[string]interface{})
	code, _ := ext["code"].(string)
	return code
}

func jsonRequest(t *testing.T, query string, variables map[string]interface{}) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"query": query, "variables": variables})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestHandlerLimits: requests over a bound are refused with a coded error,
// whichever way the request is sent; requests within run as before.
func TestHandlerLimits(t *testing.T) {
	h := newTestHandler(t)
	h.limits = Limits{MaxDepth: 4, MaxComplexity: 500}

	out := limitResponse(t, h, jsonRequest(t, `{ latestHeight }`, nil))
	assert.Empty(t, out["errors"])
	assert.Equal(t, map[string]interface{}{"latestHeight": "100"}, out["data"])

	deep := `{ block(number: 1) { transactions { nodes { block { number } } } } }`
	out = limitResponse(t, h, jsonRequest(t, deep, nil))
	assert.Equal(t, "QUERY_TOO_DEEP", errorCode(out))
	assert.Nil(t, out["data"])

	wide := `query($p: PaginationInput) { blocks(pagination: $p) { nodes { number hash } } }`
	out = limitResponse(t, h, jsonRequest(t, wide, map[string]interface{}{"p": map[string]interface{}{"limit": 300}}))
	assert.Equal(t, "QUERY_TOO_COMPLEX", errorCode(out))
	out = limitResponse(t, h, jsonRequest(t, wide, map[string]interface{}{"p": map[string]interface{}{"limit": 10}}))
	assert.Empty(t, errorCode(out), "the same query with a small page runs")

	// application/graphql body, form body and query string.
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(deep))
	req.Header.Set("Content-Type", "application/graphql")
	assert.Equal(t, "QUERY_TOO_DEEP", errorCode(limitResponse(t, h, req)))

	req = httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(url.Values{"query": {deep}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	assert.Equal(t, "QUERY_TOO_DEEP", errorCode(limitResponse(t, h, req)))

	req = httptest.NewRequest(http.MethodGet, "/graphql?"+url.Values{"query": {deep}}.Encode(), nil)
	assert.Equal(t, "QUERY_TOO_DEEP", errorCode(limitResponse(t, h, req)))

	// A syntax error is reported by the GraphQL handler as before.
	out = limitResponse(t, h, jsonRequest(t, `{ latestHeight `, nil))
	assert.NotEmpty(t, out["errors"])
	assert.Empty(t, errorCode(out))
}
