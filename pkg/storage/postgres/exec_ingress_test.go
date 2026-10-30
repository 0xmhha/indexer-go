package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func ingressOwnerFixture(t *testing.T) (*Store, port.ExecutionIngressPolicy) {
	t.Helper()
	s := newTestStore(t)
	m, payload := bootstrapEmpty(t, false)
	_, err := s.BootstrapExecution(t.Context(), bytes.NewReader(payload), m, evm.New("evm"), func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	return s, port.ExecutionIngressPolicy{Version: 1, Module: "fixture.native", Chain: m.Chain, Anchor: m.Ref, AnchorRoot: m.Root, Requirements: execwire.Hash{9}}
}

func TestExecutionIngressOwnerPinsPolicyBeforeNativeAndRejectsDowngrade(t *testing.T) {
	s, policy := ingressOwnerFixture(t)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	o, err := s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	require.Equal(t, policy, o.Policy)
	require.NotZero(t, o.PolicyDigest)
	require.Nil(t, o.Binding, "startup ownership precedes actual SDK instance creation")
	require.ErrorIs(t, s.ConfigureExecutionIngress(t.Context(), nil, false), port.ErrExecutionConflict)
	changed := policy
	changed.Requirements[0]++
	require.ErrorIs(t, s.ConfigureExecutionIngress(t.Context(), &changed, false), port.ErrExecutionConflict)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	api := openTestStore(t, s.schema, true)
	require.NoError(t, api.ConfigureExecutionIngress(t.Context(), nil, true))
	require.ErrorIs(t, api.ConfigureExecutionIngress(t.Context(), &policy, false), port.ErrReadOnly)
	require.EqualValues(t, 4, s.pool.Config().MaxConns)
}

func TestExecutionIngressOwnerBlocksLegacyClearAfterConfigRemoval(t *testing.T) {
	s, policy := ingressOwnerFixture(t)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	for _, all := range []bool{false, true} {
		_, err := s.ClearChainData(t.Context(), nil, all)
		require.ErrorIs(t, err, port.ErrExecutionConflict)
		c, err := s.ExecutionStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, policy.Anchor, c.Visible)
	}
}

func TestExecutionIngressOwnerBindsActualIdentityOnceAndPreservesOnFailure(t *testing.T) {
	s, policy := ingressOwnerFixture(t)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	o, err := s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	b := port.ExecutionIngressBinding{Version: 1, PolicyDigest: o.PolicyDigest, NativeInstance: execwire.Hash{1}, BootstrapDigest: execwire.Hash{2}, RawStoreID: execwire.Hash{3}, SourcePolicyDigest: execwire.Hash{4}, NativeIdentity: []byte(`{"label":"'); DROP TABLE blocks; --"}`)}
	require.NoError(t, s.BindExecutionIngress(t.Context(), b))
	require.NoError(t, s.BindExecutionIngress(t.Context(), b))
	changed := b
	changed.RawStoreID[0]++
	require.ErrorIs(t, s.BindExecutionIngress(t.Context(), changed), port.ErrExecutionConflict)
	o, err = s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	require.Equal(t, b, *o.Binding)
	require.NotZero(t, o.BindingDigest)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM blocks").Scan(&count))
	require.Equal(t, 1, count, "injection-shaped values are bound data")
}

func ingressArchiveFixture(t *testing.T) (*Store, port.ExecutionIngressPolicy, port.ExecutionPart, port.ExecutionIngressArchive) {
	t.Helper()
	s, policy := ingressOwnerFixture(t)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	o, err := s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	b := port.ExecutionIngressBinding{Version: 1, PolicyDigest: o.PolicyDigest, NativeInstance: execwire.Hash{1}, BootstrapDigest: execwire.Hash{2}, RawStoreID: execwire.Hash{3}, SourcePolicyDigest: execwire.Hash{4}, NativeIdentity: []byte(`{}`)}
	require.NoError(t, s.BindExecutionIngress(t.Context(), b))
	o, err = s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	p := port.ExecutionPart{Writer: port.ExecutionBlocks, Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Parent: policy.Anchor.Hash, Digest: execwire.Hash{3}}
	a := port.ExecutionIngressArchive{Version: 1, OwnerDigest: o.BindingDigest, Ref: p.Ref, Parent: p.Parent, Digest: p.Digest, InputDigest: execwire.Hash{5}, NativeEpoch: 2, NativeReadEpoch: 9, NativeRequestID: 1, ObservedRawEpoch: 17, Provenance: []byte(`{"receipt":"original"}`)}
	for _, kind := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		data := []byte("original-" + string(kind) + "'); DROP TABLE blocks; --")
		a.Datasets = append(a.Datasets, port.ExecutionIngressDataset{Kind: kind, Codec: "fixture.v1", Reference: []byte(`{"ref":"exact"}`), Digest: execwire.Hash(sha256.Sum256(data)), PayloadBytes: uint64(len(data)), Payload: data})
	}
	return s, policy, p, a
}

func TestExecutionIngressArchiveRequiredInTheSameBoundBlocksPart(t *testing.T) {
	s, policy, p, a := ingressArchiveFixture(t)
	b := &model.Block{Number: p.Ref.Height, Hash: common.Hash(p.Ref.Hash), ParentHash: common.Hash(p.Parent), StateRoot: common.Hash(policy.AnchorRoot)}
	err := s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionCore(ctx, p.Writer, b, nil) })
	require.ErrorIs(t, err, port.ErrExecutionCoverage, "owner cannot bypass immutable raw archive")
	cursor, err := s.ExecutionCursor(t.Context(), p.Writer)
	require.NoError(t, err)
	require.Equal(t, policy.Anchor, cursor.Ref)
	require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
		if err := s.ApplyExecutionCore(ctx, p.Writer, b, nil); err != nil {
			return err
		}
		return s.ApplyExecutionIngress(ctx, a)
	}))
	require.ErrorIs(t, s.ApplyExecutionIngress(t.Context(), a), port.ErrClosed)
	_, err = s.ExecutionIngressArchive(t.Context(), p)
	require.Error(t, err, "unpublished archive is not canonical/GC coverage")
}

func TestExecutionIngressArchiveAtomicRollbackAndExactPublicationCoverage(t *testing.T) {
	s, policy, p, a := ingressArchiveFixture(t)
	b := &model.Block{Number: p.Ref.Height, Hash: common.Hash(p.Ref.Hash), ParentHash: common.Hash(p.Parent), StateRoot: common.Hash(policy.AnchorRoot)}
	apply := func(ctx context.Context) error {
		if err := s.ApplyExecutionCore(ctx, p.Writer, b, nil); err != nil {
			return err
		}
		return s.ApplyExecutionIngress(ctx, a)
	}
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
		if err := apply(ctx); err != nil {
			return err
		}
		return context.Canceled
	}), context.Canceled)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM exec_ingress_payloads").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, s.WriteExecutionPart(t.Context(), p, apply))
	for _, writer := range port.ExecutionWriters() {
		if writer == port.ExecutionBlocks {
			continue
		}
		part := p
		part.Writer = writer
		require.NoError(t, s.WriteExecutionPart(t.Context(), part, func(context.Context) error { return nil }))
	}
	fresh := 0
	_, err := s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: p.Ref, Parent: p.Parent, Digest: p.Digest, Root: policy.AnchorRoot}, func(context.Context) error { return nil }, func(context.Context) error { fresh++; return nil })
	require.NoError(t, err)
	require.Equal(t, 1, fresh)
	got, err := s.ExecutionIngressArchive(t.Context(), p)
	require.NoError(t, err)
	require.Equal(t, a, got)
	require.EqualValues(t, 4, s.pool.Config().MaxConns)
	// A common prefix keeps its actual publication generation after a newer
	// coordinator generation. This fixture changes only control metadata.
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_control SET generation=7 WHERE id")
	require.NoError(t, err)
	got, err = s.ExecutionIngressArchive(t.Context(), p)
	require.NoError(t, err)
	require.Equal(t, a, got)
	require.EqualValues(t, 1, got.Ref.Generation)
	require.EqualValues(t, 2, got.NativeEpoch)
	require.EqualValues(t, 17, got.ObservedRawEpoch)
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_ingress_payloads SET body=$1", []byte("corrupted"))
	require.NoError(t, err)
	_, err = s.ExecutionIngressArchive(t.Context(), p)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
}

func TestExecutionIngressOwnerRefusesMissingPinnedRowAndInvalidAdoption(t *testing.T) {
	s, policy := ingressOwnerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, s.ConfigureExecutionIngress(ctx, &policy, false), context.Canceled)
	_, err := s.ExecutionIngressOwner(t.Context())
	require.ErrorIs(t, err, port.ErrNotFound)
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_control SET coverage='partial' WHERE id")
	require.NoError(t, err)
	require.ErrorIs(t, s.ConfigureExecutionIngress(t.Context(), &policy, false), port.ErrExecutionCoverage)
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_control SET coverage='full' WHERE id")
	require.NoError(t, err)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.IngressOwned)
	_, err = s.pool.Exec(t.Context(), "DELETE FROM exec_source_owner")
	require.NoError(t, err)
	_, err = s.ExecutionIngressOwner(t.Context())
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	require.ErrorIs(t, s.ConfigureExecutionIngress(t.Context(), nil, false), port.ErrExecutionConflict)
	_, err = s.ClearChainData(t.Context(), nil, true)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
}

func TestExecutionIngressOwnerCannotAdoptExistingUnpublishedParts(t *testing.T) {
	s, policy := ingressOwnerFixture(t)
	part := port.ExecutionPart{Writer: port.ExecutionAccounts, Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}, Parent: policy.Anchor.Hash, Digest: execwire.Hash{3}}
	require.NoError(t, s.WriteExecutionPart(t.Context(), part, func(context.Context) error { return nil }))
	require.ErrorIs(t, s.ConfigureExecutionIngress(t.Context(), &policy, false), port.ErrExecutionCoverage)
	_, err := s.ExecutionIngressOwner(t.Context())
	require.ErrorIs(t, err, port.ErrNotFound)
}
