package capture_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/window"
)

var ctx = context.Background()
var chain = model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
var plan = model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{
	{Kind: model.DatasetBodies, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}},
	{Kind: model.DatasetReceipts, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}},
}}

type database interface {
	port.CaptureDatabase
	Close() error
}
type opener func() database

// The same source-facts/atomicity/GC suite runs against each real backend.
// SQL tests use their own schema and a disposable, fsync-on server.
func openFactory(t *testing.T, backend string) opener {
	t.Helper()
	if backend == "pebble" {
		path := t.TempDir()
		return func() database {
			s, err := storage.NewPebbleStorage(storage.DefaultConfig(path))
			require.NoError(t, err)
			return s
		}
	}
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set INDEXER_TEST_POSTGRES for actual PostgreSQL capture tests")
	}
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	var fsync string
	err = conn.QueryRow(ctx, "SHOW fsync").Scan(&fsync)
	require.NoError(t, err)
	require.NoError(t, conn.Close(ctx))
	if fsync != "on" {
		t.Skip("capture durability suite requires fsync=on; refusal tested by postgres package")
	}
	var b [6]byte
	_, err = rand.Read(b[:])
	require.NoError(t, err)
	schema := "capture_" + hex.EncodeToString(b[:])
	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dsn)
		if e != nil {
			t.Error(e)
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, e = c.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, e)
	})
	return func() database {
		s, e := postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4})
		require.NoError(t, e)
		return s
	}
}

type oracle struct {
	mu     sync.Mutex
	hashes map[uint64]common.Hash
	calls  int
}

func newOracle() *oracle {
	return &oracle{hashes: map[uint64]common.Hash{0: chain.GenesisHash, 1: {2}, 2: {3}, 3: {4}}}
}
func (o *oracle) HashAt(_ context.Context, n uint64) (common.Hash, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	return o.hashes[n], nil
}
func (o *oracle) fork(n uint64, h common.Hash) { o.mu.Lock(); defer o.mu.Unlock(); o.hashes[n] = h }
func service(t *testing.T, db port.CaptureDatabase, o *oracle) *capture.Store {
	t.Helper()
	s, e := capture.New(db, chain, o)
	require.NoError(t, e)
	return s
}
func initialize(t *testing.T, s *capture.Store, consumers ...string) {
	t.Helper()
	require.NoError(t, s.Initialize(ctx, plan, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, consumers))
}

func input(n uint64, g uint64, h, parent common.Hash) capture.Input {
	digest, _ := plan.Digest()
	in := capture.Input{Manifest: model.CaptureManifest{Identity: model.CaptureIdentity{Block: model.FeedBlock{Chain: chain, Number: n, Hash: h}, Generation: g, PlanVersion: plan.Version, PlanDigest: digest}}, ParentHash: parent}
	for _, r := range plan.Required {
		v := []byte(fmt.Sprintf("%d/%s/'\"; DROP TABLE capture_records; --", n, r.Kind))
		in.Payloads = append(in.Payloads, capture.Payload{Kind: r.Kind, Bytes: v})
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: r.Kind, Coverage: r.Coverage, Status: model.DatasetPersisted, Digest: sha256.Sum256(v), Codec: "fixture.v1", Source: "fixture'; --", Verification: model.VerificationHashBound})
	}
	return in
}

func TestCaptureStores(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			for name, test := range map[string]func(*testing.T, opener){
				"atomic-restart": atomicRestart, "rollback": rollback, "fork-fencing": forkFencing,
				"window-gc": windowGC, "kv-contract": kvContract, "concurrent-updates": concurrentUpdates,
				"empty-evidence":              emptyEvidence,
				"failed-deletion-fence":       failedDeletionFence,
				"uncertain-deletion-commit":   uncertainDeletionFence,
				"deletion-recovery":           deletionRecovery,
				"missing-deletion-receipt":    missingDeletionReceipt,
				"recovery-excludes-active-gc": recoveryExcludesActiveGC,
				"gc-verifies-block-once":      gcVerifiesBlockOnce,
				"checked-open-before-unlink":  checkedOpenBeforeUnlink,
			} {
				t.Run(name, func(t *testing.T) { test(t, openFactory(t, backend)) })
			}
		})
	}
}

func emptyEvidence(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	// Synthetic codec/plan fixture: emptiness is explicit evidence, never a
	// fallback for an RPC error. Applicability is the adapter's fork policy.
	a.Payloads = []capture.Payload{{Kind: model.DatasetBodies, Bytes: []byte{}}}
	a.Manifest.Datasets[0].Status = model.DatasetVerifiedEmpty
	a.Manifest.Datasets[0].Digest = sha256.Sum256(nil)
	a.Manifest.Datasets[1].Status = model.DatasetNotApplicable
	a.Manifest.Datasets[1].Verification = model.VerificationForkRules
	a.Manifest.Datasets[1].Reason = "synthetic fixture fork omits this dataset"
	require.NoError(t, s.Commit(ctx, a))
	got, err := s.Read(ctx, a.Manifest.Identity)
	require.NoError(t, err)
	require.Len(t, got.Payloads, 1)
	require.Empty(t, got.Payloads[0].Bytes)
	a.Manifest.Datasets[1].Reason = ""
	require.ErrorIs(t, s.Commit(ctx, a), model.ErrCaptureIncomplete)
}

func atomicRestart(t *testing.T, open opener) {
	db := open()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.Commit(ctx, a))
	// Duplicate facts still perform a fresh canonical lookup.
	require.Equal(t, 4, o.calls)
	st, err := s.State(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), st.Through.Number)
	require.Equal(t, uint64(1), st.Sequence)
	require.NoError(t, db.Close())
	db = open()
	defer func() { require.NoError(t, db.Close()) }()
	s = service(t, db, o)
	initialize(t, s)
	got, err := s.Read(ctx, a.Manifest.Identity)
	require.NoError(t, err)
	require.ElementsMatch(t, a.Manifest.Datasets, got.Manifest.Datasets)
	require.ElementsMatch(t, a.Payloads, got.Payloads)
	events, err := s.Events(ctx, 0, 256)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "facts_ready", events[0].Kind)
	b := input(2, 1, common.Hash{3}, common.Hash{2})
	require.NoError(t, s.Commit(ctx, b))
	// Retrying older facts cannot rewind a later cursor.
	require.NoError(t, s.Commit(ctx, a))
	st, err = s.State(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), st.Through.Number)
	a.Manifest.Datasets[0].Source = "conflicting provenance"
	require.ErrorIs(t, s.Commit(ctx, a), capture.ErrConflict)
	bad := input(3, 1, common.Hash{4}, common.Hash{0xff})
	require.ErrorIs(t, s.Commit(ctx, bad), capture.ErrGap)
	bad = input(3, 1, common.Hash{0xee}, common.Hash{3})
	require.ErrorIs(t, s.Commit(ctx, bad), capture.ErrCanonical)
	bad = input(3, 1, common.Hash{4}, common.Hash{3})
	bad.Manifest.Datasets[0].Status = model.DatasetPruned
	require.ErrorIs(t, s.Commit(ctx, bad), model.ErrCaptureIncomplete)
	bad = input(3, 1, common.Hash{4}, common.Hash{3})
	bad.Payloads = bad.Payloads[:1]
	require.ErrorIs(t, s.Commit(ctx, bad), model.ErrCaptureIncomplete)
}

type failDB struct {
	port.CaptureDatabase
	at int
}

type uncertainDB struct{ port.CaptureDatabase }

func (db uncertainDB) CaptureUpdate(c context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	if err := db.CaptureDatabase.CaptureUpdate(c, ch, fn); err != nil {
		return err
	}
	return errInjected // durable commit succeeded, caller lost its response
}

type failWriter struct {
	port.CaptureWriter
	at, n int
}

var errInjected = errors.New("errInjected transaction failure")

func (w *failWriter) Put(k, v []byte) error {
	w.n++
	if w.n == w.at {
		return errInjected
	}
	return w.CaptureWriter.Put(k, v)
}
func (db failDB) CaptureUpdate(c context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	return db.CaptureDatabase.CaptureUpdate(c, ch, func(w port.CaptureWriter) error { return fn(&failWriter{CaptureWriter: w, at: db.at}) })
}

type corruptDB struct{ port.CaptureDatabase }
type corruptWriter struct{ port.CaptureWriter }

func (w corruptWriter) Get(k []byte) ([]byte, error) {
	if strings.HasSuffix(string(k), "/receipts") {
		return []byte("damaged permanent receipt bytes"), nil
	}
	return w.CaptureWriter.Get(k)
}
func (db corruptDB) CaptureUpdate(c context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	return db.CaptureDatabase.CaptureUpdate(c, ch, func(w port.CaptureWriter) error { return fn(corruptWriter{w}) })
}

func rollback(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	// Both payloads, manifest, canonical pointer, event and final cursor.
	for at := 1; at <= 6; at++ {
		broken := service(t, failDB{db, at}, o)
		require.ErrorIs(t, broken.Commit(ctx, a), errInjected)
		st, err := s.State(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(0), st.Through.Number)
		require.Zero(t, st.Sequence)
		_, err = s.Read(ctx, a.Manifest.Identity)
		require.ErrorIs(t, err, port.ErrNotFound)
		err = db.CaptureView(ctx, chain, func(r port.CaptureReader) error {
			_, e := r.Get([]byte("canonical/00000000000000000001"))
			require.ErrorIs(t, e, port.ErrNotFound)
			return nil
		})
		require.NoError(t, err)
	}
	require.NoError(t, s.Commit(ctx, a))
	// An ACK lost after commit must be distinguishable from a rollback:
	// retry reads the durable identity rather than emitting another event.
	b := input(2, 1, common.Hash{3}, common.Hash{2})
	require.ErrorIs(t, service(t, uncertainDB{db}, o).Commit(ctx, b), errInjected)
	require.NoError(t, s.Commit(ctx, b))
	events, err := s.Events(ctx, 0, 256)
	require.NoError(t, err)
	require.Len(t, events, 2)
}

func forkFencing(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s, "legacy")
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	b := input(2, 1, common.Hash{3}, common.Hash{2})
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.Commit(ctx, b))
	require.NoError(t, s.Finalize(ctx, 1, a.Manifest.Identity.Block))
	require.ErrorIs(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}), capture.ErrConflict)
	require.NoError(t, s.Reset(ctx, 1, a.Manifest.Identity.Block))
	require.ErrorIs(t, s.Commit(ctx, b), capture.ErrStale)
	o.fork(2, common.Hash{0xa3})
	b2 := input(2, 2, common.Hash{0xa3}, common.Hash{2})
	require.NoError(t, s.Commit(ctx, b2))
	old, err := s.Read(ctx, b.Manifest.Identity)
	require.NoError(t, err)
	require.Equal(t, b.Manifest.Identity, old.Manifest.Identity)
	require.ErrorIs(t, s.AckRaw(ctx, b.Manifest.Identity, "legacy"), capture.ErrConflict)
	// A raw consumer may finish an unchanged common ancestor after a reset.
	require.NoError(t, s.AckRaw(ctx, a.Manifest.Identity, "legacy"))
	p := a.Payloads[0]
	r := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Digest: sha256.Sum256(p.Bytes), PayloadBytes: int64(len(p.Bytes))}
	// The current-generation fence can authorize a retained, finalized
	// ancestor's older immutable manifest and ACK.
	require.NoError(t, s.WithDeletePermit(ctx, []window.Reference{r}, func() error { return nil }))
	require.ErrorIs(t, s.AckRaw(ctx, b2.Manifest.Identity, "unregistered"), port.ErrInvalidData)
	_, err = s.Events(ctx, 0, 0)
	require.ErrorIs(t, err, port.ErrInvalidData)
	events, err := s.Events(ctx, 0, 256)
	require.NoError(t, err)
	require.Len(t, events, 4)
	require.Equal(t, "capture_reset", events[2].Kind)
}

func windowGC(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s, "legacy")
	now := time.Now()
	root := t.TempDir()
	opts := window.Options{Volume: window.VolumeSpec{Root: root, ID: "capture-test", AllowDirectory: true}, Path: filepath.Join(root, "raw"), Chain: chain, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 2, MaxSegments: 16, Now: func() time.Time { return now }}
	require.NoError(t, window.InitializeVolume(opts.Volume))
	w, err := window.Create(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	var refs []window.Reference
	for _, p := range a.Payloads {
		r, e := w.Append(ctx, chainfeed.Envelope{Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Payload: p.Bytes})
		require.NoError(t, e)
		refs = append(refs, r)
	}
	require.NoError(t, w.Seal(ctx))
	now = now.Add(25 * time.Hour)
	n, err := w.Collect(ctx, s)
	require.Error(t, err)
	require.Zero(t, n)
	require.NoError(t, s.Commit(ctx, a))
	n, err = w.Collect(ctx, s)
	require.ErrorIs(t, err, window.ErrUnsafeGC)
	require.Zero(t, n)
	require.NoError(t, s.Finalize(ctx, 1, a.Manifest.Identity.Block))
	n, err = w.Collect(ctx, s)
	require.ErrorIs(t, err, port.ErrNotFound)
	require.Zero(t, n)
	require.NoError(t, s.AckRaw(ctx, a.Manifest.Identity, "legacy"))
	corrupt := service(t, corruptDB{db}, o)
	require.ErrorIs(t, corrupt.Commit(ctx, a), capture.ErrConflict)
	require.ErrorIs(t, corrupt.WithDeletePermit(ctx, refs[:1], func() error { t.Fatal("deleted despite missing required receipt data"); return nil }), capture.ErrConflict)
	// Wrong digest cannot substitute for permanent source bytes.
	wrong := append([]window.Reference(nil), refs...)
	wrong[0].Digest = common.Hash{0xfe}
	require.ErrorIs(t, s.WithDeletePermit(ctx, wrong, func() error { t.Fatal("unsafe deletion"); return nil }), window.ErrUnsafeGC)
	// The durable pin is visible through a separate snapshot and blocks a
	// generation transition BEFORE the callback can remove any file.
	require.NoError(t, s.WithDeletePermit(ctx, refs, func() error {
		st, e := s.State(ctx)
		require.NoError(t, e)
		require.NotEqual(t, common.Hash{}, st.PendingDelete)
		require.ErrorIs(t, s.Reset(ctx, 1, a.Manifest.Identity.Block), capture.ErrDeletePending)
		return nil
	}))
	st, err := s.State(ctx)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}, st.PendingDelete)
	lease, err := w.Acquire(ctx, refs[0])
	require.NoError(t, err)
	n, err = w.Collect(ctx, s)
	require.NoError(t, err)
	require.Zero(t, n)
	require.NoError(t, lease.Close())
	n, err = w.Collect(ctx, s)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = w.Lookup(ctx, refs[0].Block, refs[0].Kind)
	require.ErrorIs(t, err, window.ErrNotFound)
	got, err := s.Read(ctx, a.Manifest.Identity)
	require.NoError(t, err)
	require.ElementsMatch(t, a.Payloads, got.Payloads)
}

func kvContract(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	key := []byte("'\"; DROP TABLE capture_records; --\x00")
	value := []byte("payload\x00'; TRUNCATE capture_locks; --")
	var escaped port.CaptureWriter
	require.NoError(t, db.CaptureUpdate(ctx, chain, func(w port.CaptureWriter) error {
		escaped = w
		require.NoError(t, w.Put(key, value))
		got, e := w.Get(key)
		require.NoError(t, e)
		require.Equal(t, value, got)
		return nil
	}))
	require.ErrorIs(t, escaped.Put(key, []byte("later")), port.ErrClosed)
	_, err := escaped.Get(key)
	require.ErrorIs(t, err, port.ErrClosed)
	check := func(c model.FeedChain, k, v []byte) {
		t.Helper()
		require.NoError(t, db.CaptureView(ctx, c, func(r port.CaptureReader) error {
			got, e := r.Get(k)
			require.NoError(t, e)
			require.Equal(t, v, got)
			return nil
		}))
	}
	check(chain, key, value)
	other := chain
	other.GenesisHash = common.Hash{0xff}
	require.NoError(t, db.CaptureView(ctx, other, func(r port.CaptureReader) error {
		_, e := r.Get(key)
		require.ErrorIs(t, e, port.ErrNotFound)
		return nil
	}))
	require.ErrorIs(t, db.CaptureUpdate(ctx, chain, func(w port.CaptureWriter) error {
		require.NoError(t, w.Put(key, []byte("rolled-back")))
		return errInjected
	}), errInjected)
	check(chain, key, value)
	require.ErrorIs(t, db.CaptureUpdate(ctx, chain, func(w port.CaptureWriter) error {
		_ = w.Put(nil, value)
		_ = w.Put(key, []byte("ignored error"))
		return nil
	}), port.ErrInvalidData)
	check(chain, key, value)
	c, cancel := context.WithCancel(ctx)
	require.ErrorIs(t, db.CaptureUpdate(c, chain, func(w port.CaptureWriter) error {
		require.NoError(t, w.Put(key, []byte("cancelled")))
		cancel()
		return nil
	}), context.Canceled)
	check(chain, key, value)
	// Consistent snapshots do not observe a concurrent writer's second value.
	require.NoError(t, db.CaptureView(ctx, chain, func(r port.CaptureReader) error {
		first, e := r.Get(key)
		require.NoError(t, e)
		require.NoError(t, db.CaptureUpdate(ctx, chain, func(w port.CaptureWriter) error { return w.Put(key, []byte("next")) }))
		second, e := r.Get(key)
		require.NoError(t, e)
		require.Equal(t, first, second)
		return nil
	}))
}

func failedDeletionFence(t *testing.T, open opener) {
	db := open()
	s := service(t, db, newOracle())
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.Finalize(ctx, 1, a.Manifest.Identity.Block))
	p := a.Payloads[0]
	ref := window.Reference{StoreID: common.Hash{8}, Segment: 5, Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Digest: sha256.Sum256(p.Bytes), PayloadBytes: int64(len(p.Bytes))}
	require.ErrorIs(t, s.WithDeletePermit(ctx, []window.Reference{ref}, func() error { return errInjected }), errInjected)
	require.NoError(t, db.Close())
	db = open()
	defer func() { require.NoError(t, db.Close()) }()
	s = service(t, db, newOracle())
	intent, err := s.PendingDeletion(ctx)
	require.NoError(t, err)
	require.NotEqual(t, common.Hash{}, intent.Token)
	require.Equal(t, []window.Reference{ref}, intent.References)
	require.ErrorIs(t, s.Reset(ctx, 1, a.Manifest.Identity.Block), capture.ErrDeletePending)
	require.ErrorIs(t, s.WithDeletePermit(ctx, []window.Reference{ref}, func() error { t.Fatal("pending permit was bypassed"); return nil }), capture.ErrDeletePending)
	_, err = s.Read(ctx, a.Manifest.Identity)
	require.NoError(t, err)
}

func uncertainDeletionFence(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.Finalize(ctx, 1, a.Manifest.Identity.Block))
	p := a.Payloads[0]
	ref := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Digest: sha256.Sum256(p.Bytes), PayloadBytes: int64(len(p.Bytes))}
	uncertain := service(t, uncertainDB{db}, newOracle())
	require.ErrorIs(t, uncertain.WithDeletePermit(ctx, []window.Reference{ref}, func() error { t.Fatal("deletion on uncertain commit"); return nil }), errInjected)
	intent, err := s.PendingDeletion(ctx)
	require.NoError(t, err)
	require.Equal(t, []window.Reference{ref}, intent.References)
	require.ErrorIs(t, s.Reset(ctx, 1, a.Manifest.Identity.Block), capture.ErrDeletePending)
}

func concurrentUpdates(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- db.CaptureUpdate(ctx, chain, func(w port.CaptureWriter) error {
				v, e := w.Get([]byte("counter"))
				if e != nil && !errors.Is(e, port.ErrNotFound) {
					return e
				}
				n := byte(0)
				if len(v) > 0 {
					n = v[0]
				}
				return w.Put([]byte("counter"), []byte{n + 1})
			})
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.NoError(t, db.CaptureView(ctx, chain, func(r port.CaptureReader) error {
		v, e := r.Get([]byte("counter"))
		require.NoError(t, e)
		require.Equal(t, []byte{workers}, v)
		return nil
	}))
}

// Kill the process after durable facts commit or after GC-fence commit but
// before the callback can delete files/release the fence. No fake sync oracle.
func TestCaptureProcessCrash(t *testing.T) {
	if path := os.Getenv("INDEXER_CAPTURE_CRASH_PATH"); path != "" {
		db, err := storage.NewPebbleStorage(storage.DefaultConfig(path))
		require.NoError(t, err)
		s := service(t, db, newOracle())
		initialize(t, s)
		a := input(1, 1, common.Hash{2}, chain.GenesisHash)
		require.NoError(t, s.Commit(ctx, a))
		if os.Getenv("INDEXER_CAPTURE_CRASH_PHASE") == "facts" {
			os.Exit(17)
		}
		require.NoError(t, s.Finalize(ctx, 1, a.Manifest.Identity.Block))
		p := a.Payloads[0]
		ref := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Digest: sha256.Sum256(p.Bytes), PayloadBytes: int64(len(p.Bytes))}
		_ = s.WithDeletePermit(ctx, []window.Reference{ref}, func() error { os.Exit(17); return nil })
		t.Fatal("crash checkpoint was not reached")
	}
	for _, phase := range []string{"facts", "gc"} {
		t.Run(phase, func(t *testing.T) {
			path := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCaptureProcessCrash$")
			cmd.Env = append(os.Environ(), "INDEXER_CAPTURE_CRASH_PATH="+path, "INDEXER_CAPTURE_CRASH_PHASE="+phase)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(out))
			require.Equal(t, 17, exit.ExitCode(), string(out))
			db, e := storage.NewPebbleStorage(storage.DefaultConfig(path))
			require.NoError(t, e)
			defer func() { require.NoError(t, db.Close()) }()
			s := service(t, db, newOracle())
			st, e := s.State(ctx)
			require.NoError(t, e)
			require.Equal(t, uint64(1), st.Through.Number)
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			got, e := s.Read(ctx, a.Manifest.Identity)
			require.NoError(t, e)
			require.ElementsMatch(t, a.Payloads, got.Payloads)
			events, e := s.Events(ctx, 0, 256)
			require.NoError(t, e)
			require.Len(t, events, 1)
			if phase == "gc" {
				require.NotEqual(t, common.Hash{}, st.PendingDelete)
				intent, e := s.PendingDeletion(ctx)
				require.NoError(t, e)
				require.Equal(t, st.PendingDelete, intent.Token)
				require.Len(t, intent.References, 1)
				require.ErrorIs(t, s.Reset(ctx, 1, a.Manifest.Identity.Block), capture.ErrDeletePending)
			} else {
				require.Equal(t, common.Hash{}, st.PendingDelete)
			}
		})
	}
}

// Keep cancellation/races bounded without relying on sleeps.
func TestCaptureConcurrentGenerationFence(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.Finalize(ctx, 1, a.Manifest.Identity.Block))
	p := a.Payloads[0]
	ref := window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Digest: sha256.Sum256(p.Bytes), PayloadBytes: int64(len(p.Bytes))}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	var called atomic.Bool
	go func() {
		done <- s.WithDeletePermit(ctx, []window.Reference{ref}, func() error { called.Store(true); close(entered); <-release; return nil })
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("no permit callback")
	}
	err := s.Reset(ctx, 1, a.Manifest.Identity.Block)
	close(release)
	require.ErrorIs(t, err, capture.ErrDeletePending)
	require.NoError(t, <-done)
	require.True(t, called.Load())
	require.NoError(t, s.Reset(ctx, 1, a.Manifest.Identity.Block))
}
