package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/0xmhha/indexer-go/x/stablenet/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSeedPriority3E6OptInIndexerIPC(t *testing.T) {
	data, e := os.ReadFile("../../testdata/execution/consensus/native.json")
	require.NoError(t, e)
	var fixture struct {
		Policy  consensus.TrustPolicy
		Headers [][]byte
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	var anchor, h types.Header
	require.NoError(t, rlp.DecodeBytes(fixture.Policy.AnchorHeader, &anchor))
	require.NoError(t, rlp.DecodeBytes(fixture.Headers[0], &h))
	raw, e := rlp.EncodeToBytes(types.NewBlockWithHeader(&h))
	require.NoError(t, e)
	result := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash(stablenet.HeaderHash(&h))}, Parent: execwire.Hash(h.ParentHash), StateRoot: execwire.Hash(h.Root), ReceiptsRoot: execwire.Hash(h.ReceiptHash), Time: h.Time, BlockRLP: raw, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Finalize, TxIndex: -1}}}
	payload, e := execwire.MarshalResult(result)
	require.NoError(t, e)
	f := executionFrozen{Version: 1, Profile: "stablenet", Chain: fixture.Policy.Chain, ParentHeader: fixture.Policy.AnchorHeader, Baseline: port.ExecutionBaseline{Ref: execwire.Ref{Generation: 1, Hash: execwire.Hash(stablenet.HeaderHash(&anchor))}, Root: execwire.Hash(anchor.Root)}, Payload: payload}
	s, cfg := preparedExecutionDB(t, f)
	require.NoError(t, s.Close())
	_, srv := fixtureRPC(t, f)
	cfg.RPC.Endpoint = srv.URL
	cfg.RPC.CoalesceReads = false
	parameters, e := json.Marshal(fixture.Policy)
	require.NoError(t, e)
	cfg.Source.Execution.Consensus = &chains.ConsensusPolicy{Profile: "stablenet", Protocol: consensus.IndependentProtocol, Parameters: string(parameters)}
	ipc, e := os.MkdirTemp("", "e6-ipc-")
	require.NoError(t, e)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(ipc)) })
	cfg.Source.Execution.Socket = filepath.Join(ipc, "feed.sock")
	no := false
	cfg.Features = map[string]config.FeatureConfig{"token.metadata": {Enabled: &no}}
	a, e := NewApp(cfg, zap.NewNop(), false, "stablenet")
	require.NoError(t, e)
	defer a.Shutdown()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.run(ctx) }()
	finish := sync.OnceFunc(func() {
		cancel()
		a.executionFeed.Stop()
		e := <-done
		if !errors.Is(e, context.Canceled) && !errors.Is(e, context.DeadlineExceeded) {
			t.Errorf("execution runtime: %v", e)
		}
	})
	defer finish()
	c := dialExecutionFixture(t, ctx, cfg.Source.Execution.Socket, f.Chain, f.Baseline.Ref)
	ref := sendExecutionFixture(t, ctx, c, f, 1)
	require.Equal(t, result.Ref, ref)
	store := a.storage.(*postgres.Store)
	before, e := store.LastOutboxSeq(ctx)
	require.NoError(t, e)
	require.NoError(t, c.Close())
	c = dialExecutionFixture(t, ctx, cfg.Source.Execution.Socket, f.Chain, f.Baseline.Ref)
	require.Equal(t, ref, sendExecutionFixture(t, ctx, c, f, 1))
	after, e := store.LastOutboxSeq(ctx)
	require.NoError(t, e)
	require.Equal(t, before, after)
	_, e = store.BeginExecutionRewind(ctx, f.Baseline.Ref, f.Baseline.Root)
	require.ErrorIs(t, e, port.ErrExecutionConflict)
	require.NoError(t, c.Close())
	_, e = store.BeginExecutionRewind(ctx, ref, result.StateRoot)
	require.NoError(t, e)
	finish()
	a.Shutdown()
	reopened, e := NewApp(cfg, zap.NewNop(), false, "stablenet")
	require.NoError(t, e)
	result.Ref.Generation++
	f.Payload, e = execwire.MarshalResult(result)
	require.NoError(t, e)
	confirmed, e := reopened.fetcher.ConfirmExecution(t.Context(), f.Payload)
	require.NoError(t, e)
	require.True(t, confirmed)
	reopened.Shutdown()
	cfg.Source.Execution.Consensus = nil
	weakened, e := NewApp(cfg, zap.NewNop(), false, "stablenet")
	if weakened != nil {
		weakened.Shutdown()
	}
	require.ErrorIs(t, e, port.ErrExecutionConflict)
}
