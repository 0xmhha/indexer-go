package graphql

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// countingConn records the writes that reach the network.
type countingConn struct {
	net.Conn
	writes [][]byte
}

func (c *countingConn) Write(p []byte) (int, error) {
	c.writes = append(c.writes, append([]byte{}, p...))
	return len(p), nil
}

// TestCorkConnBatchesWrites: writes while corked reach the network as one
// write on Uncork, in order; other writes pass through at once.
func TestCorkConnBatchesWrites(t *testing.T) {
	under := &countingConn{}
	c := &corkConn{Conn: under}
	_, err := c.Write([]byte("a"))
	require.NoError(t, err)
	require.Len(t, under.writes, 1)

	c.Cork()
	for _, s := range []string{"b", "c", "d"} {
		n, err := c.Write([]byte(s))
		require.NoError(t, err)
		require.Equal(t, 1, n)
	}
	require.Len(t, under.writes, 1, "held while corked")
	require.NoError(t, c.Uncork())
	require.Equal(t, [][]byte{[]byte("a"), []byte("bcd")}, under.writes)
	require.NoError(t, c.Uncork(), "nothing held")
	require.Len(t, under.writes, 2)
}

// TestUpgradeWritesThroughCorkConn: the WebSocket upgrade hijacks the
// connection through corkWriter and gorilla writes to the corkConn, so the
// server's batches are corked.
func TestUpgradeWritesThroughCorkConn(t *testing.T) {
	got := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &corkWriter{ResponseWriter: w}
		conn, err := (&websocket.Upgrader{}).Upgrade(cw, r, nil)
		if err != nil {
			got <- false
			return
		}
		defer func() { _ = conn.Close() }()
		got <- cw.conn != nil && conn.UnderlyingConn() == net.Conn(cw.conn)
		cw.conn.Cork()
		_ = conn.WriteMessage(websocket.TextMessage, []byte("one"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("two"))
		_ = cw.conn.Uncork()
	}))
	defer srv.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	require.True(t, <-got)
	for _, want := range []string{"one", "two"} {
		require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, data, err := client.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, want, string(data))
	}
}
