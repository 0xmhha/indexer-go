package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestNotificationStream (subscriptions design phase 4c): a stream setting
// registered through GraphQL with a key sends every transaction of the
// test chain, once, to that key's connection at /v1/subscriptions/stream
// and to no other key's. A connection needs a key and a key holds at most
// notifications.max_streams_per_owner connections.
func TestNotificationStream(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	keys := map[string]string{"alice": "alice-key-0123456789abcdef01", "bob": "bob-key-0123456789abcdef0123"}
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableWebSocket = false
	cfg.API.Keys = keys
	cfg.Notifications.Enabled = true
	cfg.Notifications.MaxStreamsPerOwner = 1
	enableTestChainFeatures(cfg)
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	svc := app.notificationService.(*notifications.NotificationService)
	require.NoError(t, svc.Start(ctx))
	defer func() { _ = svc.Stop(context.Background()) }()

	base := fmt.Sprintf("127.0.0.1:%d", cfg.API.Port)
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + base + "/health")
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	data, _ := json.Marshal(map[string]any{
		"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id type delivery } }`,
		"variables": map[string]any{"in": map[string]any{"name": "s", "type": "STREAM", "delivery": "fast",
			"eventTypes": []string{"TRANSACTION"}, "destination": map[string]any{}}},
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+base+"/graphql", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", keys["alice"])
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var created map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	_ = resp.Body.Close()
	require.Empty(t, created["errors"])
	setting := created["data"].(map[string]any)["createNotificationSetting"].(map[string]any)
	assert.Equal(t, "STREAM", setting["type"])

	url := "ws://" + base + "/v1/subscriptions/stream"
	dial := func(who string) (*gws.Conn, *http.Response, error) {
		h := http.Header{}
		if who != "" {
			h.Set("X-API-Key", keys[who])
		}
		return gws.DefaultDialer.Dial(url, h)
	}
	_, resp, err = dial("")
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "a stream needs a key")
	alice, _, err := dial("alice")
	require.NoError(t, err)
	defer func() { _ = alice.Close() }()
	_, resp, err = dial("alice")
	require.Error(t, err)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "one connection per key here")
	bob, _, err := dial("bob")
	require.NoError(t, err)
	defer func() { _ = bob.Close() }()
	require.Eventually(t, func() bool { return svc.Streams().Connected("bob") == 1 }, 5*time.Second, 5*time.Millisecond)

	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	want := map[string]bool{}
	for n := uint64(0); n <= sc.Chain.Head(); n++ {
		b, err := app.storage.GetBlock(ctx, n)
		require.NoError(t, err)
		for _, tx := range b.Transactions {
			want[tx.Hash.Hex()] = true
		}
	}
	require.NotEmpty(t, want)

	got := map[string]int{}
	for len(got) < len(want) {
		require.NoError(t, alice.SetReadDeadline(time.Now().Add(10*time.Second)))
		var m notifications.StreamMessage
		require.NoError(t, alice.ReadJSON(&m), "received %d of %d transactions", len(got), len(want))
		require.Equal(t, notifications.StreamNotification, m.Type)
		require.Equal(t, setting["id"], m.Notification.SettingID)
		var tx struct {
			Hash string `json:"hash"`
		}
		require.NoError(t, json.Unmarshal(m.Notification.Payload.Data, &tx))
		got[tx.Hash]++
	}
	for h := range want {
		assert.Equal(t, 1, got[h], "transaction %s once", h)
	}

	require.NoError(t, bob.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	_, _, err = bob.ReadMessage()
	var netErr interface{ Timeout() bool }
	require.ErrorAs(t, err, &netErr, "bob receives none of alice's notifications")
	assert.True(t, netErr.Timeout())
}
