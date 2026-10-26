package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestExecutionStateProofRejectsMalformedInput(t *testing.T) {
	var f struct {
		Chain   execwire.Chain
		Payload []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-storage.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	r, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	r.StateProofBundle = &execwire.StateProofBundle{Version: 1, Format: execwire.StateProofFormat, Profile: "evm", Provider: "retained-state", Response: []byte(`{}`)}
	raw, e = execwire.MarshalResult(r)
	require.NoError(t, e)
	_, e = PrepareContext(context.Background(), evm.New("evm"), f.Chain, raw, false)
	require.Error(t, e, "supplied unverified state evidence must not reach writers")
}

func TestReceiverRequiresStateProofsBeforeLostACKConfirmation(t *testing.T) {
	var f struct {
		Chain   execwire.Chain
		Payload []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-storage.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	result, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	r, _, _ := receiverFixture(t, &controlledBackend{})
	r.Chain = f.Chain
	r.RequiredStateProofs = &stateproof.Selection{Targets: []stateproof.Target{{}}}
	store := r.Store.(*controllerStoreProbe)
	store.status.Chain = f.Chain
	store.status.Anchor = execwire.Ref{Generation: 1, Hash: result.Parent}
	store.status.Visible = result.Ref
	var confirmed atomic.Int32
	r.Confirm = func(context.Context, []byte) (bool, error) { confirmed.Add(1); return true, nil }
	ctx, conn, finish, done := startReceiverSession(t, r)
	defer finish()
	require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: f.Chain, Ref: store.status.Anchor}}))
	_, e = conn.Read(ctx)
	require.NoError(t, e)
	parts, e := execwire.Split(f.Chain, result.Ref, f.Payload, execwire.MaxFrameBytes)
	require.NoError(t, e)
	for _, part := range parts {
		require.NoError(t, conn.Write(ctx, part))
	}
	ack, e := conn.Read(ctx)
	require.Error(t, e, "missing required selection must never be ACKed: %v", ack.Kind)
	require.ErrorIs(t, <-done, stateproof.ErrProof)
	require.Zero(t, confirmed.Load())
}

func TestExecutionStateProofAttachmentRetainsActualNativeEvidence(t *testing.T) {
	var f struct {
		Chain               execwire.Chain
		Payload, ProofInput []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-state-proofs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	out, e := AttachStateProofInput(t.Context(), evm.New("evm"), f.Chain, f.Payload, "retained-state", f.ProofInput)
	require.NoError(t, e)
	require.False(t, bytes.Equal(f.Payload, out), "before-spool attachment must retain the actual native proofs in the exact input")
	r, e := execwire.UnmarshalResult(out)
	require.NoError(t, e)
	require.NotNil(t, r.StateProofBundle)
}

type stateProofCallerProbe struct {
	t      *testing.T
	bundle stateproof.Bundle
	hash   execwire.Hash
	calls  int
	fail   bool
}

func (p *stateProofCallerProbe) BatchCallContext(ctx context.Context, batch []rpc.BatchElem) error {
	p.calls++
	require.Len(p.t, batch, len(p.bundle.Evidence)*2)
	for i := range batch {
		selector := batch[i].Args[len(batch[i].Args)-1].(map[string]interface{})
		require.Equal(p.t, common.Hash(p.hash), selector["blockHash"])
		require.Equal(p.t, true, selector["requireCanonical"])
		index := i / 2
		switch batch[i].Method {
		case "eth_getProof":
			*batch[i].Result.(*stateproof.Proof) = p.bundle.Evidence[index].Proof
		case "eth_getCode":
			*batch[i].Result.(*hexutil.Bytes) = *p.bundle.Evidence[index].Code
		default:
			p.t.Fatalf("unexpected RPC %s", batch[i].Method)
		}
		if p.fail {
			batch[i].Error = errors.New("pruned state")
		}
	}
	return ctx.Err()
}
func TestExecutionStateProofBorrowedSupplyBatchesExactHashWithoutFallback(t *testing.T) {
	var f struct {
		Chain               execwire.Chain
		Payload, ProofInput []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-state-proofs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	r, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	var b stateproof.Bundle
	require.NoError(t, json.Unmarshal(f.ProofInput, &b))
	caller := &stateProofCallerProbe{t: t, bundle: b, hash: r.Ref.Hash}
	out, e := AcquireStateProofInput(t.Context(), evm.New("evm"), f.Chain, f.Payload, "borrowed-rpc", b.Selection, caller)
	require.NoError(t, e)
	require.Equal(t, 1, caller.calls, "one shared-client batch must fetch all selected proofs and code")
	require.False(t, bytes.Equal(f.Payload, out))
	_, e = PrepareWithRequirements(t.Context(), evm.New("evm"), f.Chain, out, Requirements{StateProofs: &b.Selection})
	require.NoError(t, e)
	caller.fail = true
	out, e = AcquireStateProofInput(t.Context(), evm.New("evm"), f.Chain, f.Payload, "borrowed-rpc", b.Selection, caller)
	require.Error(t, e)
	require.Nil(t, out)
	require.Equal(t, 2, caller.calls, "no partial completion, retry, height/latest or new client fallback")
}

func TestExecutionStateProofRejectsChangedPathsSelectionAndForeignProfile(t *testing.T) {
	var f struct {
		Chain               execwire.Chain
		Payload, ProofInput []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-state-proofs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	profile := evm.New("evm")
	var original stateproof.Bundle
	require.NoError(t, json.Unmarshal(f.ProofInput, &original))
	out, e := AttachStateProofInput(t.Context(), profile, f.Chain, f.Payload, "native-state", f.ProofInput)
	require.NoError(t, e)
	r, e := execwire.UnmarshalResult(out)
	require.NoError(t, e)
	for name, mutate := range map[string]func(*stateproof.Bundle){
		"missingEvidence": func(b *stateproof.Bundle) { b.Evidence = b.Evidence[:1] },
		"falseNonce":      func(b *stateproof.Bundle) { *b.Evidence[0].Proof.Nonce++ },
		"falseZero":       func(b *stateproof.Bundle) { b.Evidence[0].Proof.StorageProof[0].Value = new(hexutil.Big) },
		"missingPath":     func(b *stateproof.Bundle) { b.Evidence[0].Proof.AccountProof = nil },
		"wrongCode":       func(b *stateproof.Bundle) { code := hexutil.Bytes{0}; b.Evidence[0].Code = &code },
		"missingSlot":     func(b *stateproof.Bundle) { b.Evidence[0].Proof.StorageProof = b.Evidence[0].Proof.StorageProof[:1] },
	} {
		t.Run(name, func(t *testing.T) {
			var b stateproof.Bundle
			require.NoError(t, json.Unmarshal(f.ProofInput, &b))
			mutate(&b)
			bad, e := json.Marshal(b)
			require.NoError(t, e)
			_, e = AttachStateProofInput(t.Context(), profile, f.Chain, f.Payload, "native-state", bad)
			require.Error(t, e)
		})
	}
	wrong := original.Selection
	wrong.Targets = append([]stateproof.Target{}, wrong.Targets...)
	wrong.Targets[0].Code = false
	_, e = PrepareWithRequirements(t.Context(), profile, f.Chain, out, Requirements{StateProofs: &wrong})
	require.Error(t, e)
	r.StateProofBundle.Profile = "other"
	changed, e := execwire.MarshalResult(r)
	require.NoError(t, e)
	_, e = PrepareContext(t.Context(), profile, f.Chain, changed, false)
	require.Error(t, e)
	for _, bad := range [][]byte{[]byte(`{}`), bytes.Replace(f.ProofInput, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(f.ProofInput, []byte(`"nonce":`), []byte(`"unexpected":true,"nonce":`), 1)} {
		_, e = AttachStateProofInput(t.Context(), profile, f.Chain, f.Payload, "native-state", bad)
		require.Error(t, e)
	}
	_, e = AttachStateProofInput(t.Context(), profile, f.Chain, out, "native-state", f.ProofInput)
	require.ErrorIs(t, e, port.ErrExecutionConflict)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = AttachStateProofInput(ctx, profile, f.Chain, f.Payload, "native-state", f.ProofInput)
	require.ErrorIs(t, e, context.Canceled)
}
func TestExecutionStateProofFilesRetainPrivateImmutableInput(t *testing.T) {
	var f struct {
		Chain               execwire.Chain
		Payload, ProofInput []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-state-proofs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	input, proofs, output := filepath.Join(dir, "original.exr"), filepath.Join(dir, "proofs.json"), filepath.Join(dir, "complete.exr")
	require.NoError(t, os.WriteFile(input, f.Payload, 0600))
	require.NoError(t, os.WriteFile(proofs, f.ProofInput, 0600))
	info, e := AttachStateProofFiles(t.Context(), evm.New("evm"), f.Chain, input, proofs, output, "native-state")
	require.NoError(t, e)
	data, e := os.ReadFile(output)
	require.NoError(t, e)
	require.Equal(t, execwire.Hash(sha256.Sum256(data)), info.Digest)
	stat, e := os.Stat(output)
	require.NoError(t, e)
	require.Equal(t, os.FileMode(0600), stat.Mode().Perm())
	_, e = AttachStateProofFiles(t.Context(), evm.New("evm"), f.Chain, input, proofs, output, "native-state")
	require.ErrorIs(t, e, os.ErrExist)
	kept, e := os.ReadFile(input)
	require.NoError(t, e)
	require.True(t, bytes.Equal(kept, f.Payload))
	kept, e = os.ReadFile(proofs)
	require.NoError(t, e)
	require.True(t, bytes.Equal(kept, f.ProofInput))
	require.NoError(t, os.WriteFile(proofs, []byte(`{}`), 0600))
	_, e = AttachStateProofFiles(t.Context(), evm.New("evm"), f.Chain, input, proofs, filepath.Join(dir, "bad.exr"), "native-state")
	require.Error(t, e)
	_, e = os.Stat(filepath.Join(dir, "bad.exr"))
	require.True(t, os.IsNotExist(e))
}

func TestExecutionStateProofBorrowedSupplyValidatesMetadataBeforeIO(t *testing.T) {
	var f struct {
		Chain               execwire.Chain
		Payload, ProofInput []byte
	}
	raw, e := os.ReadFile("../../testdata/execution/evm-state-proofs.json")
	require.NoError(t, e)
	require.NoError(t, json.Unmarshal(raw, &f))
	var b stateproof.Bundle
	require.NoError(t, json.Unmarshal(f.ProofInput, &b))
	r, e := execwire.UnmarshalResult(f.Payload)
	require.NoError(t, e)
	caller := &stateProofCallerProbe{t: t, bundle: b, hash: r.Ref.Hash}
	_, e = AcquireStateProofInput(t.Context(), evm.New("evm"), f.Chain, f.Payload, "https://private", b.Selection, caller)
	require.Error(t, e)
	require.Zero(t, caller.calls, "invalid provenance must fail before shared-client IO")
}
