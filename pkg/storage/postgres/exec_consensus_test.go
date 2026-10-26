package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/0xmhha/indexer-go/x/stablenet/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func TestSeedPriority3E6PostgresPublicationRecovery(t *testing.T) {
	ctx := t.Context()
	data, e := os.ReadFile("../../../testdata/execution/consensus/native.json")
	require.NoError(t, e)
	var fixture struct {
		Policy  consensus.TrustPolicy
		Headers [][]byte
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	parameters, e := json.Marshal(fixture.Policy)
	require.NoError(t, e)
	policy := chains.ConsensusPolicy{Profile: "stablenet", Protocol: consensus.IndependentProtocol, Parameters: string(parameters)}
	s := newTestStore(t)
	var fsync string
	require.NoError(t, s.pool.QueryRow(ctx, "SHOW fsync").Scan(&fsync))
	require.Equal(t, "on", fsync)
	var anchor types.Header
	require.NoError(t, rlp.DecodeBytes(fixture.Policy.AnchorHeader, &anchor))
	cfg := port.ExecutionConfig{Chain: fixture.Policy.Chain, Anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash(stablenet.HeaderHash(&anchor))}, AnchorRoot: execwire.Hash(anchor.Root), PartitionBlocks: 2}
	require.NoError(t, s.SetBlock(ctx, &model.Block{Hash: common.Hash(cfg.Anchor.Hash), StateRoot: anchor.Root}))
	require.NoError(t, s.InitExecution(ctx, cfg))
	require.NoError(t, s.SeedExecution(ctx, port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot}))
	require.NoError(t, s.ConfigureExecutionConsensus(ctx, policy))
	require.ErrorIs(t, s.ConfigureExecutionConsensus(ctx, chains.ConsensusPolicy{}), port.ErrExecutionConflict)
	changed := policy
	changed.Parameters += " "
	require.ErrorIs(t, s.ConfigureExecutionConsensus(ctx, changed), port.ErrExecutionConflict)
	failure := errors.New("new uncached fresh unavailable")
	var lastPayload []byte
	for _, header := range fixture.Headers {
		var h types.Header
		require.NoError(t, rlp.DecodeBytes(header, &h))
		block := types.NewBlockWithHeader(&h)
		raw, e := rlp.EncodeToBytes(block)
		require.NoError(t, e)
		r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: h.Number.Uint64(), Hash: execwire.Hash(stablenet.HeaderHash(&h))}, Parent: execwire.Hash(h.ParentHash), StateRoot: execwire.Hash(h.Root), ReceiptsRoot: execwire.Hash(h.ReceiptHash), Time: h.Time, BlockRLP: raw, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Finalize, TxIndex: -1}}}
		payload, e := execwire.MarshalResult(r)
		require.NoError(t, e)
		lastPayload = payload
		prepared, e := execution.Prepare(stablenet.New(), cfg.Chain, payload)
		require.NoError(t, e)
		backend := &execution.StoreBackend{Store: s}
		for _, w := range port.ExecutionWriters() {
			require.NoError(t, backend.Part(ctx, w, prepared))
		}
		p := port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, Digest: execwire.Hash(sha256.Sum256(payload)), ConsensusHeader: header}
		previous, e := readConsensus(ctx, s.pool)
		require.NoError(t, e)
		apply := func(bound context.Context) error { return s.Put(bound, []byte("e6-publication"), header) }
		bad := p
		bad.ConsensusHeader = nil
		_, e = s.CommitExecution(ctx, bad, apply, func(context.Context) error { t.Fatal("missing proof reached fresh"); return nil })
		require.Error(t, e)
		_, e = s.CommitExecution(ctx, p, apply, func(bound context.Context) error {
			local, e := readConsensus(bound, s.q(bound))
			require.NoError(t, e)
			require.Equal(t, p.Ref.Height, local.point.Height)
			external, e := readConsensus(ctx, s.pool)
			require.NoError(t, e)
			require.Equal(t, previous, external)
			return failure
		})
		require.ErrorIs(t, e, failure)
		retained, e := readConsensus(ctx, s.pool)
		require.NoError(t, e)
		require.Equal(t, previous, retained)
		_, e = s.CommitExecution(ctx, p, apply, func(bound context.Context) error {
			_, e := s.q(bound).Exec(bound, "UPDATE exec_control SET generation=generation+1 WHERE id")
			return e
		})
		require.ErrorIs(t, e, port.ErrExecutionStale)
		cancelled, cancel := context.WithCancel(ctx)
		_, e = s.CommitExecution(cancelled, p, apply, func(context.Context) error { cancel(); return nil })
		require.ErrorIs(t, e, context.Canceled)
		committed, e := s.CommitExecution(ctx, p, apply, func(context.Context) error { return nil })
		require.NoError(t, e)
		require.True(t, committed)
		finalized, e := readConsensus(ctx, s.pool)
		require.NoError(t, e)
		require.Equal(t, p.Ref.Height, finalized.point.Height)
		_, e = s.BeginExecutionRewind(ctx, execwire.Ref{Generation: 1, Height: previous.point.Height, Hash: previous.point.Hash}, previous.point.Root)
		require.ErrorIs(t, e, port.ErrExecutionConflict)
		// A newly opened store has no in-memory verifier state. Its persisted
		// policy and finality must support an exact lost-ACK confirmation.
		reopened, e := Open(ctx, Options{DSN: testDSN(t), Schema: s.schema, MaxConns: 4})
		require.NoError(t, e)
		require.NoError(t, reopened.ConfigureExecutionConsensus(ctx, policy))
		ok, e := reopened.ConfirmExecution(ctx, payload, func(context.Context, execwire.Ref) error { return nil })
		require.NoError(t, e)
		require.True(t, ok)
		require.ErrorIs(t, reopened.ConfigureExecutionConsensus(ctx, chains.ConsensusPolicy{}), port.ErrExecutionConflict)
		require.NoError(t, reopened.Close())
	}
	current, e := s.ExecutionStatus(ctx)
	require.NoError(t, e)
	_, e = s.BeginExecutionRewind(ctx, current.Visible, current.Root)
	require.NoError(t, e)
	require.NoError(t, s.ConfigureExecutionConsensus(ctx, policy), "restart must permit recovery of unpublished work at the finalized tip")
	_, e = s.ResumeExecutionRewind(ctx, nil, func(context.Context, execwire.Ref) error { return failure })
	require.ErrorIs(t, e, failure)
	held, e := s.ExecutionStatus(ctx)
	require.NoError(t, e)
	require.True(t, held.Rewinding)
	require.NoError(t, s.ConfigureExecutionConsensus(ctx, policy))
	_, e = s.ResumeExecutionRewind(ctx, nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	recovered, e := s.ExecutionStatus(ctx)
	require.NoError(t, e)
	require.False(t, recovered.Rewinding)
	require.Equal(t, current.Visible.Height, recovered.Visible.Height)
	require.Equal(t, current.Visible.Hash, recovered.Visible.Hash)
	require.Equal(t, current.Generation+1, recovered.Generation)
	retained, e := execwire.UnmarshalResult(lastPayload)
	require.NoError(t, e)
	retained.Ref.Generation = recovered.Generation
	lastPayload, e = execwire.MarshalResult(retained)
	require.NoError(t, e)
	ok, e := s.ConfirmExecution(ctx, lastPayload, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	require.True(t, ok)
	_, e = s.pool.Exec(ctx, "UPDATE exec_consensus SET state=$1 WHERE id", []byte("{}"))
	require.NoError(t, e)
	noFresh := func(context.Context, execwire.Ref) error {
		t.Fatal("corrupt/missing consensus trust reached fresh or ACK")
		return nil
	}
	ok, e = s.ConfirmExecution(ctx, lastPayload, noFresh)
	require.Error(t, e)
	require.False(t, ok)
	_, e = s.pool.Exec(ctx, "DELETE FROM exec_consensus WHERE id")
	require.NoError(t, e)
	ok, e = s.ConfirmExecution(ctx, lastPayload, noFresh)
	require.Error(t, e)
	require.False(t, ok)

}

func TestSeedPriority3E6CorruptRecoveryFailsBeforeAdmission(t *testing.T) {
	ctx := t.Context()
	data, e := os.ReadFile("../../../testdata/execution/consensus/native.json")
	require.NoError(t, e)
	var f struct{ Policy consensus.TrustPolicy }
	require.NoError(t, json.Unmarshal(data, &f))
	parameters, e := json.Marshal(f.Policy)
	require.NoError(t, e)
	policy := chains.ConsensusPolicy{Profile: "stablenet", Protocol: consensus.IndependentProtocol, Parameters: string(parameters)}
	s := newTestStore(t)
	v, e := consensus.NewIndependent(f.Policy)
	require.NoError(t, e)
	point, e := v.AnchorPoint()
	require.NoError(t, e)
	cfg := port.ExecutionConfig{Chain: f.Policy.Chain, Anchor: execwire.Ref{Generation: 1, Height: point.Height, Hash: point.Hash}, AnchorRoot: point.Root, PartitionBlocks: 2}
	require.NoError(t, s.SetBlock(ctx, &model.Block{Number: point.Height, Hash: common.Hash(point.Hash), StateRoot: common.Hash(point.Root)}))
	require.NoError(t, s.InitExecution(ctx, cfg))
	require.NoError(t, s.SeedExecution(ctx, port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot}))
	require.NoError(t, s.ConfigureExecutionConsensus(ctx, policy))
	_, e = s.pool.Exec(ctx, "UPDATE exec_consensus SET state=$1 WHERE id", []byte("{}"))
	require.NoError(t, e)
	require.Error(t, s.ConfigureExecutionConsensus(ctx, policy), "corrupt durable trust must fail before source admission, including ACK-only recovery")
	_, e = s.pool.Exec(ctx, "DELETE FROM exec_consensus WHERE id")
	require.NoError(t, e)
	require.Error(t, s.ConfigureExecutionConsensus(ctx, policy), "missing pinned trust must never be recreated as a new source")

}
