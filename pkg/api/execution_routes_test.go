package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type routeSnapshotKey struct{}
type routeSnapshotProbe struct {
	port.QueryStore
	calls int
	fail  error
}

func (s *routeSnapshotProbe) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	s.calls++
	if s.fail != nil {
		return s.fail
	}
	return fn(context.WithValue(ctx, routeSnapshotKey{}, true))
}
func TestExecutionRegisteredReadRouteUsesSnapshot(t *testing.T) {
	store := &routeSnapshotProbe{}
	route := Route{Method: http.MethodGet, Handler: func(port.QueryStore, *zap.Logger) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(routeSnapshotKey{}) != true {
				http.Error(w, "missing snapshot", 500)
				return
			}
			w.WriteHeader(204)
		})
	}}
	s := &Server{storage: store, logger: zap.NewNop()}
	handler := s.readRoute(route)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 204, rr.Code)
	require.Equal(t, 1, store.calls)
	store.fail = port.ErrExecutionRewinding
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 503, rr.Code)
}

type chainLeaseKey struct{}
type chainReadProbe struct {
	*mockStorage
	reserved, closed int
}

func (s *chainReadProbe) GetLatestHeight(context.Context) (uint64, error) { return 9, nil }
func (s *chainReadProbe) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Value(chainLeaseKey{}) != true {
		return errors.New("missing chain reservation context")
	}
	return fn(ctx)
}
func (s *chainReadProbe) ReserveQueryReader(context.Context) (port.QueryReaderLease, error) {
	s.reserved++
	return &chainReadLease{store: s}, nil
}

type chainReadLease struct {
	store *chainReadProbe
	once  sync.Once
}

func (l *chainReadLease) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, chainLeaseKey{}, true)
}
func (l *chainReadLease) Close() { l.once.Do(func() { l.store.closed++ }) }
func TestExecutionChainAPIUsesOwnReservedReader(t *testing.T) {
	store := &chainReadProbe{mockStorage: &mockStorage{}}
	cfg := DefaultConfig()
	cfg.ReserveQueryReader = true
	cfg.EnableRateLimit = false
	s, err := NewServerWithOptions(cfg, zap.NewNop(), nil, &ServerOptions{Chains: oneChain{store: store}})
	require.NoError(t, err)
	stop := sync.OnceFunc(func() { require.NoError(t, s.Stop(context.Background())) })
	defer stop()
	req := httptest.NewRequest("POST", "/chains/a/graphql", strings.NewReader(`{"query":"{latestHeight}"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.Router().ServeHTTP(rr, req)
	require.NotContains(t, rr.Body.String(), `"errors"`)
	require.Equal(t, 1, store.reserved)
	stop()
	require.Equal(t, 1, store.closed)
}
