package evmstate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func projectionFixture(t *testing.T, balance, nonce, slot uint64, exists bool) proofFixture {
	t.Helper()
	f := fixture(t)
	st, storage := trie.NewEmpty(nil), trie.NewEmpty(nil)
	code := hexutil.Bytes{0x60, byte(nonce), 0x00}
	codeHash := crypto.Keccak256Hash(code)
	if exists {
		v, e := rlp.EncodeToBytes(new(big.Int).SetUint64(slot).Bytes())
		require.NoError(t, e)
		if slot > 0 {
			require.NoError(t, storage.Update(crypto.Keccak256(f.slot[:]), v))
		}
		root := storage.Hash()
		a := types.StateAccount{Nonce: nonce, Balance: uint256.NewInt(balance), Root: root, CodeHash: codeHash[:]}
		v, e = rlp.EncodeToBytes(&a)
		require.NoError(t, e)
		require.NoError(t, st.Update(crypto.Keccak256(f.account[:]), v))
	}
	f.root = st.Hash()
	f.state = st
	f.bundle = Bundle{Version: 1, Selection: f.selection, Evidence: make([]Evidence, len(f.selection.Targets))}
	for i, target := range f.selection.Targets {
		p := Proof{Address: ptr(target.Address), Nonce: ptr(hexutil.Uint64(0)), Balance: new(hexutil.Big), CodeHash: ptr(types.EmptyCodeHash), StorageHash: ptr(types.EmptyRootHash), AccountProof: proofNodes(t, st, crypto.Keccak256(target.Address[:])), StorageProof: []StorageProof{}}
		body := hexutil.Bytes{}
		if i == 0 && exists {
			p.Nonce = ptr(hexutil.Uint64(nonce))
			p.Balance = (*hexutil.Big)(new(big.Int).SetUint64(balance))
			p.CodeHash = ptr(codeHash)
			p.StorageHash = ptr(storage.Hash())
			body = code
		}
		for _, key := range target.Slots {
			value := new(hexutil.Big)
			nodes := Nodes{}
			if i == 0 && exists {
				nodes = proofNodes(t, storage, crypto.Keccak256(key[:]))
				if key == f.slot {
					value = (*hexutil.Big)(new(big.Int).SetUint64(slot))
				}
			}
			p.StorageProof = append(p.StorageProof, StorageProof{Key: key.Hex(), Value: value, Proof: nodes})
		}
		f.bundle.Evidence[i] = Evidence{Proof: p, Code: &body}
	}
	return f
}

type projectionFresh struct {
	mu     sync.Mutex
	hashes map[uint64]common.Hash
	calls  int
}

func (f *projectionFresh) HashAt(_ context.Context, n uint64) (common.Hash, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.hashes[n], nil
}
func fixtureBundle(t *testing.T, f proofFixture, header []byte) []byte {
	t.Helper()
	b := cloneBundle(t, f.bundle)
	b.Header = header
	raw, e := json.Marshal(b)
	require.NoError(t, e)
	return raw
}
func TestStateProjectionVerifiedChangesAndFork(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openProofDB(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			f := fixture(t)
			_, parent, _ := captured(t, db, f.root)
			fresh := &projectionFresh{hashes: map[uint64]common.Hash{0: parent.Block.Chain.GenesisHash, 1: parent.Block.Hash}}
			store, e := capture.New(db, parent.Block.Chain, fresh)
			require.NoError(t, e)
			store = store.WithSupplementVerifier(Verifier(evm.New("evm")))
			sel, hash, e := f.selection.Normalize()
			require.NoError(t, e)
			raw, e := json.Marshal(sel)
			require.NoError(t, e)
			stream := model.SupplementStreamDescriptor{Version: 1, Name: "changes-fixture", Codec: Codec, SelectionHash: hash, Selection: raw, FromHeight: 2, ParentCheckpoint: true}
			d := ProjectionDescriptor(stream)
			_, e = store.InitializeSupplementStream(t.Context(), stream)
			require.NoError(t, e)
			_, e = store.InitializeStateProjection(t.Context(), d)
			require.NoError(t, e)
			in, e := store.Read(t.Context(), parent)
			require.NoError(t, e)
			var parentHeader []byte
			for _, p := range in.Payloads {
				if p.Kind == model.DatasetHeaders {
					parentHeader = p.Bytes
				}
			}
			data := fixtureBundle(t, f, parentHeader)
			require.NoError(t, store.CommitSupplementCheckpoint(t.Context(), 1, stream, capture.SupplementCheckpoint{Block: parent.Block, Data: data, Digest: sha256.Sum256(data)}))
			apply := func(height, epoch, balance, nonce, slot uint64, exists bool, parentHash common.Hash) model.CaptureIdentity {
				next := projectionFixture(t, balance, nonce, slot, exists)
				h := &types.Header{ParentHash: parentHash, UncleHash: types.EmptyUncleHash, Root: next.root, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: new(big.Int).SetUint64(height), GasLimit: 30_000_000, Time: height}
				encoded, e := rlp.EncodeToBytes(h)
				require.NoError(t, e)
				id := parent
				id.Generation = epoch
				id.Block.Number = height
				id.Block.Hash = h.Hash()
				fresh.hashes[height] = id.Block.Hash
				require.NoError(t, commitProofBlock(t, store, id, parentHash, encoded))
				data := fixtureBundle(t, next, encoded)
				supp := capture.Supplement{Identity: id, Codec: Codec, Selection: hash, Data: data, Digest: sha256.Sum256(data)}
				require.NoError(t, store.CommitStreamSupplement(t.Context(), epoch, stream, supp))
				count := fresh.calls
				require.NoError(t, store.ApplyStateProjection(t.Context(), epoch, d, id, Projector(evm.New("evm"))))
				require.Equal(t, count+1, fresh.calls, "pure projector adds only commit fresh, no state RPC")
				return id
			}
			updated := apply(2, 1, 999999, 8, 99, true, parent.Block.Hash)
			out, _, e := capture.ReadStateProjectionOutput(t.Context(), db, parent.Block.Chain, d, 2)
			require.NoError(t, e)
			var changes Changes
			require.NoError(t, json.Unmarshal(out.Changes, &changes))
			require.Len(t, changes.Accounts, 1)
			a := changes.Accounts[0]
			require.Equal(t, "123456", a.Before.Balance.ToInt().String())
			require.Equal(t, "999999", a.After.Balance.ToInt().String())
			require.EqualValues(t, 7, a.Before.Nonce)
			require.EqualValues(t, 8, a.After.Nonce)
			require.Equal(t, common.HexToHash("0x2a"), a.Before.Storage[0].Value)
			require.Equal(t, common.HexToHash("0x63"), a.After.Storage[0].Value)
			require.NotEqual(t, a.Before.CodeHash, a.After.CodeHash)
			deleted := apply(3, 1, 0, 0, 0, false, updated.Block.Hash)
			created := apply(4, 1, 300, 1, 0, true, deleted.Block.Hash)
			head, _, e := capture.ReadStateProjectionHead(t.Context(), db, parent.Block.Chain, d)
			require.NoError(t, e)
			var values Values
			require.NoError(t, json.Unmarshal(head.Values, &values))
			require.True(t, values.Accounts[0].Exists)
			require.Equal(t, "300", values.Accounts[0].Balance.ToInt().String())
			require.Equal(t, created, *head.Input)
			require.NoError(t, store.Reset(t.Context(), 1, updated.Block))
			_, e = store.InitializeSupplementStream(t.Context(), stream)
			require.NoError(t, e)
			cursor, e := store.InitializeStateProjection(t.Context(), d)
			require.NoError(t, e)
			require.Equal(t, updated, *cursor.Complete)
			head, epoch, e := capture.ReadStateProjectionHead(t.Context(), db, parent.Block.Chain, d)
			require.NoError(t, e)
			require.EqualValues(t, 2, epoch)
			require.NoError(t, json.Unmarshal(head.Values, &values))
			require.Equal(t, "999999", values.Accounts[0].Balance.ToInt().String())
			branch := apply(3, 2, 700, 10, 3, true, updated.Block.Hash)
			head, _, e = capture.ReadStateProjectionHead(t.Context(), db, parent.Block.Chain, d)
			require.NoError(t, e)
			require.Equal(t, branch, *head.Input)
			// Claimed proof completion with missing bytes is corruption, not ordinary lag.
			bad, e := capture.New(missingProjectionProofDB{db}, parent.Block.Chain, fresh)
			require.NoError(t, e)
			check, cancel := context.WithTimeout(t.Context(), time.Second)
			e = RunProjection(check, bad, evm.New("evm"), d, 20*time.Millisecond)
			cancel()
			require.ErrorIs(t, e, port.ErrNotFound)
			historical, e := capture.ReadStateProjectionInput(t.Context(), db, parent.Block.Chain, d, created)
			require.NoError(t, e)
			require.Equal(t, created, historical.Identity)
			events, e := capture.ReadStateProjectionEvents(t.Context(), db, parent.Block.Chain, d, 0, 20)
			require.NoError(t, e)
			require.Len(t, events, 7)
			require.Equal(t, "revert", events[3].Kind)
			require.Equal(t, created, *events[3].Input)
			require.Equal(t, "revert", events[4].Kind)
			require.Equal(t, deleted, *events[4].Input)
			require.Equal(t, "reset", events[5].Kind)
		})
	}
}

type missingProjectionProofDB struct{ port.CaptureDatabase }
type missingProjectionProofWriter struct{ port.CaptureWriter }

func (w missingProjectionProofWriter) Get(k []byte) ([]byte, error) {
	if strings.HasPrefix(string(k), "supplement/") {
		return nil, port.ErrNotFound
	}
	return w.CaptureWriter.Get(k)
}
func (d missingProjectionProofDB) CaptureUpdate(ctx context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	return d.CaptureDatabase.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error { return fn(missingProjectionProofWriter{w}) })
}
func TestStateProjectionCancellationPreservesIO(t *testing.T) {
	db := openProofDB(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	f := fixture(t)
	_, id, fresh := captured(t, db, f.root)
	sel, digest, e := f.selection.Normalize()
	require.NoError(t, e)
	raw, e := json.Marshal(sel)
	require.NoError(t, e)
	stream := model.SupplementStreamDescriptor{Version: 1, Name: "projection-cancel", Codec: Codec, Selection: raw, SelectionHash: digest, FromHeight: 1, ParentCheckpoint: true}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store, e := capture.New(cancelProofDB{CaptureDatabase: db, cancel: cancel}, id.Block.Chain, fresh)
	require.NoError(t, e)
	e = RunProjection(ctx, store, evm.New("evm"), ProjectionDescriptor(stream), 0)
	require.ErrorIs(t, e, context.Canceled)
	require.ErrorIs(t, e, errProofIO)
}

// A reset can commit after initialization rolls back its fresh check, before
// the worker starts waiting. The wait must retain the failed attempt's epoch.
func TestStateProjectionCanonicalFailureDoesNotMissEpoch(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openProofDB(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			f := fixture(t)
			_, id, _ := captured(t, db, f.root)
			fresh := &projectionFresh{hashes: map[uint64]common.Hash{0: id.Block.Chain.GenesisHash, 1: id.Block.Hash}}
			base, e := capture.New(db, id.Block.Chain, fresh)
			require.NoError(t, e)
			sel, digest, e := f.selection.Normalize()
			require.NoError(t, e)
			raw, e := json.Marshal(sel)
			require.NoError(t, e)
			d := ProjectionDescriptor(model.SupplementStreamDescriptor{Version: 1, Name: "epoch-race", Codec: Codec, Selection: raw, SelectionHash: digest, FromHeight: 1, ParentCheckpoint: true})
			_, e = base.InitializeStateProjection(t.Context(), d)
			require.NoError(t, e)
			require.NoError(t, base.Reset(t.Context(), 1, id.Block))
			// Force a real failed fresh check in generation 2. The DB wrapper
			// advances to generation 3 only AFTER that transaction rolls back.
			fresh.hashes[0] = common.Hash{99}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			failed, retried := false, false
			wrapped := projectionEpochDB{CaptureDatabase: db, after: func(err error) {
				if !failed {
					require.ErrorIs(t, err, capture.ErrCanonical)
					failed = true
					fresh.hashes[0] = id.Block.Chain.GenesisHash
					require.NoError(t, base.Reset(ctx, 2, id.Block))
					return
				}
				require.NoError(t, err)
				retried = true
				cancel()
			}}
			store, e := capture.New(wrapped, id.Block.Chain, fresh)
			require.NoError(t, e)
			e = RunProjection(ctx, store, evm.New("evm"), d, 20*time.Millisecond)
			require.ErrorIs(t, e, ctx.Err())
			require.True(t, failed, "must exercise a rolled-back fresh check")
			require.True(t, retried, "must not wait for a fourth generation after already observing the third")
			cursor, e := base.InitializeStateProjection(t.Context(), d)
			require.NoError(t, e)
			require.EqualValues(t, 3, cursor.Epoch)
			require.EqualValues(t, 1, cursor.Sequence, "only the successful reconciliation may emit a reset")
		})
	}
}

type projectionEpochDB struct {
	port.CaptureDatabase
	after func(error)
}

func (d projectionEpochDB) CaptureUpdate(ctx context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	err := d.CaptureDatabase.CaptureUpdate(ctx, ch, fn)
	d.after(err)
	return err
}
