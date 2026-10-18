package evmtrace

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type traceDB interface {
	port.CaptureDatabase
	Close() error
}

func traceDatabase(t *testing.T, backend string) func() traceDB {
	t.Helper()
	if backend == "pebble" {
		path := t.TempDir()
		return func() traceDB {
			db, e := storage.NewPebbleStorage(storage.DefaultConfig(path))
			require.NoError(t, e)
			return db
		}
	}
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("requires actual fsync-on PostgreSQL")
	}
	var nonce [8]byte
	_, e := rand.Read(nonce[:])
	require.NoError(t, e)
	schema := "calltrace_" + hex.EncodeToString(nonce[:])
	t.Cleanup(func() {
		c, e := pgx.Connect(context.Background(), dsn)
		require.NoError(t, e)
		defer func() { require.NoError(t, c.Close(context.Background())) }()
		_, e = c.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, e)
	})
	return func() traceDB {
		db, e := postgres.Open(context.Background(), postgres.Options{DSN: dsn, Schema: schema, MaxConns: 1})
		require.NoError(t, e)
		return db
	}
}

type traceFresh struct {
	fixture traceFixture
	client  *rpc.Client
	wrong   atomic.Bool
	calls   atomic.Int32
}

func (f *traceFresh) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	f.calls.Add(1)
	if f.client != nil {
		var v struct {
			Hash common.Hash `json:"hash"`
		}
		e := f.client.CallContext(ctx, &v, "eth_getBlockByNumber", hexutil.EncodeUint64(n), false)
		return v.Hash, e
	}
	if n == 0 {
		return f.fixture.chain.Block(0).Block.Hash(), nil
	}
	if f.wrong.Load() {
		return common.Hash{99}, nil
	}
	return f.fixture.block.Hash, nil
}
func initializeTrace(t *testing.T, db port.CaptureDatabase, f traceFixture, o *traceFresh) (*capture.Store, model.CaptureIdentity, model.SupplementStreamDescriptor) {
	t.Helper()
	chain := model.FeedChain{ID: "1", GenesisHash: f.chain.Block(0).Block.Hash()}
	anchor := model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}
	store, e := capture.New(db, chain, o)
	require.NoError(t, e)
	plan := evmdata.BasePlan()
	digest, _ := plan.Digest()
	id := model.CaptureIdentity{Block: model.FeedBlock{Chain: chain, Number: 1, Hash: f.block.Hash}, Generation: 1, PlanVersion: plan.Version, PlanDigest: digest}
	require.NoError(t, store.Initialize(t.Context(), plan, anchor, nil))
	in := capture.Input{Manifest: model.CaptureManifest{Identity: id}, ParentHash: anchor.Hash}
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		value := [][]byte{f.header, f.body, f.encodedReceipts}[i]
		in.Payloads = append(in.Payloads, capture.Payload{Kind: k, Bytes: value})
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: k, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}, Status: model.DatasetPersisted, Digest: sha256.Sum256(value), Codec: evmdata.Codec(k), Source: "signed-independent-fixture", Verification: model.VerificationCommitment})
	}
	require.NoError(t, store.Commit(t.Context(), in))
	d, e := (Config{Name: "calls-v1", Provider: "fixture-public-pool", StartHeight: 1}).Descriptor(anchor, "evm")
	require.NoError(t, e)
	_, e = store.InitializeSupplementStream(t.Context(), d)
	require.NoError(t, e)
	return store, id, d
}

type traceRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}
type traceResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   any             `json:"error,omitempty"`
}
type traceWire struct {
	fixture    traceFixture
	mode       atomic.Int32
	mu         sync.Mutex
	times      []time.Time
	traceCalls atomic.Int32
}

func (w *traceWire) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	w.times = append(w.times, time.Now())
	w.mu.Unlock()
	if w.mode.CompareAndSwap(3, 0) {
		rw.WriteHeader(503)
		return
	}
	var raw json.RawMessage
	if json.NewDecoder(r.Body).Decode(&raw) != nil {
		rw.WriteHeader(400)
		return
	}
	var reqs []traceRequest
	batch := len(raw) > 0 && raw[0] == '['
	if batch {
		_ = json.Unmarshal(raw, &reqs)
	} else {
		var req traceRequest
		_ = json.Unmarshal(raw, &req)
		reqs = []traceRequest{req}
	}
	responses := []traceResponse{}
	for _, req := range reqs {
		response := traceResponse{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "debug_traceBlockByHash":
			w.traceCalls.Add(1)
			if len(req.Params) != 2 || string(req.Params[0]) != fmt.Sprintf("%q", w.fixture.block.Hash.Hex()) {
				rw.WriteHeader(400)
				return
			}
			var opts map[string]any
			if json.Unmarshal(req.Params[1], &opts) != nil || opts["tracer"] != "callTracer" || opts["timeout"] != "5s" || opts["reexec"] != float64(0) {
				rw.WriteHeader(400)
				return
			}
			flags, ok := opts["tracerConfig"].(map[string]any)
			if !ok || flags["onlyTopCall"] != false || flags["withLog"] != false || len(flags) != 2 {
				rw.WriteHeader(400)
				return
			}
			switch w.mode.Load() {
			case 1:
				response.Error = map[string]any{"code": -32601, "message": "tracer unavailable or pruned"}
			case 2:
				response.Result = json.RawMessage(`[]`)
			default:
				response.Result = json.RawMessage(w.fixture.raw)
			}
		case "eth_getBlockByNumber":
			hash := w.fixture.block.Hash
			if string(req.Params[0]) == `"0x0"` {
				hash = w.fixture.chain.Block(0).Block.Hash()
			}
			if w.mode.Load() == 4 {
				hash = common.Hash{99}
			}
			response.Result = map[string]any{"hash": hash}
		default:
			response.Error = map[string]any{"code": -32601, "message": "unexpected method"}
		}
		responses = append(responses, response)
	}
	rw.Header().Set("Content-Type", "application/json")
	if batch {
		_ = json.NewEncoder(rw).Encode(responses)
	} else {
		_ = json.NewEncoder(rw).Encode(responses[0])
	}
}
func traceClient(t *testing.T, w *traceWire) (*rpc.Client, *rpcpool.Pool) {
	t.Helper()
	primary := httptest.NewServer(w)
	fallback := httptest.NewServer(w)
	t.Cleanup(primary.Close)
	t.Cleanup(fallback.Close)
	p, e := rpcpool.New(rpcpool.Config{Endpoints: []string{primary.URL, fallback.URL}, RateLimit: 5, Timeout: 2 * time.Second})
	require.NoError(t, e)
	t.Cleanup(p.Close)
	c, e := rpc.DialOptions(t.Context(), p.Primary(), rpc.WithHTTPClient(&http.Client{Transport: p}))
	require.NoError(t, e)
	t.Cleanup(c.Close)
	return c, p
}
func TestCallTraceSupplyDurabilityFreshAndReadOnly(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := traceDatabase(t, backend)
			db := open()
			f := nativeFixture(t)
			fresh := &traceFresh{fixture: f}
			store, id, d := initializeTrace(t, db, f, fresh)
			wire := &traceWire{fixture: f}
			client, pool := traceClient(t, wire)
			fresh.client = client
			supplier, e := New(store, evm.New("evm"), client)
			require.NoError(t, e)
			wire.mode.Store(1)
			_, e = supplier.Acquire(t.Context(), 1, id, d)
			require.ErrorIs(t, e, ErrUnavailable)
			status, e := store.SupplementStreamStatus(t.Context(), d)
			require.NoError(t, e)
			require.Nil(t, status.Complete)
			require.Zero(t, status.Sequence)
			wire.mode.Store(2)
			_, e = supplier.Acquire(t.Context(), 1, id, d)
			require.ErrorIs(t, e, ErrTrace)
			wire.mode.Store(4)
			_, e = supplier.Acquire(t.Context(), 1, id, d)
			require.ErrorIs(t, e, capture.ErrCanonical)
			_, e = store.ReadSupplement(t.Context(), 1, id, Codec, d.SelectionHash)
			require.ErrorIs(t, e, port.ErrNotFound)
			wire.mode.Store(0)
			out, e := supplier.Acquire(t.Context(), 1, id, d)
			require.NoError(t, e)
			require.Len(t, out.Frames, 10)
			require.Equal(t, "provider_reported_root_consistency", out.Verification)
			count := wire.traceCalls.Load()
			wire.mode.Store(1)
			_, e = supplier.Acquire(t.Context(), 1, id, d)
			require.NoError(t, e)
			require.Equal(t, count, wire.traceCalls.Load(), "durable retry reuses bytes but keeps a fresh canonical request")
			status, e = store.SupplementStreamStatus(t.Context(), d)
			require.NoError(t, e)
			require.EqualValues(t, 1, status.Sequence)
			require.Zero(t, status.Backlog)
			reader, e := capture.NewReader(db, id.Block.Chain, nil)
			require.NoError(t, e)
			before := fresh.calls.Load()
			page, e := ReadPage(t.Context(), reader, evm.New("evm"), 1, id, d, 0, 3)
			require.NoError(t, e)
			require.Equal(t, 10, page.Total)
			require.EqualValues(t, 3, *page.Next)
			require.Len(t, page.Frames, 3)
			second, e := ReadPage(t.Context(), reader, evm.New("evm"), 1, id, d, *page.Next, 256)
			require.NoError(t, e)
			require.Len(t, second.Frames, 7)
			require.Nil(t, second.Next)
			require.Equal(t, before, fresh.calls.Load())
			require.Equal(t, count, wire.traceCalls.Load())
			_, e = ReadPage(t.Context(), reader, evm.New("evm"), 1, id, d, 11, 1)
			require.ErrorIs(t, e, port.ErrInvalidData)
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			store, e = capture.New(db, id.Block.Chain, fresh)
			require.NoError(t, e)
			_, e = store.InitializeSupplementStream(t.Context(), d)
			require.NoError(t, e)
			reader, e = capture.NewReader(db, id.Block.Chain, nil)
			require.NoError(t, e)
			recovered, e := ReadSnapshot(t.Context(), reader, evm.New("evm"), 1, id, d)
			require.NoError(t, e)
			require.Equal(t, out, recovered)
			require.Equal(t, count, wire.traceCalls.Load())
			state, e := store.State(t.Context())
			require.NoError(t, e)
			require.NoError(t, store.Reset(t.Context(), 1, state.Anchor))
			_, e = ReadSnapshot(t.Context(), reader, evm.New("evm"), 1, id, d)
			require.ErrorIs(t, e, capture.ErrStale)
			_, e = store.InitializeSupplementStream(t.Context(), d)
			require.NoError(t, e)
			status, e = store.SupplementStreamStatus(t.Context(), d)
			require.NoError(t, e)
			require.Nil(t, status.Complete)
			total := uint64(0)
			for _, ep := range pool.Status() {
				total += ep.Requests
			}
			wire.mu.Lock()
			require.EqualValues(t, total, len(wire.times))
			for i := 1; i < len(wire.times); i++ {
				require.GreaterOrEqual(t, wire.times[i].Sub(wire.times[i-1]), 175*time.Millisecond)
			}
			wire.mu.Unlock()
		})
	}
}
func TestCallTraceSharedLimitChargesFailover(t *testing.T) {
	db := traceDatabase(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	f := nativeFixture(t)
	fresh := &traceFresh{fixture: f}
	store, id, d := initializeTrace(t, db, f, fresh)
	w := &traceWire{fixture: f}
	w.mode.Store(3)
	client, p := traceClient(t, w)
	fresh.client = client
	s, e := New(store, evm.New("evm"), client)
	require.NoError(t, e)
	_, e = s.Acquire(t.Context(), 1, id, d)
	require.NoError(t, e)
	count := uint64(0)
	for _, ep := range p.Status() {
		count += ep.Requests
	}
	require.EqualValues(t, 3, count)
	w.mu.Lock()
	defer w.mu.Unlock()
	require.Len(t, w.times, 3)
	for i := 1; i < len(w.times); i++ {
		require.GreaterOrEqual(t, w.times[i].Sub(w.times[i-1]), 175*time.Millisecond)
	}
}

var errTraceIO = errors.New("trace driver I/O fixture")

type cancelTraceDB struct{ entered chan struct{} }

func (c cancelTraceDB) CaptureView(context.Context, model.FeedChain, func(port.CaptureReader) error) error {
	return errTraceIO
}
func (c cancelTraceDB) CaptureUpdate(ctx context.Context, _ model.FeedChain, _ func(port.CaptureWriter) error) error {
	close(c.entered)
	<-ctx.Done()
	return errTraceIO
}

type neverTraceRPC struct{}

func (neverTraceRPC) BatchCallContext(context.Context, []rpc.BatchElem) error { return errTraceIO }

type neverFresh struct{}

func (neverFresh) HashAt(context.Context, uint64) (common.Hash, error) {
	return common.Hash{}, source.ErrNotFound
}
func TestCallTraceRuntimeCancellationPreservesIO(t *testing.T) {
	chain := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	anchor := model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}
	db := cancelTraceDB{entered: make(chan struct{})}
	store, e := capture.New(db, chain, neverFresh{})
	require.NoError(t, e)
	supplier, e := New(store, evm.New("evm"), neverTraceRPC{})
	require.NoError(t, e)
	d, e := (Config{Name: "calls", Provider: "fixture", StartHeight: 1}).Descriptor(anchor, "evm")
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- supplier.Run(ctx, d, time.Millisecond*20) }()
	<-db.entered
	cancel()
	e = <-done
	require.ErrorIs(t, e, context.Canceled)
	require.ErrorIs(t, e, errTraceIO)
}

// Byte-level JSON checks precede any call-tree allocation.
func TestCallTraceBoundedRPCResponse(t *testing.T) {
	var r boundedResponse
	require.ErrorIs(t, r.UnmarshalJSON(bytes.Repeat([]byte(" "), MaxResponseBytes+1)), ErrTrace)
	require.Empty(t, r)
}
