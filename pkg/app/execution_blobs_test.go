package app

import (
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// Actual Cancun transaction/prelude replay -> private retained full-blob file
// -> seven queues -> durable PG -> reserved DB-only API -> exact ACK/RO reopen.
func TestExecutionNativeBlobsPublicationPagesAndRestart(t *testing.T) {
	testExecutionNativeBlobsPublicationPagesAndRestart(t, false)
}
func TestExecutionEngineBlobsPublicationPagesAndRestart(t *testing.T) {
	testExecutionNativeBlobsPublicationPagesAndRestart(t, true)
}
func testExecutionNativeBlobsPublicationPagesAndRestart(t *testing.T, engine bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	f := loadExecutionFrozen(t, "evm-blobs")
	original := append([]byte{}, f.Payload...)
	result, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	response := proof.Response{Version: 1, Block: blobdata.Block(f.Chain, result.Ref), Entries: make([]proof.Entry, 3)}
	for i := range response.Entries {
		response.Entries[i].Index = uint64(i)
		response.Entries[i].Commitment[0] = 0xc0
		response.Entries[i].Proof[0] = 0xc0
	}
	encoded, e := rlp.EncodeToBytes(response)
	require.NoError(t, e)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	input, sidecar, complete := filepath.Join(dir, "original.exr"), filepath.Join(dir, "sidecars.rlp"), filepath.Join(dir, "complete.exr")
	require.NoError(t, os.WriteFile(input, original, 0600))
	require.NoError(t, os.WriteFile(sidecar, encoded, 0600))
	profile, ok := chains.Lookup(f.Profile)
	require.True(t, ok)

	m, srv := fixtureRPC(t, f)
	cl, e := client.NewClient(&client.Config{Endpoint: srv.URL, Timeout: time.Second, RateLimit: 5, Logger: zap.NewNop()})
	require.NoError(t, e)
	defer cl.Close()
	var attachment execution.BlobAttachment
	if engine {
		var parent, header types.Header
		require.NoError(t, rlp.DecodeBytes(f.ParentHeader, &parent))
		parts, err := proof.SplitList(result.BlockRLP, 4)
		require.NoError(t, err)
		require.NoError(t, rlp.DecodeBytes(parts[0], &header))
		m.mu.Lock()
		m.headers = map[uint64]*types.Header{0: &parent, 1: &header}
		for _, entry := range response.Entries {
			m.engineBlobs = append(m.engineBlobs, map[string]any{"blob": hexutil.Bytes(entry.Blob[:]), "proof": hexutil.Bytes(entry.Proof[:])})
		}
		m.mu.Unlock()
		attachment, e = execution.AcquireEngineBlobFiles(ctx, profile.(chains.BinaryProfile), execution.BlobSupplyPolicy{Contract: execution.DevEngineBlobContract, Chain: f.Chain, Provider: "engine-v1", MaxLagBlocks: 64}, input, complete, cl.RPCClient())
	} else {
		attachment, e = execution.AttachBlobFiles(ctx, profile.(chains.BinaryProfile), f.Chain, input, sidecar, complete, "retained-sidecars")
	}
	require.NoError(t, e)
	f.Payload, e = os.ReadFile(complete)
	require.NoError(t, e)
	require.Equal(t, result.Ref, attachment.Ref)
	s, cfg := preparedExecutionDB(t, f)
	cfg.Source.Execution.RequireBlobSidecars = true

	fe := fetch.NewFetcher(cl, s, &fetch.Config{StatePrefetch: true, RPCTimeout: time.Second, NoOutbox: true}, zap.NewNop(), nil)
	defer fe.Close()
	pipeline, e := feature.Build([]string{"address.index", "balance.native", "token.transfers"}, feature.Deps{Storage: s, Logger: zap.NewNop(), Profile: profile, NativeBalances: feature.NativeBalancesFromContext, BlockAt: s.GetBlock})
	require.NoError(t, e)
	fe.SetFeatures(pipeline)
	fe.SetExecutionCanonical(sourcerpc.New(cl.RPCClient(), profile))
	prepared, e := execution.PrepareContext(ctx, profile.(chains.BinaryProfile), f.Chain, f.Payload, cfg.Source.Execution.RequireBlobSidecars)
	require.NoError(t, e)
	result, e = execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	require.NotNil(t, result.BlobBundle)
	apiConfig := api.DefaultConfig()
	apiConfig.EnableRateLimit = false
	apiConfig.ReserveQueryReader = true
	server, e := api.NewServer(apiConfig, zap.NewNop(), s)
	require.NoError(t, e)
	require.Equal(t, 1, server.ReservedQueryReaders())
	stop := sync.OnceFunc(func() { require.NoError(t, server.Stop(context.Background())) })
	defer stop()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		server.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	path := "/execution/blobs/blocks/1?limit=1"
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
		Epoch   uint64  `json:"epoch"`
		Total   uint32  `json:"total"`
		Next    *uint32 `json:"next"`
		Entries []struct {
			Index      uint32 `json:"index"`
			Blob       string `json:"blob"`
			Commitment string `json:"commitment"`
			Proof      string `json:"proof"`
		} `json:"entries"`
	}
	var first page
	w := get(path)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.EqualValues(t, 3, first.Total)
	seen := 0
	current := first
	for {
		for _, entry := range current.Entries {
			require.EqualValues(t, seen, entry.Index)
			require.Len(t, entry.Blob, 2+131072*2)
			require.True(t, strings.HasPrefix(entry.Commitment, "0xc0"))
			require.Equal(t, entry.Commitment, entry.Proof)
			seen++
		}
		if current.Next == nil {
			break
		}
		w = get(path + "&after=" + strconv.FormatUint(uint64(*current.Next), 10) + "&epoch=" + strconv.FormatUint(first.Epoch, 10) + "&hash=" + common.Hash(result.Ref.Hash).Hex())
		require.Equal(t, 200, w.Code)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &current))
	}
	require.Equal(t, 3, seen)
	w = get("/execution/blobs/transactions/" + common.Hash(result.Steps[1].TxHash).Hex() + "?limit=4")
	require.Equal(t, 200, w.Code)
	var byTx page
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &byTx))
	require.Len(t, byTx.Entries, 3)
	m.mu.Lock()
	require.Len(t, m.methods, before, "all blob API pages must use zero RPC")
	m.mu.Unlock()
	confirmed, e := fe.ConfirmExecution(t.Context(), f.Payload)
	require.NoError(t, e)
	require.True(t, confirmed)
	require.NoError(t, os.Remove(complete), "owned test input removal proves DB-only reads, not operational GC")
	kept, e := os.ReadFile(input)
	require.NoError(t, e)
	require.Equal(t, original, kept)
	closeScheduler()
	stop()
	require.NoError(t, s.Close())
	ro, e := postgres.Open(t.Context(), postgres.Options{DSN: cfg.Database.Postgres.DSN, Schema: cfg.Database.Postgres.Schema, MaxConns: 4, ReadOnly: true})
	require.NoError(t, e)
	defer func() { require.NoError(t, ro.Close()) }()
	reopened, e := ro.ExecutionBlobs(t.Context(), port.ExecutionBlobQuery{Height: 1, Limit: 4})
	require.NoError(t, e)
	require.EqualValues(t, 3, reopened.Total)
	require.Len(t, reopened.Entries, 3)
}
