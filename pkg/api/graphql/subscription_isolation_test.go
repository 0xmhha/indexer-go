package graphql

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

func dialSubscriber(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	require.NoError(t, err)
	require.NoError(t, conn.WriteJSON(map[string]any{"type": "connection_init"}))
	var ack map[string]any
	require.NoError(t, conn.ReadJSON(&ack))
	require.Equal(t, "connection_ack", ack["type"])
	return conn
}

func subscribeNewBlock(t *testing.T, conn *websocket.Conn, id string) {
	t.Helper()
	require.NoError(t, conn.WriteJSON(map[string]any{
		"id": id, "type": "subscribe",
		"payload": map[string]any{"query": "subscription { newBlock { number } }"},
	}))
}

// expectNext reads until a "next" message for id arrives or the timeout ends.
func expectNext(conn *websocket.Conn, id string, within time.Duration) bool {
	_ = conn.SetReadDeadline(time.Now().Add(within))
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			return false
		}
		if msg["type"] == "next" && msg["id"] == id {
			return true
		}
	}
}

// TestSubscriptionIDsAreScopedPerConnection covers C2-1: the client-chosen
// id was used as the bus-wide id, so two clients using "1" overwrote each
// other and one client's complete cancelled the other's subscription.
func TestSubscriptionIDsAreScopedPerConnection(t *testing.T) {
	bus := events.NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()

	sub := NewSubscriptionServer(bus, zap.NewNop(), true)
	srv := httptest.NewServer(http.HandlerFunc(sub.ServeHTTP))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	a := dialSubscriber(t, url)
	defer a.Close()
	b := dialSubscriber(t, url)
	defer b.Close()
	subscribeNewBlock(t, a, "1")
	subscribeNewBlock(t, b, "1")
	time.Sleep(100 * time.Millisecond)

	require.True(t, bus.Publish(events.NewBlockEvent(createTestBlock(1))))
	require.True(t, expectNext(a, "1", 2*time.Second), "client A must receive")
	require.True(t, expectNext(b, "1", 2*time.Second), "client B must receive")

	// A completes its "1"; B's "1" must keep working.
	require.NoError(t, a.WriteJSON(map[string]any{"id": "1", "type": "complete"}))
	time.Sleep(100 * time.Millisecond)
	require.True(t, bus.Publish(events.NewBlockEvent(createTestBlock(2))))
	require.True(t, expectNext(b, "1", 2*time.Second), "client B must still receive after A completed")
}

// TestSendAfterCleanupDoesNotPanic covers C2-2: cleanup closed the send
// channel while event loops could still send to it.
func TestSendAfterCleanupDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &subscriptionClient{
		server:        &SubscriptionServer{},
		send:          make(chan []byte, 1),
		subscriptions: map[string]*clientSubscription{},
		logger:        zap.NewNop(),
		ctx:           ctx,
		cancel:        cancel,
		connID:        "test",
	}
	c.cleanup()
	require.NotPanics(t, func() { c.sendMessage(wsMessage{Type: "next", ID: "1"}) })
}
