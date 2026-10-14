package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// Preserve upstream members, but optionally reject state envelopes or replace
// one member's response. The block/receipt source still uses its normal batches.
func stateBatchGateway(t *testing.T, upstream string, reject *atomic.Bool, mutate func(preparedRPCRequest, bool) (int, string), envelopes *atomic.Int64) *httptest.Server {
	t.Helper()
	u, err := url.Parse(upstream)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(u)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		isBatch := len(raw) > 0 && raw[0] == '['
		var calls []preparedRPCRequest
		if isBatch {
			_ = json.Unmarshal(raw, &calls)
		} else {
			var call preparedRPCRequest
			_ = json.Unmarshal(raw, &call)
			calls = []preparedRPCRequest{call}
		}
		state := len(calls) > 0
		for _, call := range calls {
			state = state && (call.Method == "eth_getBalance" || call.Method == "eth_call")
		}
		if isBatch && state {
			if envelopes != nil {
				envelopes.Add(1)
			}
			if reject != nil && reject.Load() {
				http.Error(w, "state batch unsupported", http.StatusBadRequest)
				return
			}
		}
		capture := httptest.NewRecorder()
		proxy.ServeHTTP(capture, r)
		if capture.Code != http.StatusOK || mutate == nil {
			w.WriteHeader(capture.Code)
			_, _ = w.Write(capture.Body.Bytes())
			return
		}
		var replies []map[string]json.RawMessage
		if isBatch {
			_ = json.Unmarshal(capture.Body.Bytes(), &replies)
		} else {
			var reply map[string]json.RawMessage
			_ = json.Unmarshal(capture.Body.Bytes(), &reply)
			replies = []map[string]json.RawMessage{reply}
		}
		for _, call := range calls {
			code, message := mutate(call, isBatch)
			if code == 0 {
				continue
			}
			for _, reply := range replies {
				if bytes.Equal(reply["id"], call.ID) {
					delete(reply, "result")
					reply["error"], _ = json.Marshal(map[string]any{"code": code, "message": message})
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if isBatch {
			_ = json.NewEncoder(w).Encode(replies)
		} else {
			_ = json.NewEncoder(w).Encode(replies[0])
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestStateBatchMatchesIndividualReads(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chain func() *testchain.Chain
		from  uint64
	}{
		{"evm", func() *testchain.Chain { return testchain.BuildDefault().Chain }, 0},
		{"recent", func() *testchain.Chain { return testchain.BuildDefault().Chain }, 1},
		{"stablenet", func() *testchain.Chain { return testchain.BuildStableNet().Chain }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := tc.chain()
			srv := testchain.NewServer(chain)
			defer srv.Close()
			var dirs []string
			for _, size := range []int{1, 16} {
				dir := filepath.Join(t.TempDir(), "db")
				dirs = append(dirs, dir)
				app := newPreparedApp(t, srv.URL(), dir, true, tc.from, func(cfg *config.Config) { cfg.Indexer.StateBatchSize = size })
				require.NoError(t, app.fetcher.FetchRange(t.Context(), tc.from, chain.Head()))
				for _, addr := range chain.Accounts() {
					balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), addr, chain.Head())
					if tracked {
						require.Equal(t, 0, balance.Cmp(chain.BalanceAt(addr, chain.Head())), "%s", addr)
					}
				}
				app.Shutdown()
			}
			diff := testchain.DiffKeyspace(dumpDir(t, dirs[0]), dumpDir(t, dirs[1]), 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
		})
	}
}

func TestStateBatchUnsupportedEnvelopeUsesIndividualReads(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	var reject atomic.Bool
	reject.Store(true)
	var envelopes atomic.Int64
	proxy := stateBatchGateway(t, srv.URL(), &reject, nil, &envelopes)
	app := newPreparedApp(t, proxy.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) { cfg.Indexer.StateBatchSize = 16 })
	t.Cleanup(app.Shutdown)
	require.NoError(t, app.fetcher.FetchRange(t.Context(), 0, sc.Chain.Head()))
	require.Positive(t, envelopes.Load())
	md, err := app.storage.GetTokenMetadata(t.Context(), sc.ERC20)
	require.NoError(t, err)
	require.Equal(t, "TT", md.Symbol)
	for _, addr := range sc.Chain.Accounts() {
		balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), addr, sc.Chain.Head())
		if tracked {
			require.Equal(t, 0, balance.Cmp(sc.Chain.BalanceAt(addr, sc.Chain.Head())))
		}
	}
}

func TestStateBatchTokenMemberFailureRollsBackAndRetries(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	var missing atomic.Bool
	proxy := stateBatchGateway(t, srv.URL(), nil, func(call preparedRPCRequest, batch bool) (int, string) {
		if missing.Load() && batch && call.Method == "eth_call" {
			var args struct{ Input string }
			var at struct{ BlockHash common.Hash }
			_ = json.Unmarshal(call.Params[0], &args)
			_ = json.Unmarshal(call.Params[1], &at)
			if at.BlockHash == sc.Chain.Block(2).Block.Hash() && args.Input == "0x06fdde03" {
				return -32000, "missing trie node: token batch member"
			}
		}
		return 0, ""
	}, nil)
	app := newPreparedApp(t, proxy.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) { cfg.Indexer.StateBatchSize = 16 })
	t.Cleanup(app.Shutdown)
	require.NoError(t, app.fetcher.FetchRange(t.Context(), 0, 1))
	before, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	missing.Store(true)
	require.ErrorContains(t, app.fetcher.FetchBlock(t.Context(), 2), "token batch member")
	requireFailedStateBlockAbsent(t, app, sc.Chain, 2)
	_, err = app.storage.GetTokenMetadata(t.Context(), sc.ERC20)
	require.ErrorIs(t, err, port.ErrNotFound)
	after, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
	missing.Store(false)
	require.NoError(t, app.fetcher.FetchBlock(t.Context(), 2))
	md, err := app.storage.GetTokenMetadata(t.Context(), sc.ERC20)
	require.NoError(t, err)
	require.Equal(t, "TT", md.Symbol)
}

func TestStateBatchNativeMemberFailureRollsBackAndRetries(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	var missing atomic.Bool
	proxy := stateBatchGateway(t, srv.URL(), nil, func(call preparedRPCRequest, batch bool) (int, string) {
		if missing.Load() && batch && call.Method == "eth_getBalance" {
			var address common.Address
			_ = json.Unmarshal(call.Params[0], &address)
			if address == sc.Accounts[1].Address {
				return -32000, "missing trie node: balance batch member"
			}
		}
		return 0, ""
	}, nil)
	app := newPreparedApp(t, proxy.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) { cfg.Indexer.StateBatchSize = 16 })
	t.Cleanup(app.Shutdown)
	require.NoError(t, app.fetcher.FetchBlock(t.Context(), 0))
	before, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	missing.Store(true)
	require.ErrorContains(t, app.fetcher.FetchBlock(t.Context(), 1), "balance batch member")
	requireFailedStateBlockAbsent(t, app, sc.Chain, 1)
	after, err := app.storage.(port.Outbox).ReadOutbox(t.Context(), 0, 0)
	require.NoError(t, err)
	require.Equal(t, before, after)
	missing.Store(false)
	require.NoError(t, app.fetcher.FetchBlock(t.Context(), 1))
	for _, address := range sc.Chain.Accounts() {
		balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), address, 1)
		if tracked {
			require.Equal(t, 0, balance.Cmp(sc.Chain.BalanceAt(address, 1)))
		}
	}
}

func TestStateBatchHashCapabilityErrorDoesNotUseZero(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	proxy := stateBatchGateway(t, srv.URL(), nil, func(call preparedRPCRequest, batch bool) (int, string) {
		if batch && call.Method == "eth_getBalance" {
			return -32602, "hash selector unsupported"
		}
		return 0, ""
	}, nil)
	app := newPreparedApp(t, proxy.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) {
		cfg.Indexer.StateBatchSize = 16
		require.NoError(t, cfg.SetFeatureSettings("balance.native", map[string]any{"allow_pruned_state": true}))
	})
	t.Cleanup(app.Shutdown)
	require.ErrorContains(t, app.fetcher.FetchBlock(t.Context(), 0), "hash selector unsupported")
	requireFailedStateBlockAbsent(t, app, sc.Chain, 0)
}

func TestStateBatchFallbackDoesNotReenterSharedPermits(t *testing.T) {
	sender := testchain.NewAccount(701)
	chain := testchain.NewChain(testchain.DefaultChainID, map[common.Address]*big.Int{sender.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)})
	var specs []testchain.TxSpec
	for i := range 80 {
		address := common.BigToAddress(big.NewInt(int64(10000 + i)))
		specs = append(specs, testchain.TxSpec{From: sender, Tx: &types.LegacyTx{To: &address, Value: big.NewInt(1), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)}})
	}
	chain.AddBlock(specs...)
	srv := testchain.NewServer(chain)
	t.Cleanup(srv.Close)
	var reject atomic.Bool
	reject.Store(true)
	proxy := stateBatchGateway(t, srv.URL(), &reject, nil, nil)
	app := newPreparedApp(t, proxy.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) { cfg.Indexer.StateBatchSize = 16 })
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, 1))
	for _, address := range chain.Accounts() {
		balance, tracked := indexedBalanceAt(t, app.storage.(port.HistoricalReader), address, 1)
		if tracked {
			require.Equal(t, 0, balance.Cmp(chain.BalanceAt(address, 1)))
		}
	}
}

func TestStateBatchCancellationDrainsPreparation(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	blocked, release := make(chan struct{}), make(chan struct{})
	var blockOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	proxy := stateBatchGateway(t, srv.URL(), nil, func(call preparedRPCRequest, batch bool) (int, string) {
		if batch && call.Method == "eth_getBalance" {
			blockOnce.Do(func() { close(blocked) })
			<-release
		}
		return 0, ""
	}, nil)
	app := newPreparedApp(t, proxy.URL, filepath.Join(t.TempDir(), "db"), true, 0, func(cfg *config.Config) { cfg.Indexer.StateBatchSize = 16 })
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()) }()
	<-blocked
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	releaseOnce.Do(func() { close(release) })
	require.Zero(t, app.fetcher.GetPipelineMetrics().InFlight)
	require.Zero(t, app.fetcher.GetPipelineMetrics().StateBytes)
	requireFailedStateBlockAbsent(t, app, sc.Chain, 0)
}
