package rpcpool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// node is a JSON-RPC endpoint answering eth_blockNumber with its id, or
// with an HTTP status when status is set.
type node struct {
	id     int
	status atomic.Int32
	calls  atomic.Int32
	srv    *httptest.Server
}

func newNode(t *testing.T, id int) *node {
	n := &node{id: id}
	n.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.calls.Add(1)
		if s := n.status.Load(); s != 0 {
			w.WriteHeader(int(s))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%x"}`, n.id)
	}))
	t.Cleanup(n.srv.Close)
	return n
}

// call sends eth_blockNumber through the pool and returns the answering node.
func call(t *testing.T, p *Pool) (int, error) {
	t.Helper()
	c, err := gethrpc.DialOptions(context.Background(), p.Primary(), gethrpc.WithHTTPClient(&http.Client{Transport: p}))
	require.NoError(t, err)
	defer c.Close()
	var res string
	if err := c.Call(&res, "eth_blockNumber"); err != nil {
		return 0, err
	}
	var id int
	_, err = fmt.Sscanf(res, "0x%x", &id)
	return id, err
}

func newPool(t *testing.T, cfg Config, nodes ...*node) (*Pool, *time.Time) {
	t.Helper()
	for _, n := range nodes {
		cfg.Endpoints = append(cfg.Endpoints, n.srv.URL)
	}
	p, err := New(cfg)
	require.NoError(t, err)
	now := time.Unix(1_700_000_000, 0)
	p.now = func() time.Time { return now }
	return p, &now
}

func TestUsesPrimaryWhileHealthy(t *testing.T) {
	a, b := newNode(t, 1), newNode(t, 2)
	p, _ := newPool(t, Config{}, a, b)
	for i := 0; i < 3; i++ {
		id, err := call(t, p)
		require.NoError(t, err)
		assert.Equal(t, 1, id)
	}
	assert.Zero(t, b.calls.Load())
}

func TestFailsOverOnConnectionError(t *testing.T) {
	a, b := newNode(t, 1), newNode(t, 2)
	p, _ := newPool(t, Config{}, a, b)
	a.srv.Close()
	id, err := call(t, p)
	require.NoError(t, err)
	assert.Equal(t, 2, id)
	st := p.Status()
	assert.False(t, st[0].Available, "the failed endpoint cools down")
	assert.Equal(t, uint64(1), st[0].Failures)

	id, err = call(t, p)
	require.NoError(t, err)
	assert.Equal(t, 2, id, "a cooling endpoint is skipped")
	assert.Equal(t, uint64(1), p.Status()[0].Requests, "without being tried again")
}

func TestFailsOverOnServerErrorAndReturnsAfterCooldown(t *testing.T) {
	a, b := newNode(t, 1), newNode(t, 2)
	p, now := newPool(t, Config{Cooldown: time.Minute}, a, b)
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusTooManyRequests} {
		a.status.Store(int32(status))
		id, err := call(t, p)
		require.NoError(t, err)
		assert.Equal(t, 2, id, "HTTP %d", status)
		*now = now.Add(2 * time.Minute) // cooldown over
	}
	a.status.Store(0)
	id, err := call(t, p)
	require.NoError(t, err)
	assert.Equal(t, 1, id, "the primary is used again after its cooldown")
	assert.True(t, p.Status()[0].Available)
}

func TestJSONRPCErrorIsNotAnEndpointFailure(t *testing.T) {
	a := newNode(t, 1)
	a.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"header not found"}}`))
	})
	b := newNode(t, 2)
	p, _ := newPool(t, Config{}, a, b)
	_, err := call(t, p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "header not found")
	assert.Zero(t, b.calls.Load(), "the node's answer is returned, not retried elsewhere")
	assert.True(t, p.Status()[0].Available)
}

func TestAllEndpointsDown(t *testing.T) {
	a, b := newNode(t, 1), newNode(t, 2)
	p, _ := newPool(t, Config{}, a, b)
	a.srv.Close()
	b.srv.Close()
	_, err := call(t, p)
	require.Error(t, err)

	// Both cooling down: they are still tried rather than failing at once.
	b2 := newNode(t, 3)
	p.endpoints[1].url, _ = p.endpoints[1].url.Parse(b2.srv.URL)
	id, err := call(t, p)
	require.NoError(t, err)
	assert.Equal(t, 3, id)
}

func TestTimeoutFailsOver(t *testing.T) {
	slow := newNode(t, 1)
	slow.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // lets the server notice the client going away
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	b := newNode(t, 2)
	p, _ := newPool(t, Config{Timeout: 50 * time.Millisecond}, slow, b)
	start := time.Now()
	id, err := call(t, p)
	require.NoError(t, err)
	assert.Equal(t, 2, id)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestCancelledRequestDoesNotMarkEndpoint(t *testing.T) {
	a := newNode(t, 1)
	a.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // lets the server notice the client going away
		<-r.Context().Done()
	})
	p, _ := newPool(t, Config{}, a)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Primary(), nil)
	require.NoError(t, err)
	_, err = p.RoundTrip(req)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "%v", err)
	assert.True(t, p.Status()[0].Available)
}

func TestRateLimit(t *testing.T) {
	a := newNode(t, 1)
	p, _ := newPool(t, Config{RateLimit: 20, Burst: 1}, a)
	start := time.Now()
	for i := 0; i < 6; i++ {
		_, err := call(t, p)
		require.NoError(t, err)
	}
	assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "6 calls at 20/s take at least 250ms after the first")
}

func TestRejectsBadEndpoints(t *testing.T) {
	_, err := New(Config{})
	assert.Error(t, err)
	_, err = New(Config{Endpoints: []string{"ws://node:8546"}})
	assert.Error(t, err)
}
