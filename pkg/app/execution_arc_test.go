package app

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/client"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExecutionArcVerifiedProfileAndNativeWireCompatibility(t *testing.T) {
	require.True(t, chains.SupportsExecution(arc.New()), "Arc must opt in only with its independently verified native producer")
	for _, name := range []string{"arc-storage", "arc-create-gone"} {
		f := loadExecutionFrozen(t, name)
		r, err := execwire.UnmarshalResult(f.Payload)
		require.NoError(t, err, "Rust bytes must exactly match canonical Go wire metadata")
		require.Equal(t, execwire.PreBlock, r.Steps[0].Cause)
		require.Equal(t, execwire.Finalize, r.Steps[len(r.Steps)-1].Cause)
		prepared, err := execution.Prepare(arc.New(), f.Chain, f.Payload)
		require.NoError(t, err, "Arc block/header/body/receipt commitments are independently decoded in Go")
		require.Equal(t, r.Ref, prepared.Ref())
	}
}

func TestExecutionArcFreshFailureRetainsUnpublishedParts(t *testing.T) {
	f := loadExecutionFrozen(t, "arc-storage")
	s, _ := preparedExecutionDB(t, f)
	m, srv := fixtureRPC(t, f)
	cl, err := client.NewClient(&client.Config{Endpoint: srv.URL, Timeout: time.Second, RateLimit: 5, Logger: zap.NewNop()})
	require.NoError(t, err)
	defer cl.Close()
	profile := arc.New()
	fe := fetch.NewFetcher(cl, s, &fetch.Config{StatePrefetch: true, RPCTimeout: time.Second, NoOutbox: true}, zap.NewNop(), nil)
	defer fe.Close()
	pipeline, err := feature.Build([]string{"address.index", "balance.native", "token.transfers"}, feature.Deps{Storage: s, Logger: zap.NewNop(), Profile: profile, NativeBalances: feature.NativeBalancesFromContext, BlockAt: s.GetBlock})
	require.NoError(t, err)
	fe.SetFeatures(pipeline)
	fe.SetExecutionCanonical(sourcerpc.New(cl.RPCClient(), profile))
	prepared, err := execution.Prepare(profile, f.Chain, f.Payload)
	require.NoError(t, err)
	backend := &execution.StoreBackend{Store: s, Publisher: fe}
	for _, writer := range port.ExecutionWriters() {
		require.NoError(t, backend.Part(t.Context(), writer, prepared))
	}
	m.mu.Lock()
	m.canonical[1] = execwire.Hash{9}
	m.mu.Unlock()
	require.Error(t, backend.Publish(t.Context(), prepared))
	status, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, f.Baseline.Ref, status.Visible, "durable part completion cannot publish across failed fresh")
	visible, err := s.GetLatestHeight(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint64(0), visible)
	for _, expected := range f.Baseline.Accounts {
		a, err := s.VisibleExecutionAccount(t.Context(), expected.Address)
		require.NoError(t, err)
		require.Equal(t, expected.After, a.Account, "API reads the public anchor despite ahead-of-visible parts")
	}
	m.mu.Lock()
	m.canonical[1] = prepared.Ref().Hash
	m.mu.Unlock()
	require.NoError(t, backend.Publish(t.Context(), prepared))
	assertExecutionFrozen(t, t.Context(), s, f, prepared.Ref())
	confirmed, err := fe.ConfirmExecution(context.Background(), f.Payload)
	require.NoError(t, err)
	require.True(t, confirmed, "lost-ACK confirmation still performs fresh before granting durable proof")
	m.mu.Lock()
	defer m.mu.Unlock()
	require.GreaterOrEqual(t, m.fresh, 3)
}
