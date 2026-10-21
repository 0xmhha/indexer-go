package facts_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"gopkg.in/yaml.v3"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	_ "github.com/0xmhha/indexer-go/internal/features/address"
	_ "github.com/0xmhha/indexer-go/internal/features/balance"
	_ "github.com/0xmhha/indexer-go/internal/features/token"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc"
)

var ctx = context.Background()

type database interface {
	port.CaptureDatabase
	storage.Storage
}
type opener func() database

func dbFactory(t *testing.T, backend string) opener {
	t.Helper()
	return dbFactoryWithConns(t, backend, 1)
}
func dbFactoryWithConns(t *testing.T, backend string, maxConns int32) opener {
	t.Helper()
	if backend == "pebble" {
		path := t.TempDir()
		return func() database {
			s, e := storage.NewPebbleStorage(storage.DefaultConfig(path))
			require.NoError(t, e)
			return s
		}
	}
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set INDEXER_TEST_POSTGRES for fsync-on PostgreSQL tests")
	}
	c, e := pgx.Connect(ctx, dsn)
	require.NoError(t, e)
	var fsync string
	require.NoError(t, c.QueryRow(ctx, "SHOW fsync").Scan(&fsync))
	require.NoError(t, c.Close(ctx))
	if fsync != "on" {
		t.Skip("facts durability requires fsync=on")
	}
	var nonce [8]byte
	_, e = rand.Read(nonce[:])
	require.NoError(t, e)
	schema := "facts_" + hex.EncodeToString(nonce[:])
	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dsn)
		require.NoError(t, e)
		defer func() { _ = c.Close(ctx) }()
		_, e = c.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, e)
	})
	return func() database {
		s, e := postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: schema, MaxConns: maxConns})
		require.NoError(t, e)
		return s
	}
}

type oracle struct {
	chain *testchain.Chain
	mu    sync.Mutex
	wrong bool
	calls []uint64
}

func (o *oracle) HashAt(c context.Context, n uint64) (common.Hash, error) {
	if e := c.Err(); e != nil {
		return common.Hash{}, e
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, n)
	if o.wrong && n > 0 {
		return common.Hash{99}, nil
	}
	b := o.chain.Block(n)
	if b == nil {
		return common.Hash{}, source.ErrNotFound
	}
	return b.Block.Hash(), nil
}
func (o *oracle) setWrong(v bool) { o.mu.Lock(); o.wrong = v; o.mu.Unlock() }

type fixture struct {
	chain   *testchain.Chain
	profile chains.BinaryProfile
	fresh   *oracle
	store   *capture.Store
	feed    model.FeedChain
	inputs  []capture.Input
	actors  []common.Address
	alloc   *big.Int
	rawPath string
}

func build(t *testing.T, db port.CaptureDatabase, isArc bool) *fixture {
	t.Helper()
	id := int64(1337)
	var p chains.BinaryProfile = evm.New("facts-fixture")
	if isArc {
		id = 5042002
		p = arc.New()
	}
	a, b := testchain.NewAccount(1), testchain.NewAccount(2)
	allocation := new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)
	c := testchain.NewChain(id, map[common.Address]*big.Int{a.Address: allocation})
	for i := 0; i < 3; i++ {
		logs := []*types.Log{{Address: common.Address{9}, Topics: []common.Hash{testchain.SigTransfer, common.BytesToHash(a.Address.Bytes()), common.BytesToHash(b.Address.Bytes())}, Data: common.LeftPadBytes(big.NewInt(10).Bytes(), 32)}}
		if isArc {
			logs = append(logs, &types.Log{Address: arc.NativeEmitter, Topics: []common.Hash{testchain.SigTransfer, common.BytesToHash(a.Address.Bytes()), common.BytesToHash(b.Address.Bytes())}, Data: common.LeftPadBytes(big.NewInt(10).Bytes(), 32)})
		}
		c.AddBlock(testchain.TxSpec{From: a, Tx: &types.DynamicFeeTx{To: &b.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(10)}, GasUsed: 21000, Failed: i == 1, Logs: logs})
	}
	c.SetHead(3)
	f := &fixture{chain: c, profile: p, fresh: &oracle{chain: c}, feed: model.FeedChain{ID: fmt.Sprint(id), GenesisHash: c.Block(0).Block.Hash()}, alloc: allocation}
	s, e := capture.New(db, f.feed, f.fresh)
	require.NoError(t, e)
	f.store = s
	anchor := model.FeedBlock{Chain: f.feed, Hash: f.feed.GenesisHash}
	require.NoError(t, s.Initialize(ctx, evmdata.BasePlan(), anchor, nil))
	root := t.TempDir()
	v := window.VolumeSpec{Root: root, ID: "facts-fixture", AllowDirectory: true}
	require.NoError(t, window.InitializeVolume(v))
	f.rawPath = filepath.Join(root, "raw")
	raw, e := window.Create(window.Options{Volume: v, Path: f.rawPath, Chain: f.feed, MaxBytes: 2 << 20, SegmentBytes: 64 << 10, MaxPayloadBytes: 32 << 10, MaxSegmentRecords: 32})
	require.NoError(t, e)
	actors := map[common.Address]bool{a.Address: true, b.Address: true}
	for n := uint64(1); n <= 3; n++ {
		block := c.Block(n)
		actors[block.Block.Coinbase()] = true
		header, e := rlp.EncodeToBytes(block.Block.Header())
		require.NoError(t, e)
		body, e := rlp.EncodeToBytes(block.Block.Body())
		require.NoError(t, e)
		receipts, e := rlp.EncodeToBytes(block.Receipts)
		require.NoError(t, e)
		var refs []window.Reference
		for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
			r, e := raw.Append(ctx, chainfeed.Envelope{Block: model.FeedBlock{Chain: f.feed, Number: n, Hash: block.Block.Hash()}, Kind: k, Codec: evmdata.Codec(k), Payload: [][]byte{header, body, receipts}[i]})
			require.NoError(t, e)
			refs = append(refs, r)
		}
		st, e := s.State(ctx)
		require.NoError(t, e)
		in, e := (evmdata.Verifier{Profile: p, Provenance: "fixture"}).Assemble(ctx, raw, st, refs)
		require.NoError(t, e)
		require.NoError(t, s.Commit(ctx, in))
		f.inputs = append(f.inputs, in)
	}
	require.NoError(t, raw.Close())
	require.NoError(t, os.RemoveAll(f.rawPath)) // DB-only after raw is gone
	for a := range actors {
		f.actors = append(f.actors, a)
	}
	return f
}
func (f *fixture) source(t *testing.T, consumer string) *facts.Source {
	t.Helper()
	s, e := facts.New(ctx, facts.Config{Store: f.store, Profile: f.profile, Canonical: f.fresh, Consumer: consumer})
	require.NoError(t, e)
	return s
}
func makeFetcher(t *testing.T, db database, src source.Source, client fetch.Client, deps feature.Deps) *fetch.Fetcher {
	t.Helper()
	cfg := &fetch.Config{StartHeight: 1, NumWorkers: 2, MaxRetries: 1, StatePrefetch: true, StateMaxBytes: 16 << 20}
	f := fetch.NewFetcher(client, db, cfg, zap.NewNop(), nil)
	f.SetSource(src)
	deps.Publish = f.Publish
	pipeline, e := feature.Build([]string{"address.index", "token.transfers", "balance.native"}, deps)
	require.NoError(t, e)
	f.SetFeatures(pipeline)
	t.Cleanup(f.Close)
	return f
}
func seed(t *testing.T, db database, f *fixture) {
	t.Helper()
	w := db.(port.HistoricalWriter)
	for _, a := range f.actors {
		value := new(big.Int)
		if a == testchain.NewAccount(1).Address {
			value.Set(f.alloc)
		}
		require.NoError(t, w.SetBalance(ctx, a, 0, value))
	}
}

var errInjected = errors.New("errInjected before commit")

func TestFactsWriterBackends(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		for _, isArc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/arc=%v", backend, isArc), func(t *testing.T) {
				open := dbFactory(t, backend)
				db := open()
				f := build(t, db, isArc)
				seed(t, db, f)
				src := f.source(t, "projection-v1")
				indexer := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
				// Rollback all staged source-independent projection rows/events on failure.
				indexer.SetBeforeCommitHook(func(uint64) error { return errInjected })
				require.ErrorIs(t, indexer.FetchBlock(ctx, 1), errInjected)
				_, e := db.GetBlock(ctx, 1)
				require.ErrorIs(t, e, port.ErrNotFound)
				_, e = f.store.ProjectionAck(ctx, "projection-v1", src.Epoch(), f.inputs[0].Manifest.Identity)
				require.ErrorIs(t, e, port.ErrNotFound)
				indexer.SetBeforeCommitHook(func(uint64) error { f.fresh.setWrong(true); return nil })
				require.ErrorIs(t, indexer.FetchBlock(ctx, 1), capture.ErrCanonical)
				f.fresh.setWrong(false)
				indexer.SetBeforeCommitHook(nil)
				require.NoError(t, indexer.FetchRange(ctx, 1, 3))
				require.NoError(t, indexer.FetchBlock(ctx, 3))
				stats := indexer.GetPipelineMetrics()
				require.EqualValues(t, 5, stats.SourceValidate.Count)
				require.EqualValues(t, 1, stats.SourceValidate.Errors)
				require.EqualValues(t, 4, stats.SourceAck.Count)
				require.Zero(t, stats.SourceAck.Errors)
				h, e := db.GetLatestHeight(ctx)
				require.NoError(t, e)
				require.EqualValues(t, 3, h)
				for n := uint64(1); n <= 3; n++ {
					got, e := db.GetBlock(ctx, n)
					require.NoError(t, e)
					want, rs, e := src.BlockWithReceipts(ctx, n)
					require.NoError(t, e)
					x, e := model.EncodeBlock(got)
					require.NoError(t, e)
					y, e := model.EncodeBlock(want)
					require.NoError(t, e)
					require.Equal(t, y, x)
					for _, r := range rs {
						stored, e := db.GetReceipt(ctx, r.TxHash)
						require.NoError(t, e)
						x, e := model.EncodeReceipt(stored)
						require.NoError(t, e)
						y, e := model.EncodeReceipt(r)
						require.NoError(t, e)
						require.Equal(t, y, x)
					}
					receipt, e := f.store.ProjectionAck(ctx, "projection-v1", src.Epoch(), f.inputs[n-1].Manifest.Identity)
					require.NoError(t, e)
					require.Equal(t, f.inputs[n-1].Manifest.Identity, receipt.Identity)
				}
				// Compare against independently decoded JSON-RPC blocks and
				// the same profile/writer, including balances/log indexes/outbox.
				reference(t, backend, db, f, true)
				logs, e := db.GetLogsByBlock(ctx, 3)
				require.NoError(t, e)
				wantLogs, e := src.Logs(ctx, 3)
				require.NoError(t, e)
				require.Equal(t, wantLogs, logs)
				// DB-only replay cannot supply unknown historical account state.
				_, e = src.BalanceAt(ctx, common.Address{88}, big.NewInt(1))
				require.ErrorIs(t, e, facts.ErrCoverage)
				// All projection ACKs are independent of source event/capture progress.
				st, e := f.store.State(ctx)
				require.NoError(t, e)
				require.EqualValues(t, 3, st.Sequence)
				// An existing projection can undo even after raw segments were removed.
				indexer.Close()
				_, e = db.(port.Rollbacker).RollbackTo(ctx, 1, nil)
				require.NoError(t, e)
				_, e = db.GetBlock(ctx, 2)
				require.ErrorIs(t, e, port.ErrNotFound)
				require.NoError(t, db.Close())
				db = open()
				defer func() { require.NoError(t, db.Close()) }()
				s, e := capture.New(db, f.feed, f.fresh)
				require.NoError(t, e)
				f.store = s
				src = f.source(t, "projection-v1")
				indexer = makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
				require.NoError(t, indexer.FetchRange(ctx, 2, 3))
				indexer.Close()
			})
		}
	}
}

func TestFactsCoverageAndEpoch(t *testing.T) {
	db := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	f := build(t, db, false)
	s := f.source(t, "epoch")
	head, e := s.Head(ctx)
	require.NoError(t, e)
	require.EqualValues(t, 3, head)
	_, e = s.Block(ctx, 0)
	require.ErrorIs(t, e, facts.ErrCoverage)
	_, e = s.Block(ctx, 4)
	require.ErrorIs(t, e, source.ErrNotFound)
	b, _, e := s.BlockWithReceipts(ctx, 2)
	require.NoError(t, e)
	require.NoError(t, f.store.Reset(ctx, 1, f.inputs[0].Manifest.Identity.Block))
	_, e = s.Head(ctx)
	require.ErrorIs(t, e, capture.ErrStale)
	require.ErrorIs(t, s.ValidateCommit(ctx, b), capture.ErrStale)
	require.ErrorIs(t, s.Acknowledge(ctx, b), capture.ErrStale)
	s = f.source(t, "epoch")
	_, e = s.Block(ctx, 2)
	require.ErrorIs(t, e, source.ErrNotFound)
	b, _, e = s.BlockWithReceipts(ctx, 1)
	require.NoError(t, e)
	require.NoError(t, s.ValidateCommit(ctx, b))
	require.NoError(t, s.Acknowledge(ctx, b)) // retained ancestor's old capture identity
}

func reference(t *testing.T, backend string, actual database, fixture *fixture, compareOutbox bool) {
	t.Helper()
	db := dbFactory(t, backend)()
	defer func() { require.NoError(t, db.Close()) }()
	seed(t, db, fixture)
	server := testchain.NewServer(fixture.chain)
	defer server.Close()
	rpc, e := gethrpc.DialContext(ctx, server.URL())
	require.NoError(t, e)
	defer rpc.Close()
	src := sourcerpc.New(rpc, fixture.profile)
	// State is an explicit known checkpoint for both paths; a request would be
	// a missing-coverage bug rather than a reason to query old node state.
	factsClient := fixture.source(t, "reference-client")
	deps := factsClient.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()})
	deps.Profile = fixture.profile
	deps.BlockAt = src.Block
	indexer := makeFetcher(t, db, src, factsClient, deps)
	defer indexer.Close()
	require.NoError(t, indexer.FetchRange(ctx, 1, 3))
	for _, a := range fixture.actors {
		got, e := actual.(port.HistoricalReader).GetAddressBalance(ctx, a, 3)
		require.NoError(t, e)
		want, e := db.(port.HistoricalReader).GetAddressBalance(ctx, a, 3)
		require.NoError(t, e)
		require.Equal(t, want.String(), got.String())
	}
	for n := uint64(1); n <= 3; n++ {
		x, e := actual.GetBlock(ctx, n)
		require.NoError(t, e)
		y, e := db.GetBlock(ctx, n)
		require.NoError(t, e)
		// Binary Size can differ from JSON's reported size; consensus and all tx
		// fields must match. Optional nil/empty lists share the model codec.
		x.Size = y.Size
		xb, e := model.EncodeBlock(x)
		require.NoError(t, e)
		yb, e := model.EncodeBlock(y)
		require.NoError(t, e)
		require.Equal(t, yb, xb)
		for _, tx := range x.Transactions {
			a, e := actual.GetReceipt(ctx, tx.Hash)
			require.NoError(t, e)
			b, e := db.GetReceipt(ctx, tx.Hash)
			require.NoError(t, e)
			ab, e := model.EncodeReceipt(a)
			require.NoError(t, e)
			bb, e := model.EncodeReceipt(b)
			require.NoError(t, e)
			require.Equal(t, bb, ab)
		}
	}
	if compareOutbox {
		a, e := actual.(port.Outbox).ReadOutbox(ctx, 0, 1000)
		require.NoError(t, e)
		b, e := db.(port.Outbox).ReadOutbox(ctx, 0, 1000)
		require.NoError(t, e)
		require.Len(t, a, len(b))
		for i := range a {
			require.Equal(t, a[i].Seq, b[i].Seq)
			require.Equal(t, a[i].Type, b[i].Type)
			var x, y map[string]any
			require.NoError(t, json.Unmarshal(a[i].Data, &x))
			require.NoError(t, json.Unmarshal(b[i].Data, &y))
			delete(x, "created_at")
			delete(y, "created_at")
			require.Equal(t, y, x)
		}
	}
	calls := server.Calls()
	require.Zero(t, calls["eth_getBalance"])
	require.Zero(t, calls["eth_call"])
	require.Zero(t, calls["eth_getCode"])
}

type failedACK struct {
	*facts.Source
	fail bool
}

func (s *failedACK) Acknowledge(c context.Context, b *model.Block) error {
	if s.fail {
		return errInjected
	}
	return s.Source.Acknowledge(c, b)
}
func TestFactsLostACKRetry(t *testing.T) {
	db := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	fixture := build(t, db, false)
	seed(t, db, fixture)
	src := fixture.source(t, "ack-retry")
	wrapped := &failedACK{Source: src, fail: true}
	f := makeFetcher(t, db, wrapped, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
	require.ErrorIs(t, f.FetchBlock(ctx, 1), errInjected)
	block, e := db.GetBlock(ctx, 1)
	require.NoError(t, e)
	require.Equal(t, fixture.chain.Block(1).Block.Hash(), block.Hash)
	_, e = fixture.store.ProjectionAck(ctx, "ack-retry", 1, fixture.inputs[0].Manifest.Identity)
	require.ErrorIs(t, e, port.ErrNotFound)
	outbox := db.(port.Outbox)
	before, e := outbox.LastOutboxSeq(ctx)
	require.NoError(t, e)
	require.Greater(t, before, uint64(0))
	// A duplicate retry must still reject fresh-canonical disagreement.
	fixture.fresh.setWrong(true)
	require.ErrorIs(t, f.FetchBlock(ctx, 1), capture.ErrCanonical)
	fixture.fresh.setWrong(false)
	wrapped.fail = false
	require.NoError(t, f.FetchBlock(ctx, 1))
	after, e := outbox.LastOutboxSeq(ctx)
	require.NoError(t, e)
	require.Equal(t, before, after)
	_, e = fixture.store.ProjectionAck(ctx, "ack-retry", 1, fixture.inputs[0].Manifest.Identity)
	require.NoError(t, e)
	f.Close()
}
func TestFactsMissingStateNeverFallsBack(t *testing.T) {
	for _, prefetch := range []bool{false, true} {
		t.Run(fmt.Sprint(prefetch), func(t *testing.T) {
			db := dbFactory(t, "pebble")()
			defer func() { require.NoError(t, db.Close()) }()
			fixture := build(t, db, false)
			src := fixture.source(t, "no-state")
			f := fetch.NewFetcher(src, db, &fetch.Config{StartHeight: 1, MaxRetries: 1, StatePrefetch: prefetch}, zap.NewNop(), nil)
			f.SetSource(src)
			defer f.Close()
			deps := src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop(), Settings: func(_ string, into any) error { return yaml.Unmarshal([]byte("allow_pruned_state: true"), into) }})
			pipeline, e := feature.Build([]string{"balance.native"}, deps)
			require.NoError(t, e)
			f.SetFeatures(pipeline)
			require.ErrorIs(t, f.FetchBlock(ctx, 1), facts.ErrCoverage)
			_, e = db.GetBlock(ctx, 1)
			require.ErrorIs(t, e, port.ErrNotFound)
			seq, e := db.(port.Outbox).LastOutboxSeq(ctx)
			require.NoError(t, e)
			require.Zero(t, seq)
			_, e = fixture.store.ProjectionAck(ctx, "no-state", 1, fixture.inputs[0].Manifest.Identity)
			require.ErrorIs(t, e, port.ErrNotFound)
			_, e = src.CodeAt(ctx, common.Address{1}, nil)
			require.ErrorIs(t, e, facts.ErrCoverage)
		})
	}
}
func TestFactsSharedRPCBudget(t *testing.T) {
	db := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	fixture := build(t, db, false)
	seed(t, db, fixture)
	server := testchain.NewServer(fixture.chain)
	defer server.Close()
	pool, e := rpcpool.New(rpcpool.Config{Endpoints: []string{server.URL()}, RateLimit: 5, Burst: 1, CoalesceReads: true})
	require.NoError(t, e)
	rpc, e := gethrpc.DialOptions(ctx, pool.Primary(), gethrpc.WithHTTPClient(&http.Client{Transport: pool}))
	require.NoError(t, e)
	defer rpc.Close()
	live := sourcerpc.New(rpc, fixture.profile)
	src, e := facts.New(ctx, facts.Config{Store: fixture.store, Profile: fixture.profile, Canonical: live, Consumer: "limited"})
	require.NoError(t, e)
	before := server.Calls()["eth_getBlockByNumber"]
	start := time.Now()
	f := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
	require.NoError(t, f.FetchRange(ctx, 1, 3))
	f.Close()
	calls := server.Calls()
	require.Equal(t, 3, calls["eth_getBlockByNumber"]-before)
	require.GreaterOrEqual(t, time.Since(start), 400*time.Millisecond)
	require.EqualValues(t, calls["eth_getBlockByNumber"], pool.Status()[0].Requests)
	require.Zero(t, calls["eth_getBlockReceipts"])
	require.Zero(t, calls["eth_getBalance"])
	require.Zero(t, calls["eth_call"])
}
func TestFactsReadResetRace(t *testing.T) {
	db := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	fixture := build(t, db, false)
	src := fixture.source(t, "race")
	start := make(chan struct{})
	var b *model.Block
	var readErr, resetErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; b, _, readErr = src.BlockWithReceipts(ctx, 2) }()
	go func() {
		defer wg.Done()
		<-start
		resetErr = fixture.store.Reset(ctx, 1, fixture.inputs[0].Manifest.Identity.Block)
	}()
	close(start)
	wg.Wait()
	require.NoError(t, resetErr)
	if readErr != nil {
		require.ErrorIs(t, readErr, capture.ErrStale)
	} else {
		require.ErrorIs(t, src.ValidateCommit(ctx, b), capture.ErrStale)
	}
	require.ErrorIs(t, src.Acknowledge(ctx, &model.Block{Number: 2, Hash: fixture.inputs[1].Manifest.Identity.Block.Hash}), capture.ErrStale)
}
func appendFact(t *testing.T, f *fixture, n uint64) capture.Input {
	t.Helper()
	in := assembleFact(t, f, n)
	require.NoError(t, f.store.Commit(ctx, in))
	return in
}
func assembleFact(t *testing.T, f *fixture, n uint64) capture.Input {
	t.Helper()
	block := f.chain.Block(n)
	header, e := rlp.EncodeToBytes(block.Block.Header())
	require.NoError(t, e)
	body, e := rlp.EncodeToBytes(block.Block.Body())
	require.NoError(t, e)
	receipts, e := rlp.EncodeToBytes(block.Receipts)
	require.NoError(t, e)
	id := model.FeedBlock{Chain: f.feed, Number: n, Hash: block.Block.Hash()}
	_, _, e = evmdata.Decode(f.profile, id, header, body, receipts)
	require.NoError(t, e)
	st, e := f.store.State(ctx)
	require.NoError(t, e)
	pd, e := st.Plan.Digest()
	require.NoError(t, e)
	in := capture.Input{ParentHash: block.Block.ParentHash(), Manifest: model.CaptureManifest{Identity: model.CaptureIdentity{Block: id, Generation: st.Generation, PlanDigest: pd, PlanVersion: st.Plan.Version}}}
	for _, r := range st.Plan.Required {
		var value []byte
		switch r.Kind {
		case model.DatasetHeaders:
			value = header
		case model.DatasetBodies:
			value = body
		case model.DatasetReceipts:
			value = receipts
		}
		in.Payloads = append(in.Payloads, capture.Payload{Kind: r.Kind, Bytes: value})
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: r.Kind, Coverage: r.Coverage, Status: model.DatasetPersisted, Codec: evmdata.Codec(r.Kind), Source: "fixture", Verification: model.VerificationCommitment, Digest: sha256.Sum256(value)})
	}
	return in
}
func TestFactsWriterReorg(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := dbFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			fixture := build(t, db, true)
			seed(t, db, fixture)
			src := fixture.source(t, "reorg")
			f := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
			require.NoError(t, f.FetchRange(ctx, 1, 3))
			f.Close()
			before, e := db.(port.Outbox).LastOutboxSeq(ctx)
			require.NoError(t, e)
			old := fixture.inputs[1].Manifest.Identity
			fixture.chain.Reorg(1)
			a, b := testchain.NewAccount(1), testchain.NewAccount(2)
			for i := 0; i < 2; i++ {
				fixture.chain.AddBlockWithExtra([]byte("replacement"), testchain.TxSpec{From: a, Tx: &types.DynamicFeeTx{To: &b.Address, Value: big.NewInt(0), Gas: 21000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000)}, GasUsed: 21000})
			}
			fixture.chain.SetHead(3)
			require.NoError(t, fixture.store.Reset(ctx, 1, fixture.inputs[0].Manifest.Identity.Block))
			tail := []capture.Input{appendFact(t, fixture, 2), appendFact(t, fixture, 3)}
			src = fixture.source(t, "reorg")
			f = makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
			defer f.Close()
			ancestor, e := f.HandleReorg(ctx, 3)
			require.NoError(t, e)
			require.EqualValues(t, 1, ancestor)
			require.NoError(t, f.FetchRange(ctx, 2, 3))
			for _, in := range tail {
				block, e := db.GetBlock(ctx, in.Manifest.Identity.Block.Number)
				require.NoError(t, e)
				require.Equal(t, in.Manifest.Identity.Block.Hash, block.Hash)
				_, e = fixture.store.ProjectionAck(ctx, "reorg", 2, in.Manifest.Identity)
				require.NoError(t, e)
			}
			_, e = fixture.store.Read(ctx, old)
			require.NoError(t, e)
			_, e = fixture.store.ProjectionAck(ctx, "reorg", 1, old)
			require.NoError(t, e)
			entries, e := db.(port.Outbox).ReadOutbox(ctx, before, 1000)
			require.NoError(t, e)
			require.NotEmpty(t, entries)
			found := false
			for _, entry := range entries {
				if entry.Type == "reorg" {
					found = true
				}
			}
			require.True(t, found)
			// Both paths produce the replacement chain's balances from the same seed.
			reference(t, backend, db, fixture, false)
		})
	}
}

func TestFactsContractMetadataRequiresCoverage(t *testing.T) {
	db := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	fixture := build(t, db, false)
	fixture.chain.Reorg(0)
	fixture.chain.AddBlock(testchain.TxSpec{From: testchain.NewAccount(1), Tx: &types.DynamicFeeTx{Gas: 100000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: new(big.Int), Data: []byte{0x60, 0x00}}, GasUsed: 55000})
	fixture.chain.SetHead(1)
	require.NoError(t, fixture.store.Reset(ctx, 1, model.FeedBlock{Chain: fixture.feed, Hash: fixture.feed.GenesisHash}))
	appendFact(t, fixture, 1)
	src := fixture.source(t, "metadata")
	deps := src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop(), Settings: func(_ string, into any) error { return yaml.Unmarshal([]byte("allow_pruned_state: true"), into) }})
	f := fetch.NewFetcher(src, db, &fetch.Config{StartHeight: 1, MaxRetries: 1, StatePrefetch: true}, zap.NewNop(), nil)
	f.SetSource(src)
	defer f.Close()
	pipeline, e := feature.Build([]string{"token.metadata"}, deps)
	require.NoError(t, e)
	f.SetFeatures(pipeline)
	require.ErrorIs(t, f.FetchBlock(ctx, 1), facts.ErrCoverage)
	_, e = db.GetBlock(ctx, 1)
	require.ErrorIs(t, e, port.ErrNotFound)
}

func TestFactsPermanentObjectInput(t *testing.T) {
	original := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, original.Close()) }()
	f := build(t, original, false)
	db := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	v := window.VolumeSpec{Root: t.TempDir(), ID: "facts-objects", AllowDirectory: true}
	require.NoError(t, window.InitializeVolume(v))
	objects, e := window.CreateObjects(window.ObjectOptions{Volume: v, Path: filepath.Join(v.Root, "objects"), Chain: f.feed, MaxBytes: 32 << 20})
	require.NoError(t, e)
	defer func() { require.NoError(t, objects.Close()) }()
	store, e := capture.NewWithObjects(db, f.feed, f.fresh, objects)
	require.NoError(t, e)
	require.NoError(t, store.Initialize(ctx, evmdata.BasePlan(), model.FeedBlock{Chain: f.feed, Hash: f.feed.GenesisHash}, nil))
	for _, old := range f.inputs {
		in := old
		in.Payloads = nil
		for _, p := range old.Payloads {
			if p.Kind != model.DatasetBodies {
				in.Payloads = append(in.Payloads, p)
				continue
			}
			r, e := objects.Put(ctx, sha256.Sum256(p.Bytes), int64(len(p.Bytes)), bytes.NewReader(p.Bytes))
			require.NoError(t, e)
			in.Objects = append(in.Objects, capture.ObjectPayload{Kind: p.Kind, Ref: r})
		}
		require.NoError(t, store.Commit(ctx, in))
	}
	src, e := facts.New(ctx, facts.Config{Store: store, Profile: f.profile, Canonical: f.fresh, Consumer: "objects"})
	require.NoError(t, e)
	seed(t, db, f)
	indexer := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
	require.NoError(t, indexer.FetchRange(ctx, 1, 3))
	indexer.Close()
	b, e := db.GetBlock(ctx, 3)
	require.NoError(t, e)
	require.Equal(t, f.chain.Block(3).Block.Hash(), b.Hash)
}

func TestFactsRejectsInvalidCommitment(t *testing.T) {
	original := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, original.Close()) }()
	fixture := build(t, original, false)
	db := dbFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	store, e := capture.New(db, fixture.feed, fixture.fresh)
	require.NoError(t, e)
	require.NoError(t, store.Initialize(ctx, evmdata.BasePlan(), model.FeedBlock{Chain: fixture.feed, Hash: fixture.feed.GenesisHash}, nil))
	in := fixture.inputs[0]
	in.Payloads = append([]capture.Payload(nil), in.Payloads...)
	in.Manifest.Datasets = append([]model.DatasetEvidence(nil), in.Manifest.Datasets...)
	empty, e := rlp.EncodeToBytes(&types.Body{})
	require.NoError(t, e)
	for i, p := range in.Payloads {
		if p.Kind == model.DatasetBodies {
			in.Payloads[i].Bytes = empty
		}
	}
	for i, d := range in.Manifest.Datasets {
		if d.Kind == model.DatasetBodies {
			in.Manifest.Datasets[i].Digest = sha256.Sum256(empty)
		}
	}
	// Simulate an erroneous supplier's commitment claim. DB digest equality
	// alone must not replace profile/header root verification during replay.
	require.NoError(t, store.Commit(ctx, in))
	src, e := facts.New(ctx, facts.Config{Store: store, Profile: fixture.profile, Canonical: fixture.fresh, Consumer: "bad-input"})
	require.NoError(t, e)
	_, _, e = src.BlockWithReceipts(ctx, 1)
	require.Error(t, e)
	indexer := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
	require.Error(t, indexer.FetchBlock(ctx, 1))
	indexer.Close()
	_, e = db.GetBlock(ctx, 1)
	require.ErrorIs(t, e, port.ErrNotFound)
	_, e = store.ProjectionAck(ctx, "bad-input", 1, in.Manifest.Identity)
	require.ErrorIs(t, e, port.ErrNotFound)
}
