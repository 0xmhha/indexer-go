package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type nativeCallSnapshotKey struct{}
type nativeCallAPIProbe struct {
	*mockStorage
	query port.ExecutionCallQuery
	err   error
	reads int
	bound bool
}

func (p *nativeCallAPIProbe) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	p.reads++
	return fn(context.WithValue(ctx, nativeCallSnapshotKey{}, true))
}
func (p *nativeCallAPIProbe) ExecutionCalls(ctx context.Context, q port.ExecutionCallQuery) (port.ExecutionCallPage, error) {
	p.query = q
	p.bound = ctx.Value(nativeCallSnapshotKey{}) == true
	return port.ExecutionCallPage{Epoch: 1, Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Profile: "evm", Frames: []port.ExecutionCall{}}, p.err
}

func TestExecutionCallRoutesValidateAndUsePublicSnapshot(t *testing.T) {
	p := &nativeCallAPIProbe{mockStorage: &mockStorage{}}
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
	w := get("/execution/traces/blocks/1?limit=1")
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), `"epoch":1`)
	require.True(t, p.bound)
	require.EqualValues(t, 1, p.query.Height)
	require.EqualValues(t, 1, p.query.Limit)
	for _, suffix := range []string{"limit=0", "limit=257", "after=1", "hash=0x01", "epoch=1", "epoch=0", "limit=%ZZ", "limit=1&limit=2", "internal=maybe", "unknown=1"} {
		w = get("/execution/traces/blocks/1?" + suffix)
		require.Equal(t, 400, w.Code, suffix)
	}
	for _, key := range []string{"-1", "18446744073709551616", "xyz"} {
		require.Equal(t, 400, get("/execution/traces/blocks/"+key).Code)
	}
	var hash execwire.Hash
	hash[0] = 1
	text, e := hash.MarshalText()
	require.NoError(t, e)
	w = get("/execution/traces/transactions/" + string(text) + "?internal=true")
	require.Equal(t, 200, w.Code)
	require.Equal(t, &hash, p.query.Transaction)
	require.True(t, p.query.InternalOnly)
	for _, test := range []struct {
		err    error
		status int
	}{{port.ErrNotFound, 404}, {port.ErrExecutionStale, 409}, {port.ErrExecutionRewinding, 503}, {port.ErrExecutionUninitialized, 503}, {errors.New("private credential/path must never enter response"), 500}} {
		p.err = test.err
		w = get("/execution/traces/blocks/1")
		require.Equal(t, test.status, w.Code)
		require.NotContains(t, w.Body.String(), "private credential")
	}
}

func TestExecutionCallRouteMountsWithChainSnapshot(t *testing.T) {
	p := &nativeCallAPIProbe{mockStorage: &mockStorage{}}
	cfg := DefaultConfig()
	cfg.EnableRateLimit = false
	s, e := NewServerWithOptions(cfg, zap.NewNop(), nil, &ServerOptions{Chains: oneChain{store: p}})
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Stop(context.Background())) }()
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/chains/a/execution/traces/blocks/1?limit=1", nil))
	require.Equal(t, 200, w.Code)
	require.True(t, p.bound)
	require.Equal(t, 1, p.reads)
	require.EqualValues(t, 1, p.query.Height)
}
