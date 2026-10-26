package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestExecutionProofStorageContract(t *testing.T) {
	s := newTestStore(t)
	_, ok := any(s).(port.ExecutionProofWriter)
	require.True(t, ok, "existing accounts part must retain selected full state evidence")
	_, ok = any(s).(port.ExecutionProofReader)
	require.True(t, ok, "published selected proofs must survive raw file removal")
}

func stateProofFixture(t *testing.T, collected bool, modify ...func(*stateproof.Bundle)) (*Store, execwire.Result, []byte, stateproof.Bundle) {
	t.Helper()
	var f struct {
		Chain                             execwire.Chain
		ParentHeader, Payload, ProofInput []byte
		Baseline                          port.ExecutionBaseline
	}
	raw, e := os.ReadFile("../../../testdata/execution/evm-state-proofs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	var b stateproof.Bundle
	require.NoError(t, json.Unmarshal(f.ProofInput, &b))
	for _, fn := range modify {
		fn(&b)
	}
	if len(modify) > 0 {
		f.ProofInput, e = json.Marshal(b)
		require.NoError(t, e)
	}
	profile := evm.New("evm")
	if collected {
		f.Payload, e = execution.AttachStateProofInput(t.Context(), profile, f.Chain, f.Payload, "native-state", f.ProofInput)
		require.NoError(t, e)
	}
	result, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	s := newTestStore(t)
	parent, e := profile.DecodeHeaderRLP(f.ParentHeader)
	require.NoError(t, e)
	require.NoError(t, s.SetBlock(t.Context(), parent))
	require.NoError(t, s.InitExecution(t.Context(), port.ExecutionConfig{Chain: f.Chain, Anchor: f.Baseline.Ref, AnchorRoot: f.Baseline.Root, PartitionBlocks: 2}))
	require.NoError(t, s.SeedExecution(t.Context(), f.Baseline))
	prepared, e := execution.Prepare(profile, f.Chain, f.Payload)
	require.NoError(t, e)
	backend := execution.StoreBackend{Store: s}
	for _, writer := range port.ExecutionWriters() {
		require.NoError(t, backend.Part(t.Context(), writer, prepared))
	}
	return s, result, f.Payload, b
}
func stateProofPublication(r execwire.Result, raw []byte) port.ExecutionPublication {
	return port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, Digest: execwire.Hash(sha256.Sum256(raw)), CallTraces: r.CallTraces != nil, Blobs: r.BlobBundle != nil, StateProofs: r.StateProofBundle != nil}
}
func TestExecutionProofDurabilityFreshACKCorruptionAndRewind(t *testing.T) {
	s, r, raw, b := stateProofFixture(t, true)
	query := port.ExecutionProofQuery{Height: r.Ref.Height, Address: execwire.Address(b.Selection.Targets[0].Address)}
	_, e := s.ExecutionProof(t.Context(), query)
	require.ErrorIs(t, e, port.ErrNotFound)
	pub := stateProofPublication(r, raw)
	failure := errors.New("fresh failed")
	_, e = s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(context.Context) error { return failure })
	require.ErrorIs(t, e, failure)
	_, e = s.ExecutionProof(t.Context(), query)
	require.ErrorIs(t, e, port.ErrNotFound)
	fresh := 0
	_, e = s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(context.Context) error { fresh++; return nil })
	require.NoError(t, e)
	require.Equal(t, 1, fresh)
	view, e := s.ExecutionProof(t.Context(), query)
	require.NoError(t, e)
	require.True(t, view.Account.Exists)
	require.Len(t, view.Account.Storage, 2)
	require.NotNil(t, view.Account.Code)
	require.NotEmpty(t, *view.Account.Code)
	require.Equal(t, pub.Root, view.Root)
	absent := query
	absent.Address = execwire.Address(b.Selection.Targets[1].Address)
	av, e := s.ExecutionProof(t.Context(), absent)
	require.NoError(t, e)
	require.False(t, av.Account.Exists)
	require.Zero(t, av.Account.ConsensusBalance)
	require.Len(t, av.Account.Storage, 1)
	missing := query
	missing.Address = execwire.Address{18: 255}
	_, e = s.ExecutionProof(t.Context(), missing)
	require.ErrorIs(t, e, port.ErrNotFound)
	pin := query
	pin.Epoch = view.Epoch
	pin.Hash = view.Ref.Hash
	_, e = s.ExecutionProof(t.Context(), pin)
	require.NoError(t, e)
	pin.Hash[0] ^= 1
	_, e = s.ExecutionProof(t.Context(), pin)
	require.ErrorIs(t, e, port.ErrExecutionStale)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = s.ExecutionProof(canceled, query)
	require.ErrorIs(t, e, context.Canceled)
	ok, e := s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { fresh++; return nil })
	require.NoError(t, e)
	require.True(t, ok)
	var meta, response []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), `SELECT metadata,response FROM exec_state_proofs WHERE height=$1`, int64(r.Ref.Height)).Scan(&meta, &response))
	var record port.ExecutionProofRecord
	require.NoError(t, json.Unmarshal(meta, &record))
	var broken stateproof.Bundle
	require.NoError(t, json.Unmarshal(response, &broken))
	*broken.Evidence[0].Proof.Nonce++
	bad, e := json.Marshal(broken)
	require.NoError(t, e)
	record.InputDigest = execwire.Hash(sha256.Sum256(bad))
	badMeta, e := json.Marshal(record)
	require.NoError(t, e)
	badDigest := sha256.Sum256(badMeta)
	_, e = s.pool.Exec(t.Context(), `UPDATE exec_state_proofs SET metadata=$1,metadata_digest=$2,response=$3 WHERE height=$4`, badMeta, badDigest[:], bad, int64(r.Ref.Height))
	require.NoError(t, e)
	_, e = s.ExecutionProof(t.Context(), query)
	require.Error(t, e, "matching metadata checksums cannot authenticate false trie values")
	fresh = 0
	_, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { fresh++; return nil })
	require.Error(t, e)
	require.Zero(t, fresh)
	digest := sha256.Sum256(meta)
	_, e = s.pool.Exec(t.Context(), `UPDATE exec_state_proofs SET metadata=$1,metadata_digest=$2,response=$3 WHERE height=$4`, meta, digest[:], response, int64(r.Ref.Height))
	require.NoError(t, e)
	_, e = s.BeginExecutionRewind(t.Context(), r.Ref, r.StateRoot)
	require.NoError(t, e)
	_, e = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	r.Ref.Generation++
	raw, e = execwire.MarshalResult(r)
	require.NoError(t, e)
	ok, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	require.True(t, ok)
	view, e = s.ExecutionProof(t.Context(), query)
	require.NoError(t, e)
	require.EqualValues(t, 2, view.Epoch)
	require.EqualValues(t, 1, view.Ref.Generation)
	require.Equal(t, pub.Digest, view.Digest)
	_, e = s.pool.Exec(t.Context(), `DELETE FROM exec_state_proofs WHERE height=$1`, int64(r.Ref.Height))
	require.NoError(t, e)
	_, e = s.ConfirmExecution(t.Context(), raw, func(context.Context, execwire.Ref) error { t.Fatal("missing proof invoked fresh"); return nil })
	require.ErrorIs(t, e, execwire.ErrIncomplete)
	c, e := s.ExecutionStatus(t.Context())
	require.NoError(t, e)
	anchor := c.Anchor
	anchor.Generation = c.Generation
	_, e = s.BeginExecutionRewind(t.Context(), anchor, c.AnchorRoot)
	require.NoError(t, e)
	_, e = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, e)
	pin.Epoch = view.Epoch
	pin.Hash = view.Ref.Hash
	_, e = s.ExecutionProof(t.Context(), pin)
	require.ErrorIs(t, e, port.ErrExecutionStale)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), `SELECT count(*) FROM exec_state_proofs`).Scan(&count))
	require.Zero(t, count)
}
func TestExecutionProofAbsenceIsNotUncollected(t *testing.T) {
	s, r, raw, b := stateProofFixture(t, false)
	_, e := s.CommitExecution(t.Context(), stateProofPublication(r, raw), func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, e)
	_, e = s.ExecutionProof(t.Context(), port.ExecutionProofQuery{Height: r.Ref.Height, Address: execwire.Address(b.Selection.Targets[1].Address)})
	require.ErrorIs(t, e, port.ErrNotFound)
}

func TestExecutionProofNoSlotsAndUnmatchedPublicationMarker(t *testing.T) {
	s, r, raw, b := stateProofFixture(t, true, func(b *stateproof.Bundle) {
		b.Selection.Targets[0].Slots = nil
		b.Selection.Targets[0].Code = false
		b.Evidence[0].Code = nil
		b.Evidence[0].Proof.StorageProof = nil
	})
	pub := stateProofPublication(r, raw)
	pub.StateProofs = false
	_, e := s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(context.Context) error { t.Fatal("unmatched marker invoked fresh"); return nil })
	require.ErrorIs(t, e, port.ErrExecutionConflict)
	pub.StateProofs = true
	_, e = s.CommitExecution(t.Context(), pub, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	require.NoError(t, e)
	view, e := s.ExecutionProof(t.Context(), port.ExecutionProofQuery{Height: 1, Address: execwire.Address(b.Selection.Targets[0].Address)})
	require.NoError(t, e)
	require.Empty(t, view.Account.Storage)
	require.Nil(t, view.Account.Code)
	require.Contains(t, string(view.Proof), `"storageProof":[]`)
	require.ErrorIs(t, s.ApplyExecutionProofs(t.Context(), execwire.Chain{}, r), port.ErrClosed)
}
