package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
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
