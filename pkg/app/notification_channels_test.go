package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/notifications"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestNotificationChannels: one setting registered through GraphQL with
// type STREAM and channels [WEBHOOK] sends every transaction of the test
// chain once to the key's stream and once to the webhook.
func TestNotificationChannels(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	var mu sync.Mutex
	hooked := map[string]int{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data struct {
				Data json.RawMessage `json:"data"`
			} `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var tx struct {
			Hash string `json:"hash"`
		}
		_ = json.Unmarshal(body.Data.Data, &tx)
		mu.Lock()
		hooked[strings.ToLower(tx.Hash)]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	key := "alice-key-0123456789abcdef01"
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableWebSocket = false
	cfg.API.Keys = map[string]string{"alice": key}
	cfg.Notifications.Enabled = true
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.AllowPrivateDestinations = true // the hook is a local test server
	cfg.Notifications.DestinationRateLimit = 0
	cfg.Notifications.Queue.FlushInterval = 20 * time.Millisecond
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
		"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id type channels } }`,
		"variables": map[string]any{"in": map[string]any{"name": "both", "type": "STREAM", "channels": []string{"WEBHOOK"}, "delivery": "fast",
			"eventTypes": []string{"TRANSACTION"}, "destination": map[string]any{"webhookURL": hook.URL}}},
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+base+"/graphql", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", key)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	_ = resp.Body.Close()
	require.Empty(t, out["errors"])
	setting := out["data"].(map[string]any)["createNotificationSetting"].(map[string]any)
	assert.Equal(t, []any{"STREAM", "WEBHOOK"}, setting["channels"])

	conn, _, err := gws.DefaultDialer.Dial("ws://"+base+"/v1/subscriptions/stream", http.Header{"X-API-Key": {key}})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	want := map[string]bool{}
	for n := uint64(0); n <= sc.Chain.Head(); n++ {
		b, err := app.storage.GetBlock(ctx, n)
		require.NoError(t, err)
		for _, tx := range b.Transactions {
			want[strings.ToLower(tx.Hash.Hex())] = true
		}
	}
	streamed := map[string]int{}
	for len(streamed) < len(want) {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
		var m notifications.StreamMessage
		require.NoError(t, conn.ReadJSON(&m))
		var tx struct {
			Hash string `json:"hash"`
		}
		require.NoError(t, json.Unmarshal(m.Notification.Payload.Data, &tx))
		streamed[strings.ToLower(tx.Hash)]++
	}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(hooked) == len(want)
	}, time.Minute, 20*time.Millisecond, "the webhook gets every transaction")
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for h := range want {
		assert.Equal(t, 1, streamed[h], "stream: %s once", h)
		assert.Equal(t, 1, hooked[h], "webhook: %s once", h)
	}
}
