package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	h := NewHandlerWithCache(exec, 0)

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

// countingExecutor counts executions and holds each until release is
// closed (when set).
type countingExecutor struct {
	runs    atomic.Int64
	release chan struct{}
}

func (c *countingExecutor) Execute(ctx context.Context, _ string, _ map[string]interface{}) *graphql.Result {
	n := c.runs.Add(1)
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
		}
	}
	return &graphql.Result{Data: map[string]interface{}{"run": n}}
}

// TestRequestsShareExecutions (refactoring plan R4-5): requests arriving
// together share one execution, later ones within the TTL are answered
// from the kept response, and after the TTL the path runs again.
func TestRequestsShareExecutions(t *testing.T) {
	exec := &countingExecutor{release: make(chan struct{})}
	h := NewHandlerWithCache(exec, 100*time.Millisecond)

	const clients = 100
	var wg sync.WaitGroup
	bodies := make(chan string, clients)
	for range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := serve(h, "GET", "/v1/blocks?limit=10", nil)
			bodies <- rec.Body.String()
		}()
	}
	require.Eventually(t, func() bool { return exec.runs.Load() == 1 }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let the others join
	close(exec.release)
	wg.Wait()
	close(bodies)
	for b := range bodies {
		assert.JSONEq(t, `{"data":{"run":1}}`, b)
	}
	assert.Equal(t, int64(1), exec.runs.Load())

	// Kept: same path and parameters (in any order or with ignored ones).
	serve(h, "GET", "/v1/blocks?limit=10&unused=1", nil)
	assert.Equal(t, int64(1), exec.runs.Load())
	// Other parameters are another response.
	serve(h, "GET", "/v1/blocks?limit=11", nil)
	assert.Equal(t, int64(2), exec.runs.Load())

	time.Sleep(150 * time.Millisecond)
	rec := serve(h, "GET", "/v1/blocks?limit=10", nil)
	assert.JSONEq(t, `{"data":{"run":3}}`, rec.Body.String(), "expired: runs again")
}

// TestFailuresAreNotKept: a response with errors is executed again by the
// next request.
func TestFailuresAreNotKept(t *testing.T) {
	failure := gqlerrors.FormatError(errors.New("storage unavailable"))
	exec := &fakeExecutor{result: &graphql.Result{Data: map[string]interface{}{"blocks": nil}, Errors: []gqlerrors.FormattedError{failure}}}
	h := NewHandler(exec)
	assert.Equal(t, http.StatusInternalServerError, serve(h, "GET", "/v1/blocks", nil).Code)
	exec.result = &graphql.Result{Data: map[string]interface{}{"blocks": map[string]interface{}{"totalCount": 1}}}
	assert.Equal(t, http.StatusOK, serve(h, "GET", "/v1/blocks", nil).Code)
}

// TestWaitingRequestLeaves: a request whose context ends stops waiting for
// a shared execution, which completes for the others.
func TestWaitingRequestLeaves(t *testing.T) {
	exec := &countingExecutor{release: make(chan struct{})}
	h := NewHandler(exec)
	r := chi.NewRouter()
	r.Handle("/v1/*", h)

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/blocks", nil).WithContext(ctx))
		first <- rec.Code
	}()
	require.Eventually(t, func() bool { return exec.runs.Load() == 1 }, 5*time.Second, time.Millisecond)
	second := make(chan string, 1)
	go func() { second <- serve(h, "GET", "/v1/blocks", nil).Body.String() }()
	cancel()
	assert.Equal(t, http.StatusInternalServerError, <-first)
	close(exec.release)
	assert.JSONEq(t, `{"data":{"run":1}}`, <-second)
}
