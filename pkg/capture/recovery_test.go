package capture_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/window"
)

func rawOptions(root string) window.Options {
	return window.Options{Volume: window.VolumeSpec{Root: root, ID: "capture-recovery", AllowDirectory: true}, Path: filepath.Join(root, "raw"), Chain: chain, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 2, MaxSegments: 16}
}

func recoveryRaw(t *testing.T, root string) (*window.Store, window.Options, []window.Reference) {
	t.Helper()
	opts := rawOptions(root)
	now := time.Now().Add(-25 * time.Hour)
	opts.Now = func() time.Time { return now }
	require.NoError(t, window.InitializeVolume(opts.Volume))
	raw, err := window.Create(opts)
	require.NoError(t, err)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	refs := []window.Reference{}
	for _, p := range a.Payloads {
		r, e := raw.Append(ctx, chainfeed.Envelope{Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Payload: p.Bytes})
		require.NoError(t, e)
		refs = append(refs, r)
	}
	require.NoError(t, raw.Seal(ctx))
	now = now.Add(25 * time.Hour)
	return raw, opts, refs
}

func recoveryFacts(t *testing.T, db port.CaptureDatabase) *capture.Store {
	t.Helper()
	s := service(t, db, newOracle())
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.Finalize(ctx, 1, a.Manifest.Identity.Block))
	return s
}

type failUpdateDB struct {
	port.CaptureDatabase
	at, n int
}

type countReadsDB struct {
	port.CaptureDatabase
	receipts atomic.Int32
}
type countReadsWriter struct {
	port.CaptureWriter
	db *countReadsDB
}

func (w countReadsWriter) Get(k []byte) ([]byte, error) {
	if strings.HasSuffix(string(k), "/receipts") {
		w.db.receipts.Add(1)
	}
	return w.CaptureWriter.Get(k)
}
func (db *countReadsDB) CaptureUpdate(c context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	return db.CaptureDatabase.CaptureUpdate(c, ch, func(w port.CaptureWriter) error { return fn(countReadsWriter{w, db}) })
}
func gcVerifiesBlockOnce(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	recoveryFacts(t, db)
	w, _, refs := recoveryRaw(t, t.TempDir())
	defer func() { require.NoError(t, w.Close()) }()
	counted := &countReadsDB{CaptureDatabase: db}
	s := service(t, counted, newOracle())
	for i := int32(1); i <= 2; i++ {
		require.NoError(t, s.WithDeletePermit(ctx, refs, func() error { return nil }))
		require.Equal(t, i, counted.receipts.Load(), "all references share one full block verification per transaction")
	}
}

func (db *failUpdateDB) CaptureUpdate(c context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	db.n++
	if db.n == db.at {
		return errInjected
	}
	return db.CaptureDatabase.CaptureUpdate(c, ch, fn)
}

type missingIntentDB struct{ port.CaptureDatabase }
type missingIntentReader struct{ port.CaptureReader }

func (r missingIntentReader) Get(k []byte) ([]byte, error) {
	if string(k) == "deletion_intent" {
		return nil, port.ErrNotFound
	}
	return r.CaptureReader.Get(k)
}
func (db missingIntentDB) CaptureView(c context.Context, ch model.FeedChain, fn func(port.CaptureReader) error) error {
	return db.CaptureDatabase.CaptureView(c, ch, func(r port.CaptureReader) error { return fn(missingIntentReader{r}) })
}

func deletionRecovery(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	s := recoveryFacts(t, db)
	raw, opts, refs := recoveryRaw(t, t.TempDir())
	defer func() { require.NoError(t, raw.Close()) }()
	require.ErrorIs(t, s.WithDeletePermit(ctx, refs, func() error { return errInjected }), errInjected)
	// Missing intent metadata is a conflict, not "no pending deletion".
	require.ErrorIs(t, service(t, missingIntentDB{db}, newOracle()).RecoverDeletion(ctx, raw), capture.ErrConflict)
	wrong, _, _ := recoveryRaw(t, t.TempDir())
	require.ErrorIs(t, s.RecoverDeletion(ctx, wrong), window.ErrConflict)
	require.NoError(t, wrong.Close())
	lease, err := raw.Acquire(ctx, refs[0])
	require.NoError(t, err)
	require.ErrorIs(t, s.RecoverDeletion(ctx, raw), window.ErrBusy)
	require.NoError(t, lease.Close())
	bad := service(t, corruptDB{db}, newOracle())
	require.ErrorIs(t, bad.RecoverDeletion(ctx, raw), capture.ErrConflict)
	_, err = raw.Lookup(ctx, refs[0].Block, refs[0].Kind)
	require.NoError(t, err)
	c, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, s.RecoverDeletion(c, raw), context.Canceled)
	// The disk phase succeeds but DB completion fails. The next process
	// must use the durable receipt rather than treating absence as success.
	failed := service(t, &failUpdateDB{CaptureDatabase: db, at: 2}, newOracle())
	require.ErrorIs(t, failed.RecoverDeletion(ctx, raw), errInjected)
	st, err := s.State(ctx)
	require.NoError(t, err)
	require.NotEqual(t, common.Hash{}, st.PendingDelete)
	require.NoError(t, raw.Close())
	raw, err = window.Open(opts)
	require.NoError(t, err)
	require.NoError(t, s.RecoverDeletion(ctx, raw))
	st, err = s.State(ctx)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}, st.PendingDelete)
	require.NoError(t, s.RecoverDeletion(ctx, raw))
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	got, err := s.Read(ctx, a.Manifest.Identity)
	require.NoError(t, err)
	require.ElementsMatch(t, a.Payloads, got.Payloads)
	require.NoError(t, s.Reset(ctx, 1, a.Manifest.Identity.Block))
}

func missingDeletionReceipt(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	s := recoveryFacts(t, db)
	raw, opts, refs := recoveryRaw(t, t.TempDir())
	require.ErrorIs(t, s.WithDeletePermit(ctx, refs, func() error { return errInjected }), errInjected)
	require.NoError(t, raw.Close())
	for _, suffix := range []string{"raw", "idx", "seal"} {
		require.NoError(t, os.Remove(filepath.Join(opts.Path, "00000000000000000001."+suffix)))
	}
	raw, err := window.Open(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, raw.Close()) }()
	require.ErrorIs(t, s.RecoverDeletion(ctx, raw), os.ErrNotExist)
	st, err := s.State(ctx)
	require.NoError(t, err)
	require.NotEqual(t, common.Hash{}, st.PendingDelete)
}

type observingDB struct {
	port.CaptureDatabase
	read chan struct{}
	once sync.Once
}
type observingReader struct {
	port.CaptureReader
	db *observingDB
}

func (r observingReader) Get(k []byte) ([]byte, error) {
	v, err := r.CaptureReader.Get(k)
	if err == nil && string(k) == "deletion_intent" {
		r.db.once.Do(func() { close(r.db.read) })
	}
	return v, err
}
func (db *observingDB) CaptureView(c context.Context, ch model.FeedChain, fn func(port.CaptureReader) error) error {
	return db.CaptureDatabase.CaptureView(c, ch, func(r port.CaptureReader) error { return fn(observingReader{r, db}) })
}

type heldGuard struct {
	facts            *capture.Store
	entered, release chan struct{}
}

func (g heldGuard) WithDeletePermit(c context.Context, refs []window.Reference, fn func() error) error {
	return g.facts.WithDeletePermit(c, refs, func() error {
		if err := fn(); err != nil {
			return err
		}
		close(g.entered)
		<-g.release
		return nil
	})
}
func recoveryExcludesActiveGC(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	s := recoveryFacts(t, db)
	raw, _, _ := recoveryRaw(t, t.TempDir())
	defer func() { require.NoError(t, raw.Close()) }()
	entered, release := make(chan struct{}), make(chan struct{})
	collected := make(chan error, 1)
	go func() { _, err := raw.Collect(ctx, heldGuard{s, entered, release}); collected <- err }()
	select {
	case <-entered:
	case err := <-collected:
		t.Fatalf("no live deletion: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("no GC callback")
	}
	observed := &observingDB{CaptureDatabase: db, read: make(chan struct{})}
	other := service(t, observed, newOracle())
	recovered := make(chan error, 1)
	go func() { recovered <- other.RecoverDeletion(ctx, raw) }()
	select {
	case <-observed.read:
	case <-time.After(10 * time.Second):
		t.Fatal("recovery did not read old intent")
	}
	// Another coordinator has the old DB token, but cannot get past gcMu
	// while the actual Collect callback/DB completion is still active.
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.ErrorIs(t, s.Reset(ctx, 1, a.Manifest.Identity.Block), capture.ErrDeletePending)
	close(release)
	require.NoError(t, <-collected)
	require.ErrorIs(t, <-recovered, capture.ErrStale)
	require.NoError(t, s.Reset(ctx, 1, a.Manifest.Identity.Block))
}

type crashingGuard struct {
	facts *capture.Store
	phase string
}

type obstructedGuard struct {
	facts      *capture.Store
	root, path string
}

func (g obstructedGuard) WithDeletePermit(c context.Context, refs []window.Reference, fn func() error) error {
	return g.facts.WithDeletePermit(c, refs, func() error {
		idx := filepath.Join(g.path, "00000000000000000001.idx")
		if err := os.Rename(idx, filepath.Join(g.root, "saved-index")); err != nil {
			return err
		}
		if err := os.Mkdir(idx, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(idx, "obstacle"), []byte("I/O failure fixture"), 0600); err != nil {
			return err
		}
		return fn() // durable .gc + raw unlink, then nonempty idx directory fails
	})
}
func checkedOpenBeforeUnlink(t *testing.T, open opener) {
	db := open()
	defer func() { require.NoError(t, db.Close()) }()
	s := recoveryFacts(t, db)
	root := t.TempDir()
	raw, opts, _ := recoveryRaw(t, root)
	_, err := raw.Collect(ctx, obstructedGuard{s, root, opts.Path})
	require.Error(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, os.RemoveAll(filepath.Join(opts.Path, "00000000000000000001.idx")))
	// The remaining seal/gc must survive failed DB revalidation at Open,
	// even though the raw file was already removed by the interrupted GC.
	_, err = service(t, corruptDB{db}, newOracle()).OpenWindow(ctx, opts)
	require.ErrorIs(t, err, capture.ErrConflict)
	for _, suffix := range []string{"seal", "gc"} {
		_, err := os.Stat(filepath.Join(opts.Path, "00000000000000000001."+suffix))
		require.NoError(t, err)
	}
	st, err := s.State(ctx)
	require.NoError(t, err)
	require.NotEqual(t, common.Hash{}, st.PendingDelete)
	raw, err = s.OpenWindow(ctx, opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, raw.Close()) }()
	st, err = s.State(ctx)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}, st.PendingDelete)
}

func (g crashingGuard) WithDeletePermit(c context.Context, refs []window.Reference, fn func() error) error {
	return g.facts.WithDeletePermit(c, refs, func() error {
		if g.phase == "before-files" {
			os.Exit(17)
		}
		if err := fn(); err != nil {
			return err
		}
		os.Exit(17)
		return nil
	})
}

func TestCaptureWindowRecoveryProcessCrash(t *testing.T) {
	if root := os.Getenv("INDEXER_CAPTURE_RECOVERY_ROOT"); root != "" {
		db, err := storage.NewPebbleStorage(storage.DefaultConfig(filepath.Join(root, "db")))
		require.NoError(t, err)
		s := recoveryFacts(t, db)
		raw, _, _ := recoveryRaw(t, root)
		_, _ = raw.Collect(ctx, crashingGuard{s, os.Getenv("INDEXER_CAPTURE_RECOVERY_PHASE")})
		t.Fatal("crash checkpoint not reached")
	}
	for _, phase := range []string{"before-files", "after-files"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCaptureWindowRecoveryProcessCrash$")
			cmd.Env = append(os.Environ(), "INDEXER_CAPTURE_RECOVERY_ROOT="+root, "INDEXER_CAPTURE_RECOVERY_PHASE="+phase)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(out))
			require.Equal(t, 17, exit.ExitCode(), string(out))
			db, err := storage.NewPebbleStorage(storage.DefaultConfig(filepath.Join(root, "db")))
			require.NoError(t, err)
			defer func() { require.NoError(t, db.Close()) }()
			s := service(t, db, newOracle())
			intent, err := s.PendingDeletion(ctx)
			require.NoError(t, err)
			raw, err := s.OpenWindow(ctx, rawOptions(root))
			require.NoError(t, err)
			defer func() { require.NoError(t, raw.Close()) }()
			require.NoError(t, s.RecoverDeletion(ctx, raw))
			st, err := s.State(ctx)
			require.NoError(t, err)
			require.Equal(t, common.Hash{}, st.PendingDelete)
			_, err = raw.Lookup(ctx, intent.References[0].Block, intent.References[0].Kind)
			require.ErrorIs(t, err, window.ErrNotFound)
			events, err := s.Events(ctx, 0, 256)
			require.NoError(t, err)
			require.Len(t, events, 1)
		})
	}
}

func TestSessionWorkerJoin(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	opts := rawOptions(t.TempDir())
	require.NoError(t, window.InitializeVolume(opts.Volume))
	refs := make(chan window.Reference, 2)
	entered := make(chan struct{})
	stopped := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	op := capture.SessionOptions{Window: opts, Plan: plan, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, Create: true}
	errPeer := errors.New("peer shutdown error")
	go func() {
		done <- capture.RunSession(ctx, db, newOracle(), op, func(c context.Context, w *window.Store, _ *capture.Store) error {
			for _, p := range input(1, 1, common.Hash{2}, chain.GenesisHash).Payloads {
				r, err := w.Append(c, chainfeed.Envelope{Block: input(1, 1, common.Hash{2}, chain.GenesisHash).Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Payload: p.Bytes})
				if err != nil {
					return err
				}
				refs <- r
			}
			<-entered
			return errInjected
		}, func(c context.Context, w *window.Store, s *capture.Store) error {
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			a.Payloads = nil
			leases := []*window.Lease{}
			defer func() {
				for _, l := range leases {
					_ = l.Close()
				}
			}()
			for i := 0; i < 2; i++ {
				select {
				case r := <-refs:
					l, err := w.Acquire(c, r)
					if err != nil {
						return err
					}
					leases = append(leases, l)
					e, err := l.Envelope()
					if err != nil {
						return err
					}
					a.Payloads = append(a.Payloads, capture.Payload{Kind: e.Kind, Bytes: e.Payload})
				case <-c.Done():
					return c.Err()
				}
			}
			if err := s.Commit(c, a); err != nil {
				return err
			}
			close(entered)
			<-c.Done()
			close(stopped)
			<-release
			return errors.Join(c.Err(), errPeer)
		})
	}()
	select {
	case <-stopped:
	case err := <-done:
		t.Fatalf("session stopped prematurely: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("workers did not cooperate")
	}
	_, err := window.Open(opts)
	require.ErrorIs(t, err, window.ErrLocked)
	close(release)
	err = <-done
	require.ErrorIs(t, err, errInjected)
	require.ErrorIs(t, err, errPeer)
	w, err := window.Open(opts)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	s := service(t, db, newOracle())
	_, err = s.Read(ctx, input(1, 1, common.Hash{2}, chain.GenesisHash).Manifest.Identity)
	require.NoError(t, err)
}

func TestSessionRecoversBeforeWorkers(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := recoveryFacts(t, db)
	w, opts, refs := recoveryRaw(t, t.TempDir())
	require.ErrorIs(t, s.WithDeletePermit(ctx, refs, func() error { return errInjected }), errInjected)
	require.NoError(t, w.Close())
	createOpts := opts
	createOpts.Path = filepath.Join(opts.Volume.Root, "must-not-create")
	noStart := func(context.Context, *window.Store, *capture.Store) error {
		t.Fatal("worker started in replacement window")
		return nil
	}
	require.ErrorIs(t, capture.RunSession(ctx, db, newOracle(), capture.SessionOptions{Window: createOpts, Plan: plan, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, Create: true}, noStart, noStart), capture.ErrDeletePending)
	_, statErr := os.Stat(createOpts.Path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	started := make(chan error, 2)
	done := make(chan error, 1)
	task := func(c context.Context, _ *window.Store, f *capture.Store) error {
		st, err := f.State(c)
		if err == nil && st.PendingDelete != (common.Hash{}) {
			err = errors.New("started with pending deletion")
		}
		started <- err
		<-c.Done()
		return c.Err()
	}
	go func() {
		done <- capture.RunSession(c, db, newOracle(), capture.SessionOptions{Window: opts, Plan: plan, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}}, task, task)
	}()
	for i := 0; i < 2; i++ {
		select {
		case err := <-started:
			require.NoError(t, err)
		case err := <-done:
			t.Fatalf("session start failed: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("no startup")
		}
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	// A missing raw path on restart is never created as a replacement.
	_, err := s.State(ctx)
	require.NoError(t, err)
	missing := opts
	missing.Path = filepath.Join(opts.Volume.Root, "missing")
	var called atomic.Bool
	noStart = func(context.Context, *window.Store, *capture.Store) error { called.Store(true); return nil }
	require.Error(t, capture.RunSession(ctx, db, newOracle(), capture.SessionOptions{Window: missing, Plan: plan, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}}, noStart, noStart))
	require.False(t, called.Load())
	_, err = os.Stat(missing.Path)
	require.ErrorIs(t, err, os.ErrNotExist)
}
