package evmstate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func TestRequiredProofCheckpointTransitionAndGC(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t)
			db := openProofDB(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			sel, digest, e := f.selection.Normalize()
			require.NoError(t, e)
			raw, e := json.Marshal(sel)
			require.NoError(t, e)
			d := model.SupplementStreamDescriptor{Version: 1, Name: "required-trie", Codec: Codec, Selection: raw, SelectionHash: digest, FromHeight: 2, Required: true, ParentCheckpoint: true}
			store, parent, fresh := captured(t, db, f.root, func(s *capture.Store) {
				require.NoError(t, s.BindRequiredSupplement(t.Context(), &d))
				_, e := s.InitializeSupplementStream(t.Context(), d)
				require.NoError(t, e)
			})
			store = store.WithSupplementVerifier(Verifier(evm.New("evm")))
			client, wire := proofRPC(t, f, fresh)
			fresh.client = client
			worker, e := New(store, evm.New("evm"), client)
			require.NoError(t, e)
			cp, e := worker.AcquireCheckpoint(t.Context(), 1, d)
			require.NoError(t, e)
			require.Equal(t, parent.Block, cp.Block)
			require.EqualValues(t, 2, wire.calls.Load(), "parent proof batch and fresh; captured header adds no HTTP")
			h := &types.Header{ParentHash: parent.Block.Hash, UncleHash: types.EmptyUncleHash, Root: f.root, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: big.NewInt(2), GasLimit: 30_000_000, Time: 2}
			header, e := rlp.EncodeToBytes(h)
			require.NoError(t, e)
			child := parent
			child.Block.Number = 2
			child.Block.Hash = h.Hash()
			fresh.hash = child.Block.Hash
			require.NoError(t, commitProofBlock(t, store, child, h.ParentHash, header))
			_, e = worker.AcquireStream(t.Context(), 1, child, d)
			require.NoError(t, e)
			require.EqualValues(t, 5, wire.calls.Load(), "block commit fresh + proof batch + proof commit fresh")
			before, after, e := worker.ReadTransition(t.Context(), 1, d, 2)
			require.NoError(t, e)
			require.Equal(t, parent.Block, before.Block)
			require.Equal(t, child.Block, after.Block)
			require.Equal(t, before.Accounts, after.Accounts)
			require.Equal(t, "123456", (*big.Int)(before.Accounts[0].Balance).String())
			require.Equal(t, common.HexToHash("0x2a"), before.Accounts[0].Storage[0].Value)
			count := wire.calls.Load()
			wire.mode.Store(2)
			_, e = worker.AcquireCheckpoint(t.Context(), 1, d)
			require.NoError(t, e)
			_, _, e = ReadTransition(t.Context(), store, evm.New("evm"), 1, d, 2)
			require.NoError(t, e)
			require.Equal(t, count, wire.calls.Load())
			require.NoError(t, store.RequiredSupplementReady(t.Context(), 1, child.Block))
			require.NoError(t, store.Finalize(t.Context(), 1, child.Block))
			in, e := store.Read(t.Context(), child)
			require.NoError(t, e)
			p := in.Payloads[0]
			ref := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: child.Block, Kind: p.Kind, Codec: in.Manifest.Datasets[0].Codec, Digest: sha256.Sum256(p.Bytes), PayloadBytes: int64(len(p.Bytes))}
			called := 0
			remove := func() error { called++; return nil }
			require.NoError(t, store.WithDeletePermit(t.Context(), []window.Reference{ref}, remove))
			require.Equal(t, 1, called)
			// Forge valid payload SHA and its receipt together. The GC codec verifier
			// must still reject the altered account nonce against the authentic MPT.
			original, e := store.ReadSupplement(t.Context(), 1, child, Codec, d.SelectionHash)
			require.NoError(t, e)
			var altered Bundle
			require.NoError(t, json.Unmarshal(original.Data, &altered))
			(*altered.Evidence[0].Proof.Nonce)++
			forged, e := json.Marshal(altered)
			require.NoError(t, e)
			tampered := tamperRequiredDB{CaptureDatabase: db, digest: sha256.Sum256(forged)}
			bad, e := capture.New(tampered, child.Block.Chain, fresh)
			require.NoError(t, e)
			bad = bad.WithSupplementVerifier(Verifier(evm.New("evm")))
			require.ErrorIs(t, bad.WithDeletePermit(t.Context(), []window.Reference{ref}, remove), ErrProof)
			require.Equal(t, 1, called)
		})
	}
}
func commitProofBlock(t *testing.T, store *capture.Store, id model.CaptureIdentity, parent common.Hash, header []byte) error {
	t.Helper()
	in := capture.Input{Manifest: model.CaptureManifest{Identity: id}, ParentHash: parent}
	body, e := rlp.EncodeToBytes(&types.Body{})
	require.NoError(t, e)
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		b := [][]byte{header, body, {0xc0}}[i]
		in.Payloads = append(in.Payloads, capture.Payload{Kind: k, Bytes: b})
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: k, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}, Status: model.DatasetPersisted, Digest: sha256.Sum256(b), Codec: evmdata.Codec(k), Source: "trie-checkpoint-fixture", Verification: model.VerificationCommitment})
	}
	return store.Commit(t.Context(), in)
}

type tamperRequiredDB struct {
	port.CaptureDatabase
	digest common.Hash
}
type tamperRequiredWriter struct {
	port.CaptureWriter
	digest common.Hash
}

func (w *tamperRequiredWriter) Get(k []byte) ([]byte, error) {
	raw, e := w.CaptureWriter.Get(k)
	if e != nil {
		return nil, e
	}
	if strings.HasPrefix(string(k), "supplement/") {
		var in capture.Supplement
		if e := json.Unmarshal(raw, &in); e != nil {
			return nil, e
		}
		var bundle Bundle
		if e := json.Unmarshal(in.Data, &bundle); e != nil {
			return nil, e
		}
		(*bundle.Evidence[0].Proof.Nonce)++
		in.Data, e = json.Marshal(bundle)
		if e != nil {
			return nil, e
		}
		in.Digest = sha256.Sum256(in.Data)
		return json.Marshal(in)
	}
	if strings.Contains(string(k), "/input/") {
		var receipt struct {
			Identity model.CaptureIdentity `json:"identity"`
			Digest   common.Hash           `json:"digest"`
		}
		if e := json.Unmarshal(raw, &receipt); e != nil {
			return nil, e
		}
		receipt.Digest = w.digest
		return json.Marshal(receipt)
	}

	return raw, nil
}
func (d tamperRequiredDB) CaptureUpdate(ctx context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	return d.CaptureDatabase.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error { return fn(&tamperRequiredWriter{CaptureWriter: w, digest: d.digest}) })
}

// A driver is allowed to return a transport error when ctx cancellation wins
// an I/O race. The worker must report both causes, never discard either one.
func TestProofRuntimeCancellationPreservesIO(t *testing.T) {
	db := openProofDB(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	f := fixture(t)
	_, id, fresh := captured(t, db, f.root)
	sel, digest, e := f.selection.Normalize()
	require.NoError(t, e)
	raw, e := json.Marshal(sel)
	require.NoError(t, e)
	d := model.SupplementStreamDescriptor{Version: 1, Name: "cancel-io", Codec: Codec, Selection: raw, SelectionHash: digest, FromHeight: 1}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	broken, e := capture.New(cancelProofDB{CaptureDatabase: db, cancel: cancel}, id.Block.Chain, fresh)
	require.NoError(t, e)
	client, _ := proofRPC(t, f, fresh)
	s, e := New(broken, evm.New("evm"), client)
	require.NoError(t, e)
	e = s.Run(ctx, d, 0)
	require.ErrorIs(t, e, context.Canceled)
	require.ErrorIs(t, e, errProofIO)
}

var errProofIO = errors.New("injected driver transport failure during cancellation")

type cancelProofDB struct {
	port.CaptureDatabase
	cancel context.CancelFunc
}

func (d cancelProofDB) CaptureUpdate(context.Context, model.FeedChain, func(port.CaptureWriter) error) error {
	d.cancel()
	return errProofIO
}
