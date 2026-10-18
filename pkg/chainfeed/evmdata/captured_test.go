package evmdata_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

type capturedFixtureReader struct {
	in                    capture.Input
	epoch                 uint64
	checks, reads, copies int
}

func (r *capturedFixtureReader) Read(context.Context, model.CaptureIdentity) (capture.Input, error) {
	r.reads++
	return r.in, nil
}
func (r *capturedFixtureReader) CopyPayload(context.Context, model.CaptureIdentity, model.DatasetKind, io.Writer) error {
	r.copies++
	return errors.New("unexpected object fallback")
}
func (r *capturedFixtureReader) ValidateProjection(_ context.Context, epoch uint64, _ model.CaptureIdentity) error {
	r.checks++
	if r.epoch != epoch {
		return capture.ErrStale
	}
	return nil
}

type materializedFixtureReader struct {
	*capturedFixtureReader
	calls int
	err   error
}

func (r *materializedFixtureReader) ReadProjectionInput(_ context.Context, epoch uint64, _ model.CaptureIdentity) (capture.Input, error) {
	r.calls++
	if r.err != nil {
		return r.in, r.err
	}
	if r.epoch != epoch {
		return capture.Input{}, capture.ErrStale
	}
	return r.in, nil
}

type duringDecode struct {
	chains.BinaryProfile
	action func()
}

func (p duringDecode) DecodeBlockRLP(h, b []byte) (*model.Block, error) {
	p.action()
	return p.BinaryProfile.DecodeBlockRLP(h, b)
}

func capturedFixture(t *testing.T) capture.Input {
	t.Helper()
	c := testchain.NewChain(1, nil)
	b := c.AddBlock()
	plan := evmdata.BasePlan()
	digest, e := plan.Digest()
	require.NoError(t, e)
	in := capture.Input{Manifest: model.CaptureManifest{Identity: model.CaptureIdentity{Block: model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: c.Block(0).Block.Hash()}, Number: 1, Hash: b.Block.Hash()}, Generation: 1, PlanVersion: 1, PlanDigest: digest}}, ParentHash: b.Block.ParentHash()}
	header, e := rlp.EncodeToBytes(b.Block.Header())
	require.NoError(t, e)
	body, e := rlp.EncodeToBytes(b.Block.Body())
	require.NoError(t, e)
	receipts, e := rlp.EncodeToBytes(b.Receipts)
	require.NoError(t, e)
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		value := [][]byte{header, body, receipts}[i]
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: k, Codec: evmdata.Codec(k), Status: model.DatasetPersisted, Verification: model.VerificationCommitment, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}, Source: "fixture", Digest: sha256.Sum256(value)})
		in.Payloads = append(in.Payloads, capture.Payload{Kind: k, Bytes: value})
	}
	return in
}

func TestCapturedMaterializedAndLegacyKeepEpochChecks(t *testing.T) {
	in := capturedFixture(t)
	id := in.Manifest.Identity
	legacy := &capturedFixtureReader{in: in, epoch: 1}
	b, rs, e := evmdata.ReadCaptured(t.Context(), legacy, evm.New("fixture"), 1, id)
	require.NoError(t, e)
	require.Equal(t, id.Block.Hash, b.Hash)
	require.Empty(t, rs)
	require.Equal(t, 2, legacy.checks)
	require.Equal(t, 1, legacy.reads)
	fast := &materializedFixtureReader{capturedFixtureReader: &capturedFixtureReader{in: in, epoch: 1}}
	b, _, e = evmdata.ReadCaptured(t.Context(), fast, evm.New("fixture"), 1, id)
	require.NoError(t, e)
	require.Equal(t, id.Block.Hash, b.Hash)
	require.Equal(t, 1, fast.calls)
	require.Equal(t, 1, fast.checks)
	require.Zero(t, fast.reads)
	require.Zero(t, fast.copies)
	fast = &materializedFixtureReader{capturedFixtureReader: &capturedFixtureReader{in: in, epoch: 1}}
	profile := duringDecode{BinaryProfile: evm.New("fixture"), action: func() { fast.epoch = 2 }}
	b, rs, e = evmdata.ReadCaptured(t.Context(), fast, profile, 1, id)
	require.ErrorIs(t, e, capture.ErrStale)
	require.Nil(t, b)
	require.Nil(t, rs)
	require.Equal(t, 1, fast.checks, "the final independent epoch barrier is mandatory")
	fast = &materializedFixtureReader{capturedFixtureReader: &capturedFixtureReader{in: in, epoch: 1}, err: io.ErrUnexpectedEOF}
	fast.in.Objects = []capture.ObjectPayload{{Kind: model.DatasetBodies}}
	_, _, e = evmdata.ReadCaptured(t.Context(), fast, evm.New("fixture"), 1, id)
	require.ErrorIs(t, e, io.ErrUnexpectedEOF)
	require.Zero(t, fast.reads)
	require.Zero(t, fast.copies, "an optional fast-path failure is never legacy fallback")
	fast.err = nil
	_, _, e = evmdata.ReadCaptured(t.Context(), fast, evm.New("fixture"), 1, id)
	require.ErrorIs(t, e, capture.ErrConflict)
	require.Zero(t, fast.copies, "materialized inputs cannot retain unresolved object refs")
}
