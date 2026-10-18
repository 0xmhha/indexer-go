package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	graphqlhandler "github.com/graphql-go/handler"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Compare the new HTTP pipeline against the pinned upstream adapter, not an
// imitation of its decoding/validation/error formatting. Bounds here admit
// the documents; over-limit behavior has its own no-resolver assertions.
// JSON differs only by removed formatting whitespace; HTML stays byte-identical.
func TestSingleParseHTTPCompatibility(t *testing.T) {
	query := `{ latestHeight }`
	jsonBody, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, method, target, contentType, body, accept string
	}{
		{"json", "POST", "/graphql", "application/json", string(jsonBody), ""},
		{"json_charset", "POST", "/graphql", "application/json; charset=utf-8", string(jsonBody), ""},
		{"graphql_body", "POST", "/graphql", "application/graphql", query, ""},
		{"form", "POST", "/graphql", "application/x-www-form-urlencoded", url.Values{"query": {query}}.Encode(), ""},
		{"get", "GET", "/graphql?" + url.Values{"query": {query}}.Encode(), "", "", ""},
		{"url_precedence", "POST", "/graphql?" + url.Values{"query": {query}}.Encode(), "application/json", `{"query":"{ missingField }"}`, ""},
		{"operation_name", "POST", "/graphql", "application/json", `{"query":"query A { a: latestHeight } query B { b: latestHeight }","operationName":"B"}`, ""},
		{"operation_missing", "POST", "/graphql", "application/json", `{"query":"query A { a: latestHeight } query B { b: latestHeight }"}`, ""},
		{"variables_fragments", "POST", "/graphql", "application/json", `{"query":"query Q($n: String!) { alias: block(number:$n) { ...F } } fragment F on Block { number hash }","variables":{"n":"1"},"operationName":"Q"}`, ""},
		{"variables_string", "POST", "/graphql", "application/json", `{"query":"query Q($n: String!) { block(number:$n) { number } }","variables":"{\"n\":\"1\"}"}`, ""},
		{"missing_variable", "POST", "/graphql", "application/json", `{"query":"query Q($n: String!) { block(number:$n) { number } }"}`, ""},
		{"wrong_variable_type", "POST", "/graphql", "application/json", `{"query":"query Q($n: String!) { block(number:$n) { number } }","variables":{"n":true}}`, ""},
		{"syntax_error", "POST", "/graphql", "application/json", `{"query":"{ latestHeight"}`, ""},
		{"unknown_field", "POST", "/graphql", "application/json", `{"query":"{ missingField }"}`, ""},
		{"partial_error", "POST", "/graphql", "application/json", `{"query":"{ latestHeight broken: block(number:\"bad value\") { number } }"}`, ""},
		{"invalid_json", "POST", "/graphql", "application/json", `{`, ""},
		{"missing_query", "GET", "/graphql", "", "", ""},
		{"html", "GET", "/graphql", "", "", "text/html"},
		{"html_query", "POST", "/chains/5042002/graphql", "application/json", string(jsonBody), "text/html"},
		{"raw_html", "POST", "/graphql?raw", "application/json", string(jsonBody), "text/html"},
		{"prefer_json", "POST", "/graphql", "application/json", string(jsonBody), "text/html, application/json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, bounded := range []bool{false, true} {
				h := newTestHandler(t)
				if bounded {
					h.limits = Limits{MaxDepth: 8, MaxComplexity: 2000}
				}
				legacy := graphqlhandler.New(&graphqlhandler.Config{Schema: &h.schema.schema, Pretty: true, Playground: true})
				request := func() *http.Request {
					r := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
					r.Header.Set("Content-Type", tc.contentType)
					r.Header.Set("Accept", tc.accept)
					return r
				}
				old, current := httptest.NewRecorder(), httptest.NewRecorder()
				legacy.ServeHTTP(old, request())
				seen := make(map[string]int)
				r := request()
				r = r.WithContext(ObserveRequest(r.Context(), func(o RequestObservation) { seen[o.Stage]++ }))
				h.ServeHTTP(current, r)
				require.Equal(t, old.Code, current.Code)
				require.Equal(t, old.Header().Get("Content-Type"), current.Header().Get("Content-Type"))
				if strings.HasPrefix(old.Header().Get("Content-Type"), "application/json") {
					var compact bytes.Buffer
					require.NoError(t, json.Compact(&compact, old.Body.Bytes()))
					require.Equal(t, compact.String(), current.Body.String(), "only JSON formatting changes, bounded=%v", bounded)
					if tc.name == "partial_error" {
						require.Contains(t, current.Body.String(), `"latestHeight":"100"`)
						require.Contains(t, current.Body.String(), `"errors":[`)
						require.Contains(t, current.Body.String(), `"path":["broken"]`)
					}
				} else {
					require.Equal(t, old.Body.String(), current.Body.String(), "Playground remains unchanged, bounded=%v", bounded)
				}
				require.Equal(t, 1, seen["request_prepare"])
				require.Equal(t, 1, seen["parse"], "one request-local parse with or without bounds")
			}
		})
	}
}

type requestStore struct {
	port.QueryStore
	calls    atomic.Int32
	entered  chan context.Context
	finished chan struct{}
}

func (s *requestStore) GetBlock(ctx context.Context, number uint64) (*model.Block, error) {
	s.calls.Add(1)
	return s.QueryStore.GetBlock(ctx, number)
}

func (s *requestStore) GetLatestHeight(ctx context.Context) (uint64, error) {
	s.calls.Add(1)
	if s.entered != nil {
		s.entered <- ctx
		<-ctx.Done()
		close(s.finished)
		return 0, ctx.Err()
	}
	return 100, nil
}

func TestSingleParseBoundsAndValidationBeforeResolvers(t *testing.T) {
	for _, tc := range []struct {
		name, query, code string
		limits            Limits
	}{
		{"complexity", `{ a: latestHeight b: latestHeight }`, "QUERY_TOO_COMPLEX", Limits{MaxComplexity: 1}},
		{"fragment_depth", `{ block(number:"1") { ...F } } fragment F on Block { number }`, "QUERY_TOO_DEEP", Limits{MaxDepth: 1}},
		{"fragment_cycle", `{ ...A } fragment A on Query { ...A }`, "", Limits{MaxDepth: 8}},
		{"fragment_cycle_without_limits", `{ ...A } fragment A on Query { ...A }`, "", Limits{}},
		{"indirect_fragment_cycle", `{ ...A } fragment A on Query { ...B } fragment B on Query { ...A }`, "", Limits{MaxDepth: 8}},
		{"standard_validation", `{ latestHeight missingField }`, "", Limits{MaxDepth: 8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := newTestHandler(t)
			store := &requestStore{QueryStore: base.schema.storage}
			h, err := NewHandler(store, zap.NewNop())
			require.NoError(t, err)
			h.limits = tc.limits
			out := limitResponse(t, h, jsonRequest(t, tc.query, nil))
			require.NotEmpty(t, out["errors"])
			if strings.Contains(tc.name, "cycle") {
				require.Contains(t, out["errors"].([]interface{})[0].(map[string]interface{})["message"], "Cannot spread fragment")
			}
			require.Equal(t, tc.code, errorCode(out))
			require.Zero(t, store.calls.Load())
		})
	}
}

type requestContextKey struct{}

func TestSingleParseCancellation(t *testing.T) {
	for _, preCanceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "during_execution", true: "before_execution"}[preCanceled], func(t *testing.T) {
			base := newTestHandler(t)
			store := &requestStore{QueryStore: base.schema.storage, entered: make(chan context.Context, 1), finished: make(chan struct{})}
			h, err := NewHandler(store, zap.NewNop())
			require.NoError(t, err)
			h.limits = Limits{MaxDepth: 4, MaxComplexity: 100}
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), requestContextKey{}, "kept"))
			defer cancel()
			if preCanceled {
				cancel()
			}
			w := httptest.NewRecorder()
			r := jsonRequest(t, `{ latestHeight }`, nil).WithContext(ctx)
			done := make(chan struct{})
			go func() { defer close(done); h.ServeHTTP(w, r) }()
			watchdog := time.NewTimer(10 * time.Second) // hang protection, not a latency assertion
			defer watchdog.Stop()
			select {
			case got := <-store.entered:
				require.Equal(t, "kept", got.Value(requestContextKey{}))
			case <-watchdog.C:
				t.Fatal("resolver did not receive the request context")
			}
			cancel()
			select {
			case <-done:
			case <-watchdog.C:
				t.Fatal("handler ignored cancellation")
			}
			select {
			case <-store.finished:
			case <-watchdog.C:
				t.Fatal("resolver did not stop")
			}
			require.Contains(t, w.Body.String(), context.Canceled.Error())
		})
	}
}

type failingRequestBody struct{ closed bool }

func (*failingRequestBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b *failingRequestBody) Close() error           { b.closed = true; return nil }

func TestSingleParseBodyFailureWithURLPrecedence(t *testing.T) {
	h := newTestHandler(t)
	h.limits = Limits{MaxDepth: 4}
	for _, target := range []string{"/graphql", "/graphql?" + url.Values{"query": {`{ latestHeight }`}}.Encode()} {
		body := &failingRequestBody{}
		r := httptest.NewRequest("POST", target, nil)
		r.Body = body
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "failed to read the request\n", w.Body.String())
		require.True(t, body.closed)
	}
}
