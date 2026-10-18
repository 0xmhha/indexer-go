package graphql

import (
	"bufio"
	"net"
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
	server *keepAliveTestConn
}

// Separate protocol/deadline renewal from the OS timer. The controlled mode
// records the REAL server's deadline calls without arming a wall-clock timer;
// expiry is covered separately with real deadlines and an explicit expired read.
// Otherwise a 180ms ping leaves only 20ms to schedule both peers under -race.
type keepAliveTestConn struct {
	net.Conn
	controlled bool
	deadlines  chan time.Time
	failures   chan keepAliveReadFailure
}

type keepAliveReadFailure struct {
	at  time.Time
	err error
}

func (c *keepAliveTestConn) SetReadDeadline(deadline time.Time) error {
	if !c.controlled || deadline.IsZero() {
		if err := c.Conn.SetReadDeadline(deadline); err != nil {
			return err
		}
	}
	if !deadline.IsZero() { // ignore net/http's initial zero deadline
		select {
		case c.deadlines <- deadline:
		default:
		}
	}
	return nil
}

func (c *keepAliveTestConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		select {
		case c.failures <- keepAliveReadFailure{time.Now(), err}:
		default:
		}
	}
	return n, err
}

// Wrap only after HTTP hijacking, so net/http can still use its own temporary
// deadlines to stop a background read during the upgrade.
type keepAliveUpgrade struct {
	http.ResponseWriter
	controlled bool
	accepted   chan *keepAliveTestConn
}

func (w *keepAliveUpgrade) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	c := &keepAliveTestConn{Conn: conn, controlled: w.controlled,
		deadlines: make(chan time.Time, 64), failures: make(chan keepAliveReadFailure, 1)}
	w.accepted <- c
	return c, rw, nil
}

func keepAliveReceive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(10 * time.Second): // hang protection, not a latency target
		t.Fatalf("waiting for %s", what)
		var zero T
		return zero
	}
}

func dialKeepAlive(t *testing.T, keepAlive bool, wait time.Duration, answer, controlled bool) *keepAliveClient {
	t.Helper()
	bus := events.NewEventBus(16, 16)
	go bus.Run()
	t.Cleanup(bus.Stop)
	sub := NewSubscriptionServer(bus, zap.NewNop(), keepAlive)
	sub.pongWait = wait
	accepted := make(chan *keepAliveTestConn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub.ServeHTTP(&keepAliveUpgrade{ResponseWriter: w, controlled: controlled, accepted: accepted}, r)
	}))
	t.Cleanup(srv.Close)
	c := &keepAliveClient{conn: dialSubscriber(t, "ws"+strings.TrimPrefix(srv.URL, "http")),
		msgs: make(chan map[string]any, 16), closed: make(chan error, 1),
		server: keepAliveReceive(t, accepted, "accepted connection")}
	readDone := make(chan struct{})
	t.Cleanup(func() {
		_ = c.conn.Close()
		keepAliveReceive(t, readDone, "client reader shutdown")
	})
	c.conn.SetPingHandler(func(data string) error {
		c.pings.Add(1)
		if !answer {
			return nil
		}
		return c.conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
	go func() {
		defer close(readDone)
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
func (c *keepAliveClient) assertAlive(t *testing.T) {
	t.Helper()
	require.NoError(t, c.conn.WriteJSON(map[string]any{"type": "ping"}))
	select {
	case m := <-c.msgs:
		require.Equal(t, "pong", m["type"])
	case err := <-c.closed:
		t.Fatalf("connection closed before protocol pong: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("waiting for protocol pong")
	}
}

// TestKeepAlive checks real control frames and observed server deadlines.
// Only the silent-client case uses the shortened wall-clock expiry; renewal
// must not depend on squeezing network/goroutine scheduling into 20ms.
func TestKeepAlive(t *testing.T) {
	const wait = 200 * time.Millisecond
	require.Equal(t, 60*time.Second, (&SubscriptionServer{}).keepAliveWait())
	require.Equal(t, 54*time.Second, (&subscriptionClient{pongWait: defaultPongWait}).pingPeriod())
	require.Equal(t, 180*time.Millisecond, (&subscriptionClient{pongWait: wait}).pingPeriod())

	t.Run("answered pings renew server deadline", func(t *testing.T) {
		c := dialKeepAlive(t, true, wait, true, true)
		deadline := keepAliveReceive(t, c.server.deadlines, "initial read deadline")
		for i := 0; i < 3; i++ {
			next := keepAliveReceive(t, c.server.deadlines, "pong renewal")
			require.True(t, next.After(deadline), "each accepted pong renews the deadline")
			deadline = next
		}
		require.GreaterOrEqual(t, c.pings.Load(), int32(3), "real WebSocket control pings were answered")
		c.assertAlive(t)
	})

	t.Run("silent client is closed", func(t *testing.T) {
		c := dialKeepAlive(t, true, wait, false, false)
		deadline := keepAliveReceive(t, c.server.deadlines, "initial read deadline")
		failure := keepAliveReceive(t, c.server.failures, "server read timeout")
		var timeout net.Error
		require.ErrorAs(t, failure.err, &timeout)
		require.True(t, timeout.Timeout())
		require.False(t, failure.at.Before(deadline), "not before the actual server deadline")
		require.Error(t, keepAliveReceive(t, c.closed, "silent client disconnect"))
	})

	t.Run("expired read cannot be rescued by a late pong", func(t *testing.T) {
		c := dialKeepAlive(t, true, wait, false, true)
		keepAliveReceive(t, c.server.deadlines, "initial read deadline")
		// Force the real socket's read to expire, without depending on when
		// a scheduler happens to deliver a short test timer or control frame.
		require.NoError(t, c.server.Conn.SetReadDeadline(time.Now().Add(-time.Nanosecond)))
		failure := keepAliveReceive(t, c.server.failures, "expired server read")
		var timeout net.Error
		require.ErrorAs(t, failure.err, &timeout)
		require.True(t, timeout.Timeout())
		// TCP may accept this write even after the peer's read failed. Either
		// outcome must still lead to disconnect, never renewed server reads.
		_ = c.conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second))
		require.Error(t, keepAliveReceive(t, c.closed, "expired client disconnect"))
		select {
		case <-c.server.deadlines:
			t.Fatal("a late pong renewed an already expired read")
		default:
		}
	})

	t.Run("no keep-alive has no read deadline", func(t *testing.T) {
		c := dialKeepAlive(t, false, wait, false, false)
		c.assertAlive(t)
		require.Zero(t, c.pings.Load())
		select {
		case <-c.server.deadlines:
			t.Fatal("keep-alive disabled installed an idle read deadline")
		default:
		}
	})
}
