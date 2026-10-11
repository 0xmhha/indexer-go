package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestMultiChainNotifications: in multichain mode each chain runs its own
// notification service. A stream setting registered under
// /chains/a/graphql belongs to chain a only: chain b's API does not list
// it, the key's connection to chain a's stream receives every transaction
// of chain a once, and its connection to chain b's stream receives nothing.
// Chain b's JSON-RPC notification API answers for chain b's settings.
func TestMultiChainNotifications(t *testing.T) {
	a, b := testchain.BuildDefault(), testchain.BuildDefault()
	headA, headB := a.Chain.Head(), b.Chain.Head()
	a.Chain.SetHead(0) // shown once the stream listens
	b.Chain.SetHead(0)
	aSrv, bSrv := testchain.NewServer(a.Chain), testchain.NewServer(b.Chain)
	t.Cleanup(aSrv.Close)
	t.Cleanup(bSrv.Close)

	key := "alice-key-0123456789abcdef01"
	cfg := multiChainConfig(t, filepath.Join(t.TempDir(), "db"), chainEntry("a", aSrv.URL()), chainEntry("b", bSrv.URL()))
	cfg.API.Keys = map[string]string{"alice": key}
	cfg.Notifications.Enabled = true
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		<-done
		app.Shutdown()
	})
	base := fmt.Sprintf("127.0.0.1:%d", cfg.API.Port)

	try := func(path string, body any) (map[string]any, error) {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "http://"+base+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		return out, json.NewDecoder(resp.Body).Decode(&out)
	}
	post := func(path string, body any) map[string]any {
		out, err := try(path, body)
		require.NoError(t, err, path)
		return out
	}
	create := map[string]any{
		"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id } }`,
		"variables": map[string]any{"in": map[string]any{"name": "a txs", "type": "STREAM", "delivery": "fast",
			"eventTypes": []string{"TRANSACTION"}, "destination": map[string]any{}}},
	}
	for _, chain := range []string{"a", "b"} {
		require.Eventually(t, func() bool {
			// Until the server listens and the chain runs, requests fail
			// or answer 503.
			out, err := try("/chains/"+chain+"/graphql", map[string]any{"query": `{ notificationSettings { id } }`})
			return err == nil && out["data"] != nil
		}, time.Minute, 50*time.Millisecond, "chain %s serves its notification API", chain)
	}
	created := post("/chains/a/graphql", create)
	require.Empty(t, created["errors"])

	count := func(chain string) int {
		out := post("/chains/"+chain+"/graphql", map[string]any{"query": `{ notificationSettings { id } }`})
		require.Empty(t, out["errors"], chain)
		return len(out["data"].(map[string]any)["notificationSettings"].([]any))
	}
	assert.Equal(t, 1, count("a"))
	assert.Equal(t, 0, count("b"), "chain b has its own settings")
	rpc := post("/chains/b/rpc", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notification_getSettings", "params": map[string]any{}})
	require.Nil(t, rpc["error"])
	assert.Empty(t, rpc["result"], "chain b's JSON-RPC answers for chain b")
	rpc = post("/chains/a/rpc", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notification_getSettings", "params": map[string]any{}})
	require.Nil(t, rpc["error"])
	assert.Len(t, rpc["result"], 1, "chain a's JSON-RPC answers for chain a, whichever chain's handlers were built last")

	dial := func(chain string) *gws.Conn {
		conn, _, err := gws.DefaultDialer.Dial("ws://"+base+"/chains/"+chain+"/v1/subscriptions/stream", http.Header{"X-API-Key": {key}})
		require.NoError(t, err, chain)
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	streamA, streamB := dial("a"), dial("b")
	_, resp, err := gws.DefaultDialer.Dial("ws://"+base+"/chains/nope/v1/subscriptions/stream", http.Header{"X-API-Key": {key}})
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "unknown chain")

	a.Chain.SetHead(headA)
	b.Chain.SetHead(headB)
	want := map[string]bool{}
	for n := uint64(1); n <= headA; n++ {
		for _, tx := range a.Chain.Block(n).Block.Transactions() {
			want[strings.ToLower(tx.Hash().Hex())] = true
		}
	}
	require.NotEmpty(t, want)
	got := map[string]int{}
	for len(got) < len(want) {
		require.NoError(t, streamA.SetReadDeadline(time.Now().Add(20*time.Second)))
		var m notifications.StreamMessage
		require.NoError(t, streamA.ReadJSON(&m), "received %d of %d", len(got), len(want))
		require.Equal(t, notifications.StreamNotification, m.Type)
		var tx struct {
			Hash string `json:"hash"`
		}
		require.NoError(t, json.Unmarshal(m.Notification.Payload.Data, &tx))
		got[strings.ToLower(tx.Hash)]++
	}
	for h := range want {
		assert.Equal(t, 1, got[h], "transaction %s of chain a once", h)
	}

	require.Eventually(t, func() bool {
		ci, err := app.multichainManager.GetChain("b")
		if err != nil {
			return false
		}
		h, ok := ci.IndexedHeight(ctx)
		return ok && h >= headB
	}, time.Minute, 20*time.Millisecond, "chain b indexed")
	require.NoError(t, streamB.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	_, _, err = streamB.ReadMessage()
	var netErr interface{ Timeout() bool }
	require.ErrorAs(t, err, &netErr, "chain b's stream gets none of chain a's notifications")
}
