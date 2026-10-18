package evmstate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type proofDB interface {
	port.CaptureDatabase
	Close() error
}

func openProofDB(t *testing.T, backend string) func() proofDB {
	t.Helper()
	if backend == "pebble" {
		path := t.TempDir()
		return func() proofDB {
			db, e := storage.NewPebbleStorage(storage.DefaultConfig(path))
			require.NoError(t, e)
			return db
		}
	}
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("actual PostgreSQL requires INDEXER_TEST_POSTGRES")
	}
	var nonce [8]byte
	_, e := rand.Read(nonce[:])
	require.NoError(t, e)
	schema := "stateproof_" + hex.EncodeToString(nonce[:])
	t.Cleanup(func() {
		c, e := pgx.Connect(context.Background(), dsn)
		require.NoError(t, e)
		defer func() { require.NoError(t, c.Close(context.Background())) }()
		_, e = c.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, e)
	})
	return func() proofDB {
		db, e := postgres.Open(context.Background(), postgres.Options{DSN: dsn, Schema: schema, MaxConns: 1})
		require.NoError(t, e)
		return db
	}
}

type freshOracle struct {
	genesis, hash common.Hash
	client        *rpc.Client
	wrong         atomic.Bool
}

func (f *freshOracle) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	if f.client != nil {
		var h struct {
			Hash common.Hash `json:"hash"`
		}
		e := f.client.CallContext(ctx, &h, "eth_getBlockByNumber", hexutil.EncodeUint64(n), false)
		return h.Hash, e
	}
	if n == 0 {
		return f.genesis, nil
	}
	if f.wrong.Load() {
		return common.Hash{99}, nil
	}
	return f.hash, nil
}
func captured(t *testing.T, db proofDB, root common.Hash, before ...func(*capture.Store)) (*capture.Store, model.CaptureIdentity, *freshOracle) {
	t.Helper()
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	h := &types.Header{ParentHash: ch.GenesisHash, UncleHash: types.EmptyUncleHash, Root: root, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: big.NewInt(1), GasLimit: 30_000_000, Time: 1}
	header, e := rlp.EncodeToBytes(h)
	require.NoError(t, e)
	body, e := rlp.EncodeToBytes(&types.Body{})
	require.NoError(t, e)
	p := evmdata.BasePlan()
	digest, e := p.Digest()
	require.NoError(t, e)
	id := model.CaptureIdentity{Block: model.FeedBlock{Chain: ch, Number: 1, Hash: h.Hash()}, Generation: 1, PlanVersion: p.Version, PlanDigest: digest}
	f := &freshOracle{genesis: ch.GenesisHash, hash: id.Block.Hash}
	s, e := capture.New(db, ch, f)
	require.NoError(t, e)
	require.NoError(t, s.Initialize(t.Context(), p, model.FeedBlock{Chain: ch, Hash: ch.GenesisHash}, nil))
	for _, hook := range before {
		hook(s)
	}
	in := capture.Input{Manifest: model.CaptureManifest{Identity: id}, ParentHash: ch.GenesisHash}
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		b := [][]byte{header, body, {0xc0}}[i]
		in.Payloads = append(in.Payloads, capture.Payload{Kind: k, Bytes: b})
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: k, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}, Status: model.DatasetPersisted, Digest: sha256.Sum256(b), Codec: evmdata.Codec(k), Source: "independent-trie-fixture", Verification: model.VerificationCommitment})
	}
	require.NoError(t, s.Commit(t.Context(), in))
	return s, id, f
}

type rpcRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
}
type wireOracle struct {
	mode    atomic.Int32
	calls   atomic.Int64
	members atomic.Int64
	mu      sync.Mutex
	times   []time.Time
}

// JSON-RPC is a separate oracle: request selector, slot order, batch IDs and
// response order are checked here, not inferred from the supplier's output.
func proofRPC(t *testing.T, f proofFixture, fresh *freshOracle) (*rpc.Client, *wireOracle) {
	t.Helper()
	w := &wireOracle{}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.calls.Add(1)
		w.mu.Lock()
		w.times = append(w.times, time.Now())
		w.mu.Unlock()
		var raw json.RawMessage
		if e := json.NewDecoder(r.Body).Decode(&raw); e != nil {
			t.Error(e)
			rw.WriteHeader(400)
			return
		}
		batch := len(raw) > 0 && raw[0] == '['
		var reqs []rpcRequest
		if batch {
			require.NoError(t, json.Unmarshal(raw, &reqs))
		} else {
			var req rpcRequest
			require.NoError(t, json.Unmarshal(raw, &req))
			reqs = []rpcRequest{req}
		}
		responses := []rpcResponse{}
		for _, req := range reqs {
			resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
			if req.Method == "eth_getBlockByNumber" {
				var number string
				require.NoError(t, json.Unmarshal(req.Params[0], &number))
				h := fresh.hash
				if number == "0x0" {
					h = fresh.genesis
				}
				if fresh.wrong.Load() {
					h = common.Hash{99}
				}
				resp.Result = map[string]interface{}{"hash": h}
			} else {
				w.members.Add(1)
				require.True(t, batch, "state members must share a batch")
				var a common.Address
				require.NoError(t, json.Unmarshal(req.Params[0], &a))
				var selector struct {
					BlockHash common.Hash `json:"blockHash"`
					Canonical bool        `json:"requireCanonical"`
				}
				require.NoError(t, json.Unmarshal(req.Params[len(req.Params)-1], &selector))
				require.Equal(t, fresh.hash, selector.BlockHash)
				require.True(t, selector.Canonical)
				i := 0
				if a == f.absent {
					i = 1
				} else {
					require.Equal(t, f.account, a)
				}
				switch req.Method {
				case "eth_getProof":
					var slots []common.Hash
					require.NoError(t, json.Unmarshal(req.Params[1], &slots))
					require.Equal(t, f.selection.Targets[i].Slots, slots)
					p := f.bundle.Evidence[i].Proof
					if w.mode.Load() == 1 {
						p.Balance = new(hexutil.Big)
					}
					resp.Result = p
				case "eth_getCode":
					resp.Result = *f.bundle.Evidence[i].Code
				default:
					t.Errorf("unexpected method %s", req.Method)
				}
				if w.mode.Load() == 2 && req.Method == "eth_getProof" {
					resp.Result = nil
					resp.Error = map[string]interface{}{"code": -32000, "message": "missing trie node: pruned state"}
				}
				if w.mode.Load() == 3 && req.Method == "eth_getCode" {
					resp.Result = hexutil.Bytes{0xff}
				}
				if w.mode.Load() == 4 {
					continue
				} // omitted members cannot become empty proof/code
				if w.mode.Load() == 5 && req.Method == "eth_getProof" {
					resp.Result = map[string]interface{}{}
				}
			}
			responses = append([]rpcResponse{resp}, responses...) // deliberately reversed IDs
		}
		rw.Header().Set("Content-Type", "application/json")
		if batch {
			require.NoError(t, json.NewEncoder(rw).Encode(responses))
		} else {
			require.NoError(t, json.NewEncoder(rw).Encode(responses[0]))
		}
	}))
	t.Cleanup(srv.Close)
	pool, e := rpcpool.New(rpcpool.Config{Endpoints: []string{srv.URL}, RateLimit: 5, Burst: 1, Timeout: 3 * time.Second, CoalesceReads: true})
	require.NoError(t, e)
	t.Cleanup(pool.Close)
	c, e := rpc.DialOptions(t.Context(), srv.URL, rpc.WithHTTPClient(&http.Client{Transport: pool}))
	require.NoError(t, e)
	t.Cleanup(c.Close)
	return c, w
}

func TestSupplierBatchPersistenceAndFresh(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t)
			open := openProofDB(t, backend)
			db := open()
			s, id, fresh := captured(t, db, f.root)
			c, wire := proofRPC(t, f, fresh)
			fresh.client = c
			supplier, e := New(s, evm.New("evm"), c)
			require.NoError(t, e)
			_, e = supplier.Read(t.Context(), 1, id, f.selection)
			require.ErrorIs(t, e, port.ErrNotFound)
			out, e := supplier.Acquire(t.Context(), 1, id, f.selection)
			require.NoError(t, e)
			require.EqualValues(t, 2, wire.calls.Load(), "one state batch plus separate fresh canonical read")
			require.EqualValues(t, 4, wire.members.Load())
			metrics := supplier.Metrics()
			require.EqualValues(t, 1, metrics.RPCBatch.Count)
			require.EqualValues(t, 1, metrics.Verify.Count)
			require.EqualValues(t, 1, metrics.Commit.Count)
			require.Zero(t, metrics.Commit.Failures)
			require.EqualValues(t, 42, out.Accounts[0].Storage[0].Value.Big().Uint64())
			got, e := ReadSnapshot(t.Context(), s, evm.New("evm"), 1, id, f.selection)
			require.NoError(t, e)
			require.Equal(t, out, got)
			require.EqualValues(t, 2, wire.calls.Load())
			wire.mode.Store(2) // a committed retry needs no state RPC even after pruning
			_, e = supplier.Acquire(t.Context(), 1, id, f.selection)
			require.NoError(t, e)
			require.EqualValues(t, 3, wire.calls.Load())
			require.EqualValues(t, 4, wire.members.Load())
			fresh.wrong.Store(true)
			_, e = supplier.Acquire(t.Context(), 1, id, f.selection)
			require.ErrorIs(t, e, capture.ErrCanonical)
			fresh.wrong.Store(false)
			require.EqualValues(t, 1, supplier.Metrics().Commit.Failures)
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			s, e = capture.New(db, id.Block.Chain, fresh)
			require.NoError(t, e)
			before := wire.calls.Load()
			got, e = ReadSnapshot(t.Context(), s, evm.New("evm"), 1, id, f.selection)
			require.NoError(t, e)
			require.Equal(t, out, got)
			require.Equal(t, before, wire.calls.Load())
			for i := 0; i < 2; i++ {
				_, e = fresh.HashAt(t.Context(), 1)
				require.NoError(t, e)
			}
			wire.mu.Lock()
			times := append([]time.Time{}, wire.times...)
			wire.mu.Unlock()
			require.GreaterOrEqual(t, times[5].Sub(times[0]), 950*time.Millisecond, "state and canonical calls share the same 5/s physical budget")
			require.NoError(t, s.Reset(t.Context(), 1, id.Block))
			_, e = ReadSnapshot(t.Context(), s, evm.New("evm"), 1, id, f.selection)
			require.ErrorIs(t, e, capture.ErrStale)
			got, e = ReadSnapshot(t.Context(), s, evm.New("evm"), 2, id, f.selection)
			require.NoError(t, e)
			require.EqualValues(t, 2, got.Epoch)
			st, e := s.State(t.Context())
			require.NoError(t, e)
			require.NoError(t, s.Reset(t.Context(), 2, st.Anchor))
			_, e = ReadSnapshot(t.Context(), s, evm.New("evm"), 3, id, f.selection)
			require.Error(t, e)
		})
	}
}
func TestSupplierRejectsUnavailableAndFalseState(t *testing.T) {
	for mode := int32(1); mode <= 5; mode++ {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := fixture(t)
			db := openProofDB(t, "pebble")()
			defer func() { require.NoError(t, db.Close()) }()
			s, id, fresh := captured(t, db, f.root)
			c, wire := proofRPC(t, f, fresh)
			wire.mode.Store(mode)
			fresh.client = c
			supplier, e := New(s, evm.New("evm"), c)
			require.NoError(t, e)
			_, e = supplier.Acquire(t.Context(), 1, id, f.selection)
			require.Error(t, e)
			if mode == 1 || mode == 3 {
				require.ErrorIs(t, e, ErrProof)
			} else {
				require.ErrorIs(t, e, ErrUnavailable)
			}
			_, _, e = f.selection.Normalize()
			require.NoError(t, e)
			_, e = supplier.Read(t.Context(), 1, id, f.selection)
			require.ErrorIs(t, e, port.ErrNotFound)
			require.EqualValues(t, 1, wire.calls.Load(), "failed state batch must neither commit nor fresh-check a nonexistent record")
		})
	}
}

func TestReadReverifiesEvenHashBoundCorruption(t *testing.T) {
	f := fixture(t)
	db := openProofDB(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s, id, _ := captured(t, db, f.root)
	// The generic capture layer asserts bytes/owner, not MPT semantics. A
	// forged RPC balance with an otherwise valid digest must fail on read.
	f.bundle.Evidence[0].Proof.Balance = new(hexutil.Big)
	raw, e := json.Marshal(f.bundle)
	require.NoError(t, e)
	_, digest, e := f.selection.Normalize()
	require.NoError(t, e)
	require.NoError(t, s.CommitSupplement(t.Context(), 1, capture.Supplement{Identity: id, Codec: Codec, Selection: digest, Digest: sha256.Sum256(raw), Data: raw}))
	_, e = ReadSnapshot(t.Context(), s, evm.New("evm"), 1, id, f.selection)
	require.ErrorIs(t, e, ErrProof)
}

func TestAccountWithoutRequestedCode(t *testing.T) {
	f := fixture(t)
	f.selection.Targets = f.selection.Targets[:1]
	f.selection.Targets[0].Code = false
	db := openProofDB(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s, id, fresh := captured(t, db, f.root)
	c, wire := proofRPC(t, f, fresh)
	fresh.client = c
	supplier, e := New(s, evm.New("evm"), c)
	require.NoError(t, e)
	out, e := supplier.Acquire(t.Context(), 1, id, f.selection)
	require.NoError(t, e)
	require.Len(t, out.Accounts, 1)
	require.Nil(t, out.Accounts[0].Code, "unrequested code is not represented as proven empty code")
	require.NotEqual(t, types.EmptyCodeHash, out.Accounts[0].CodeHash)
	require.EqualValues(t, 1, wire.members.Load())
	f.selection.Targets[0].Code = true
	_, e = supplier.Read(t.Context(), 1, id, f.selection)
	require.ErrorIs(t, e, port.ErrNotFound)
}
