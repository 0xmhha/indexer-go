package app

import (
	"context"
	"encoding/json"
	"errors"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/exporter"
	"github.com/0xmhha/indexer-go/execwire/spool"
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

type arcCollectedFixture struct{ result execwire.Result }

func (b arcCollectedFixture) Head(context.Context) (execwire.Ref, error) { return b.result.Ref, nil }
func (b arcCollectedFixture) Canonical(_ context.Context, height uint64) (execwire.Hash, error) {
	if height == b.result.Ref.Height {
		return b.result.Ref.Hash, nil
	}
	if height+1 == b.result.Ref.Height {
		return b.result.Parent, nil
	}
	return execwire.Hash{}, errors.New("fixture header unavailable")
}
func (b arcCollectedFixture) Replay(_ context.Context, height, generation uint64) (execwire.Result, error) {
	if height != b.result.Ref.Height {
		return execwire.Result{}, errors.New("fixture state unavailable")
	}
	r := b.result
	r.Ref.Generation = generation
	return r, nil
}

func TestExecutionArcCollectedSpoolPublicationAndLostACK(t *testing.T) {
	testArcCollectedSpool(t, "arc-storage")
}
func TestExecutionArcNativeProofSpoolPublicationAndLostACK(t *testing.T) {
	testArcCollectedSpool(t, "arc-state-proofs")
}
func testArcCollectedSpool(t *testing.T, name string) {
	f := loadExecutionFrozen(t, name)
	s, _ := preparedExecutionDB(t, f)
	_, srv := fixtureRPC(t, f)
	cl, err := client.NewClient(&client.Config{Endpoint: srv.URL, Timeout: time.Second, RateLimit: 5, Logger: zap.NewNop()})
	require.NoError(t, err)
	defer cl.Close()
	profile := arc.New()
	fe := fetch.NewFetcher(cl, s, &fetch.Config{StatePrefetch: true, RPCTimeout: time.Second}, zap.NewNop(), nil)
	defer fe.Close()
	pipeline, err := feature.Build([]string{"address.index", "balance.native", "token.transfers"}, feature.Deps{Storage: s, Logger: zap.NewNop(), Profile: profile, NativeBalances: feature.NativeBalancesFromContext, BlockAt: s.GetBlock})
	require.NoError(t, err)
	fe.SetFeatures(pipeline)
	fe.SetExecutionCanonical(sourcerpc.New(cl.RPCClient(), profile))
	backend := &execution.StoreBackend{Store: s, Publisher: fe}
	r, err := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, err)
	receiver := &execution.Receiver{Chain: f.Chain, Profile: profile, Store: s, Confirm: fe.ConfirmExecution, Timeout: 3 * time.Second}
	var admission *execwire.Hash
	if r.StateProofBundle != nil {
		var bundle stateproof.Bundle
		require.NoError(t, json.Unmarshal(r.StateProofBundle.Response, &bundle))
		receiver.RequiredStateProofs = &bundle.Selection
		_, h, e := bundle.Selection.Normalize()
		require.NoError(t, e)
		hash := execwire.Hash(h)
		admission = &hash
	}
	receiver.OpenController = func(ctx context.Context) (*execution.Controller, error) {
		return execution.NewController(ctx, execution.Limits{MaxBlocks: 4, MaxBytes: 16 << 20}, s, backend, func(context.Context) error { return nil })
	}
	ipc, err := os.MkdirTemp("", "arc-ipc-")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(ipc)) }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	socket := filepath.Join(ipc, "s")
	serverDone := make(chan error, 1)
	go func() { serverDone <- receiver.Run(ctx, socket) }()
	defer func() { cancel(); require.ErrorIs(t, <-serverDone, context.Canceled) }()
	require.Eventually(t, func() bool { _, err := os.Stat(socket); return err == nil }, 3*time.Second, time.Millisecond)
	var source *spool.Store
	var publishedSequence uint64
	for attempt := range 2 {
		// A second fresh source has the original cursor but the DB already has
		// the committed block: this is a lost-ACK replay, not Hello promotion.
		source, err = spool.Open(ctx, spool.Options{Path: filepath.Join(t.TempDir(), "spool"), Chain: f.Chain, Anchor: f.Baseline.Ref, MaxBytes: 2 << 20, MaxRecords: 8, Admission: admission})
		require.NoError(t, err)
		if attempt == 1 {
			_, err = source.Append(ctx, r)
			require.NoError(t, err)
		}
		clientCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		c := &exporter.Client{Chain: f.Chain, Spool: source, Backend: arcCollectedFixture{r}, Socket: socket, Window: 2, Poll: time.Millisecond, Retry: time.Millisecond, Timeout: 3 * time.Second}
		go func() { done <- c.Run(clientCtx) }()
		finish := sync.OnceFunc(func() { stop(); require.ErrorIs(t, <-done, context.Canceled); require.NoError(t, source.Close()) })
		t.Cleanup(finish)
		require.Eventually(t, func() bool {
			status, e := source.Status(ctx)
			return e == nil && status.Committed == r.Ref && status.Pending == 0
		}, 5*time.Second, time.Millisecond)
		assertExecutionFrozen(t, ctx, s, f, r.Ref)
		if r.StateProofBundle != nil {
			record, e := s.ExecutionProof(ctx, port.ExecutionProofQuery{Height: r.Ref.Height, Address: execwire.Address(receiver.RequiredStateProofs.Targets[0].Address)})
			require.NoError(t, e)
			require.Equal(t, r.Ref, record.Ref)
			require.Equal(t, "arc-native", record.Provider)
		}
		seq, e := s.LastOutboxSeq(ctx)
		require.NoError(t, e)
		if attempt == 0 {
			publishedSequence = seq
			require.NotZero(t, seq)
		} else {
			require.Equal(t, publishedSequence, seq, "duplicate ACK recovery must not repeat outbox publication")
		}
		finish()
	}
}

func TestExecutionArcVerifiedProfileAndNativeWireCompatibility(t *testing.T) {
	require.True(t, chains.SupportsExecution(arc.New()), "Arc must opt in only with its independently verified native producer")
	for _, name := range []string{"arc-storage", "arc-create-gone", "arc-calls", "arc-call-from-oog", "arc-call-from-direct", "arc-state-proofs"} {
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
	testArcFreshFailure(t, "arc-storage")
}
func TestExecutionArcNativeProofFreshFailureRetainsUnpublishedParts(t *testing.T) {
	testArcFreshFailure(t, "arc-state-proofs")
}
func testArcFreshFailure(t *testing.T, name string) {
	f := loadExecutionFrozen(t, name)
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
