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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestNotificationAPINeedsAKeyAndRefusesInternalWebhooks: through the
// served API, the notification operations refuse requests without an API
// key (the rest of the API stays open), an unknown key is refused, and a
// webhook to an internal address (cloud metadata, loopback) cannot be
// registered, so the server cannot be made to call into its own network.
func TestNotificationAPINeedsAKeyAndRefusesInternalWebhooks(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	const key = "test-key-0123456789abcdef0123"
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
	cfg.API.Keys = map[string]string{"ops": key}
	cfg.Notifications.Enabled = true
	cfg.Notifications.Webhook.Enabled = true
	enableTestChainFeatures(cfg)
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.API.Port)
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	post := func(path, apiKey string, body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("X-API-Key", apiKey)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	create := func(url string) map[string]any {
		return map[string]any{
			"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id } }`,
			"variables": map[string]any{"in": map[string]any{
				"name": "hook", "type": "WEBHOOK", "eventTypes": []string{"BLOCK"},
				"destination": map[string]any{"webhookURL": url},
			}},
		}
	}
	errorCode := func(out map[string]any) string {
		errs, _ := out["errors"].([]any)
		if len(errs) == 0 {
			return ""
		}
		ext, _ := errs[0].(map[string]any)["extensions"].(map[string]any)
		code, _ := ext["code"].(string)
		return code
	}

	status, out := post("/graphql", "", create("https://hooks.example.com/notify"))
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "UNAUTHENTICATED", errorCode(out), "no key: refused")

	status, out = post("/graphql", "", map[string]any{"query": `{ latestHeight }`})
	assert.Equal(t, http.StatusOK, status)
	assert.Empty(t, out["errors"], "the rest of the API stays open")

	status, _ = post("/graphql", "wrong-key", map[string]any{"query": `{ latestHeight }`})
	assert.Equal(t, http.StatusUnauthorized, status, "an unknown key is refused, not taken for none")

	for _, internal := range []string{"http://169.254.169.254/latest/meta-data", "http://127.0.0.1:8080/admin", "http://localhost/hook"} {
		_, out = post("/graphql", key, create(internal))
		require.NotEmpty(t, out["errors"], internal)
		assert.Contains(t, fmt.Sprint(out["errors"]), "not allowed", internal)
	}

	_, out = post("/graphql", key, create("https://hooks.example.com/notify"))
	require.Empty(t, out["errors"], "a public destination with a key")

	_, out = post("/rpc", "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notification_getSettings", "params": map[string]any{}})
	rpcErr, _ := out["error"].(map[string]any)
	require.NotNil(t, rpcErr, "JSON-RPC without a key")
	assert.Equal(t, float64(-32001), rpcErr["code"])
	_, out = post("/rpc", key, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notification_getSettings", "params": map[string]any{}})
	assert.Nil(t, out["error"], "JSON-RPC with a key")
}

// TestNotificationSettingsAreSeparatedByKey: through the served API a key
// sees only the settings it created, and an operator key sees all.
func TestNotificationSettingsAreSeparatedByKey(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	keys := map[string]string{"alice": "alice-key-0123456789abcdef01", "bob": "bob-key-0123456789abcdef0123", "ops": "ops-key-0123456789abcdef0123"}
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
	cfg.API.Keys = keys
	cfg.Notifications.Enabled = true
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.OperatorLabels = []string{"ops"}
	enableTestChainFeatures(cfg)
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.API.Port)
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	gql := func(who, query string, vars map[string]any) map[string]any {
		data, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		req, _ := http.NewRequest(http.MethodPost, base+"/graphql", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", keys[who])
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.Empty(t, out["errors"], "%s: %v", who, out["errors"])
		return out["data"].(map[string]any)
	}
	created := gql("alice", `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id } }`,
		map[string]any{"in": map[string]any{"name": "a", "type": "WEBHOOK", "eventTypes": []string{"BLOCK"},
			"destination": map[string]any{"webhookURL": "https://hooks.example.com/a"}}})
	id := created["createNotificationSetting"].(map[string]any)["id"].(string)

	count := func(who string) int {
		return len(gql(who, `{ notificationSettings { id } }`, nil)["notificationSettings"].([]any))
	}
	assert.Equal(t, 1, count("alice"))
	assert.Equal(t, 0, count("bob"), "bob does not see alice's setting")
	assert.Equal(t, 1, count("ops"), "the operator sees every setting")
	assert.Nil(t, gql("bob", `query($id: ID!) { notificationSetting(id: $id) { id } }`, map[string]any{"id": id})["notificationSetting"])
}

// TestNotificationOfOneDecodedEvent: a setting registered through the API
// with a contract, an event signature and a participant is notified of
// that event only where the participant takes part, and the webhook gets
// the event's decoded arguments. A malformed filter is refused.
func TestNotificationOfOneDecodedEvent(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	var mu sync.Mutex
	var got []map[string]any
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	const key = "event-key-0123456789abcdef012"
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableWebSocket = false
	cfg.API.Keys = map[string]string{"trader": key}
	cfg.Notifications.Enabled = true
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.AllowPrivateDestinations = true // the hook is a local test server
	cfg.Notifications.Queue.FlushInterval = 20 * time.Millisecond
	enableTestChainFeatures(cfg)
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.notificationService.Start(ctx))
	defer func() { _ = app.notificationService.Stop(context.Background()) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.API.Port)
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	create := func(filter map[string]any) map[string]any {
		data, _ := json.Marshal(map[string]any{
			"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id filter { event participants } } }`,
			"variables": map[string]any{"in": map[string]any{
				"name": "transfers to b", "type": "WEBHOOK", "eventTypes": []string{"LOG"},
				"filter": filter, "destination": map[string]any{"webhookURL": hook.URL},
			}},
		})
		req, _ := http.NewRequest(http.MethodPost, base+"/graphql", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", key)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out
	}
	const transfer = "Transfer(address indexed from, address indexed to, uint256 value)"
	b := sc.Accounts[1].Address
	for name, bad := range map[string]map[string]any{
		"event":       {"event": "Transfer(address,"},
		"participant": {"participants": []string{"0x123"}},
		"topic":       {"topics": [][]string{{"0x01"}}},
	} {
		out := create(bad)
		assert.NotEmpty(t, out["errors"], "a malformed %s is refused", name)
	}
	out := create(map[string]any{"addresses": []string{sc.ERC20.Hex()}, "event": transfer, "participants": []string{b.Hex()}})
	require.Empty(t, out["errors"])
	setting := out["data"].(map[string]any)["createNotificationSetting"].(map[string]any)
	assert.Equal(t, transfer, setting["filter"].(map[string]any)["event"])

	app.fetcher.StartRelay()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	want := map[string]any{"from": strings.ToLower(sc.Accounts[0].Address.Hex()), "to": strings.ToLower(b.Hex()), "value": "1000"}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, body := range got {
			data, _ := body["data"].(map[string]any)
			if fmt.Sprint(data["decoded"]) == fmt.Sprint(want) {
				return true
			}
		}
		return false
	}, 30*time.Second, 20*time.Millisecond, "the transfer to b arrives decoded")
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, body := range got {
		decoded := body["data"].(map[string]any)["decoded"].(map[string]any)
		assert.True(t, decoded["from"] == want["to"] || decoded["to"] == want["to"], "only transfers where b takes part: %v", decoded)
	}
}
