package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mixedRPCMeter struct {
	mu       sync.Mutex
	attempts []time.Time
	next     http.Handler
}

func (m *mixedRPCMeter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.attempts = append(m.attempts, time.Now())
	m.mu.Unlock()
	m.next.ServeHTTP(w, r)
}
func (m *mixedRPCMeter) times() []time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Time(nil), m.attempts...)
}

type mixedAPIObservation struct {
	Route, Phase, Result string
	MS                   float64
	Diagnostics          mixedAPIDiagnostics
	Client               mixedAPIClientTiming
	OutsideHandlerMS     *float64
	Started, Finished    time.Time
}
type mixedAPISummary struct {
	Requests, Success                                 int
	Errors                                            map[string]int
	AllMS, SuccessMS                                  *sourceSearchLatency
	AcquireMS, QueryMS, ServerMS, OtherServerMS       *sourceSearchLatency
	Queries                                           map[int]int
	PrepareMS, HTTPMS, ValidationMS, OutsideHandlerMS *sourceSearchLatency
	IncompleteDiagnostics, InvalidServerResiduals     int
}

func mixedLatency(samples []float64) *sourceSearchLatency {
	if len(samples) == 0 {
		return nil
	}
	v := sourceSearchPercentiles(append([]float64(nil), samples...))
	return &v
}
func mixedAPISummaries(all []mixedAPIObservation) map[string]mixedAPISummary {
	groups := map[string][]mixedAPIObservation{}
	for _, v := range all {
		groups[v.Phase+"/"+v.Route] = append(groups[v.Phase+"/"+v.Route], v)
	}
	out := map[string]mixedAPISummary{}
	for key, group := range groups {
		v := mixedAPISummary{Requests: len(group), Errors: map[string]int{}, Queries: map[int]int{}}
		var every, success, acquire, query, server, other, prepare, httpMS, validation, outside []float64
		for _, row := range group {
			every = append(every, row.MS)
			prepare = append(prepare, row.Client.PrepareMS)
			if row.Client.HTTPStarted {
				httpMS = append(httpMS, row.Client.HTTPMS)
			}
			if row.Client.ValidationStarted {
				validation = append(validation, row.Client.ValidationMS)
			}
			if row.OutsideHandlerMS != nil {
				outside = append(outside, *row.OutsideHandlerMS)
			}
			if row.Diagnostics.HandlerComplete {
				acquire = append(acquire, row.Diagnostics.AcquireMS)
				query = append(query, row.Diagnostics.QueryMS)
				server = append(server, row.Diagnostics.ServerMS)
				residual := row.Diagnostics.ServerMS - row.Diagnostics.AcquireMS - row.Diagnostics.QueryMS
				if residual >= 0 {
					other = append(other, residual)
				} else {
					v.InvalidServerResiduals++
				}
			} else {
				v.IncompleteDiagnostics++
			}
			v.Queries[row.Diagnostics.Queries]++
			if row.Result == "ok" {
				v.Success++
				success = append(success, row.MS)
			} else {
				v.Errors[row.Result]++
			}
		}
		v.AllMS, v.SuccessMS = mixedLatency(every), mixedLatency(success)
		v.AcquireMS, v.QueryMS, v.ServerMS, v.OtherServerMS = mixedLatency(acquire), mixedLatency(query), mixedLatency(server), mixedLatency(other)
		v.PrepareMS, v.HTTPMS, v.ValidationMS, v.OutsideHandlerMS = mixedLatency(prepare), mixedLatency(httpMS), mixedLatency(validation), mixedLatency(outside)
		out[key] = v
	}
	return out
}

func mixedGraphQL(ctx context.Context, client *http.Client, endpoint, query string, chain *testchain.Chain, list bool) string {
	result, _ := mixedGraphQLTimed(ctx, client, endpoint, query, chain, list)
	return result
}

func mixedGraphQLTimed(ctx context.Context, client *http.Client, endpoint, query string, chain *testchain.Chain, list bool) (result string, timing mixedAPIClientTiming) {
	prepare := time.Now()
	request, _ := json.Marshal(map[string]string{"query": query})
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/graphql", bytes.NewReader(request))
	if e != nil {
		timing.PrepareMS = mixedMS(time.Since(prepare))
		return "request_error", timing
	}
	r.Header.Set("Content-Type", "application/json")
	if id, ok := ctx.Value(mixedTraceKey{}).(string); ok {
		r.Header.Set("X-Indexer-Measure", id)
	}
	timing.PrepareMS = mixedMS(time.Since(prepare))
	httpStart := time.Now()
	timing.HTTPStarted = true
	response, e := client.Do(r)
	if e != nil {
		timing.HTTPMS = mixedMS(time.Since(httpStart))
		return mixedHTTPError(e, "transport_error"), timing
	}
	defer func() { _ = response.Body.Close() }()
	value, e := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	timing.HTTPMS = mixedMS(time.Since(httpStart))
	if e != nil {
		return mixedHTTPError(e, "body_error"), timing
	}
	validationStart := time.Now()
	timing.ValidationStarted = true
	defer func() { timing.ValidationMS = mixedMS(time.Since(validationStart)) }()
	return mixedValidateGraphQL(value, response.StatusCode, chain, list), timing
}

func mixedHTTPError(err error, fallback string) string {
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return fallback
}

func mixedValidateGraphQL(value []byte, status int, chain *testchain.Chain, list bool) string {
	if status != http.StatusOK {
		return fmt.Sprint("http_", status)
	}
	type block struct {
		Number           json.RawMessage
		Hash             common.Hash
		TransactionCount int
	}
	var out struct {
		Errors []json.RawMessage
		Data   struct {
			Block  *block
			Blocks struct{ Nodes []block }
		}
	}
	if json.Unmarshal(value, &out) != nil {
		return "invalid_data"
	}
	if len(out.Errors) != 0 {
		return "graphql_error"
	}
	var blocks []block
	if list {
		blocks = out.Data.Blocks.Nodes
		if len(blocks) < 1 || len(blocks) > 8 {
			return "invalid_data"
		}
	} else if out.Data.Block != nil {
		blocks = []block{*out.Data.Block}
	} else {
		return "invalid_data"
	}
	for _, b := range blocks {
		n, e := strconv.ParseUint(strings.Trim(string(b.Number), "\""), 10, 64)
		if e != nil || n == 0 || n > chain.Head() || chain.Block(n).Block.Hash() != b.Hash || b.TransactionCount != 2 || !list && n != 1 {
			return "invalid_data"
		}
	}
	return "ok"
}

// Opt-in comparison of REAL ETH68 intake/window/capture, the ordered writer
// and production GraphQL handlers sharing ONE PostgreSQL Store/pool. No
// source lookup worker or API proxy adds RPC demand. Runtime limits/fences
// remain unchanged. The four-block mode is for race/lifecycle verification.
func TestMeasureManagedFeedPoolLoad(t *testing.T) {
	if os.Getenv("INDEXER_MEASURE_MIXED_POOL") != "1" || !testOnPostgres() {
		t.Skip("set INDEXER_MEASURE_MIXED_POOL=1 and INDEXER_TEST_DRIVER=postgres")
	}
	budget, e := strconv.Atoi(os.Getenv("INDEXER_MEASURE_MIXED_CONNS"))
	require.NoError(t, e)
	require.Contains(t, []int{2, 4}, budget)
	blocks := 32
	if value := os.Getenv("INDEXER_MEASURE_MIXED_BLOCKS"); value != "" {
		blocks, e = strconv.Atoi(value)
		require.NoError(t, e)
		require.Contains(t, []int{4, 32, 128}, blocks)
	}
	arrivalMS := 500
	if value := os.Getenv("INDEXER_MEASURE_MIXED_INTERVAL_MS"); value != "" {
		arrivalMS, e = strconv.Atoi(value)
		require.NoError(t, e)
		require.Contains(t, []int{1, 500}, arrivalMS, "1ms is a queued backlog fixture, not a network block-time claim")
	}
	const warm = 2
	height := uint64(warm + blocks)
	from, to := testchain.NewAccount(21), testchain.NewAccount(22)
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	for n := 1; n <= warm+blocks; n++ {
		var specs []testchain.TxSpec
		for tx := 0; tx < 2; tx++ {
			var logs []*types.Log
			for i := 0; i < 8; i++ {
				logs = append(logs, &types.Log{Address: common.Address{byte(n % 16)}, Topics: []common.Hash{{7}, {byte(i % 4)}}, Data: []byte("fixed-mixed-pool-fixture")})
			}
			specs = append(specs, testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000)}, GasUsed: 21000, Logs: logs})
		}
		c.AddBlock(specs...)
	}
	c.SetHead(warm)
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Database.Postgres.MaxConns = int32(budget)
	cfg.Indexer.Workers = 2
	cfg.Indexer.PipelineMetrics = true
	cfg.Source.Feed.Telemetry = &feed.TelemetryConfig{ReceiveIntervalTarget: 450 * time.Millisecond}
	target, e := url.Parse(rpc.URL())
	require.NoError(t, e)
	meter := &mixedRPCMeter{next: httputil.NewSingleHostReverseProxy(target)}
	proxy := httptest.NewServer(meter)
	defer proxy.Close()
	cfg.RPC.Endpoint = proxy.URL
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	defer a.Shutdown()
	apiConfig := api.DefaultConfig()
	apiConfig.ReserveQueryReader = true
	apiServer, e := api.NewServer(apiConfig, zap.NewNop(), a.storage)
	require.NoError(t, e)
	a.apiServer = apiServer // App Shutdown owns the production handler background tasks.
	var traces sync.Map
	httpServer := httptest.NewServer(mixedObservedHTTP(apiServer.Router(), &traces))
	defer httpServer.Close()
	apiClient := httpServer.Client()
	apiClient.Timeout = 2 * time.Second
	defer apiClient.CloseIdleConnections()
	runCtx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var appErr error
	go func() { appErr = a.Run(runCtx); close(done) }()
	defer func() { cancel(); <-done }()
	require.Eventually(t, func() bool { return a.fetcher.GetPipelineMetrics().SourceAck.Count == warm }, 30*time.Second, 10*time.Millisecond)
	queries := []string{`{ block(number: "1") { number hash transactionCount } }`, `{ blocks(pagination: {limit: 8}) { nodes { number hash transactionCount } } }`}
	for i, q := range queries {
		require.Equal(t, "ok", mixedGraphQL(t.Context(), apiClient, httpServer.URL, q, c, i == 1), "warm API query")
	}
	var draining atomic.Bool
	var apiMu sync.Mutex
	var apiRows []mixedAPIObservation
	stopAPI := make(chan struct{})
	var stopOnce sync.Once
	var apiWG sync.WaitGroup
	stopLoad := func() { stopOnce.Do(func() { close(stopAPI) }); apiWG.Wait() }
	defer stopLoad()
	started := time.Now()
	beforeRPC := rpc.Calls()
	for worker := 0; worker < 2; worker++ {
		apiWG.Add(1)
		go func(worker int) {
			defer apiWG.Done()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for i := worker; ; i++ {
				select {
				case <-stopAPI:
					return
				case <-ticker.C:
				}
				phase := "injection"
				if draining.Load() {
					phase = "drain"
				}
				q := i % 2
				start := time.Now()
				id := fmt.Sprintf("%d/%d", worker, i)
				trace := &mixedAPITrace{done: make(chan struct{})}
				traces.Store(id, trace)
				result, clientTiming := mixedGraphQLTimed(context.WithValue(runCtx, mixedTraceKey{}, id), apiClient, httpServer.URL, queries[q], c, q == 1)
				finished := time.Now()
				select {
				case <-trace.done:
				case <-runCtx.Done():
				case <-time.After(2 * time.Second):
				}
				traces.Delete(id)
				diagnostics := trace.snapshot()
				row := mixedAPIObservation{Route: []string{"block", "blocks8"}[q], Phase: phase, Result: result, MS: mixedMS(finished.Sub(start)), Started: start, Finished: finished, Diagnostics: diagnostics, Client: clientTiming}
				row.OutsideHandlerMS = mixedOutsideHandler(row)
				apiMu.Lock()
				apiRows = append(apiRows, row)
				apiMu.Unlock()
			}
		}(worker)
	}
	released := make([]time.Time, height+1)
	acked := make([]time.Time, height+1)
	next := uint64(warm + 1)
	var injectionEnd, ended time.Time
	var observed, captured, acknowledged uint64
	maxGaps := map[string]uint64{}
	var timeline []map[string]any
	plan, e := a.managedFeed.program.Plan.Digest()
	require.NoError(t, e)
	readRuntime := func() feedtelemetry.Snapshot {
		m := a.managedFeed
		return feedtelemetry.Read(m.telemetry.Descriptor(), m.program.Anchor, plan)
	}
	arrival := time.NewTicker(time.Duration(arrivalMS) * time.Millisecond)
	defer arrival.Stop()
	sample := time.NewTicker(20 * time.Millisecond)
	defer sample.Stop()
	deadline := time.NewTimer(180 * time.Second)
	defer deadline.Stop()
	var injectionSnapshot map[string]uint64
	for ended.IsZero() {
		select {
		case <-done:
			t.Fatalf("app stopped during load: %v", appErr)
		case <-deadline.C:
			t.Fatalf("load did not drain: released=%d observed=%d captured=%d ACK=%d", next-1, observed, captured, acknowledged)
		case <-arrival.C:
			if next <= height {
				released[next] = time.Now()
				if trace.IsEnabled() {
					trace.Logf(runCtx, "feed.release", "%d", next)
				}
				c.SetHead(next)
				next++
				if next > height {
					injectionEnd = time.Now()
					draining.Store(true)
					arrival.Stop()
				}
			}
		case now := <-sample.C:
			runtime := readRuntime()
			if runtime.Observed != nil {
				observed = runtime.Observed.Block.Number
			}
			if p, ok := runtime.Progress["captured"]; ok {
				captured = p.Input.Block.Number
			}
			stats := a.fetcher.GetPipelineMetrics()
			for n := acknowledged + 1; n <= stats.SourceAck.Count && n <= height; n++ {
				acked[n] = time.Now() // upper-bound observation; 20ms nominal polling
			}
			acknowledged = stats.SourceAck.Count
			for name, pair := range map[string][2]uint64{"intake": {next - 1, observed}, "capture": {observed, captured}, "writer": {captured, acknowledged}, "total": {next - 1, acknowledged}} {
				if pair[0] >= pair[1] {
					maxGaps[name] = max(maxGaps[name], pair[0]-pair[1])
				}
			}
			if len(timeline) == 0 || now.Sub(started).Seconds() >= float64(len(timeline))/2 {
				timeline = append(timeline, map[string]any{"seconds": now.Sub(started).Seconds(), "released": next - 1, "observed": observed, "captured": captured, "acknowledged": acknowledged})
			}
			if !injectionEnd.IsZero() && injectionSnapshot == nil {
				injectionSnapshot = map[string]uint64{"released": next - 1, "observed": observed, "captured": captured, "acknowledged": acknowledged}
			}
			if captured == height && acknowledged == height {
				ended = time.Now()
			}
		}
	}
	timedRPC := rpc.Calls()
	stopLoad()
	stats := a.fetcher.GetPipelineMetrics()
	runtime := readRuntime()
	cancel()
	<-done
	require.ErrorIs(t, appErr, context.Canceled)
	require.Eventually(t, func() bool { return readRuntime().Metrics["rpcHTTP"].InFlight == 0 }, 3*time.Second, time.Millisecond)
	beforeOffline := rpc.Calls()
	for i, q := range queries {
		require.Equal(t, "ok", mixedGraphQL(t.Context(), apiClient, httpServer.URL, q, c, i == 1))
	}
	require.Equal(t, beforeOffline, rpc.Calls(), "GraphQL queries are DB-only")
	require.EqualValues(t, height, stats.SourceValidate.Count)
	require.EqualValues(t, height, stats.SourceAck.Count)
	require.Zero(t, stats.SourceValidate.Errors)
	require.Zero(t, stats.SourceAck.Errors)
	require.EqualValues(t, height, runtime.Metrics["captureCommit"].Count)
	require.Zero(t, runtime.Metrics["captureCommit"].Failures)
	var releaseCapture, releaseACK []float64
	var blockObservations []map[string]any
	for n := uint64(warm + 1); n <= height; n++ {
		age := feedtelemetry.PendingAge(a.managedFeed.telemetry.Descriptor(), a.managedFeed.program.Anchor, plan, feedtelemetry.Captured, 1, n)
		require.NotNil(t, age)
		captureAt := time.Now().Add(-time.Duration(*age * float64(time.Millisecond)))
		releaseCapture = append(releaseCapture, float64(captureAt.Sub(released[n]))/float64(time.Millisecond))
		releaseACK = append(releaseACK, float64(acked[n].Sub(released[n]))/float64(time.Millisecond))
		blockObservations = append(blockObservations, map[string]any{"height": n, "released": released[n], "captured": captureAt, "ackObserved": acked[n]})
	}
	var attempts []time.Time
	for _, at := range meter.times() {
		if !at.Before(started) && !at.After(ended) {
			attempts = append(attempts, at)
		}
	}
	for i, at := range attempts {
		require.GreaterOrEqual(t, at.Sub(attempts[0]), time.Duration(i)*200*time.Millisecond-20*time.Millisecond)
	}
	// A cancelled shutdown attempt can fail before reaching the loopback
	// endpoint. Every observed HTTP arrival must still belong to this pool.
	require.GreaterOrEqual(t, a.client.Pool().Status()[0].Requests, uint64(len(meter.times())))
	counts := map[string]int{}
	for method, count := range timedRPC {
		counts[method] = count - beforeRPC[method]
	}
	require.Zero(t, counts["eth_getBlockReceipts"])
	require.Zero(t, counts["eth_getBalance"])
	require.Zero(t, counts["eth_call"])
	state, e := a.managedFeed.store.State(t.Context())
	require.NoError(t, e)
	require.Equal(t, height, state.Through.Number)
	digest := sha256.New()
	for n := uint64(1); n <= height; n++ {
		b, e := a.storage.GetBlock(t.Context(), n)
		require.NoError(t, e)
		require.Equal(t, c.Block(n).Block.Hash(), b.Hash)
		value, e := model.EncodeBlock(b)
		require.NoError(t, e)
		_, _ = digest.Write(value)
		for _, tx := range b.Transactions {
			r, e := a.storage.GetReceipt(t.Context(), tx.Hash)
			require.NoError(t, e)
			value, e := model.EncodeReceipt(r)
			require.NoError(t, e)
			_, _ = digest.Write(value)
		}
		id, e := a.managedFeed.store.CanonicalAt(t.Context(), state.Generation, n)
		require.NoError(t, e)
		ack, e := a.managedFeed.store.ProjectionAck(t.Context(), cfg.Source.Feed.Consumer, state.Generation, id)
		require.NoError(t, e)
		require.Equal(t, id, ack.Identity)
	}
	var events []port.OutboxEntry
	var sequence uint64
	for {
		page, err := a.storage.(port.Outbox).ReadOutbox(t.Context(), sequence, 1000)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		events = append(events, page...)
		require.LessOrEqual(t, len(events), (warm+blocks)*32, "bounded fixture outbox")
		sequence = page[len(page)-1].Seq
	}
	for _, event := range events {
		var data map[string]any
		require.NoError(t, json.Unmarshal(event.Data, &data))
		delete(data, "created_at")
		value, e := json.Marshal([]any{event.Seq, event.Type, data})
		require.NoError(t, e)
		_, _ = digest.Write(value)
	}
	var measuredAPI []mixedAPIObservation
	var excludedAPI []mixedAPIObservation
	var completedInjection, completedRun int
	for _, row := range apiRows {
		if row.Started.After(ended) {
			excludedAPI = append(excludedAPI, row)
			continue
		}
		measuredAPI = append(measuredAPI, row)
		if !row.Finished.After(injectionEnd) {
			completedInjection++
		}
		if !row.Finished.After(ended) {
			completedRun++
		}
	}
	apiSummary := mixedAPISummaries(measuredAPI)
	result := map[string]any{"maxConns": budget, "timedBlocks": blocks, "warmBlocks": warm, "transactions": (warm + blocks) * 2, "logs": (warm + blocks) * 16, "seconds": ended.Sub(started).Seconds(), "injectionSeconds": injectionEnd.Sub(started).Seconds(), "drainSeconds": ended.Sub(injectionEnd).Seconds(), "blocksPerSecond": float64(blocks) / ended.Sub(started).Seconds(), "apiTargetRPS": 20, "apiConcurrency": 2, "api": apiSummary, "releaseToCaptureMS": mixedLatency(releaseCapture), "releaseToACKObservedMS": mixedLatency(releaseACK), "maxHeightGaps": maxGaps, "injectionEnd": injectionSnapshot, "physicalRequests": len(attempts), "logicalRPC": counts, "pipeline": stats, "runtime": runtime, "timeline": timeline, "outputDigest": hex.EncodeToString(digest.Sum(nil)), "outboxEvents": len(events)}
	result["apiCompletedRPSInjection"] = float64(completedInjection) / injectionEnd.Sub(started).Seconds()
	result["arrivalIntervalMS"] = arrivalMS
	result["apiCompletedRPSRun"] = float64(completedRun) / ended.Sub(started).Seconds()
	result["apiLateCompletions"] = len(measuredAPI) - completedRun
	result["apiReservedReadSlots"] = apiServer.ReservedQueryReaders()
	slowest := append([]mixedAPIObservation(nil), measuredAPI...)
	sort.Slice(slowest, func(i, j int) bool { return slowest[i].MS > slowest[j].MS })
	if len(slowest) > 8 {
		slowest = slowest[:8]
	}
	result["slowestAPI"] = slowest
	// Keep every correlated request, including failures, for point-tail attribution.
	result["apiObservations"] = measuredAPI
	result["apiExcludedObservations"] = excludedAPI
	verdict := mixedAPIVerdict(apiRows)
	result["apiValidation"] = verdict
	result["timingSemantics"] = map[string]string{
		"AllMS":            "Client preparation + HTTP through body read + JSON/fixture validation + cleanup; unchanged overall boundary",
		"HTTPMS":           "Client.Do through response body read, including transport/server/scheduling; not pure network time",
		"QueryMS":          "pgx SQL roundtrip through row consumption; not pure PostgreSQL execution",
		"GraphQLMS":        "Nested limits/request_prepare/parse/validation/execution/response; execution includes DB and decode",
		"OutsideHandlerMS": "Only when complete server interval lies inside client interval; residual is not network-only",
	}
	result["blockObservations"] = blockObservations
	value, e := json.Marshal(result)
	require.NoError(t, e)
	fmt.Printf("MIXED_POOL_MEASURE %s\n", value)
	// Emit correlated failures BEFORE failing, so a timeout cannot look like
	// a successful Go run or disappear from diagnostics when the runner stops.
	require.True(t, verdict.Passed, "API validation failed: %+v", verdict)
}

// Test-only request correlation; the header is recognized only by this fixture.
type mixedTraceKey struct{}
type mixedAPIDiagnostics struct {
	AcquireMS, QueryMS, ServerMS, MaxAcquireMS, MaxQueryMS float64
	Queries                                                int
	HandlerComplete                                        bool
	ServerStarted, ServerFinished                          time.Time
	GraphQLMS                                              map[string]float64
	BlockDecodeMS, AdmissionMS                             float64
	AdmissionPaths                                         map[string]int
	BorrowProbes, AdmissionFailures                        int
}
type mixedAPITrace struct {
	mu   sync.Mutex
	data mixedAPIDiagnostics
	done chan struct{}
	// Set before dispatch, never mutated while callbacks are running.
	readHook func(postgres.ReadObservation)
}

func (t *mixedAPITrace) observe(o postgres.QueryObservation) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ms := float64(o.Elapsed) / float64(time.Millisecond)
	switch o.Stage {
	case "acquire":
		t.data.AcquireMS += ms
		if ms > t.data.MaxAcquireMS {
			t.data.MaxAcquireMS = ms
		}
	case "query":
		t.data.QueryMS += ms
		t.data.Queries++
		if ms > t.data.MaxQueryMS {
			t.data.MaxQueryMS = ms
		}
	}
}
