package evmblob

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

// This test-only RPC method demonstrates a relay adapter borrowing the SAME
// client as canonical reads. It is not an advertised Arc/Engine RPC method.
type rpcProvider struct{ client *rpc.Client }

func (p rpcProvider) Open(ctx context.Context, block model.FeedBlock, _ []Reference) (io.ReadCloser, error) {
	var raw hexutil.Bytes
	e := p.client.CallContext(ctx, &raw, "fixture_fullBlobs", block.Hash)
	if e != nil {
		return nil, e
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}
func TestBlobSharedFiveHTTPBudgetAndFreshFailover(t *testing.T) {
	db := database(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	f := blockFixture(t, []Entry{vector(false)})
	o := &oracle{block: f.id.Block}
	s, d := initialize(t, db, f, o)
	var mu sync.Mutex
	var times []time.Time
	var fail atomic.Bool
	fail.Store(true)
	wire := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		if fail.CompareAndSwap(true, false) {
			w.WriteHeader(503)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		var result any
		switch req.Method {
		case "fixture_fullBlobs":
			result = hexutil.Encode(f.raw)
		case "eth_getBlockByNumber":
			result = map[string]any{"hash": f.id.Block.Hash}
		default:
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	})
	primary := httptest.NewServer(wire)
	defer primary.Close()
	fallback := httptest.NewServer(wire)
	defer fallback.Close()
	pool, e := rpcpool.New(rpcpool.Config{Endpoints: []string{primary.URL, fallback.URL}, RateLimit: 5, Timeout: time.Second})
	require.NoError(t, e)
	defer pool.Close()
	client, e := rpc.DialOptions(t.Context(), pool.Primary(), rpc.WithHTTPClient(&http.Client{Transport: pool}))
	require.NoError(t, e)
	defer client.Close()
	o.client = client
	supplier, e := New(s, evm.New("evm"), rpcProvider{client: client})
	require.NoError(t, e)
	for i := 0; i < 4; i++ {
		_, e = supplier.Acquire(t.Context(), 1, f.id, d)
		require.NoError(t, e)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, times, 6, "one failed relay attempt, one retry, four fresh checks")
	for i := 1; i < len(times); i++ {
		require.GreaterOrEqual(t, times[i].Sub(times[i-1]), 180*time.Millisecond, "every physical attempt consumes the shared limiter")
	}
}
