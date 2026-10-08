package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExecutor records the last document and variables and answers with
// result.
type fakeExecutor struct {
	document  string
	variables map[string]interface{}
	result    *graphql.Result
}

func (f *fakeExecutor) Execute(_ context.Context, document string, variables map[string]interface{}) *graphql.Result {
	f.document, f.variables = document, variables
	return f.result
}

func serve(h http.Handler, method, target string, header http.Header) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Handle("/v1/*", h)
	req := httptest.NewRequest(method, target, nil)
	for k, vs := range header {
		req.Header[k] = vs
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

const addr = "0x00000000000000000000000000000000000AA001"

func TestPaths(t *testing.T) {
	got := Paths()
	sort.Strings(got)
	assert.Equal(t, []string{
		"addresses/{address}/balance", "addresses/{address}/overview", "addresses/{address}/tokens",
		"addresses/{address}/transactions", "blocks", "stats/miners", "stats/network", "transactions",
	}, got)
}

func TestVariables(t *testing.T) {
	exec := &fakeExecutor{result: &graphql.Result{Data: map[string]interface{}{"x": 1}}}
	h := NewHandler(exec)

	rec := serve(h, "GET", "/v1/blocks?limit=5&offset=10&numberFrom=1&miner="+addr, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, map[string]interface{}{"limit": 5, "offset": 10, "numberFrom": "1", "miner": addr}, exec.variables)
	assert.Contains(t, exec.document, "blocks(")

	rec = serve(h, "GET", "/v1/addresses/"+addr+"/transactions?after=abc", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, map[string]interface{}{"address": addr, "after": "abc"}, exec.variables)

	rec = serve(h, "GET", "/v1/transactions?type=2", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, map[string]interface{}{"type": 2}, exec.variables)
}

func TestBadRequests(t *testing.T) {
	exec := &fakeExecutor{result: &graphql.Result{Data: map[string]interface{}{"x": 1}}}
	h := NewHandler(exec)
	for target, want := range map[string]int{
		"/v1/blocks?limit=0":                                 http.StatusBadRequest,
		"/v1/blocks?limit=101":                               http.StatusBadRequest,
		"/v1/blocks?offset=-1":                               http.StatusBadRequest,
		"/v1/blocks?numberFrom=0x10":                         http.StatusBadRequest,
		"/v1/blocks?miner=nope":                              http.StatusBadRequest,
		"/v1/blocks?after=abc":                               http.StatusOK, // not a parameter of blocks: ignored
		"/v1/transactions?type=x":                            http.StatusBadRequest,
		"/v1/addresses/0x12/balance":                         http.StatusBadRequest,
		"/v1/addresses/" + addr + "/tokens?tokenType=ERC-20": http.StatusBadRequest,
		"/v1/stats/network?fromTime=1":                       http.StatusBadRequest,
		"/v1/nothing":                                        http.StatusNotFound,
		"/v1/addresses/" + addr + "/nothing":                 http.StatusNotFound,
		"/v1/addresses/" + addr:                              http.StatusNotFound,
	} {
		rec := serve(h, "GET", target, nil)
		assert.Equal(t, want, rec.Code, "%s: %s", target, rec.Body.String())
		if want != http.StatusOK {
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), target)
		}
	}
	rec := serve(h, "POST", "/v1/blocks", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestCaching(t *testing.T) {
	exec := &fakeExecutor{result: &graphql.Result{Data: map[string]interface{}{"blocks": map[string]interface{}{"totalCount": 3}}}}
	h := NewHandler(exec)

	rec := serve(h, "GET", "/v1/blocks", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":{"blocks":{"totalCount":3}}}`, rec.Body.String())
	assert.Equal(t, cacheControl, rec.Header().Get("Cache-Control"))
	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	rec = serve(h, "GET", "/v1/blocks", http.Header{"If-None-Match": {`"other", ` + etag}})
	assert.Equal(t, http.StatusNotModified, rec.Code)
	assert.Empty(t, rec.Body.String())

	rec = serve(h, "HEAD", "/v1/blocks", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, etag, rec.Header().Get("ETag"))

	exec.result = &graphql.Result{Data: map[string]interface{}{"blocks": map[string]interface{}{"totalCount": 4}}}
	rec = serve(h, "GET", "/v1/blocks", http.Header{"If-None-Match": {etag}})
	assert.Equal(t, http.StatusOK, rec.Code, "new data, new ETag")
	assert.NotEqual(t, etag, rec.Header().Get("ETag"))
}

func TestResolverErrors(t *testing.T) {
	failure := gqlerrors.FormatError(errors.New("storage unavailable"))
	exec := &fakeExecutor{result: &graphql.Result{Data: map[string]interface{}{"blocks": nil}, Errors: []gqlerrors.FormattedError{failure}}}
	h := NewHandler(exec)
	rec := serve(h, "GET", "/v1/blocks", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "storage unavailable")
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Empty(t, rec.Header().Get("ETag"))

	// A part of the answer failed: the rest is still served, not cached.
	exec.result = &graphql.Result{Data: map[string]interface{}{"blocks": map[string]interface{}{"totalCount": 1}}, Errors: []gqlerrors.FormattedError{failure}}
	rec = serve(h, "GET", "/v1/blocks", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}
