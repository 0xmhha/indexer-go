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
	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/internal/features/dex"
	"github.com/0xmhha/indexer-go/pkg/events"
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
	// Nearest rank, as in pipeline metrics. With 60 blocks, floor(q*(n-1))
	// discarded the slowest block at p99 even though it affects 1/60 of
	// each subscriber's messages.
	at := func(p int) time.Duration { return samples[(len(samples)*p+99)/100-1] }
	return sloStats{at(50), at(99), samples[len(samples)-1]}
}

type sloBlockLatency struct {
	height  uint64
	elapsed time.Duration
}

// sloResult is what a load run measured.
type sloResult struct {
	// endToEnd runs from the node showing a block to a subscriber holding
	// one of its trades; ingest from the node showing the block to its
	// events reaching the in-process bus (fetch, index, commit, outbox,
	// relay); push from the bus to the subscriber (engine, WebSocket).
	endToEnd, ingest, push    sloStats
	trades                    int // per subscriber
	slowClosed                int // non-reading subscribers disconnected as slow
	worstIngest, worstReceive sloBlockLatency
	pipeline                  string // opt-in diagnostic snapshot; no profiling in normal runs
}

// runSLOLoad (refactoring plan R5-5) indexes a DEX chain whose blocks the
// node shows one per interval, each with swaps trades, while subscribers
// WebSocket clients subscribe to dexTrade, slow of which never read.
func runSLOLoad(t *testing.T, subscribers, slow, blocks, swaps int, interval time.Duration, queue int) sloResult {
	sc := testchain.BuildDEXLoadMixed(blocks, swaps) // V2, V3 and perpetual order book trades
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	cfg := config.NewConfig()
	cfg.Indexer.StatePrefetch = os.Getenv("INDEXER_LOAD_STATE_PREFETCH") == "1"
	cfg.Indexer.AcquisitionQueue = os.Getenv("INDEXER_LOAD_ACQUISITION_QUEUE") == "1"
	cfg.Indexer.StateBatchSize = envInt(t, "INDEXER_LOAD_STATE_BATCH_SIZE", 1)
	cfg.Indexer.PipelineMetrics = os.Getenv("INDEXER_LOAD_DIAGNOSTICS") == "1"
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
	require.NoError(t, cfg.SetFeatureSettings(dex.PoolsName, dex.Settings{Venues: []dex.Venue{
		{Type: "uniswap_v2", Factory: sc.V2Factory.Hex()},
		{Type: "uniswap_v3", Factory: sc.V3Factory.Hex()},
		{Type: "perp_orderbook", Engine: sc.Engine.Hex(), OrderManager: sc.OrderManager.Hex()},
	}}))
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
		wg           sync.WaitGroup
		mu           sync.Mutex
		e2e          []time.Duration
		push         []time.Duration
		failed       atomic.Int64
		minimum      = total
		worstReceive sloBlockLatency
	)
	for i := slow; i < subscribers; i++ {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()
			var mineE2E, minePush []time.Duration
			got := 0
			var mineWorst sloBlockLatency
			defer func() {
				mu.Lock()
				e2e, push = append(e2e, mineE2E...), append(push, minePush...)
				minimum = min(minimum, got)
				if mineWorst.elapsed > worstReceive.elapsed {
					worstReceive = mineWorst
				}
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
					elapsed := time.Duration(now - shown[b].Load())
					mineE2E = append(mineE2E, elapsed)
					if elapsed > mineWorst.elapsed {
						mineWorst = sloBlockLatency{uint64(b), elapsed}
					}
					minePush = append(minePush, time.Duration(now-onBus[b].Load()))
				}
			}
		}(conns[i])
	}

	ctx := t.Context()
	stopLoop := startLoadFetcher(t, app, ctx)
	defer stopLoop()
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
	var worstIngest sloBlockLatency
	for b := 2; b <= blocks+1; b++ {
		if at := onBus[b].Load(); at != 0 {
			elapsed := time.Duration(at - shown[b].Load())
			ingest = append(ingest, elapsed)
			if elapsed > worstIngest.elapsed {
				worstIngest = sloBlockLatency{uint64(b), elapsed}
			}
		}
	}
	require.Len(t, ingest, blocks, "every block event reached the bus")

	// Subscribers that never read are disconnected as slow (when the run
	// overflowed their queue) instead of holding the others back.
	res := sloResult{endToEnd: statsOf(e2e), push: statsOf(push), ingest: statsOf(ingest), trades: total}
	res.worstIngest, res.worstReceive = worstIngest, worstReceive
	if cfg.Indexer.PipelineMetrics {
		data, err := json.Marshal(app.fetcher.GetPipelineMetrics())
		require.NoError(t, err)
		res.pipeline = string(data)
	}
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
// WebSocket. It runs the load without and with non-reading subscribers in
// turn, INDEXER_LOAD_ROUNDS times each (default 2). Every run must meet the
// p99 bound; the non-readers' effect compares the median p99 of each kind
// of run: a run's p99 is about its slowest of 60 blocks, so two single runs
// differ by up to about 15% with nothing changed, close to the 20% allowed
// (10/12: the single-run comparison failed 1 run in 7 on main). Run it with
// INDEXER_LOAD_SLO=1 (about four minutes); INDEXER_LOAD_SUBSCRIBERS, _SWAPS
// (trades per block), _BLOCKS, _INTERVAL_MS, _QUEUE and _P99_MS change the
// load or the bound, for reports beyond the service level.
func TestSLOLoad(t *testing.T) {
	if os.Getenv("INDEXER_LOAD_SLO") == "" {
		t.Skip("set INDEXER_LOAD_SLO=1")
	}
	subscribers := envInt(t, "INDEXER_LOAD_SUBSCRIBERS", sloSubscribers)
	slow := max(1, subscribers/100)
	blocks := envInt(t, "INDEXER_LOAD_BLOCKS", 60)
	swaps := envInt(t, "INDEXER_LOAD_SWAPS", sloTradesPerSec)
	interval := time.Duration(envInt(t, "INDEXER_LOAD_INTERVAL_MS", 1000)) * time.Millisecond
	// The production default; at this load no queue fills, so a smaller one
	// measured the same (1024: p99 87/96 ms, 16384: 89/94 ms on 10/12).
	queue := envInt(t, "INDEXER_LOAD_QUEUE", constants.DefaultEventBusSubscriberBuffer)
	bound := time.Duration(envInt(t, "INDEXER_LOAD_P99_MS", int(sloP99/time.Millisecond))) * time.Millisecond
	rate := float64(swaps) / interval.Seconds()
	report := func(name string, r sloResult, slow int) {
		t.Logf("%s: %d subscribers (%d not reading), %d blocks every %v with %d trades (%.0f trades/s, %.0f messages/s)",
			name, subscribers, slow, blocks, interval, swaps, rate, rate*float64(subscribers-slow))
		t.Logf("  end to end (node shows block -> subscriber has trade): %v", r.endToEnd)
		t.Logf("  ingest (node shows block -> block event on the bus):  %v", r.ingest)
		t.Logf("  push (bus -> subscriber has trade):                    %v", r.push)
		t.Logf("  slowest ingest: block %d, %v; slowest receipt: block %d, %v",
			r.worstIngest.height, r.worstIngest.elapsed, r.worstReceive.height, r.worstReceive.elapsed)
		if r.pipeline != "" {
			t.Logf("  pipeline (commit excludes RPC/queue/feature handling): %s", r.pipeline)
		}
	}

	rounds := max(1, envInt(t, "INDEXER_LOAD_ROUNDS", 2))
	var aloneP99, slowP99 []time.Duration
	for round := 1; round <= rounds; round++ {
		alone := runSLOLoad(t, subscribers, 0, blocks, swaps, interval, queue)
		report(fmt.Sprintf("round %d, readers only", round), alone, 0)
		withSlow := runSLOLoad(t, subscribers, slow, blocks, swaps, interval, queue)
		report(fmt.Sprintf("round %d, with non-readers", round), withSlow, slow)
		t.Logf("  non-readers disconnected as slow: %d of %d", withSlow.slowClosed, slow)
		require.LessOrEqual(t, alone.endToEnd.p99, bound, "p99 end to end, round %d", round)
		require.LessOrEqual(t, withSlow.endToEnd.p99, bound, "p99 end to end with non-reading subscribers, round %d", round)
		aloneP99, slowP99 = append(aloneP99, alone.endToEnd.p99), append(slowP99, withSlow.endToEnd.p99)
	}

	aloneMid, slowMid := medianDuration(aloneP99), medianDuration(slowP99)
	t.Logf("median p99 end to end over %d rounds: readers only %v, with non-readers %v (%+.0f%%)",
		rounds, aloneMid.Round(100*time.Microsecond), slowMid.Round(100*time.Microsecond), 100*(float64(slowMid)/float64(aloneMid)-1))
	require.LessOrEqual(t, float64(slowMid), sloSlowTolerance*float64(aloneMid),
		"non-reading subscribers make the readers' median p99 at most %.0f%% worse", (sloSlowTolerance-1)*100)
}

// medianDuration is the median of ds (the mean of the middle two for an
// even count).
func medianDuration(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
