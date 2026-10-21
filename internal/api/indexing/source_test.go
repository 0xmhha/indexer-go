package indexing

import (
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSourceQueryAPIAdmissionAndRelease(t *testing.T) {
	store := &blockedTraceStore{entered: make(chan struct{}, 3), release: make(chan struct{})}
	h := SourceHandler(store, zap.NewNop())
	var wg sync.WaitGroup
	responses := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
	paths := []string{"/indexing/source/blocks/1/header", "/indexing/source/blocks/2/body"}
	for i, w := range responses {
		wg.Add(1)
		go func(i int, w *httptest.ResponseRecorder) {
			defer wg.Done()
			h.ServeHTTP(w, httptest.NewRequest("GET", paths[i], nil))
		}(i, w)
	}
	<-store.entered
	<-store.entered
	rejected := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(rejected, httptest.NewRequest("GET", paths[0], nil)); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(store.release)
		wg.Wait()
		t.Fatal("source query admission did not reject promptly")
	}
	require.Equal(t, 429, rejected.Code)
	require.Equal(t, "no-store", rejected.Header().Get("Cache-Control"))
	close(store.release)
	wg.Wait()
	for _, w := range responses {
		require.Equal(t, 503, w.Code)
	}
	next := httptest.NewRecorder()
	h.ServeHTTP(next, httptest.NewRequest("GET", paths[0], nil))
	require.Equal(t, 503, next.Code)
}

func TestSourceQueryPerChainRouting(t *testing.T) {
	store := &blockedTraceStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	close(store.release)
	h := SourceHandler(store, zap.NewNop())
	router := chi.NewRouter()
	router.Method("GET", "/chains/{id}/indexing/source/*", h)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/chains/arc/indexing/source/blocks/1/header", nil))
	require.Equal(t, 503, w.Code, w.Body.String())
}
