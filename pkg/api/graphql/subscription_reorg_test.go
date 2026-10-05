package graphql

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// subscribeWS opens a graphql-transport-ws connection and starts one
// subscription.
func subscribeWS(t *testing.T, url, id, query string, variables map[string]any) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http"), header)
	require.NoError(t, err)
	require.NoError(t, conn.WriteJSON(map[string]any{"type": "connection_init"}))
	var ack map[string]any
	require.NoError(t, conn.ReadJSON(&ack))
	require.Equal(t, "connection_ack", ack["type"])
	require.NoError(t, conn.WriteJSON(map[string]any{"id": id, "type": "subscribe", "payload": map[string]any{"query": query, "variables": variables}}))
	return conn
}

func readNext(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var msg map[string]any
	require.NoError(t, conn.ReadJSON(&msg))
	require.Equal(t, "next", msg["type"], "%v", msg)
	return msg["payload"].(map[string]any)["data"].(map[string]any)
}

// TestReorgAndRemovedLogSubscriptions delivers a reorg event on the reorg
// subscription and a removed log on a filtered logs subscription.
func TestReorgAndRemovedLogSubscriptions(t *testing.T) {
	bus := events.NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()
	srv := httptest.NewServer(NewSubscriptionServer(bus, zap.NewNop(), true))
	defer srv.Close()

	reorgConn := subscribeWS(t, srv.URL, "r", `subscription { reorg { id forkNumber forkHash oldHead depth removedBlocks { number hash } detectedAt } }`, nil)
	defer func() { _ = reorgConn.Close() }()
	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	logsConn := subscribeWS(t, srv.URL, "l", `subscription($filter: LogFilter!) { logs(filter: $filter) { address logIndex removed } }`,
		map[string]any{"filter": map[string]any{"address": addr.Hex()}})
	defer func() { _ = logsConn.Close() }()
	time.Sleep(100 * time.Millisecond) // let both subscriptions register

	require.True(t, bus.Publish(&events.ReorgEvent{
		Seq: 2, ForkNumber: 10, ForkHash: common.Hash{1}, OldHead: 12,
		Removed:   []events.BlockRef{{Number: 12, Hash: common.Hash{12}}, {Number: 11, Hash: common.Hash{11}}},
		CreatedAt: time.Unix(1700000000, 0),
	}))
	require.True(t, bus.Publish(events.NewLogEvent(&types.Log{Address: common.Address{0xbb}, Index: 1, Removed: true})))
	require.True(t, bus.Publish(events.NewLogEvent(&types.Log{Address: addr, Index: 3, BlockNumber: 12, Removed: true})))

	reorg := readNext(t, reorgConn)["reorg"].(map[string]any)
	require.Equal(t, "2", reorg["id"])
	require.Equal(t, "10", reorg["forkNumber"])
	require.Equal(t, "12", reorg["oldHead"])
	require.EqualValues(t, 2, reorg["depth"])
	require.Len(t, reorg["removedBlocks"], 2)
	require.Equal(t, "1700000000", reorg["detectedAt"])

	log := readNext(t, logsConn)["logs"].(map[string]any)
	require.Equal(t, addr.Hex(), log["address"], "the filter keeps only the subscribed address")
	require.Equal(t, true, log["removed"])
}
