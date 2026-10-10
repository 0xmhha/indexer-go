package rpcproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeNode answers eth_getBalance, eth_blockNumber, eth_call (reverted
// for calls to revertingContract) and eth_getTransactionByHash (always
// unknown), counting calls per method and
// holding each answer until release is closed (when set).
type fakeNode struct {
	calls   sync.Map // method -> *atomic.Int64
	release chan struct{}
}

func (n *fakeNode) count(method string) int64 {
	v, _ := n.calls.LoadOrStore(method, new(atomic.Int64))
	return v.(*atomic.Int64).Load()
}

func (n *fakeNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, _ := n.calls.LoadOrStore(req.Method, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
	if n.release != nil {
		<-n.release
	}
	var result string
	switch req.Method {
	case "eth_call":
		if strings.Contains(strings.ToLower(string(req.Params)), strings.ToLower(revertingContract.Hex())) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":3,"message":"execution reverted"}}`, req.ID)
			return
		}
		result = `"0x01"`
	case "eth_getBalance":
		result = `"0x64"`
	case "eth_blockNumber":
		result = `"0xa"`
	default:
		result = `null` // unknown transaction
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)
}

func newTestProxy(t *testing.T, node *fakeNode, cfg *Config) *Proxy {
	t.Helper()
	srv := httptest.NewServer(node)
	t.Cleanup(srv.Close)
	rc, err := rpc.Dial(srv.URL)
	require.NoError(t, err)
	t.Cleanup(rc.Close)
	if cfg == nil {
		cfg = DefaultConfig()
	}
	p := NewProxy(ethclient.NewClient(rc), rc, nil, cfg, zap.NewNop())
	t.Cleanup(p.cache.Close)
	return p
}

var (
	account           = common.HexToAddress("0x00000000000000000000000000000000000AA001")
	revertingContract = common.HexToAddress("0x00000000000000000000000000000000000DD001")
)

// TestConcurrentMissesShareOneNodeRequest: callers asking for the same
// value at the same time get one node request between them.
func TestConcurrentMissesShareOneNodeRequest(t *testing.T) {
	node := &fakeNode{release: make(chan struct{})}
	p := newTestProxy(t, node, nil)

	const callers = 50
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := p.GetBalance(context.Background(), &BalanceRequest{Address: account})
			if err == nil && resp.Balance.Int64() != 100 {
				err = fmt.Errorf("balance %s", resp.Balance)
			}
			errs <- err
		}()
	}
	require.Eventually(t, func() bool { return node.count("eth_getBalance") == 1 }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let the other callers join the request
	close(node.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), node.count("eth_getBalance"))
	assert.Equal(t, int64(1), node.count("eth_blockNumber"))
}

// TestCacheHitsDoNotUseTheNodeAllowance: the node rate limit counts node
// requests only; cached answers are served past it.
func TestCacheHitsDoNotUseTheNodeAllowance(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.BurstSize = 0.001, 1
	p := newTestProxy(t, &fakeNode{}, cfg)
	ctx := context.Background()

	for i := range 100 {
		_, err := p.GetBalance(ctx, &BalanceRequest{Address: account})
		require.NoError(t, err, "request %d", i)
	}
	other := common.HexToAddress("0x00000000000000000000000000000000000AA002")
	_, err := p.GetBalance(ctx, &BalanceRequest{Address: other})
	assert.ErrorIs(t, err, ErrRateLimited, "a new node request is still limited")
}

// TestUnknownTransactionsKeepTheCircuitClosed: a node answering "not found"
// is working; looking up unknown transactions must not open the circuit
// breaker for every other request.
func TestUnknownTransactionsKeepTheCircuitClosed(t *testing.T) {
	node := &fakeNode{}
	p := newTestProxy(t, node, nil)
	ctx := context.Background()
	for i := range 3 * DefaultCircuitBreakerConfig().MaxFailures {
		resp, err := p.GetTransactionStatus(ctx, common.BigToHash(big.NewInt(int64(i+1))))
		require.NoError(t, err)
		assert.Equal(t, TxStatusNotFound, resp.Status)
	}
	_, err := p.GetBalance(ctx, &BalanceRequest{Address: account})
	assert.NoError(t, err)
}

// TestCallsAreCachedWithTheirReverts: an eth_call answer, data or a
// revert, is served from the cache on the next call; a revert keeps the
// circuit closed, and a new call is still limited by the node allowance.
func TestCallsAreCachedWithTheirReverts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.BurstSize = 0.001, 2
	node := &fakeNode{}
	p := newTestProxy(t, node, cfg)
	ctx := context.Background()
	call := ethereum.CallMsg{To: &account, Data: []byte{0x06, 0xfd, 0xde, 0x03}}
	revert := ethereum.CallMsg{To: &revertingContract, Data: []byte{0x01, 0xff, 0xc9, 0xa7}}

	for range 3 * DefaultCircuitBreakerConfig().MaxFailures {
		data, err := p.Call(ctx, call, nil)
		require.NoError(t, err)
		assert.Equal(t, []byte{1}, data)
		_, err = p.Call(ctx, revert, nil)
		var answered rpc.Error
		require.ErrorAs(t, err, &answered, "the revert is returned as the node answered it")
	}
	assert.Equal(t, int64(2), node.count("eth_call"), "one node request per call")
	assert.Equal(t, "closed", p.circuitBreaker.State().String())

	other := ethereum.CallMsg{To: &account, Data: []byte{0x95, 0xd8, 0x9b, 0x41}}
	_, err := p.Call(ctx, other, nil)
	assert.ErrorIs(t, err, ErrRateLimited)
}

// TestWaiterLeavesWhenItsContextEnds: a caller whose context ends stops
// waiting; the shared request goes on and serves the others.
func TestWaiterLeavesWhenItsContextEnds(t *testing.T) {
	node := &fakeNode{release: make(chan struct{})}
	p := newTestProxy(t, node, nil)

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := p.GetBalance(ctx, &BalanceRequest{Address: account})
		first <- err
	}()
	require.Eventually(t, func() bool { return node.count("eth_getBalance") == 1 }, 5*time.Second, time.Millisecond)
	second := make(chan error, 1)
	go func() {
		_, err := p.GetBalance(context.Background(), &BalanceRequest{Address: account})
		second <- err
	}()
	cancel()
	assert.True(t, errors.Is(<-first, context.Canceled))
	close(node.release)
	assert.NoError(t, <-second, "the request started by the canceled caller completes for the other")
	assert.Equal(t, int64(1), node.count("eth_getBalance"))
}

// BenchmarkCacheGetParallel measures cache hits from many goroutines (the
// lock Get takes).
func BenchmarkCacheGetParallel(b *testing.B) {
	c := NewCache(DefaultCacheConfig())
	defer c.Close()
	keys := make([]string, 1024)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
		c.Set(keys[i], i, time.Hour)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, ok := c.Get(keys[i%len(keys)]); !ok {
				b.Fatal("miss")
			}
			i++
		}
	})
}
