package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// controlledRPC only changes the advertised head and injects local HTTP
// failures. Blocks, receipts, state and chain identities still come from
// the caller's actual node. It never changes that node or sends transactions.
type controlledRPC struct {
	head   atomic.Uint64
	fail   atomic.Bool
	faults atomic.Uint64
	server *httptest.Server
}

func newControlledRPC(t *testing.T, endpoint string, head uint64, rate float64) *controlledRPC {
	t.Helper()
	pool, err := rpcpool.New(rpcpool.Config{Endpoints: []string{endpoint}, Timeout: 15 * time.Second, RateLimit: rate})
	require.NoError(t, err)
	c := &controlledRPC{}
	c.head.Store(head)
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.fail.Load() {
			c.faults.Add(1)
			http.Error(w, "injected test RPC outage", http.StatusServiceUnavailable)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &call) == nil {
			if call.Method == "eth_blockNumber" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": fmt.Sprintf("0x%x", c.head.Load())})
				return
			}
			if call.Method == "eth_getBlockByNumber" && len(call.Params) > 0 && string(call.Params[0]) == `"latest"` {
				call.Params[0], _ = json.Marshal(fmt.Sprintf("0x%x", c.head.Load()))
				raw, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "method": call.Method, "params": call.Params})
			}
		}
		out, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		out.Header.Set("Content-Type", "application/json")
		resp, err := pool.RoundTrip(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(c.server.Close)
	return c
}

// TestLiveRPCMultiChainIsolation is opt-in, using production profiles and
// features on arbitrary supplied targets. All pipelines run in one App;
// each node supplies three to one hundred recent blocks, not archive state.
func TestLiveRPCMultiChainIsolation(t *testing.T) {
	if *liveRPCTargets == "" {
		t.Skip("set -live-rpc-targets to a JSON file")
	}
	raw, err := os.ReadFile(*liveRPCTargets)
	require.NoError(t, err)
	var targets []liveRPCTarget
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&targets))
	require.GreaterOrEqual(t, len(targets), 2)
	verifyMultiChainIsolation(t, targets, 5)
}

// The same outage/restart/stream checks run without external access in CI,
// with distinct chain identities and block hashes, on either database driver.
func TestControlledRPCMultiChainIsolation(t *testing.T) {
	var targets []liveRPCTarget
	for i := range 4 {
		sender := testchain.NewAccount(uint64(9000 + i))
		chain := testchain.NewChain(int64(9000+i), map[common.Address]*big.Int{sender.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)})
		for n := range 3 {
			to := common.BigToAddress(big.NewInt(int64(10000 + i*10 + n)))
			chain.AddBlockWithExtra([]byte{byte(i + 1)}, testchain.TxSpec{From: sender, Tx: &types.LegacyTx{To: &to, Value: big.NewInt(1), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)}})
		}
		srv := testchain.NewServer(chain)
		t.Cleanup(srv.Close)
		targets = append(targets, liveRPCTarget{Name: fmt.Sprintf("local%d", i), Endpoint: srv.URL(), ChainID: uint64(9000 + i), Profile: "evm", Blocks: 3})
	}
	verifyMultiChainIsolation(t, targets, 0, true)
}

func verifyMultiChainIsolation(t *testing.T, targets []liveRPCTarget, rate float64, prepare ...bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	type sample struct {
		control    *controlledRPC
		from, head uint64
		hashes     map[uint64]common.Hash
		source     *sourcerpc.Source
	}
	samples := make([]sample, len(targets))
	entries := make([]config.ChainConfig, len(targets))
	for i, target := range targets {
		require.Positive(t, target.ChainID)
		c := newControlledRPC(t, target.Endpoint, 0, rate)
		rpc, err := gethrpc.DialContext(ctx, target.Endpoint)
		require.NoError(t, err)
		t.Cleanup(rpc.Close)
		info, err := sourcerpc.NodeInfo(ctx, rpc)
		require.NoError(t, err)
		require.Equal(t, target.ChainID, info.ChainID)
		head, err := sourcerpc.New(rpc, nil).Head(ctx)
		require.NoError(t, err)
		blocks := max(target.Blocks, 3)
		require.LessOrEqual(t, blocks, uint64(100))
		require.GreaterOrEqual(t, head, blocks)
		from := head - blocks + 1
		c.head.Store(from - 1)
		controlled, err := gethrpc.DialContext(ctx, c.server.URL)
		require.NoError(t, err)
		t.Cleanup(controlled.Close)
		src, err := sourcerpc.Detect(ctx, controlled)
		require.NoError(t, err)
		if target.Profile != "" {
			require.Equal(t, target.Profile, src.Profile().ID())
		}
		samples[i] = sample{control: c, from: from, head: head, hashes: map[uint64]common.Hash{}, source: src}
		for n := from; n <= head; n++ {
			block, _, err := src.BlockWithReceipts(ctx, n)
			require.NoError(t, err)
			samples[i].hashes[n] = block.Hash
		}
		entries[i] = config.ChainConfig{ID: fmt.Sprintf("chain%d", i), Name: target.Name, RPCEndpoint: c.server.URL,
			ChainID: target.ChainID, AdapterType: "auto", Enabled: true, StartHeight: from, Workers: 1, BatchSize: 1}
		t.Logf("%s: chain %d profile %s, bounded range %d..%d", target.Name, target.ChainID, src.Profile().ID(), from, head)
	}
	off := false
	entries[0].Features = map[string]config.FeatureConfig{"token.transfers": {Enabled: &off}}
	cfg := multiChainConfig(t, filepath.Join(t.TempDir(), "db"), entries...)
	if len(prepare) > 0 {
		cfg.Indexer.StatePrefetch = prepare[0]
	}
	cfg.Features = nil // production defaults, without the synthetic StableNet override
	cfg.RPC.Timeout = 20 * time.Second
	cfg.Indexer.ChunkSize = 1
	cfg.Indexer.PollInterval = 50 * time.Millisecond
	cfg.Notifications.Enabled = true
	key := "multichain-test-key-0123456789abcdef"
	cfg.API.Keys = map[string]string{"reader": key}
	cfg.MultiChain.AutoRestart = true
	var app *App
	logger := zap.NewNop()
	if rate > 0 {
		logConfig := zap.NewProductionConfig()
		logConfig.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
		logConfig.DisableStacktrace = true
		var err error
		logger, err = logConfig.Build()
		require.NoError(t, err)
	}
	var stop context.CancelFunc
	var done chan error
	shutdown := func() {
		if stop == nil {
			return
		}
		stop()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		var runErr error
		select {
		case err := <-done:
			runErr = err
		case <-shutdownCtx.Done():
			t.Fatal("multichain shutdown timed out")
		}
		app.Shutdown()
		stop = nil
		require.True(t, runErr == nil || errors.Is(runErr, context.Canceled), "run ended: %v", runErr)
	}
	t.Cleanup(shutdown)
	start := func() {
		var err error
		app, err = NewApp(cfg, logger, false, "")
		require.NoError(t, err)
		require.Nil(t, app.storage, "there is no shared chain database")
		var runCtx context.Context
		runCtx, stop = context.WithCancel(ctx)
		done = make(chan error, 1)
		go func(a *App, ch chan error) { ch <- a.Run(runCtx) }(app, done)
		for _, entry := range entries {
			require.Eventually(t, func() bool {
				idx, ok := app.multichainManager.ChainIndexer(entry.ID)
				if !ok {
					return false
				}
				// Readiness is the notification API, not a partially initialized App.
				r := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/chains/"+entry.ID+"/graphql", strings.NewReader(`{"query":"{ notificationSettings { id } }"}`))
				req.Header.Set("X-API-Key", key)
				app.apiServer.Router().ServeHTTP(r, req)
				return idx != nil && r.Code == http.StatusOK && strings.Contains(r.Body.String(), `"notificationSettings"`)
			}, 2*time.Minute, 20*time.Millisecond, "chain %s ready", entry.ID)
		}
	}
	post := func(id, route string, body any) map[string]any {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/chains/"+id+route, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", key)
		r := httptest.NewRecorder()
		app.apiServer.Router().ServeHTTP(r, req)
		require.Equal(t, http.StatusOK, r.Code, r.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
		require.Empty(t, out["errors"])
		require.Nil(t, out["error"])
		return out
	}
	settingIDs := make([]string, len(entries))
	streams := make([]*gws.Conn, len(entries))
	type streamResult struct {
		message notifications.StreamMessage
		err     error
	}
	messages := make([]chan streamResult, len(entries))
	connect := func() {
		for i, entry := range entries {
			var conn *gws.Conn
			require.Eventually(t, func() bool {
				var err error
				conn, _, err = gws.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/chains/%s/v1/subscriptions/stream", cfg.API.Port, entry.ID), http.Header{"X-API-Key": {key}})
				return err == nil
			}, time.Minute, 20*time.Millisecond)
			streams[i] = conn
			ch := make(chan streamResult, 102) // bounded range, plus close/error
			messages[i] = ch
			readerDone := make(chan struct{})
			// Always read, including while another chain is slow or faulted,
			// so the normal WebSocket ping/pong keepalive keeps working.
			go func() {
				defer close(readerDone)
				for {
					var msg notifications.StreamMessage
					err := conn.ReadJSON(&msg)
					select {
					case ch <- streamResult{message: msg, err: err}:
					case <-ctx.Done():
						return
					}
					if err != nil {
						return
					}
				}
			}()
			t.Cleanup(func() { _ = conn.Close(); <-readerDone })
			require.Eventually(t, func() bool {
				idx, ok := app.multichainManager.ChainIndexer(entry.ID)
				if !ok {
					return false
				}
				svc := idx.(*chainIndexer).app.notificationService.(*notifications.NotificationService)
				return svc.Streams().Connected("reader") == 1
			}, time.Minute, time.Millisecond, "stream registered before releasing blocks")
		}
	}
	waitHeight := func(i int, height uint64) {
		var previousError string
		require.Eventually(t, func() bool {
			ci, err := app.multichainManager.GetChain(entries[i].ID)
			if err != nil {
				return false
			}
			if ci.Status() == "error" {
				info := ci.HealthCheck(ctx)
				if info.LastError != previousError {
					previousError = info.LastError
					t.Logf("chain %s status %s: %s", entries[i].ID, info.Status, info.LastError)
				}
			}
			h, ok := ci.IndexedHeight(ctx)
			return ok && h == height
		}, 2*time.Minute, 20*time.Millisecond, "chain %s at %d", entries[i].ID, height)
	}
	readBlocks := func(i int, from, to uint64) {
		for n := from; n <= to; n++ {
			var result streamResult
			timer := time.NewTimer(time.Minute)
			select {
			case result = <-messages[i]:
				timer.Stop()
			case <-timer.C:
				t.Fatalf("chain %s notification for block %d timed out", entries[i].ID, n)
			case <-ctx.Done():
				timer.Stop()
				t.Fatal(ctx.Err())
			}
			require.NoError(t, result.err, "chain %s block %d", entries[i].ID, n)
			msg := result.message
			require.Equal(t, notifications.StreamNotification, msg.Type)
			require.NotNil(t, msg.Notification)
			require.Equal(t, settingIDs[i], msg.Notification.SettingID)
			require.Equal(t, targets[i].ChainID, msg.Notification.Payload.ChainID)
			require.Equal(t, n, msg.Notification.Payload.BlockNumber)
			require.Equal(t, samples[i].hashes[n], msg.Notification.Payload.BlockHash)
		}
	}
	checkSettings := func() {
		for i, entry := range entries {
			out := post(entry.ID, "/rpc", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notification_getSettings", "params": map[string]any{}})
			settings := out["result"].([]any)
			require.Len(t, settings, 1)
			require.Equal(t, settingIDs[i], settings[0].(map[string]any)["id"])
			idx, ok := app.multichainManager.ChainIndexer(entry.ID)
			require.True(t, ok)
			child := idx.(*chainIndexer).app
			require.Equal(t, samples[i].source.Profile().ID(), child.profile.ID())
			if i == 0 {
				require.NotContains(t, child.fetcherFeatures(), "token.transfers")
			} else {
				require.Contains(t, child.fetcherFeatures(), "token.transfers")
			}
		}
	}
	start()
	for i, entry := range entries {
		out := post(entry.ID, "/graphql", map[string]any{"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id } }`,
			"variables": map[string]any{"in": map[string]any{"name": entry.ID + " blocks", "type": "STREAM", "delivery": "durable", "eventTypes": []string{"BLOCK"}, "destination": map[string]any{}}}})
		settingIDs[i] = out["data"].(map[string]any)["createNotificationSetting"].(map[string]any)["id"].(string)
	}
	checkSettings()
	connect()
	for i := range samples {
		samples[i].control.head.Store(samples[i].from)
	}
	for i := range samples {
		waitHeight(i, samples[i].from)
		readBlocks(i, samples[i].from, samples[i].from)
	}
	// Hold one RPC down while every other chain advances; health and storage
	// are checked during the outage, not inferred after recovery.
	samples[0].control.fail.Store(true)
	for i := range samples {
		samples[i].control.head.Store(samples[i].head - 1)
	}
	ci, err := app.multichainManager.GetChain(entries[0].ID)
	require.NoError(t, err)
	health := ci.HealthCheck(ctx)
	require.False(t, health.IsHealthy)
	require.NotEmpty(t, health.LastError)
	for i := 1; i < len(samples); i++ {
		waitHeight(i, samples[i].head-1)
		readBlocks(i, samples[i].from+1, samples[i].head-1)
	}
	h, ok := ci.IndexedHeight(ctx)
	require.True(t, ok)
	require.Equal(t, samples[0].from, h)
	require.Positive(t, samples[0].control.faults.Load())
	samples[0].control.fail.Store(false)
	waitHeight(0, samples[0].head-1)
	readBlocks(0, samples[0].from+1, samples[0].head-1)
	t.Log("one RPC outage: other chains advanced; failed chain caught up after recovery")
	for _, stream := range streams {
		require.NoError(t, stream.Close())
	}
	shutdown()
	start() // same database paths and settings; last block withheld until streams reconnect
	checkSettings()
	connect()
	for i := range samples {
		samples[i].control.head.Store(samples[i].head)
	}
	for i, s := range samples {
		waitHeight(i, s.head)
		readBlocks(i, s.head, s.head)
		idx, ok := app.multichainManager.ChainIndexer(entries[i].ID)
		require.True(t, ok)
		checkLiveRPCStorage(t, ctx, idx.(*chainIndexer).app, s.source, s.from, s.head)
		store := idx.Store()
		for j, other := range samples {
			if i != j {
				_, err := store.GetBlockByHash(ctx, other.hashes[other.from])
				require.Error(t, err, "no foreign chain block in %s", entries[i].ID)
			}
		}
		hash := s.hashes[s.head].Hex()
		out := post(entries[i].ID, "/rpc", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "getBlock", "params": map[string]any{"number": s.head}})
		require.Equal(t, hash, out["result"].(map[string]any)["hash"])
		out = post(entries[i].ID, "/graphql", map[string]any{"query": fmt.Sprintf(`{ block(number: "%d") { hash } }`, s.head)})
		require.Equal(t, hash, out["data"].(map[string]any)["block"].(map[string]any)["hash"])
		body := serve(t, app.apiServer.Router(), http.MethodGet, fmt.Sprintf("/chains/%s/v1/blocks?numberFrom=%d&numberTo=%d", entries[i].ID, s.head, s.head), "")
		require.Contains(t, body, hash)
	}
	t.Log("same-process storage/features/API/stream isolation and persisted process restart verified")
}
