package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// startAppWithAPI starts an app with the outbox and the GraphQL API on a
// free local port, and returns it with the API's base URL.
func startAppWithAPI(t *testing.T, srv *testchain.Server, dir string) (*App, string) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, dir)
	cfg.Indexer.PollInterval = 10 * time.Millisecond
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableJSONRPC = false
	cfg.API.EnableWebSocket = false
	enableTestChainFeatures(cfg)
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.API.Port)
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
	return app, base
}

// streamSequence asks the API for the stream's position.
func streamSequence(t *testing.T, base string) uint64 {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": "{ streamSequence }"})
	resp, err := http.Post(base+"/graphql", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Data struct {
			StreamSequence *string `json:"streamSequence"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Empty(t, out.Errors)
	require.NotNil(t, out.Data.StreamSequence, "the outbox is on")
	seq, err := strconv.ParseUint(*out.Data.StreamSequence, 10, 64)
	require.NoError(t, err)
	return seq
}

// streamClient is a GraphQL WebSocket client with a newBlock and a logs
// subscription, recording the sequence of every event per subscription.
type streamClient struct {
	t    *testing.T
	conn *websocket.Conn
	got  map[string][]uint64 // subscription id -> sequences
}

const (
	blocksSub = "blocks"
	logsSub   = "logs"
)

// dialStream subscribes from the given sequence of each subscription.
func dialStream(t *testing.T, base string, from map[string]uint64) *streamClient {
	t.Helper()
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/graphql/ws", header)
	require.NoError(t, err)
	require.NoError(t, conn.WriteJSON(map[string]any{"type": "connection_init"}))
	var ack map[string]any
	require.NoError(t, conn.ReadJSON(&ack))
	require.Equal(t, "connection_ack", ack["type"])
	queries := map[string]string{
		blocksSub: `subscription { newBlock { number hash } }`,
		logsSub:   `subscription { logs(filter: {}) { blockNumber logIndex removed } }`,
	}
	for id, q := range queries {
		require.NoError(t, conn.WriteJSON(map[string]any{"id": id, "type": "subscribe",
			"payload": map[string]any{"query": q, "variables": map[string]any{"fromSequence": from[id]}}}))
	}
	return &streamClient{t: t, conn: conn, got: map[string][]uint64{}}
}

// readUntil reads until every subscription received the sequence in want
// (or a later one).
func (c *streamClient) readUntil(want map[string]uint64) {
	c.t.Helper()
	require.NoError(c.t, c.conn.SetReadDeadline(time.Now().Add(30*time.Second)))
	done := func() bool {
		for id, seq := range want {
			g := c.got[id]
			if seq != 0 && (len(g) == 0 || g[len(g)-1] < seq) {
				return false
			}
		}
		return true
	}
	for !done() {
		var m struct {
			ID      string          `json:"id"`
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		require.NoError(c.t, c.conn.ReadJSON(&m))
		require.Equal(c.t, "next", m.Type, "%s", m.Payload)
		var p struct {
			Extensions struct{ Sequence uint64 } `json:"extensions"`
		}
		require.NoError(c.t, json.Unmarshal(m.Payload, &p))
		c.got[m.ID] = append(c.got[m.ID], p.Extensions.Sequence)
	}
}

// last returns the last sequence a subscription received, 0 if none.
func (c *streamClient) last(id string) uint64 {
	g := c.got[id]
	if len(g) == 0 {
		return 0
	}
	return g[len(g)-1]
}

// outboxSequences returns the sequences of the outbox entries per
// subscription (block events for newBlock, log events for logs).
func outboxSequences(t *testing.T, app *App) map[string][]uint64 {
	t.Helper()
	entries, err := app.storage.(port.Outbox).ReadOutbox(context.Background(), 0, 0)
	require.NoError(t, err)
	out := map[string][]uint64{}
	for _, e := range entries {
		switch events.EventType(e.Type) {
		case events.EventTypeBlock:
			out[blocksSub] = append(out[blocksSub], e.Seq)
		case events.EventTypeLog:
			out[logsSub] = append(out[logsSub], e.Seq)
		}
	}
	return out
}

// TestResubscribeMissesNothing is the R3-4 criterion end to end: a client
// starts from a snapshot position (streamSequence), receives events over
// GraphQL WebSocket while the chain is indexed, loses its connection, and
// subscribes again from the sequence after the last event it received
// while indexing continues through a reorganization. Its two connections
// together receive every block and log event of the stream once, in order.
func TestResubscribeMissesNothing(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	app, base := startAppWithAPI(t, srv, filepath.Join(t.TempDir(), "db"))
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	head := sc.Chain.Head()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, 3))
	waitRelayed(t, app)

	// Snapshot: the position, then (in a real client) the state; the
	// subscription continues after the position.
	snapshot := streamSequence(t, base)
	require.NotZero(t, snapshot)
	first := dialStream(t, base, map[string]uint64{blocksSub: snapshot + 1, logsSub: snapshot + 1})
	require.NoError(t, app.fetcher.FetchRange(ctx, 4, head/2))
	waitRelayed(t, app)
	want := outboxSequences(t, app)
	first.readUntil(map[string]uint64{blocksSub: want[blocksSub][len(want[blocksSub])-1]})
	_ = first.conn.Close() // the connection is lost

	// Indexing continues, with a reorganization, while the client is away.
	require.NoError(t, app.fetcher.FetchRange(ctx, head/2+1, head))
	reorgChain(sc, head-3, 5)
	newHead := sc.Chain.Head()
	loopCtx, stopLoop := context.WithCancel(ctx)
	loopDone := make(chan error, 1)
	go func() { loopDone <- app.fetcher.Run(loopCtx) }()
	defer func() { stopLoop(); <-loopDone }()
	require.Eventually(t, func() bool {
		b, err := app.storage.GetBlock(ctx, newHead)
		return err == nil && b.Extra != nil
	}, time.Minute, 20*time.Millisecond)
	waitRelayed(t, app)

	second := dialStream(t, base, map[string]uint64{blocksSub: first.last(blocksSub) + 1, logsSub: first.last(logsSub) + 1})
	defer func() { _ = second.conn.Close() }()
	want = outboxSequences(t, app)
	second.readUntil(map[string]uint64{
		blocksSub: want[blocksSub][len(want[blocksSub])-1],
		logsSub:   want[logsSub][len(want[logsSub])-1],
	})

	for _, id := range []string{blocksSub, logsSub} {
		var expect []uint64
		for _, seq := range want[id] {
			if seq > snapshot {
				expect = append(expect, seq)
			}
		}
		got := append(append([]uint64{}, first.got[id]...), second.got[id]...)
		require.NotEmpty(t, expect, id)
		require.Equal(t, expect, got, "subscription %s receives every event after the snapshot once", id)
	}
	var reorgs int
	entries, err := app.storage.(port.Outbox).ReadOutbox(ctx, first.last(logsSub), 0)
	require.NoError(t, err)
	for _, e := range entries {
		if events.EventType(e.Type) == events.EventTypeReorg {
			reorgs++
		}
	}
	require.Equal(t, 1, reorgs, "the reorganization happened while the client was away")
}
