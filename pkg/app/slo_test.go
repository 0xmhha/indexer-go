package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/features/dex"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// envInt reads a positive integer setting of the load test.
func envInt(t *testing.T, name string, def int) int {
	t.Helper()
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	require.NoError(t, err, name)
	require.GreaterOrEqual(t, v, 0, name)
	return v
}

// sloStats are latency percentiles.
type sloStats struct{ p50, p99, max time.Duration }

func (s sloStats) String() string {
	return fmt.Sprintf("p50 %v p99 %v max %v", s.p50.Round(100*time.Microsecond), s.p99.Round(100*time.Microsecond), s.max.Round(100*time.Microsecond))
}

func statsOf(samples []time.Duration) sloStats {
	slices.Sort(samples)
	at := func(q float64) time.Duration { return samples[int(q*float64(len(samples)-1))] }
	return sloStats{at(0.5), at(0.99), samples[len(samples)-1]}
}

// sloResult is what a load run measured.
type sloResult struct {
	// endToEnd runs from the node showing a block to a subscriber holding
	// one of its trades; ingest from the node showing the block to its
	// events reaching the in-process bus (fetch, index, commit, outbox,
	// relay); push from the bus to the subscriber (engine, WebSocket).
	endToEnd, ingest, push sloStats
	trades                 int // per subscriber
	slowClosed             int // non-reading subscribers disconnected as slow
}

// runSLOLoad (refactoring plan R5-5) indexes a DEX chain whose blocks the
// node shows one per interval, each with swaps trades, while subscribers
// WebSocket clients subscribe to dexTrade, slow of which never read.
func runSLOLoad(t *testing.T, subscribers, slow, blocks, swaps int, interval time.Duration, queue int) sloResult {
	sc := testchain.BuildDEXLoad(blocks, swaps)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableJSONRPC = false
	cfg.API.EnableWebSocket = false
	cfg.API.RateLimit.Enabled = false // every client comes from 127.0.0.1
	cfg.EventBus.SubscriberBufferSize = queue
	on := true
	cfg.Features = map[string]config.FeatureConfig{dex.PoolsName: {Enabled: &on}, dex.TradesName: {Enabled: &on}}
	require.NoError(t, cfg.SetFeatureSettings(dex.PoolsName, dex.Settings{Venues: []dex.Venue{{Type: "uniswap_v2", Factory: sc.V2Factory.Hex()}}}))
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

	// When the node shows each block, and when its block event reaches the
	// bus.
	shown := make([]atomic.Int64, blocks+2)
	onBus := make([]atomic.Int64, blocks+2)
	busSub := app.eventBus.Subscribe("slo-blocks", []events.EventType{events.EventTypeBlock}, nil, blocks+16)
	go func() {
		for ev := range busSub.Channel {
			if b, ok := ev.(*events.BlockEvent); ok && b.Number < uint64(len(onBus)) {
				onBus[b.Number].Store(time.Now().UnixNano())
			}
		}
	}()

	dial := func() *websocket.Conn {
		header := http.Header{}
		header.Add("Sec-WebSocket-Protocol", "graphql-transport-ws")
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/graphql/ws", header)
		require.NoError(t, err)
		require.NoError(t, conn.WriteJSON(map[string]any{"type": "connection_init"}))
		var ack map[string]any
		require.NoError(t, conn.ReadJSON(&ack))
		require.NoError(t, conn.WriteJSON(map[string]any{"id": "t", "type": "subscribe",
			"payload": map[string]any{"query": `subscription { dexTrade { blockNumber logIndex } }`}}))
		// Messages are handled in order: the pong means the subscription
		// is in place.
		require.NoError(t, conn.WriteJSON(map[string]any{"type": "ping"}))
		var pong map[string]any
		require.NoError(t, conn.ReadJSON(&pong))
		require.Equal(t, "pong", pong["type"], "%v", pong)
		return conn
	}
	conns := make([]*websocket.Conn, subscribers)
	for i := range conns {
		conns[i] = dial()
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	total := blocks * swaps
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		e2e     []time.Duration
		push    []time.Duration
		failed  atomic.Int64
		minimum = total
	)
	for i := slow; i < subscribers; i++ {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()
			var mineE2E, minePush []time.Duration
			got := 0
			defer func() {
				mu.Lock()
				e2e, push = append(e2e, mineE2E...), append(push, minePush...)
				minimum = min(minimum, got)
				mu.Unlock()
			}()
			_ = c.SetReadDeadline(time.Now().Add(time.Duration(blocks)*interval + time.Minute))
			var last uint64
			for got < total {
				_, raw, rerr := c.ReadMessage()
				if rerr != nil {
					if failed.Add(1) == 1 {
						t.Logf("first failed reader after %d trades: %v", got, rerr)
					}
					return
				}
				var m struct {
					Type    string `json:"type"`
					Payload struct {
						Data struct {
							DexTrade struct{ BlockNumber string } `json:"dexTrade"`
						} `json:"data"`
						Extensions struct{ Sequence uint64 } `json:"extensions"`
					} `json:"payload"`
				}
				if err := json.Unmarshal(raw, &m); err != nil || m.Type != "next" || m.Payload.Extensions.Sequence <= last {
					if failed.Add(1) == 1 {
						t.Logf("first failed reader after %d trades: %v %s", got, err, raw)
					}
					return
				}
				now := time.Now().UnixNano()
				last = m.Payload.Extensions.Sequence
				got++
				b, _ := strconv.Atoi(m.Payload.Data.DexTrade.BlockNumber)
				if b > 0 && b < len(shown) {
					mineE2E = append(mineE2E, time.Duration(now-shown[b].Load()))
					minePush = append(minePush, time.Duration(now-onBus[b].Load()))
				}
			}
		}(conns[i])
	}

	ctx := t.Context()
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
	wg.Wait()
	require.Zero(t, failed.Load(), "every reading subscriber receives every trade, in order")
	require.Equal(t, total, minimum)

	var ingest []time.Duration
	for b := 2; b <= blocks+1; b++ {
		if at := onBus[b].Load(); at != 0 {
			ingest = append(ingest, time.Duration(at-shown[b].Load()))
		}
	}
	require.Len(t, ingest, blocks, "every block event reached the bus")

	// Subscribers that never read are disconnected as slow (when the run
	// overflowed their queue) instead of holding the others back.
	res := sloResult{endToEnd: statsOf(e2e), push: statsOf(push), ingest: statsOf(ingest), trades: total}
	for _, c := range conns[:slow] {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
					res.slowClosed++
				}
				break
			}
			if strings.Contains(string(data), "SLOW_SUBSCRIBER") {
				res.slowClosed++
				break
			}
		}
	}
	return res
}

// The G3 service level (refactoring plan R5-5, decided 10/9): with 1,000
// subscribers to dexTrade, trades at 10 per second (one block a second),
// the 99th percentile from the node showing a block to a subscriber holding
// one of its trades is at most 250 ms, and subscribers that never read (1%)
// make the others' 99th percentile at most 20% worse.
const (
	sloSubscribers   = 1000
	sloTradesPerSec  = 10
	sloP99           = 250 * time.Millisecond
	sloSlowTolerance = 1.2
)

// TestSLOLoad checks the G3 service level through the whole path: the node
// shows a block, the live loop fetches and indexes it, the outbox relay
// publishes its trades and the subscription engine sends them over
// WebSocket. It runs the load twice, without and with non-reading
// subscribers. Run it with INDEXER_LOAD_SLO=1 (about two minutes);
// INDEXER_LOAD_SUBSCRIBERS, _SWAPS (trades per block), _BLOCKS,
// _INTERVAL_MS, _QUEUE and _P99_MS change the load or the bound, for
// reports beyond the service level.
func TestSLOLoad(t *testing.T) {
	if os.Getenv("INDEXER_LOAD_SLO") == "" {
		t.Skip("set INDEXER_LOAD_SLO=1")
	}
	subscribers := envInt(t, "INDEXER_LOAD_SUBSCRIBERS", sloSubscribers)
	slow := max(1, subscribers/100)
	blocks := envInt(t, "INDEXER_LOAD_BLOCKS", 60)
	swaps := envInt(t, "INDEXER_LOAD_SWAPS", sloTradesPerSec)
	interval := time.Duration(envInt(t, "INDEXER_LOAD_INTERVAL_MS", 1000)) * time.Millisecond
	queue := envInt(t, "INDEXER_LOAD_QUEUE", 1024)
	bound := time.Duration(envInt(t, "INDEXER_LOAD_P99_MS", int(sloP99/time.Millisecond))) * time.Millisecond
	rate := float64(swaps) / interval.Seconds()
	report := func(name string, r sloResult, slow int) {
		t.Logf("%s: %d subscribers (%d not reading), %d blocks every %v with %d trades (%.0f trades/s, %.0f messages/s)",
			name, subscribers, slow, blocks, interval, swaps, rate, rate*float64(subscribers-slow))
		t.Logf("  end to end (node shows block -> subscriber has trade): %v", r.endToEnd)
		t.Logf("  ingest (node shows block -> block event on the bus):  %v", r.ingest)
		t.Logf("  push (bus -> subscriber has trade):                    %v", r.push)
	}

	alone := runSLOLoad(t, subscribers, 0, blocks, swaps, interval, queue)
	report("readers only", alone, 0)
	withSlow := runSLOLoad(t, subscribers, slow, blocks, swaps, interval, queue)
	report("with non-readers", withSlow, slow)
	t.Logf("  non-readers disconnected as slow: %d of %d", withSlow.slowClosed, slow)

	require.LessOrEqual(t, alone.endToEnd.p99, bound, "p99 end to end")
	require.LessOrEqual(t, withSlow.endToEnd.p99, bound, "p99 end to end with non-reading subscribers")
	require.LessOrEqual(t, float64(withSlow.endToEnd.p99), sloSlowTolerance*float64(alone.endToEnd.p99),
		"non-reading subscribers make the readers' p99 at most %.0f%% worse", (sloSlowTolerance-1)*100)
}
