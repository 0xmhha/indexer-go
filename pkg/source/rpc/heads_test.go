package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

// headService serves eth_subscribe("newHeads"), sending a head whenever
// the test asks.
type headService struct {
	send chan struct{}
}

func (h *headService) NewHeads(ctx context.Context) (*gethrpc.Subscription, error) {
	notifier, ok := gethrpc.NotifierFromContext(ctx)
	if !ok {
		return nil, gethrpc.ErrNotificationsUnsupported
	}
	sub := notifier.CreateSubscription()
	go func() {
		for {
			select {
			case <-h.send:
				_ = notifier.Notify(sub.ID, map[string]string{"number": "0x1"})
			case <-sub.Err():
				return
			}
		}
	}()
	return sub, nil
}

// wsNode starts a WebSocket JSON-RPC server with the head service.
func wsNode(t *testing.T) (*headService, *httptest.Server) {
	t.Helper()
	svc, _, hs := wsNodeServer(t)
	return svc, hs
}

func wsNodeServer(t *testing.T) (*headService, *gethrpc.Server, *httptest.Server) {
	t.Helper()
	svc, srv := headServer(t)
	current.Store(srv.WebsocketHandler([]string{"*"}))
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current.Load().(http.Handler).ServeHTTP(w, r)
	}))
	t.Cleanup(func() { hs.Close(); srv.Stop() })
	return svc, srv, hs
}

// current is the handler the test node serves; replacing it simulates a
// restarted node.
var current atomic.Value

func headServer(t *testing.T) (*headService, *gethrpc.Server) {
	t.Helper()
	svc := &headService{send: make(chan struct{})}
	srv := gethrpc.NewServer()
	require.NoError(t, srv.RegisterName("eth", svc))
	return svc, srv
}

func wsURL(hs *httptest.Server) string { return "ws" + strings.TrimPrefix(hs.URL, "http") }

func TestSubscribeHeadsSignalsNewHeads(t *testing.T) {
	svc, hs := wsNode(t)
	ctx, cancel := context.WithCancel(context.Background())
	heads := SubscribeHeads(ctx, wsURL(hs), nil)

	for i := 0; i < 3; i++ {
		select {
		case svc.send <- struct{}{}:
		case <-time.After(5 * time.Second):
			t.Fatal("subscription not established")
		}
		select {
		case <-heads:
		case <-time.After(5 * time.Second):
			t.Fatalf("head %d not signalled", i)
		}
	}

	cancel()
	select {
	case _, open := <-heads:
		for open {
			_, open = <-heads
		}
	case <-time.After(5 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestSubscribeHeadsCoalescesSignals(t *testing.T) {
	svc, hs := wsNode(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	heads := SubscribeHeads(ctx, wsURL(hs), nil)
	for i := 0; i < 5; i++ {
		select {
		case svc.send <- struct{}{}:
		case <-time.After(5 * time.Second):
			t.Fatal("subscription not established")
		}
	}
	require.Eventually(t, func() bool { return len(heads) == 1 }, 5*time.Second, 10*time.Millisecond)
	// The first head makes the signal pending while the others may still be
	// on their way; reading now would let a late one signal again. Wait for
	// them before reading.
	time.Sleep(200 * time.Millisecond)
	<-heads
	select {
	case <-heads:
		t.Fatal("five heads read late give one pending signal, not five")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSubscribeHeadsReconnects(t *testing.T) {
	svc1, srv1, hs := wsNodeServer(t)
	url := wsURL(hs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	heads := SubscribeHeads(ctx, url, nil)
	select {
	case svc1.send <- struct{}{}: // the first subscription is up
	case <-time.After(5 * time.Second):
		t.Fatal("subscription not established")
	}
	<-heads

	// The node restarts: its connections close and a new server answers.
	svc2, srv2 := headServer(t)
	current.Store(srv2.WebsocketHandler([]string{"*"}))
	defer srv2.Stop()
	srv1.Stop()

	select {
	case svc2.send <- struct{}{}:
	case <-time.After(3 * headRetry):
		t.Fatal("no new subscription after the connection dropped")
	}
	select {
	case <-heads:
	case <-time.After(5 * time.Second):
		t.Fatal("head after reconnect not signalled")
	}
}

// TestSubscribeHeadsLive follows a real node (INDEXER_LIVE_WS, e.g.
// ws://127.0.0.1:8601).
func TestSubscribeHeadsLive(t *testing.T) {
	url := os.Getenv("INDEXER_LIVE_WS")
	if url == "" {
		t.Skip("INDEXER_LIVE_WS not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	heads := SubscribeHeads(ctx, url, nil)
	for i := 0; i < 3; i++ {
		select {
		case <-heads:
		case <-time.After(30 * time.Second):
			t.Fatalf("no head %d from %s", i, url)
		}
	}
}
