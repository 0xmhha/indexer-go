package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/client"
	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Actual official-Arc replay bytes -> Go commitments -> seven writer queues ->
// fsync-on PG -> reserved DB-only REST -> lost ACK and read-only reopen.
func TestExecutionNativeCallsPublicationPagesAndRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	f := loadExecutionFrozen(t, "arc-calls")
	s, cfg := preparedExecutionDB(t, f)
	m, srv := fixtureRPC(t, f)
	cl, e := client.NewClient(&client.Config{Endpoint: srv.URL, Timeout: time.Second, RateLimit: 5, Logger: zap.NewNop()})
	require.NoError(t, e)
	defer cl.Close()
	profile, ok := chains.Lookup(f.Profile)
	require.True(t, ok)
	fe := fetch.NewFetcher(cl, s, &fetch.Config{StatePrefetch: true, RPCTimeout: time.Second, NoOutbox: true}, zap.NewNop(), nil)
	defer fe.Close()
	pipeline, e := feature.Build([]string{"address.index", "balance.native", "token.transfers"}, feature.Deps{Storage: s, Logger: zap.NewNop(), Profile: profile, NativeBalances: feature.NativeBalancesFromContext, BlockAt: s.GetBlock})
	require.NoError(t, e)
	fe.SetFeatures(pipeline)
	fe.SetExecutionCanonical(sourcerpc.New(cl.RPCClient(), profile))
	prepared, e := execution.Prepare(profile.(chains.BinaryProfile), f.Chain, f.Payload)
	require.NoError(t, e)
	result, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	require.NotNil(t, result.CallTraces)
	apiConfig := api.DefaultConfig()
	apiConfig.EnableRateLimit = false
	apiConfig.ReserveQueryReader = true
	server, e := api.NewServer(apiConfig, zap.NewNop(), s)
	require.NoError(t, e)
	stop := sync.OnceFunc(func() { require.NoError(t, server.Stop(context.Background())) })
	defer stop()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		server.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	path := "/execution/traces/blocks/1?limit=2"
	require.Equal(t, 404, get(path).Code, "unpublished work must not be exposed")
	backend := &execution.StoreBackend{Store: s, Publisher: fe}
	scheduler, e := execution.New(ctx, execution.Config{Anchor: f.Baseline.Ref, MaxBlocks: 4, MaxBytes: 64 << 20}, backend)
	require.NoError(t, e)
	closeScheduler := sync.OnceFunc(func() { require.NoError(t, scheduler.Close()) })
	defer closeScheduler()
	future, e := scheduler.Submit(ctx, prepared)
	require.NoError(t, e)
	require.NoError(t, future.Wait(ctx))
	before := func() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.methods) }()
	type page struct {
		Epoch  uint64  `json:"epoch"`
		Total  uint32  `json:"total"`
		Next   *uint32 `json:"next"`
		Frames []struct {
			Type             string `json:"type"`
			Internal         bool   `json:"internal"`
			Reverted         bool   `json:"reverted"`
			AncestorReverted bool   `json:"ancestorReverted"`
		} `json:"frames"`
	}
	var first page
	w := get(path)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.EqualValues(t, len(result.CallTraces.Frames), first.Total)
	require.True(t, first.Frames[0].Reverted)
	require.False(t, first.Frames[0].Internal)
	require.True(t, first.Frames[1].Internal)
	require.True(t, first.Frames[1].AncestorReverted)
	seen := len(first.Frames)
	current := first
	for current.Next != nil {
		w = get(path + "&after=" + strconv.FormatUint(uint64(*current.Next), 10) + "&epoch=" + strconv.FormatUint(first.Epoch, 10) + "&hash=" + common.Hash(result.Ref.Hash).Hex())
		require.Equal(t, 200, w.Code)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &current))
		seen += len(current.Frames)
	}
	require.Equal(t, len(result.CallTraces.Frames), seen)
	txPath := "/execution/traces/transactions/" + common.Hash(result.Steps[1].TxHash).Hex() + "?internal=true&limit=256"
	w = get(txPath)
	require.Equal(t, 200, w.Code)
	var internals page
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &internals))
	require.EqualValues(t, len(result.CallTraces.Frames)-1, internals.Total)
	for _, frame := range internals.Frames {
		require.True(t, frame.Internal)
		require.True(t, frame.Reverted)
	}
	m.mu.Lock()
	require.Len(t, m.methods, before, "all trace API pages must use zero RPC")
	m.mu.Unlock()
	confirmed, e := fe.ConfirmExecution(t.Context(), f.Payload)
	require.NoError(t, e)
	require.True(t, confirmed)
	closeScheduler()
	stop()
	require.NoError(t, s.Close())
	ro, e := postgres.Open(t.Context(), postgres.Options{DSN: cfg.Database.Postgres.DSN, Schema: cfg.Database.Postgres.Schema, MaxConns: 4, ReadOnly: true})
	require.NoError(t, e)
	defer func() { require.NoError(t, ro.Close()) }()
	reopened, e := ro.ExecutionCalls(t.Context(), port.ExecutionCallQuery{Height: 1, Limit: 256})
	require.NoError(t, e)
	require.EqualValues(t, len(result.CallTraces.Frames), reopened.Total)
	require.Len(t, reopened.Frames, len(result.CallTraces.Frames))
}
