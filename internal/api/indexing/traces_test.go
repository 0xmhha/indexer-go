package indexing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type blockedTraceStore struct {
	port.QueryStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockedTraceStore) ReadIndexingPage(ctx context.Context, _ string, _ int) (port.IndexingPage, error) {
	s.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return port.IndexingPage{}, ctx.Err()
	case <-s.release:
		return port.IndexingPage{}, indexingstatus.ErrUnavailable
	}
}
func (s *blockedTraceStore) CaptureView(context.Context, model.FeedChain, func(port.CaptureReader) error) error {
	return port.ErrInvalidData
}
func (s *blockedTraceStore) CaptureUpdate(context.Context, model.FeedChain, func(port.CaptureWriter) error) error {
	return port.ErrInvalidData
}
func traceRequest() *http.Request {
	r := httptest.NewRequest("GET", "/indexing/traces/blocks/1", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("height", "1")
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
}
func TestCallTraceAPIAdmissionAndRelease(t *testing.T) {
	store := &blockedTraceStore{entered: make(chan struct{}, 3), release: make(chan struct{})}
	h := TraceHandler(store, zap.NewNop())
	var wg sync.WaitGroup
	responses := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
	for _, w := range responses {
		wg.Add(1)
		go func(w *httptest.ResponseRecorder) { defer wg.Done(); h.ServeHTTP(w, traceRequest()) }(w)
	}
	<-store.entered
	<-store.entered
	rejected := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(rejected, traceRequest()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(store.release)
		wg.Wait()
		t.Fatal("excess trace query was not rejected promptly")
	}
	require.Equal(t, 429, rejected.Code)
	require.Equal(t, "no-store", rejected.Header().Get("Cache-Control"))
	close(store.release)
	wg.Wait()
	for _, w := range responses {
		require.Equal(t, 503, w.Code)
	}
	next := httptest.NewRecorder()
	h.ServeHTTP(next, traceRequest())
	require.Equal(t, 503, next.Code, "slots must be returned after failed reads")
}
