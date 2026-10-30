package app

import (
	"bytes"
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	checkpointformat "github.com/0xmhha/indexer-go/execwire/checkpoint"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const ownerPolicyModule = "test.app-origin-policy"

func init() {
	ingress.Register(ownerPolicyModule, ingress.Module{Profile: "arc", Origins: true, Validate: func(ingress.Config) error { return nil }, Prepare: func(context.Context, ingress.Config, ingress.Dependencies) (ingress.Program, error) {
		return nil, port.ErrInvalidData
	}})
}

func TestExecutionIngressPolicyBindsSemanticRequirements(t *testing.T) {
	cfg, store, _ := preparedIngressFixture()
	e := cfg.Source.Execution
	e.Ingress.Module = ownerPolicyModule
	e.RequiredStateProofs = &stateproof.Selection{Targets: []stateproof.Target{{Address: common.Address{1}, Slots: []common.Hash{{2}, {1}}}}}
	p, err := executionIngressPolicy(cfg, store.control)
	require.NoError(t, err)
	e.RequiredStateProofs.Targets[0].Slots = []common.Hash{{1}, {2}}
	e.Ingress.Native.DataDir = "/another/private/native"
	e.Ingress.MaxRawBlocks++
	e.Timeout++
	same, err := executionIngressPolicy(cfg, store.control)
	require.NoError(t, err)
	require.Equal(t, p, same, "normalized proof order and operational paths/limits cannot change source identity")
	e.RequireBlobSidecars = !e.RequireBlobSidecars
	changed, err := executionIngressPolicy(cfg, store.control)
	require.NoError(t, err)
	require.NotEqual(t, p.Requirements, changed.Requirements)
	e.RequiredStateProofs.Targets[0].Slots = append(e.RequiredStateProofs.Targets[0].Slots, common.Hash{1})
	_, err = executionIngressPolicy(cfg, store.control)
	require.Error(t, err, "invalid proof requirements must not be silently accepted")
	e.Ingress = nil
	none, err := executionIngressPolicy(cfg, store.control)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestExecutionIngressOwnerRefusesConfigRemovalBeforeRPCAndAPI(t *testing.T) {
	cfg := executionAppConfig()
	cfg.Database.Postgres.DSN = postgresDSN(t)
	cfg.Database.Postgres.Schema = testSchema(t.TempDir())
	cfg.API.Enabled = false
	t.Cleanup(func() { dropTestSchemas(cfg.Database.Postgres.DSN, cfg.Database.Postgres.Schema) })
	s, err := openPostgres(t.Context(), &cfg.Database)
	require.NoError(t, err)
	h := &types.Header{Number: big.NewInt(0), Root: types.EmptyRootHash, Difficulty: big.NewInt(1)}
	header, err := rlp.EncodeToBytes(h)
	require.NoError(t, err)
	m := checkpointformat.Manifest{Version: 1, Mode: "genesis", Coverage: "full", Chain: execwire.Chain{ID: "1", Genesis: execwire.Hash(h.Hash())}, Ref: execwire.Ref{Generation: 1, Hash: execwire.Hash(h.Hash())}, Root: execwire.Hash(h.Root), Header: header}
	var encoded bytes.Buffer
	require.NoError(t, checkpointformat.Write(&encoded, checkpointformat.Record{Kind: "manifest", Manifest: &m}))
	require.NoError(t, checkpointformat.Write(&encoded, checkpointformat.Record{Kind: "end"}))
	_, err = s.BootstrapExecution(t.Context(), &encoded, m, evm.New("evm"), func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &port.ExecutionIngressPolicy{Version: 1, Module: "fixture.native", Chain: m.Chain, Anchor: m.Ref, AnchorRoot: m.Root, Requirements: execwire.Hash{9}}, false))
	require.NoError(t, s.Close())
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"unexpected RPC"}}`))
	}))
	defer server.Close()
	cfg.RPC.Endpoint = server.URL
	cfg.Source.Execution = nil
	app, err := newApp(cfg, zap.NewNop(), StartOptions{})
	if app != nil {
		app.Shutdown()
	}
	require.ErrorIs(t, err, port.ErrExecutionConflict, "stored owner must survive removal of source config")
	require.Zero(t, calls.Load(), "owner rejection precedes any RPC or API exposure")
}
