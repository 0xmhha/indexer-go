package app

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/client"
	"github.com/0xmhha/indexer-go/pkg/api"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
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
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestExecutionNativeStateProofPublicationAndRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	f := loadExecutionFrozen(t, "evm-state-proofs")
	var source struct{ ProofInput []byte }
	encoded, e := os.ReadFile("../../testdata/execution/evm-state-proofs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(encoded, &source))
	var bundle stateproof.Bundle
	require.NoError(t, json.Unmarshal(source.ProofInput, &bundle))
	original := append([]byte{}, f.Payload...)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	input, proofs, complete := filepath.Join(dir, "original.exr"), filepath.Join(dir, "proofs.json"), filepath.Join(dir, "complete.exr")
	require.NoError(t, os.WriteFile(input, original, 0600))
	require.NoError(t, os.WriteFile(proofs, source.ProofInput, 0600))
	profile, ok := chains.Lookup(f.Profile)
	require.True(t, ok)
	attached, e := execution.AttachStateProofFiles(ctx, profile.(chains.BinaryProfile), f.Chain, input, proofs, complete, "native-state")
	require.NoError(t, e)
	f.Payload, e = os.ReadFile(complete)
	require.NoError(t, e)
	result, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	require.Equal(t, result.Ref, attached.Ref)
	s, cfg := preparedExecutionDB(t, f)
	cfg.Source.Execution.RequiredStateProofs = &bundle.Selection
	m, srv := fixtureRPC(t, f)
	cl, e := client.NewClient(&client.Config{Endpoint: srv.URL, Timeout: time.Second, RateLimit: 5, Logger: zap.NewNop()})
	require.NoError(t, e)
	defer cl.Close()
	fe := fetch.NewFetcher(cl, s, &fetch.Config{StatePrefetch: true, RPCTimeout: time.Second, NoOutbox: true}, zap.NewNop(), nil)
	defer fe.Close()
	pipeline, e := feature.Build([]string{"address.index", "balance.native", "token.transfers"}, feature.Deps{Storage: s, Logger: zap.NewNop(), Profile: profile, NativeBalances: feature.NativeBalancesFromContext, BlockAt: s.GetBlock})
	require.NoError(t, e)
	fe.SetFeatures(pipeline)
	fe.SetExecutionCanonical(sourcerpc.New(cl.RPCClient(), profile))
	prepared, e := execution.PrepareWithRequirements(ctx, profile.(chains.BinaryProfile), f.Chain, f.Payload, execution.Requirements{StateProofs: cfg.Source.Execution.RequiredStateProofs})
	require.NoError(t, e)
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
	path := "/execution/proofs/1/" + bundle.Selection.Targets[0].Address.Hex()
	require.Equal(t, 404, get(path).Code)
	scheduler, e := execution.New(ctx, execution.Config{Anchor: f.Baseline.Ref, MaxBlocks: 4, MaxBytes: 64 << 20}, &execution.StoreBackend{Store: s, Publisher: fe})
	require.NoError(t, e)
	closeScheduler := sync.OnceFunc(func() { require.NoError(t, scheduler.Close()) })
	defer closeScheduler()
	future, e := scheduler.Submit(ctx, prepared)
	require.NoError(t, e)
	require.NoError(t, future.Wait(ctx))
	m.mu.Lock()
	before := len(m.methods)
	m.mu.Unlock()
	type page struct {
		Epoch   uint64        `json:"epoch"`
		Root    execwire.Hash `json:"root"`
		Account struct {
			Exists  bool                          `json:"exists"`
			Storage []struct{ Key, Value string } `json:"storage"`
			Code    string                        `json:"code"`
		} `json:"account"`
		Proof json.RawMessage `json:"proof"`
	}
	w := get(path)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var got page
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.True(t, got.Account.Exists)
	require.Len(t, got.Account.Storage, 2)
	require.NotEqual(t, "0x", got.Account.Code)
	require.Equal(t, result.StateRoot, got.Root)
	require.True(t, json.Valid(got.Proof))
	pin := "?epoch=" + strconv.FormatUint(got.Epoch, 10) + "&hash=" + common.Hash(result.Ref.Hash).Hex()
	require.Equal(t, 200, get(path+pin).Code)
	absent := get("/execution/proofs/1/" + bundle.Selection.Targets[1].Address.Hex())
	require.Equal(t, 200, absent.Code)
	var missing page
	require.NoError(t, json.Unmarshal(absent.Body.Bytes(), &missing))
	require.False(t, missing.Account.Exists)
	require.Len(t, missing.Account.Storage, 1)
	m.mu.Lock()
	require.Len(t, m.methods, before, "proof API uses zero RPC")
	m.mu.Unlock()
	confirmed, e := fe.ConfirmExecution(ctx, f.Payload)
	require.NoError(t, e)
	require.True(t, confirmed)
	require.NoError(t, os.Remove(complete), "owned test input removal, not operational GC")
	kept, e := os.ReadFile(input)
	require.NoError(t, e)
	require.True(t, bytes.Equal(original, kept))
	closeScheduler()
	stop()
	require.NoError(t, s.Close())
	ro, e := postgres.Open(ctx, postgres.Options{DSN: cfg.Database.Postgres.DSN, Schema: cfg.Database.Postgres.Schema, MaxConns: 4, ReadOnly: true})
	require.NoError(t, e)
	defer func() { require.NoError(t, ro.Close()) }()
	reopened, e := ro.ExecutionProof(ctx, port.ExecutionProofQuery{Height: 1, Address: execwire.Address(bundle.Selection.Targets[0].Address)})
	require.NoError(t, e)
	require.True(t, reopened.Account.Exists)
	require.Len(t, reopened.Account.Storage, 2)
}
