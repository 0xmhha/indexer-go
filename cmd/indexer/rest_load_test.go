package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	gql "github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/api/rest"
)

// countedExecutor counts the GraphQL executions behind REST requests.
type countedExecutor struct {
	h    *graphql.Handler
	runs atomic.Int64
}

func (c *countedExecutor) Execute(ctx context.Context, doc string, vars map[string]interface{}) *gql.Result {
	c.runs.Add(1)
	return c.h.Execute(ctx, doc, vars)
}

// BenchmarkRESTPolling (refactoring plan R4-5) has many clients poll the
// latest 100 blocks of an indexed chain, with the handler keeping
// responses for a second (as served) and without.
//
//	go test ./cmd/indexer -run '^$' -bench BenchmarkRESTPolling
func BenchmarkRESTPolling(b *testing.B) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	app := startAppMode(b, srv, filepath.Join(b.TempDir(), "db"), atomicMode)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(b, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(b, err)

	for _, bc := range []struct {
		name string
		ttl  time.Duration
	}{{"kept", rest.DefaultCacheTTL}, {"uncached", 0}} {
		b.Run(bc.name, func(b *testing.B) {
			exec := &countedExecutor{h: h}
			r := chi.NewRouter()
			r.Handle("/v1/*", rest.NewHandlerWithCache(exec, bc.ttl))
			b.SetParallelism(16)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					rec := httptest.NewRecorder()
					r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/blocks?limit=100", nil))
					if rec.Code != http.StatusOK {
						b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
					}
				}
			})
			b.ReportMetric(float64(exec.runs.Load())/float64(b.N), "executions/op")
		})
	}
}
