package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net/http/httptest"
	"strings"
	"testing"
)

type executionProofProbe struct {
	*mockStorage
	query port.ExecutionProofQuery
	bound bool
	err   error
}

func (p *executionProofProbe) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return fn(context.WithValue(ctx, nativeCallSnapshotKey{}, true))
}
func (p *executionProofProbe) ExecutionProof(ctx context.Context, q port.ExecutionProofQuery) (port.ExecutionProofView, error) {
	p.query = q
	p.bound = ctx.Value(nativeCallSnapshotKey{}) == true
	return port.ExecutionProofView{Epoch: 1, Ref: execwire.Ref{Generation: 1, Height: q.Height, Hash: execwire.Hash{2}}, Root: execwire.Hash{3}, Digest: execwire.Hash{4}, SelectionHash: execwire.Hash{5}, Profile: "evm", Provider: "retained-state", Account: port.ExecutionProofAccount{Address: q.Address, Storage: []port.ExecutionProofSlot{}}, Proof: json.RawMessage(`{"accountProof":[]}`)}, p.err
}
func TestExecutionProofRoutesUseSnapshotAndStrictPins(t *testing.T) {
	p := &executionProofProbe{mockStorage: &mockStorage{}}
	cfg := DefaultConfig()
	cfg.EnableRateLimit = false
	s, e := NewServer(cfg, zap.NewNop(), p)
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Stop(context.Background())) }()
	path := "/execution/proofs/1/0x" + strings.Repeat("11", 20)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	w := get(path)
	require.Equal(t, 200, w.Code)
	require.True(t, p.bound)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), `"consensusBalance"`)
	require.Equal(t, 200, get("/execution/proofs/1/0x"+strings.Repeat("00", 19)+"2A").Code, "public API must accept standard checksum addresses")
	for _, suffix := range []string{"limit=1", "epoch=0", "epoch=1", "hash=0x00", "epoch=1&epoch=2", "unknown=1", "epoch=%ZZ"} {
		require.Equal(t, 400, get(path+"?"+suffix).Code, suffix)
	}
	for _, test := range []struct {
		err    error
		status int
	}{{port.ErrNotFound, 404}, {port.ErrExecutionStale, 409}, {port.ErrExecutionMaintenance, 503}, {port.ErrExecutionRewinding, 503}, {errors.New("private DSN"), 500}} {
		p.err = test.err
		w = get(path)
		require.Equal(t, test.status, w.Code)
		require.NotContains(t, w.Body.String(), "private DSN")
	}
}

func TestExecutionProofRoutesMountPerChain(t *testing.T) {
	p := &executionProofProbe{mockStorage: &mockStorage{}}
	cfg := DefaultConfig()
	cfg.EnableRateLimit = false
	s, e := NewServerWithOptions(cfg, zap.NewNop(), nil, &ServerOptions{Chains: oneChain{store: p}})
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Stop(context.Background())) }()
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/chains/a/execution/proofs/1/0x"+strings.Repeat("11", 20), nil))
	require.Equal(t, 200, w.Code)
	require.True(t, p.bound)
}
