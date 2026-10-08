package graphql

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// keepAliveClient is a subscriber connection whose reads run on their own
// goroutine: text messages arrive on msgs, the read error on closed, and
// pings are counted (and answered when answer is set).
type keepAliveClient struct {
	conn   *websocket.Conn
	pings  atomic.Int32
	msgs   chan map[string]any
	closed chan error
}

func dialKeepAlive(t *testing.T, keepAlive bool, wait time.Duration, answer bool) *keepAliveClient {
	t.Helper()
	bus := events.NewEventBus(16, 16)
	go bus.Run()
	t.Cleanup(bus.Stop)
	sub := NewSubscriptionServer(bus, zap.NewNop(), keepAlive)
	sub.pongWait = wait
	srv := httptest.NewServer(http.HandlerFunc(sub.ServeHTTP))
	t.Cleanup(srv.Close)
	c := &keepAliveClient{conn: dialSubscriber(t, "ws"+strings.TrimPrefix(srv.URL, "http")),
		msgs: make(chan map[string]any, 16), closed: make(chan error, 1)}
	t.Cleanup(func() { _ = c.conn.Close() })
	c.conn.SetPingHandler(func(data string) error {
		c.pings.Add(1)
		if !answer {
			return nil
		}
		return c.conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
	go func() {
		for {
			var m map[string]any
			if err := c.conn.ReadJSON(&m); err != nil {
				c.closed <- err
				return
			}
			c.msgs <- m
		}
	}()
	return c
}

// alive asks the server for a protocol pong: an open, served connection
// answers.
func (c *keepAliveClient) alive(t *testing.T) bool {
	t.Helper()
	if err := c.conn.WriteJSON(map[string]any{"type": "ping"}); err != nil {
		return false
	}
	select {
	case m := <-c.msgs:
		return m["type"] == "pong"
	case <-c.closed:
		return false
	case <-time.After(2 * time.Second):
		return false
	}
}

// TestKeepAlive (refactoring plan R0-7, C2 keep-alive) with a 200 ms wait
// instead of 60 s: the server pings a keep-alive connection every 9/10 of
// the wait; a client that answers stays connected well past the wait, one
// that does not is closed after it; without keep-alive an idle client is
// neither pinged nor closed.
func TestKeepAlive(t *testing.T) {
	const wait = 200 * time.Millisecond

	t.Run("answering client stays", func(t *testing.T) {
		c := dialKeepAlive(t, true, wait, true)
		time.Sleep(5 * wait)
		require.GreaterOrEqual(t, c.pings.Load(), int32(3), "pinged every 180ms")
		require.True(t, c.alive(t), "still connected after five waits")
	})

	t.Run("silent client is closed", func(t *testing.T) {
		start := time.Now()
		c := dialKeepAlive(t, true, wait, false)
		select {
		case <-c.closed:
			require.GreaterOrEqual(t, time.Since(start), wait, "not before the wait")
		case <-time.After(10 * wait):
			t.Fatal("a client that never answers pings is not closed")
		}
		require.GreaterOrEqual(t, c.pings.Load(), int32(1))
	})

	t.Run("no keep-alive: idle client stays, unpinged", func(t *testing.T) {
		c := dialKeepAlive(t, false, wait, false)
		time.Sleep(3 * wait)
		require.Zero(t, c.pings.Load())
		require.True(t, c.alive(t), "an idle subscriber is not dropped without keep-alive")
	})
}
