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

	"github.com/ethereum/go-ethereum/common"
	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestNotificationExpressions (subscriptions design phase 5): a setting
// registered through GraphQL with the ERC-20 Transfer event, a CEL
// condition on the value and a payload expression is notified of exactly
// the transfers that meet the condition, each carrying the payload's
// value and the chain id. A condition that does not type-check is refused
// at registration.
func TestNotificationExpressions(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

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

	create := func(condition string) map[string]any {
		data, _ := json.Marshal(map[string]any{
			"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id condition payload } }`,
			"variables": map[string]any{"in": map[string]any{"name": "big transfers", "type": "STREAM", "delivery": "fast",
				"eventTypes": []string{"LOG"}, "destination": map[string]any{},
				"filter":    map[string]any{"event": "Transfer(address indexed from, address indexed to, uint256 value)"},
				"condition": condition,
				"payload":   `{"to": event.to, "value": event.value, "chain": chain.id}`}},
		})
		req, _ := http.NewRequest(http.MethodPost, "http://"+base+"/graphql", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", key)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out
	}
	refused := create("event.value >= 15")
	require.NotEmpty(t, refused["errors"], "uint256 is a string: comparing it with an int does not type-check")
	assert.Contains(t, fmt.Sprint(refused["errors"]), "no matching overload")

	out := create(`bigCmp(event.value, 15) >= 0`)
	require.Empty(t, out["errors"])
	setting := out["data"].(map[string]any)["createNotificationSetting"].(map[string]any)
	assert.Equal(t, `bigCmp(event.value, 15) >= 0`, setting["condition"])

	conn, _, err := gws.DefaultDialer.Dial("ws://"+base+"/v1/subscriptions/stream", http.Header{"X-API-Key": {key}})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	// The scenario's ERC-20 transfers are 1000, 11, 12, 14, 15, 17 and 18.
	var values []string
	for len(values) < 4 {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
		var m notifications.StreamMessage
		require.NoError(t, conn.ReadJSON(&m), "received %v", values)
		require.Equal(t, notifications.StreamNotification, m.Type, "message %+v", m)
		require.Equal(t, setting["id"], m.Notification.SettingID)
		var result struct {
			To    string `json:"to"`
			Value string `json:"value"`
			Chain uint64 `json:"chain"`
		}
		require.NoError(t, json.Unmarshal(m.Notification.Payload.Result, &result))
		assert.Equal(t, uint64(testchain.DefaultChainID), result.Chain)
		assert.Equal(t, uint64(testchain.DefaultChainID), m.Notification.Payload.ChainID)
		assert.NotEmpty(t, result.To)
		values = append(values, result.Value)
	}
	assert.Equal(t, []string{"1000", "15", "17", "18"}, values, "only transfers of 15 or more, in chain order")
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err, "no further notification")
}

// TestNotificationExpressionDryRun (subscriptions design phase 5b): with a
// key, GraphQL checkNotificationExpressions and JSON-RPC
// notification_checkExpressions evaluate a condition and payload over a
// sample log taken from the indexed chain, without creating anything;
// without a key both refuse.
func TestNotificationExpressionDryRun(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	key := "alice-key-0123456789abcdef01"
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableJSONRPC = true
	cfg.API.EnableWebSocket = false
	cfg.API.Keys = map[string]string{"alice": key}
	cfg.Notifications.Enabled = true
	enableTestChainFeatures(cfg)
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.API.Port)
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	// The first ERC-20 transfer (1000) as eth_getLogs gives it.
	logs, err := app.storage.GetLogsByAddress(ctx, sc.ERC20, 0, sc.Chain.Head())
	require.NoError(t, err)
	var sample map[string]any
	for _, l := range logs {
		if len(l.Topics) == 3 {
			topics := make([]string, len(l.Topics))
			for i, tp := range l.Topics {
				topics[i] = tp.Hex()
			}
			sample = map[string]any{"address": l.Address.Hex(), "topics": topics, "data": "0x" + common.Bytes2Hex(l.Data),
				"index": l.Index, "transactionHash": l.TxHash.Hex()}
			break
		}
	}
	require.NotNil(t, sample)
	check := map[string]any{"event": "Transfer(address indexed from, address indexed to, uint256 value)",
		"condition": `bigCmp(event.value, 15) >= 0`, "payload": `{"value": event.value}`, "log": sample}

	post := func(path string, body any, withKey bool) map[string]any {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		if withKey {
			req.Header.Set("X-API-Key", key)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out
	}
	query := `query($in: NotificationExpressionCheckInput!) { checkNotificationExpressions(input: $in) { valid error notify result decoded } }`
	gqlCheck := map[string]any{}
	for k, v := range check {
		gqlCheck[k] = v
	}
	gqlCheck["log"] = map[string]any{"address": sample["address"], "topics": sample["topics"], "data": sample["data"],
		"index": sample["index"], "transactionHash": sample["transactionHash"]}
	out := post("/graphql", map[string]any{"query": query, "variables": map[string]any{"in": gqlCheck}}, true)
	require.Empty(t, out["errors"])
	got := out["data"].(map[string]any)["checkNotificationExpressions"].(map[string]any)
	assert.Equal(t, true, got["valid"])
	assert.Equal(t, true, got["notify"])
	assert.Nil(t, got["error"])
	assert.JSONEq(t, `{"value":"1000"}`, got["result"].(string))
	assert.Contains(t, got["decoded"], `"value":"1000"`)

	out = post("/graphql", map[string]any{"query": query, "variables": map[string]any{"in": gqlCheck}}, false)
	assert.Contains(t, fmt.Sprint(out["errors"]), "UNAUTHENTICATED")

	rpc := post("/rpc", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notification_checkExpressions", "params": check}, true)
	require.Nil(t, rpc["error"])
	res := rpc["result"].(map[string]any)
	assert.Equal(t, true, res["notify"])
	assert.Equal(t, map[string]any{"value": "1000"}, res["result"])

	rpc = post("/rpc", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notification_checkExpressions", "params": check}, false)
	require.NotNil(t, rpc["error"])
	assert.Equal(t, float64(-32001), rpc["error"].(map[string]any)["code"])

	settings, err := app.notificationService.ListSettings(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, settings, "a dry run creates nothing")
}
