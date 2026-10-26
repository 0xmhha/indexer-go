package execution

import (
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"os"
	"sync/atomic"
	"testing"
)

func TestPrepareRejectsUnauthenticatedBlobInput(t *testing.T) {
	chain, r, _ := preparedFixture(t)
	for _, profile := range []string{"evm", "other"} {
		r.BlobBundle = &execwire.BlobBundle{Version: 1, Format: execwire.FullBlobFormat, Profile: profile, Provider: "retained-sidecars", Response: []byte{0xc0}}
		raw, e := execwire.MarshalResult(r)
		require.NoError(t, e)
		_, e = Prepare(evm.New("evm"), chain, raw)
		require.Error(t, e, "opaque or wrong-profile sidecars must not enter any writer")
	}
}

func TestReceiverRequiresBlobsBeforeLostACKConfirmation(t *testing.T) {
	chain, raw, _ := nativeBlobFixture(t)
	result, e := execwire.UnmarshalResult(raw)
	require.NoError(t, e)
	r, _, _ := receiverFixture(t, &controlledBackend{})
	r.Chain = chain
	r.RequireBlobSidecars = true
	store := r.Store.(*controllerStoreProbe)
	store.status.Chain = chain
	store.status.Anchor = execwire.Ref{Generation: 1, Hash: result.Parent}
	store.status.Visible = result.Ref
	var confirmed atomic.Int32
	r.Confirm = func(context.Context, []byte) (bool, error) { confirmed.Add(1); return true, nil }
	ctx, conn, finish, done := startReceiverSession(t, r)
	defer finish()
	require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: store.status.Anchor}}))
	_, e = conn.Read(ctx)
	require.NoError(t, e)
	parts, e := execwire.Split(chain, result.Ref, raw, execwire.MaxFrameBytes)
	require.NoError(t, e)
	for _, part := range parts {
		require.NoError(t, conn.Write(ctx, part))
	}
	ack, e := conn.Read(ctx)
	require.Error(t, e, "missing required sidecars must not be ACKed: %v", ack.Kind)
	require.ErrorIs(t, <-done, proof.ErrUnavailable)
	require.Zero(t, confirmed.Load(), "required policy applies before a retained-prefix confirmation")
}

func nativeBlobFixture(t *testing.T) (execwire.Chain, []byte, proof.Response) {
	t.Helper()
	raw, e := os.ReadFile("../../testdata/execution/evm-blobs.json")
	require.NoError(t, e)
	var f struct {
		Chain   execwire.Chain
		Payload []byte
	}
	require.NoError(t, json.Unmarshal(raw, &f))
	p, e := Prepare(evm.New("evm"), f.Chain, f.Payload)
	require.NoError(t, e)
	refs, e := proof.References(p.block)
	require.NoError(t, e)
	require.Len(t, refs, 3)
	response := proof.Response{Version: 1, Block: blobdata.Block(f.Chain, p.Ref()), Entries: make([]proof.Entry, len(refs))}
	for i := range response.Entries {
		response.Entries[i].Index = uint64(i)
		response.Entries[i].Commitment[0] = 0xc0
		response.Entries[i].Proof[0] = 0xc0
	}
	return f.Chain, f.Payload, response
}

func TestExecutionBlobNativeInputKZGAndStrictRequirement(t *testing.T) {
	chain, raw, response := nativeBlobFixture(t)
	_, e := PrepareContext(t.Context(), evm.New("evm"), chain, raw, true)
	require.ErrorIs(t, e, proof.ErrUnavailable)
	encoded, e := rlp.EncodeToBytes(response)
	require.NoError(t, e)
	out, e := AttachBlobInput(t.Context(), evm.New("evm"), chain, raw, "retained-sidecars", encoded)
	require.NoError(t, e)
	p, e := PrepareContext(t.Context(), evm.New("evm"), chain, out, true)
	require.NoError(t, e)
	require.Len(t, p.blobs.Entries, 3)
	require.Equal(t, execwire.PreBlock, p.result.Steps[0].Cause)
	require.Len(t, p.result.Steps[0].Storage, 2, "real EIP-4788 system writes belong to pre-block, not the user call tree")
	require.Len(t, p.result.CallTraces.Frames, 1)
	for _, mutate := range []func(*proof.Response){
		func(v *proof.Response) { v.Entries[0].Proof[0] = 0x80 },
		func(v *proof.Response) { v.Entries = v.Entries[:2] },
		func(v *proof.Response) { v.Block.Hash[0] ^= 1 },
		func(v *proof.Response) { v.Entries[0].Index = 1 },
	} {
		bad := response
		bad.Entries = append([]proof.Entry{}, response.Entries...)
		mutate(&bad)
		b, e := rlp.EncodeToBytes(bad)
		require.NoError(t, e)
		_, e = AttachBlobInput(t.Context(), evm.New("evm"), chain, raw, "retained-sidecars", b)
		require.Error(t, e, "invalid or incomplete sidecars cannot acquire a new spool digest")
	}
}

func TestAttachBlobInputRetainsExactEmptyEvidenceWithoutMutation(t *testing.T) {
	chain, _, raw := preparedFixture(t)
	original := append([]byte{}, raw...)
	out, e := AttachBlobInput(t.Context(), evm.New("evm"), chain, raw, "retained-sidecars", nil)
	require.NoError(t, e)
	r, e := execwire.UnmarshalResult(out)
	require.NoError(t, e)
	require.NotNil(t, r.BlobBundle, "no-reference evidence must be explicitly retained before spool admission")
	require.Equal(t, original, raw)
	require.NotEqual(t, raw, out)
	_, e = PrepareContext(t.Context(), evm.New("evm"), chain, out, true)
	require.NoError(t, e)
	_, e = AttachBlobInput(t.Context(), evm.New("evm"), chain, out, "retained-sidecars", nil)
	require.Error(t, e, "never replace already attached input")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = AttachBlobInput(ctx, evm.New("evm"), chain, raw, "retained-sidecars", nil)
	require.ErrorIs(t, e, context.Canceled)
}

func TestExecutionBlobsPolicyUnknownAndNoReferenceEvidence(t *testing.T) {
	chain, raw, response := nativeBlobFixture(t)
	p := evm.New("evm", evm.WithBlobCapability(model.BlobCapability{Version: 1, Profile: "evm", Status: model.BlobUnsupported, Reason: "disabled_in_fixture", Basis: "node_transaction_admission", Revision: "6e764023ee6515fe70573e123ed2db912a7207b4", SourceURL: "https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-txpool/src/pool.rs"}))
	encoded, e := rlp.EncodeToBytes(response)
	require.NoError(t, e)
	_, e = AttachBlobInput(t.Context(), p, chain, raw, "retained-sidecars", encoded)
	require.ErrorIs(t, e, proof.ErrUnavailable, "audited disabled policy never becomes a full-blob source")
	emptyChain, _, empty := preparedFixture(t)
	out, e := AttachBlobInput(t.Context(), p, emptyChain, empty, "no-reference-evidence", nil)
	require.NoError(t, e)
	prepared, e := PrepareContext(t.Context(), p, emptyChain, out, true)
	require.NoError(t, e)
	require.Empty(t, prepared.blobs.References, "no refs is per-block evidence, not a supported transport claim")
	require.Equal(t, execwire.MaxBlobResponseBytes, proof.MaxResponseBytes)
	require.Equal(t, execwire.FullBlobFormat, proof.Format)
	ordinary, e := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, e)
	ordinary.block.Transactions[0].Opaque = true
	require.NoError(t, func() error {
		_, e := blobdata.Validate(t.Context(), evm.New("evm"), chain, ordinary.result, ordinary.block, false)
		return e
	}())
	_, e = blobdata.Validate(t.Context(), evm.New("evm"), chain, ordinary.result, ordinary.block, true)
	require.Error(t, e, "strict policy cannot infer absent blob refs from an opaque transaction")
}
