package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/progress"
	"github.com/0xmhha/indexer-go/pkg/projection"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Only this explicit loopback fixture implements debug tracing. Production
// testchain never pretends that receipts contain internal execution traces.
type appTraceProxy struct {
	chain    *testchain.Chain
	upstream string
	calls    atomic.Int32
	fail     atomic.Bool
	client   *http.Client
}

func (p *appTraceProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if json.NewDecoder(r.Body).Decode(&raw) != nil {
		w.WriteHeader(400)
		return
	}
	type request struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	var reqs []request
	batch := len(raw) > 0 && raw[0] == '['
	if batch {
		_ = json.Unmarshal(raw, &reqs)
	} else {
		var req request
		_ = json.Unmarshal(raw, &req)
		reqs = []request{req}
	}
	replies := []json.RawMessage{}
	for _, req := range reqs {
		if req.Method != "debug_traceBlockByHash" {
			body, _ := json.Marshal(req)
			up, e := http.NewRequestWithContext(r.Context(), "POST", p.upstream, bytes.NewReader(body))
			if e != nil {
				w.WriteHeader(500)
				return
			}
			up.Header.Set("Content-Type", "application/json")
			response, e := p.client.Do(up)
			if e != nil {
				w.WriteHeader(502)
				return
			}
			out, e := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if e != nil {
				w.WriteHeader(502)
				return
			}
			replies = append(replies, out)
			continue
		}
		p.calls.Add(1)
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if p.fail.Load() {
			reply["error"] = map[string]any{"code": -32601, "message": "fixture: debug disabled or parent state pruned"}
		} else {
			var hash common.Hash
			if len(req.Params) != 2 || json.Unmarshal(req.Params[0], &hash) != nil {
				w.WriteHeader(400)
				return
			}
			var block *testchain.Block
			for n := uint64(1); n <= p.chain.Head(); n++ {
				candidate := p.chain.Block(n)
				if candidate != nil && candidate.Block.Hash() == hash {
					block = candidate
					break
				}
			}
			if block == nil {
				reply["error"] = map[string]any{"code": -32000, "message": "missing block"}
			} else {
				result := []any{}
				for i, tx := range block.Block.Transactions() {
					from, e := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
					if e != nil {
						w.WriteHeader(500)
						return
					}
					root := map[string]any{"type": "CALL", "from": from, "to": tx.To(), "gas": hexutil.Uint64(tx.Gas()), "gasUsed": hexutil.Uint64(block.Receipts[i].GasUsed), "value": (*hexutil.Big)(tx.Value()), "input": hexutil.Bytes(tx.Data())}
					root["calls"] = []any{map[string]any{"type": "STATICCALL", "from": tx.To(), "to": common.Address{9}, "gas": "0x100", "gasUsed": "0x10", "input": "0x"}}
					if block.Receipts[i].Status == 0 {
						root["error"] = "execution reverted"
					}
					result = append(result, map[string]any{"txHash": tx.Hash(), "result": root})
				}
				reply["result"] = result
			}
		}
		out, _ := json.Marshal(reply)
		replies = append(replies, out)
	}
	w.Header().Set("Content-Type", "application/json")
	if batch {
		_ = json.NewEncoder(w).Encode(replies)
	} else {
		_, _ = w.Write(replies[0])
	}
}
func traceAppConfig(t *testing.T) (*config.Config, *testchain.Chain, *appTraceProxy) {
	t.Helper()
	a, to := testchain.NewAccount(1), testchain.NewAccount(2).Address
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{a.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	for i := 0; i < 2; i++ {
		c.AddBlock(testchain.TxSpec{From: a, Tx: &types.DynamicFeeTx{Nonce: uint64(i), To: &to, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(0)}, GasUsed: 21000})
	}
	cfg, _, rpc := feedAppConfig(t, c)
	proxy := &appTraceProxy{chain: c, upstream: rpc.URL(), client: &http.Client{Timeout: 2 * time.Second}}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	cfg.RPC.Endpoint = server.URL
	cfg.Source.Feed.CallTraces = &evmtrace.Config{Name: "call-v1", Provider: "fixture-pool", StartHeight: 1}
	cfg.Source.Feed.Telemetry = &feed.TelemetryConfig{}
	cfg.API.Enabled = true
	return cfg, c, proxy
}
func waitCallTraces(t *testing.T, a *App, n uint64) indexingstatus.Status {
	t.Helper()
	var out indexingstatus.Status
	require.Eventually(t, func() bool {
		var e error
		out, e = indexingstatus.Read(t.Context(), a.storage, "", 100)
		return e == nil && out.CallTraces != nil && out.CallTraces.Complete != nil && out.CallTraces.Complete.Height == n
	}, 30*time.Second, 20*time.Millisecond)
	return out
}
func TestManagedCallTraceRestartReorgAndReadonlyAPI(t *testing.T) {
	cfg, c, proxy := traceAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	status := waitCallTraces(t, a, 2)
	waitFeedProjection(t, a, c, 2)
	require.Len(t, a.managedFeed.extraTasks, 1)
	require.Equal(t, "reported_call_tree", status.CallTraces.Coverage)
	require.Equal(t, "provider_reported_root_consistency", status.CallTraces.Verification)
	// Optional provider reports never upgrade the base plan's trace coverage.
	for _, ds := range status.Source.Datasets {
		if ds.Kind == model.DatasetTraces {
			require.Equal(t, "unavailable", ds.Coverage)
		}
	}
	result, e := indexingstatus.ReadTraces(t.Context(), a.storage, 1, 0, 1)
	require.NoError(t, e)
	require.Len(t, result.Frames, 1)
	require.Empty(t, result.Frames[0].Path)
	require.NotNil(t, result.Next)
	source, e := facts.New(t.Context(), facts.Config{Store: a.managedFeed.store, Profile: a.managedFeed.program.Profile, Canonical: a.profileSrc, Consumer: "trace-read"})
	require.NoError(t, e)
	dbPage, e := source.CallTraces(t.Context(), 1, status.CallTraces.Descriptor, 0, 1)
	require.NoError(t, e)
	require.Equal(t, result, dbPage)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	before := proxy.calls.Load()
	secondPath := fmt.Sprintf("/indexing/traces/blocks/1?after=%d&limit=1&epoch=%d&hash=%s&selection=%s", *result.Next, result.Epoch, result.Identity.Block.Hash.Hex(), result.Selection.Hex())
	second := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(second, httptest.NewRequest("GET", secondPath, nil))
	require.Equal(t, 200, second.Code)
	stale := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(stale, httptest.NewRequest("GET", strings.Replace(secondPath, "epoch=1", "epoch=999", 1), nil))
	require.Equal(t, 409, stale.Code)

	for _, path := range []string{"/indexing/traces/blocks/1?limit=1", "/indexing/traces/blocks/2", "/indexing/status"} {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 200, w.Code, path)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.True(t, json.Valid(w.Body.Bytes()))
	}
	for _, path := range []string{"/indexing/traces/blocks/no", "/indexing/traces/blocks/1?limit=257", "/indexing/traces/blocks/1?after=4294967296", "/indexing/traces/blocks/1?after=2"} {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 400, w.Code, path)
	}
	w := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", "/indexing/traces/blocks/3", nil))
	require.Equal(t, 404, w.Code)
	require.Equal(t, before, proxy.calls.Load())
	if cfg.Database.Driver == "postgres" {
		ro, e := openStore(t.Context(), &cfg.Database, true, nil, zap.NewNop())
		require.NoError(t, e)
		out, e := indexingstatus.ReadTraces(t.Context(), ro, 1, 0, 1)
		require.NoError(t, e)
		require.Equal(t, result, out)
		require.NoError(t, ro.Close())
	}
	// Corrupt/stale source descriptors must not reach an unrelated input/API.
	kv := a.storage.(port.KV)
	saved, e := kv.Get(t.Context(), []byte(progress.DescriptorKey))
	require.NoError(t, e)
	var corrupt model.ProjectionDescriptor
	require.NoError(t, json.Unmarshal(saved, &corrupt))
	corrupt.Plan.Version++
	raw, e := json.Marshal(corrupt)
	require.NoError(t, e)
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), raw))
	w = httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", "/indexing/traces/blocks/1", nil))
	require.Equal(t, 500, w.Code)
	require.JSONEq(t, `{"error":"call_trace_error"}`, w.Body.String())
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), saved))
	require.NoError(t, json.Unmarshal(saved, &corrupt))
	corrupt.CallTraces.Required = true
	raw, e = json.Marshal(corrupt)
	require.NoError(t, e)
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), raw))
	malformed := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(malformed, httptest.NewRequest("GET", "/indexing/traces/blocks/1", nil))
	require.Equal(t, 500, malformed.Code)
	require.JSONEq(t, `{"error":"call_trace_error"}`, malformed.Body.String())
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), saved))

	a.Shutdown()
	proxy.fail.Store(true)
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	waitCallTraces(t, a, 2)
	_, e = indexingstatus.ReadTraces(t.Context(), a.storage, 1, 0, 10)
	require.NoError(t, e)
	require.Equal(t, before, proxy.calls.Load(), "restart reuses permanent traces while provider tracing is disabled")
	proxy.fail.Store(false)
	c.Reorg(1)
	c.AddBlock()
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
		return e == nil && s.CallTraces != nil && s.CallTraces.Epoch > status.CallTraces.Epoch && s.CallTraces.Complete != nil && s.CallTraces.Complete.Hash == c.Block(2).Block.Hash()
	}, 30*time.Second, 20*time.Millisecond)
	out, e := indexingstatus.ReadTraces(t.Context(), a.storage, 2, 0, 10)
	require.NoError(t, e)
	require.Empty(t, out.Frames)
	require.Equal(t, c.Block(2).Block.Hash(), out.Identity.Block.Hash)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
func TestManagedCallTraceProviderFailureJoinsTasks(t *testing.T) {
	cfg, _, proxy := traceAppConfig(t)
	proxy.fail.Store(true)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	_, done := runFeedApp(t, a)
	select {
	case e = <-done:
		require.ErrorIs(t, e, evmtrace.ErrUnavailable)
	case <-time.After(20 * time.Second):
		t.Fatal("trace failure did not join task set")
	}
	s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.NotNil(t, s.CallTraces)
	require.Nil(t, s.CallTraces.Complete)
	require.EqualValues(t, 0, s.CallTraces.Sequence)
	require.Equal(t, "stopped", s.Pipeline.Runtime.Availability)
	for _, name := range []string{"traceAcquire", "traceBatch"} {
		require.Positive(t, s.Pipeline.Runtime.Metrics[name].Failures)
	}
	a.Shutdown()
}
func TestManagedCallTraceConfigValidation(t *testing.T) {
	cfg, _, _ := traceAppConfig(t)
	require.NoError(t, cfg.Source.Feed.Validate())
	for _, name := range []string{"", "../calls", "calls';drop"} {
		copy := *cfg.Source.Feed.CallTraces
		copy.Name = name
		f := *cfg.Source.Feed
		f.CallTraces = &copy
		require.Error(t, f.Validate())
	}
	for _, provider := range []string{"", "https://node/?api-key=secret", strings.Repeat("x", 65)} {
		copy := *cfg.Source.Feed.CallTraces
		copy.Provider = provider
		f := *cfg.Source.Feed
		f.CallTraces = &copy
		require.Error(t, f.Validate())
	}
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.CallTraces.Name = cfg.Source.Feed.StateProofs.Name
	require.ErrorContains(t, cfg.Source.Feed.Validate(), "distinct names")
	var f feed.Config
	require.Error(t, yaml.Unmarshal([]byte("call_traces:\n  required: true\n"), &f))
	cfg.Source.Feed.CallTraces.Name = "calls-v1"
	d, e := cfg.Source.Feed.CallTraces.Descriptor(traceAnchor(cfg), "arc")
	require.NoError(t, e)
	d.Required = true
	pd := model.ProjectionDescriptor{Anchor: traceAnchor(cfg), CallTraces: &d}
	require.Error(t, pd.ValidateCallTraces())
}

func traceAnchor(cfg *config.Config) model.FeedBlock { b, _ := cfg.Source.Feed.Identity(); return b }

func TestManagedCallTraceAlongsideStateAndSummary(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	cfg, _, rpc := feedAppConfig(t, c)
	proxy := &appTraceProxy{chain: c, upstream: rpc.URL(), client: &http.Client{Timeout: 2 * time.Second}}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	cfg.RPC.Endpoint = server.URL
	cfg.Source.Feed.CallTraces = &evmtrace.Config{Name: "call-v1", Provider: "fixture-pool", StartHeight: 1}
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.StateProofs.ProjectChanges = true
	cfg.Source.Feed.Projections = []string{projection.Summary}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	waitCallTraces(t, a, 2)
	waitStateChanges(t, a, 2)
	waitFeedProjection(t, a, c, 2)
	require.Len(t, a.managedFeed.extraTasks, 4)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
