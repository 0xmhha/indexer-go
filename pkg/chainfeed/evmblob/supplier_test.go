package evmblob

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type blobDB interface {
	port.CaptureDatabase
	Close() error
}

func database(t *testing.T, backend string) func() blobDB {
	t.Helper()
	if backend == "pebble" {
		path := t.TempDir()
		return func() blobDB {
			db, e := storage.NewPebbleStorage(storage.DefaultConfig(path))
			require.NoError(t, e)
			return db
		}
	}
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("requires fsync-on PostgreSQL")
	}
	var nonce [8]byte
	_, e := rand.Read(nonce[:])
	require.NoError(t, e)
	schema := "blob_" + hex.EncodeToString(nonce[:])
	t.Cleanup(func() {
		c, e := pgx.Connect(context.Background(), dsn)
		require.NoError(t, e)
		defer func() { require.NoError(t, c.Close(context.Background())) }()
		_, e = c.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, e)
	})
	return func() blobDB {
		db, e := postgres.Open(context.Background(), postgres.Options{DSN: dsn, Schema: schema, MaxConns: 1})
		require.NoError(t, e)
		return db
	}
}

type oracle struct {
	block  model.FeedBlock
	wrong  atomic.Bool
	calls  atomic.Int32
	client *rpc.Client
}

func (o *oracle) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	o.calls.Add(1)
	if o.client != nil {
		var block struct {
			Hash common.Hash `json:"hash"`
		}
		e := o.client.CallContext(ctx, &block, "eth_getBlockByNumber", fmt.Sprintf("0x%x", n), false)
		return block.Hash, e
	}
	if ctx.Err() != nil {
		return common.Hash{}, ctx.Err()
	}
	if n == 0 {
		return o.block.Chain.GenesisHash, nil
	}
	if o.wrong.Load() {
		return common.Hash{99}, nil
	}
	return o.block.Hash, nil
}
func commitFixture(t *testing.T, s *capture.Store, f fixture) {
	t.Helper()
	in := capture.Input{Manifest: model.CaptureManifest{Identity: f.id}, ParentHash: f.block.ParentHash}
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		raw := [][]byte{f.header, f.body, f.receipts}[i]
		in.Payloads = append(in.Payloads, capture.Payload{Kind: k, Bytes: raw})
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: k, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}, Status: model.DatasetPersisted, Digest: sha256.Sum256(raw), Codec: evmdata.Codec(k), Source: "signed-independent-constant-polynomial", Verification: model.VerificationCommitment})
	}
	require.NoError(t, s.Commit(t.Context(), in))
}
func initialize(t *testing.T, db port.CaptureDatabase, f fixture, o *oracle) (*capture.Store, model.SupplementStreamDescriptor) {
	t.Helper()
	s, e := capture.New(db, f.id.Block.Chain, o)
	require.NoError(t, e)
	anchor := model.FeedBlock{Chain: f.id.Block.Chain, Hash: f.id.Block.Chain.GenesisHash}
	require.NoError(t, s.Initialize(t.Context(), evmdata.BasePlan(), anchor, nil))
	commitFixture(t, s, f)
	d, e := Descriptor("full-blobs-v1", 1, Definition{Provider: "local-relay-v1", Profile: "evm", Format: Format})
	require.NoError(t, e)
	_, e = s.InitializeSupplementStream(t.Context(), d)
	require.NoError(t, e)
	return s, d
}

type provider struct {
	raw   []byte
	err   error
	calls atomic.Int32
}

func (p *provider) Open(_ context.Context, _ model.FeedBlock, _ []Reference) (io.ReadCloser, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	return io.NopCloser(bytes.NewReader(p.raw)), nil
}

var errIO = errors.New("blob provider injected I/O failure")

func TestBlobSupplyDurabilityFreshAndReadOnly(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := database(t, backend)
			db := open()
			defer func() {
				if db != nil {
					require.NoError(t, db.Close())
				}
			}()
			f := blockFixture(t, []Entry{vector(false), vector(true)})
			o := &oracle{block: f.id.Block}
			s, d := initialize(t, db, f, o)
			p := &provider{raw: f.raw}
			supplier, e := New(s, evm.New("evm"), p)
			require.NoError(t, e)
			// Neither transport failure nor cryptographic failure records completion.
			p.err = errIO
			_, e = supplier.Acquire(t.Context(), 1, f.id, d)
			require.ErrorIs(t, e, ErrUnavailable)
			require.ErrorIs(t, e, errIO)
			p.err = nil
			bad := f.response
			bad.Entries = append([]Entry{}, bad.Entries...)
			bad.Entries[0].Blob[31] = 1
			p.raw, e = rlp.EncodeToBytes(bad)
			require.NoError(t, e)
			_, e = supplier.Acquire(t.Context(), 1, f.id, d)
			require.ErrorIs(t, e, ErrBlob)
			status, e := s.SupplementStreamStatus(t.Context(), d)
			require.NoError(t, e)
			require.Nil(t, status.Complete)
			require.Zero(t, status.Sequence)
			p.raw = f.raw
			o.wrong.Store(true)
			_, e = supplier.Acquire(t.Context(), 1, f.id, d)
			require.ErrorIs(t, e, capture.ErrCanonical)
			_, e = s.ReadSupplement(t.Context(), 1, f.id, Codec, d.SelectionHash)
			require.ErrorIs(t, e, port.ErrNotFound)
			o.wrong.Store(false)
			before := o.calls.Load()
			got, e := supplier.Acquire(t.Context(), 1, f.id, d)
			require.NoError(t, e)
			require.EqualValues(t, before+1, o.calls.Load())
			require.Len(t, got.References, 2)
			base, e := s.State(t.Context())
			require.NoError(t, e)
			require.EqualValues(t, 1, base.Sequence)
			require.Equal(t, evmdata.BasePlan(), base.Plan)
			reader, e := capture.NewReader(db, f.id.Block.Chain, nil)
			require.NoError(t, e)
			before = o.calls.Load()
			page, e := ReadPage(t.Context(), reader, evm.New("evm"), 1, f.id, d, 0, 1)
			require.NoError(t, e)
			require.EqualValues(t, 1, *page.Next)
			require.Equal(t, vector(false).Blob, page.Blobs[0].Data.Blob)
			page, e = ReadPage(t.Context(), reader, evm.New("evm"), 1, f.id, d, *page.Next, 1)
			require.NoError(t, e)
			require.Nil(t, page.Next)
			require.Equal(t, vector(true).Blob, page.Blobs[0].Data.Blob)
			require.Equal(t, before, o.calls.Load(), "reads have no canonical RPC")
			calls := p.calls.Load()
			p.err = errIO
			_, e = supplier.Acquire(t.Context(), 1, f.id, d)
			require.NoError(t, e)
			require.Equal(t, calls, p.calls.Load())
			require.Equal(t, before+1, o.calls.Load(), "retry still checks fresh")
			o.wrong.Store(true)
			_, e = supplier.Acquire(t.Context(), 1, f.id, d)
			require.ErrorIs(t, e, capture.ErrCanonical)
			o.wrong.Store(false)
			events, e := s.SupplementStreamEvents(t.Context(), d, 0, 10)
			require.NoError(t, e)
			require.Len(t, events, 1)
			require.NoError(t, db.Close())
			db = nil
			db = open()
			s, e = capture.New(db, f.id.Block.Chain, o)
			require.NoError(t, e)
			cursor, e := s.InitializeSupplementStream(t.Context(), d)
			require.NoError(t, e)
			require.Equal(t, f.id.Block, *cursor.Complete)
			supplier, e = New(s, evm.New("evm"), p)
			require.NoError(t, e)
			_, e = supplier.Acquire(t.Context(), 1, f.id, d)
			require.NoError(t, e)
			require.Equal(t, calls, p.calls.Load())
			// Reset removes current access to the old input. A stale page cannot
			// silently continue across epochs, even when the block hash is reused.
			anchor := model.FeedBlock{Chain: f.id.Block.Chain, Hash: f.id.Block.Chain.GenesisHash}
			require.NoError(t, s.Reset(t.Context(), 1, anchor))
			_, e = s.InitializeSupplementStream(t.Context(), d)
			require.NoError(t, e)
			_, e = ReadPage(t.Context(), s, evm.New("evm"), 1, f.id, d, 1, 1)
			require.ErrorIs(t, e, capture.ErrStale)
			f.id.Generation = 2
			commitFixture(t, s, f)
			p.err = nil
			_, e = supplier.Acquire(t.Context(), 2, f.id, d)
			require.NoError(t, e)
			_, e = ReadPage(t.Context(), s, evm.New("evm"), 2, f.id, d, 0, 1)
			require.NoError(t, e)
		})
	}
}

type failWriter struct {
	port.CaptureWriter
	at, n int
}

func (w *failWriter) Put(k, v []byte) error {
	w.n++
	if w.n == w.at {
		return errIO
	}
	return w.CaptureWriter.Put(k, v)
}

type failDB struct {
	port.CaptureDatabase
	at int
}

func (d failDB) CaptureUpdate(ctx context.Context, c model.FeedChain, fn func(port.CaptureWriter) error) error {
	return d.CaptureDatabase.CaptureUpdate(ctx, c, func(w port.CaptureWriter) error { return fn(&failWriter{CaptureWriter: w, at: d.at}) })
}
func TestBlobFourWritesAreAtomic(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := database(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			f := blockFixture(t, []Entry{vector(false)})
			o := &oracle{block: f.id.Block}
			s, d := initialize(t, db, f, o)
			for at := 1; at <= 4; at++ {
				broken, e := capture.New(failDB{CaptureDatabase: db, at: at}, f.id.Block.Chain, o)
				require.NoError(t, e)
				supplier, e := New(broken, evm.New("evm"), &provider{raw: f.raw})
				require.NoError(t, e)
				_, e = supplier.Acquire(t.Context(), 1, f.id, d)
				require.ErrorIs(t, e, errIO)
				status, e := s.SupplementStreamStatus(t.Context(), d)
				require.NoError(t, e)
				require.Nil(t, status.Complete)
				require.Zero(t, status.Sequence)
				_, e = s.ReadSupplement(t.Context(), 1, f.id, Codec, d.SelectionHash)
				require.ErrorIs(t, e, port.ErrNotFound)
				events, e := s.SupplementStreamEvents(t.Context(), d, 0, 10)
				require.NoError(t, e)
				require.Empty(t, events)
			}
		})
	}
}

type corruptReader struct {
	port.CaptureReader
	edit func([]byte) []byte
}

func (r corruptReader) Get(k []byte) ([]byte, error) {
	raw, e := r.CaptureReader.Get(k)
	if e != nil {
		return nil, e
	}
	var in capture.Supplement
	if json.Unmarshal(raw, &in) == nil && in.Codec == Codec {
		in.Data = r.edit(in.Data)
		in.Digest = sha256.Sum256(in.Data)
		return json.Marshal(in)
	}
	return raw, nil
}

type corruptDB struct {
	port.CaptureDatabase
	edit func([]byte) []byte
}

func (d corruptDB) CaptureView(ctx context.Context, c model.FeedChain, fn func(port.CaptureReader) error) error {
	return d.CaptureDatabase.CaptureView(ctx, c, func(r port.CaptureReader) error { return fn(corruptReader{CaptureReader: r, edit: d.edit}) })
}
func TestBlobStoredProofIsReverified(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := database(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			f := blockFixture(t, []Entry{vector(false)})
			o := &oracle{block: f.id.Block}
			s, d := initialize(t, db, f, o)
			supplier, e := New(s, evm.New("evm"), &provider{raw: f.raw})
			require.NoError(t, e)
			_, e = supplier.Acquire(t.Context(), 1, f.id, d)
			require.NoError(t, e)
			broken := corruptDB{CaptureDatabase: db, edit: func(raw []byte) []byte {
				var b storedBundle
				require.NoError(t, rlp.DecodeBytes(raw, &b))
				response, e := Decode(b.Response)
				require.NoError(t, e)
				response.Entries[0].Blob[31] = 1
				b.Response, e = rlp.EncodeToBytes(response)
				require.NoError(t, e)
				raw, e = rlp.EncodeToBytes(b)
				require.NoError(t, e)
				return raw
			}}
			reader, e := capture.NewReader(broken, f.id.Block.Chain, nil)
			require.NoError(t, e)
			_, e = ReadSnapshot(t.Context(), reader, evm.New("evm"), 1, f.id, d)
			require.ErrorIs(t, e, ErrBlob)
		})
	}
}

func TestBlobNoReferencesDoesNotOpenProvider(t *testing.T) {
	db := database(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	f := blockFixture(t, nil)
	o := &oracle{block: f.id.Block}
	s, d := initialize(t, db, f, o)
	p := &provider{err: errIO}
	supplier, e := New(s, evm.New("evm"), p)
	require.NoError(t, e)
	result, e := supplier.Acquire(t.Context(), 1, f.id, d)
	require.NoError(t, e)
	require.Empty(t, result.References)
	require.Zero(t, p.calls.Load())
	_, e = ReadPage(t.Context(), s, evm.New("evm"), 1, f.id, d, 0, 1)
	require.NoError(t, e)
	d.Required = true
	d.ParentCheckpoint = true
	_, e = DefinitionFor(d)
	require.ErrorIs(t, e, ErrBlob)
}

type blockedReader struct {
	started, closed chan struct{}
	closes          atomic.Int32
}

func (r *blockedReader) Read([]byte) (int, error) { close(r.started); <-r.closed; return 0, errIO }
func (r *blockedReader) Close() error {
	if r.closes.Add(1) == 1 {
		close(r.closed)
	}
	return nil
}

type readerProvider struct{ body io.ReadCloser }

func (p readerProvider) Open(context.Context, model.FeedBlock, []Reference) (io.ReadCloser, error) {
	return p.body, nil
}
func TestBlobProviderCancellationClosesAndJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &blockedReader{started: make(chan struct{}), closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, e := readProvider(ctx, readerProvider{body: body}, model.FeedBlock{}, nil); done <- e }()
	<-body.started
	cancel()
	select {
	case e := <-done:
		require.ErrorIs(t, e, context.Canceled)
		require.ErrorIs(t, e, errIO)
	case <-time.After(time.Second):
		t.Fatal("provider not joined")
	}
	require.EqualValues(t, 1, body.closes.Load())
}
func TestBlobProviderInputBound(t *testing.T) {
	raw, e := readProvider(t.Context(), &provider{raw: make([]byte, MaxResponseBytes+10)}, model.FeedBlock{}, nil)
	require.ErrorIs(t, e, ErrBlob)
	require.Len(t, raw, MaxResponseBytes+1)
}
