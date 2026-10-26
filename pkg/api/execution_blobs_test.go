package api

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net/http/httptest"
	"testing"
)

type executionBlobProbe struct {
	*mockStorage
	query port.ExecutionBlobQuery
	err   error
	bound bool
}

func (p *executionBlobProbe) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return fn(context.WithValue(ctx, nativeCallSnapshotKey{}, true))
}
func (p *executionBlobProbe) ExecutionBlobs(ctx context.Context, q port.ExecutionBlobQuery) (port.ExecutionBlobPage, error) {
	p.query = q
	p.bound = ctx.Value(nativeCallSnapshotKey{}) == true
	return port.ExecutionBlobPage{Epoch: 1, Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Availability: "no_blob_references", Entries: []port.ExecutionBlob{}, References: []port.ExecutionBlobReference{}}, p.err
}
func TestExecutionBlobRoutesUseSnapshotAndStrictBoundedPages(t *testing.T) {
	p := &executionBlobProbe{mockStorage: &mockStorage{}}
	cfg := DefaultConfig()
	cfg.EnableRateLimit = false
	s, e := NewServer(cfg, zap.NewNop(), p)
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Stop(context.Background())) }()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	w := get("/execution/blobs/blocks/1?limit=1")
	require.Equal(t, 200, w.Code)
	require.True(t, p.bound)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), `"entries":[]`)
	for _, suffix := range []string{"limit=0", "limit=5", "after=1", "epoch=0", "epoch=1", "limit=1&limit=2", "limit=%ZZ", "unknown=1", "internal=false", "after=33"} {
		require.Equal(t, 400, get("/execution/blobs/blocks/1?"+suffix).Code, suffix)
	}
	for _, test := range []struct {
		err    error
		status int
	}{{port.ErrNotFound, 404}, {port.ErrExecutionStale, 409}, {port.ErrExecutionMaintenance, 503}, {port.ErrExecutionRewinding, 503}, {errors.New("private DSN must not leak"), 500}} {
		p.err = test.err
		w = get("/execution/blobs/blocks/1")
		require.Equal(t, test.status, w.Code)
		require.NotContains(t, w.Body.String(), "private DSN")
	}
}

func TestExecutionBlobRoutesMountPerChain(t *testing.T) {
	p := &executionBlobProbe{mockStorage: &mockStorage{}}
	cfg := DefaultConfig()
	cfg.EnableRateLimit = false
	s, e := NewServerWithOptions(cfg, zap.NewNop(), nil, &ServerOptions{Chains: oneChain{store: p}})
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Stop(context.Background())) }()
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/chains/a/execution/blobs/blocks/1", nil))
	require.Equal(t, 200, w.Code)
	require.True(t, p.bound)
	require.EqualValues(t, 1, p.query.Limit)
}
