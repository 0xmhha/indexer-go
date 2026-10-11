package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// timedTap records when each block reaches the fast path, then passes it
// on.
type timedTap struct {
	fetch.BlockTap
	at []atomic.Int64
}

func (t *timedTap) OfferBlock(height uint64, evs []events.Event) {
	if height < uint64(len(t.at)) {
		t.at[height].CompareAndSwap(0, time.Now().UnixNano())
	}
	t.BlockTap.OfferBlock(height, evs)
}

// The subscriptions design's hypothesis (4.8) was p99 at most 10 ms from
// the node showing a block to a fast stream message. Measured (10/11, one
// macOS host, 1 ms polling): 9.6 to 12.1 ms over four runs, of which the
// notification path itself (block on the fast path -> message) is 1.3 to
// 5.1 ms; the rest is fetching the block. The checks leave room for
// noise: end to end at most 20 ms, the notification path at most 10 ms.
const (
	notifyEndToEndP99 = 20 * time.Millisecond
	notifyPathP99     = 10 * time.Millisecond
)

// TestNotificationLatency (subscriptions design phase 6) measures the
// real-time subscription path. The node shows a block of DEX swaps every
// interval; the live loop finds it by polling every
// INDEXER_NOTIFY_POLL_MS (default 1, standing in for newHeads, which the
// test chain lacks); settings with an event filter and a CEL condition
// (sells only) send matching swaps to a stream (fast), a webhook (fast)
// and a stream (durable). It reports, per channel, the time from the node
// showing the block and from the block reaching the fast path to each
// message, and checks that every sell arrives once on each channel and
// that the fast stream's p99 is at most INDEXER_NOTIFY_P99_MS (default 20)
// end to end and notifyPathP99 from the fast path.
// Run it with INDEXER_LOAD_SLO=1; INDEXER_NOTIFY_BLOCKS, _SWAPS and
// _INTERVAL_MS change the load.
func TestNotificationLatency(t *testing.T) {
	if os.Getenv("INDEXER_LOAD_SLO") == "" {
		t.Skip("set INDEXER_LOAD_SLO=1")
	}
	blocks := envInt(t, "INDEXER_NOTIFY_BLOCKS", 100)
	swaps := envInt(t, "INDEXER_NOTIFY_SWAPS", 10)
	interval := time.Duration(envInt(t, "INDEXER_NOTIFY_INTERVAL_MS", 100)) * time.Millisecond
	poll := time.Duration(envInt(t, "INDEXER_NOTIFY_POLL_MS", 1)) * time.Millisecond
	bound := time.Duration(envInt(t, "INDEXER_NOTIFY_P99_MS", int(notifyEndToEndP99/time.Millisecond))) * time.Millisecond

	sc := testchain.BuildDEXLoad(blocks, swaps)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	shown := make([]atomic.Int64, blocks+2)
	type arrival struct {
		block uint64
		at    int64
	}
	var mu sync.Mutex
	got := map[string][]arrival{}
	record := func(channel string, block uint64) {
		now := time.Now().UnixNano()
		mu.Lock()
		got[channel] = append(got[channel], arrival{block, now})
		mu.Unlock()
	}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data struct {
				BlockNumber uint64 `json:"block_number"`
			} `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		record("webhook fast", body.Data.BlockNumber)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	keys := map[string]string{"fast": "fast-key-0123456789abcdef0123", "durable": "durable-key-0123456789abcdef01"}
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.Indexer.PollInterval = poll
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableWebSocket = false
	cfg.API.RateLimit.Enabled = false
	cfg.API.Keys = keys
	cfg.Notifications.Enabled = true
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.AllowPrivateDestinations = true // the hook is a local test server
	cfg.Notifications.DestinationRateLimit = 0        // measure the path, not the cap
	cfg.Notifications.Queue.FlushInterval = 20 * time.Millisecond
	cfg.SetDefaults()
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	ctx := t.Context()
	svc := app.notificationService.(*notifications.NotificationService)
	require.NoError(t, svc.Start(ctx))
	defer func() { _ = svc.Stop(t.Context()) }()
	tap := &timedTap{BlockTap: svc, at: make([]atomic.Int64, blocks+2)}
	app.fetcher.SetBlockTap(tap)
	app.fetcher.StartRelay()

	base := fmt.Sprintf("127.0.0.1:%d", cfg.API.Port)
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + base + "/health")
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	swapEvent := "Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to)"
	create := func(who, typ, delivery string, dest map[string]any) {
		data, _ := json.Marshal(map[string]any{
			"query": `mutation($in: CreateNotificationSettingInput!) { createNotificationSetting(input: $in) { id } }`,
			"variables": map[string]any{"in": map[string]any{"name": typ + " " + delivery, "type": typ, "delivery": delivery,
				"eventTypes": []string{"LOG"}, "destination": dest,
				"filter":    map[string]any{"addresses": []string{sc.V2Pair.Hex()}, "event": swapEvent},
				"condition": `bigCmp(event.amount0In, 0) > 0`}},
		})
		req, _ := http.NewRequest(http.MethodPost, "http://"+base+"/graphql", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", keys[who])
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.Empty(t, out["errors"])
	}
	create("fast", "STREAM", "fast", map[string]any{})
	create("fast", "WEBHOOK", "fast", map[string]any{"webhookURL": hook.URL})
	create("durable", "STREAM", "durable", map[string]any{})

	want := blocks * (swaps / 2) // sells only
	var readers sync.WaitGroup
	for who, channel := range map[string]string{"fast": "stream fast", "durable": "stream durable"} {
		conn, _, err := gws.DefaultDialer.Dial("ws://"+base+"/v1/subscriptions/stream", http.Header{"X-API-Key": {keys[who]}})
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		readers.Add(1)
		go func(conn *gws.Conn, channel string) {
			defer readers.Done()
			_ = conn.SetReadDeadline(time.Now().Add(time.Duration(blocks)*interval + time.Minute))
			for n := 0; n < want; n++ {
				var m notifications.StreamMessage
				if err := conn.ReadJSON(&m); err != nil {
					t.Errorf("%s: %v after %d messages", channel, err, n)
					return
				}
				if m.Type != notifications.StreamNotification {
					t.Errorf("%s: unexpected %+v", channel, m)
					return
				}
				record(channel, m.Notification.Payload.BlockNumber)
			}
		}(conn, channel)
	}

	loopDone := make(chan error, 1)
	go func() { loopDone <- app.fetcher.Run(ctx) }()
	start := time.Now()
	for b := 2; b <= blocks+1; b++ {
		if wait := time.Until(start.Add(time.Duration(b-1) * interval)); wait > 0 {
			time.Sleep(wait)
		}
		shown[b].Store(time.Now().UnixNano())
		sc.Chain.SetHead(uint64(b))
	}
	readers.Wait()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got["webhook fast"]) >= want
	}, time.Minute, 10*time.Millisecond, "every sell reaches the webhook")

	t.Logf("%d blocks every %v, %d swaps each (%d sells notified per channel), polling every %v", blocks, interval, swaps, want, poll)
	var fastP99, fastPathP99 time.Duration
	mu.Lock()
	defer mu.Unlock()
	for _, channel := range []string{"stream fast", "webhook fast", "stream durable"} {
		var fromShown, fromTap []time.Duration
		perBlock := map[uint64]int{}
		for _, a := range got[channel] {
			perBlock[a.block]++
			fromShown = append(fromShown, time.Duration(a.at-shown[a.block].Load()))
			fromTap = append(fromTap, time.Duration(a.at-tap.at[a.block].Load()))
		}
		require.Len(t, got[channel], want, channel)
		for b := uint64(2); b <= uint64(blocks+1); b++ {
			require.Equal(t, swaps/2, perBlock[b], "%s: block %d", channel, b)
		}
		s, path := statsOf(fromShown), statsOf(fromTap)
		t.Logf("  %-15s node shows block -> message: %v; block on the fast path -> message: %v", channel, s, path)
		if channel == "stream fast" {
			fastP99, fastPathP99 = s.p99, path.p99
		}
	}
	require.LessOrEqual(t, fastP99, bound, "fast stream p99 from the node showing the block")
	require.LessOrEqual(t, fastPathP99, notifyPathP99, "fast stream p99 from the block reaching the fast path")
}
