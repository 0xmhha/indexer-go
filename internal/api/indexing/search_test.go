package indexing

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSourceSearchAdmissionSharedWithHashLookup(t *testing.T) {
	store := &blockedTraceStore{entered: make(chan struct{}, 3), release: make(chan struct{})}
	h := SourceHandler(store, zap.NewNop())
	paths := []string{"/indexing/source/logs?from=1&to=2", "/indexing/source/blocks/1/header"}
	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
		}(path)
	}
	<-store.entered
	<-store.entered
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, httptest.NewRequest("GET", paths[0], nil)); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(store.release)
		wg.Wait()
		t.Fatal("source search did not share admission")
	}
	require.Equal(t, 429, w.Code)
	close(store.release)
	wg.Wait()
	next := httptest.NewRecorder()
	h.ServeHTTP(next, httptest.NewRequest("GET", paths[0], nil))
	require.Equal(t, 503, next.Code)
}

func TestSourceSearchORAndTimeRequestBounds(t *testing.T) {
	a, b := common.Address{1}.Hex(), common.Address{}.Hex()
	h, z := common.Hash{1}.Hex(), common.Hash{}.Hex()
	base := "/indexing/source/logs?from=1&to=2"
	first, e := parseSourceSearch(httptest.NewRequest("GET", base+"&address="+a+","+b+"&topic1="+h+","+z+"&from_time=0&to_time=18446744073709551615", nil), "logs")
	require.NoError(t, e)
	second, e := parseSourceSearch(httptest.NewRequest("GET", base+"&address="+b+","+a+","+b+"&topic1="+z+","+h+"&from_time=0&to_time=18446744073709551615", nil), "logs")
	require.NoError(t, e)
	require.Equal(t, first.filter.Digest(), second.filter.Digest())
	for _, suffix := range []string{
		"&address=", "&address=" + a + ",", "&address=" + strings.Repeat(a+",", 16) + a,
		"&topic0=" + strings.Repeat(h+",", 16) + h, "&topic1=," + z,
		"&from_time=", "&from_time=-1", "&from_time=18446744073709551616", "&from_time=2&to_time=1",
		"&from_time=1&from_time=2", "&to_time=x", "&" + strings.Repeat("x", 8192),
	} {
		_, e := parseSourceSearch(httptest.NewRequest("GET", base+suffix, nil), "logs")
		require.ErrorIs(t, e, port.ErrInvalidData, suffix)
	}
	_, e = parseSourceSearch(httptest.NewRequest("GET", "/indexing/source/transactions?from=1&to=2&address="+a+","+b+"&role=from&from_time=0", nil), "transactions")
	require.NoError(t, e)
}
func TestSourceSearchRequestBoundsAndRouting(t *testing.T) {
	store := &blockedTraceStore{entered: make(chan struct{}, 32), release: make(chan struct{})}
	close(store.release)
	h := SourceHandler(store, zap.NewNop())
	router := chi.NewRouter()
	router.Method("GET", "/chains/{id}/indexing/source/*", h)
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/chains/arc/indexing/source/logs?from=1&to=2", 503},
		{"/chains/bsc/indexing/source/transactions?from=1&to=2", 503},
		{"/chains/arc/indexing/source/logs?to=2", 400},
		{"/chains/arc/indexing/source/logs?from=1&to=2&from=3", 400},
		{"/chains/arc/indexing/source/logs?from=1&to=2&limit=257", 400},
		{"/chains/arc/indexing/source/logs?from=1&to=2&role=from", 400},
		{"/chains/arc/indexing/source/logs?from=1&to=2&topic4=0x0", 400},
		{"/chains/arc/indexing/source/logs?from=1&to=2&after=", 400},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		require.Equal(t, tc.code, w.Code, w.Body.String())
	}
}

type corruptSourceSearchStore struct{ blockedTraceStore }

func (s *corruptSourceSearchStore) ReadIndexingPage(context.Context, string, int) (port.IndexingPage, error) {
	return port.IndexingPage{}, errors.Join(capture.ErrConflict, port.ErrNotFound)
}
func TestSourceSearchCorruptionIsNotAbsence(t *testing.T) {
	h := SourceHandler(&corruptSourceSearchStore{}, zap.NewNop())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/indexing/source/logs?from=1&to=2", nil))
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "found")
}
