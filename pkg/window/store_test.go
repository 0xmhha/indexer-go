package window

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) get() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

func fixture(t *testing.T) (Options, *testClock) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	root := t.TempDir()
	opts := Options{Volume: VolumeSpec{Root: root, ID: "fixture", AllowDirectory: true}, Path: filepath.Join(root, "chain"),
		Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048,
		MaxSegmentRecords: 2, MaxSegments: 16, ReadBudgetBytes: 64 << 10, MaxReaders: 2, Now: clock.get}
	require.NoError(t, InitializeVolume(opts.Volume))
	return opts, clock
}

func envelope(opts Options, n uint64) chainfeed.Envelope {
	return chainfeed.Envelope{Block: model.FeedBlock{Chain: opts.Chain, Number: n, Hash: common.BigToHash(newUint(n + 1))}, Kind: model.DatasetBodies, Codec: "fixture.v1", Payload: []byte("block " + strconv.FormatUint(n, 10))}
}

// Keep fixture hashes independent of integer truncation at 256 heights.
func newUint(n uint64) *big.Int { return new(big.Int).SetUint64(n) }

func created(t *testing.T, opts Options) *Store {
	t.Helper()
	s, err := Create(opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}

func TestDurableWindowRoundTripAndBoundedIndex(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	var refs []Reference
	for n := uint64(1); n <= 7; n++ {
		r, err := s.Append(ctx, envelope(opts, n))
		require.NoError(t, err)
		refs = append(refs, r)
		clock.advance(time.Second)
	}
	stats := s.Stats()
	assert.Equal(t, 7, stats.Records)
	assert.LessOrEqual(t, stats.ActiveIndexRecords, opts.MaxSegmentRecords)
	lease, err := s.Acquire(ctx, refs[0])
	require.NoError(t, err)
	e, err := lease.Envelope()
	require.NoError(t, err)
	assert.Equal(t, envelope(opts, 1), e)
	require.NoError(t, lease.Close())
	require.NoError(t, s.Close())
	s2, err := Open(opts)
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	assert.Zero(t, s2.Stats().ActiveIndexRecords, "sealed records stay indexed on disk")
	for _, r := range refs {
		got, err := s2.Lookup(ctx, r.Block, r.Kind)
		require.NoError(t, err)
		assert.Equal(t, r, got)
	}
	clock.advance(20 * time.Hour)
	got, err := s2.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	assert.Equal(t, refs[0], got, "duplicates do not refresh retention")
	conflict := envelope(opts, 1)
	conflict.Payload = []byte("different")
	_, err = s2.Append(ctx, conflict)
	require.ErrorIs(t, err, ErrConflict)
	assert.Equal(t, 7, s2.Stats().Records)
}

func TestWindowRejectsUnregisteredMissingOrReplacedVolume(t *testing.T) {
	opts, _ := fixture(t)
	missing := opts
	missing.Volume.Root = filepath.Join(t.TempDir(), "missing")
	missing.Path = filepath.Join(missing.Volume.Root, "window")
	_, err := Create(missing)
	require.Error(t, err)
	_, err = os.Stat(missing.Volume.Root)
	require.ErrorIs(t, err, os.ErrNotExist)
	wrong := opts
	wrong.Volume.ID = "another"
	_, err = Create(wrong)
	require.ErrorIs(t, err, ErrConflict)
	unmounted := opts
	unmounted.Volume.AllowDirectory = false
	_, err = Create(unmounted)
	require.ErrorIs(t, err, ErrUnavailable)
	s := created(t, opts)
	_, err = Open(opts)
	require.ErrorIs(t, err, ErrLocked)
	_, err = s.Append(context.Background(), envelope(opts, 1))
	require.NoError(t, err)
	detached := opts.Volume.Root + "-detached"
	require.NoError(t, os.Rename(opts.Volume.Root, detached))
	t.Cleanup(func() { _ = os.RemoveAll(detached) })
	require.NoError(t, os.Mkdir(opts.Volume.Root, 0700))
	_, err = s.Append(context.Background(), envelope(opts, 2))
	require.ErrorIs(t, err, ErrUnavailable)
	entries, err := os.ReadDir(opts.Volume.Root)
	require.NoError(t, err)
	assert.Empty(t, entries, "no fallback data or marker created")
	require.NoError(t, s.Close())
	require.NoError(t, os.Remove(opts.Volume.Root))
	require.NoError(t, os.Rename(detached, opts.Volume.Root))
	s2, err := Open(opts)
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	assert.Equal(t, 1, s2.Stats().Records)
}

func TestWindowRejectsPathEscapeAndMarkerChange(t *testing.T) {
	opts, _ := fixture(t)
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, opts.Path))
	_, err := Create(opts)
	require.Error(t, err)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries)
	require.NoError(t, os.Remove(opts.Path))
	s := created(t, opts)
	marker, _ := json.Marshal(volumeMarker{Version: 1, ID: "changed"})
	require.NoError(t, os.WriteFile(filepath.Join(opts.Volume.Root, ".indexer-volume.json"), marker, 0600))
	_, err = s.Append(context.Background(), envelope(opts, 1))
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestWindowCapacityAndReadBackpressure(t *testing.T) {
	opts, _ := fixture(t)
	opts.MaxSegments = 1
	opts.MaxSegmentRecords = 1
	opts.MaxReaders = 1
	s := created(t, opts)
	ctx := context.Background()
	r, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	_, err = s.Append(ctx, envelope(opts, 2))
	require.ErrorIs(t, err, ErrCapacity)
	lease, err := s.Acquire(ctx, r)
	require.NoError(t, err)
	_, err = s.Acquire(ctx, r)
	require.ErrorIs(t, err, ErrBusy)
	require.ErrorIs(t, s.Close(), ErrBusy)
	forged := r
	forged.FrameBytes = 1 << 50
	_, err = s.Acquire(ctx, forged)
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, lease.Close())
	require.NoError(t, lease.Close())
	_, err = lease.Envelope()
	require.ErrorIs(t, err, ErrClosed)
	assert.Zero(t, s.Stats().ReadReservedBytes)
	assert.Equal(t, 1, s.Stats().Records, "capacity does not evict unprocessed data")
}

func TestPagedResumePreservesAppendOrderAndDetectsExpiredCursor(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	var expected []Reference
	for _, n := range []uint64{7, 2, 9, 1, 4} {
		r, err := s.Append(ctx, envelope(opts, n))
		require.NoError(t, err)
		expected = append(expected, r)
	}
	require.NoError(t, s.Close())
	s2, err := Open(opts)
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	var all []Reference
	var cursor, first Cursor
	for {
		items, next, err := s2.Page(ctx, cursor, 2)
		require.NoError(t, err)
		if len(items) == 0 {
			break
		}
		all = append(all, items...)
		cursor = next
		if first.Segment == 0 {
			first = cursor
		}
	}
	assert.Equal(t, expected, all, "closed sorted indexes must resume in raw append order")
	bad := cursor
	bad.Offset--
	_, _, err = s2.Page(ctx, bad, 2)
	require.ErrorIs(t, err, ErrConflict)
	clock.advance(24 * time.Hour)
	_, err = s2.Collect(ctx, approved)
	require.NoError(t, err)
	_, _, err = s2.Page(ctx, first, 2)
	require.ErrorIs(t, err, ErrCursorExpired, "never silently skip collected input")
}

type guardFunc func(context.Context, []Reference, func() error) error

func (f guardFunc) WithDeletePermit(ctx context.Context, refs []Reference, fn func() error) error {
	return f(ctx, refs, fn)
}

var approved = guardFunc(func(_ context.Context, _ []Reference, fn func() error) error { return fn() }) // test-only DB/finality oracle

func TestLeaseBlocksCollectionAcrossSeal(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	r, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	lease, err := s.Acquire(ctx, r)
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	clock.advance(24 * time.Hour)
	n, err := s.Collect(ctx, approved)
	require.NoError(t, err)
	assert.Zero(t, n)
	require.NoError(t, lease.Close())
	_, err = s.Collect(ctx, nil)
	require.ErrorIs(t, err, ErrUnsafeGC)
	n, err = s.Collect(ctx, guardFunc(func(context.Context, []Reference, func() error) error { return nil }))
	require.ErrorIs(t, err, ErrUnsafeGC)
	assert.Zero(t, n)
	n, err = s.Collect(ctx, approved)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	_, err = s.Lookup(ctx, r.Block, r.Kind)
	require.ErrorIs(t, err, ErrNotFound)
	r2, err := s.Append(ctx, envelope(opts, 2))
	require.NoError(t, err)
	assert.Greater(t, r2.Segment, r.Segment, "segment IDs are never reused after GC")
}

func TestCollectionRechecksLeaseAndRetentionAfterApproval(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	r, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	clock.advance(24*time.Hour - time.Nanosecond)
	n, err := s.Collect(ctx, approved)
	require.NoError(t, err)
	assert.Zero(t, n)
	clock.advance(time.Nanosecond)
	var lease *Lease
	race := guardFunc(func(_ context.Context, refs []Reference, fn func() error) error {
		require.Equal(t, []Reference{r}, refs)
		lease, err = s.Acquire(ctx, r)
		require.NoError(t, err)
		return fn()
	})
	n, err = s.Collect(ctx, race)
	require.ErrorIs(t, err, ErrBusy)
	assert.Zero(t, n)
	require.NoError(t, lease.Close())
	denied := errors.New("missing persisted state/finality")
	n, err = s.Collect(ctx, guardFunc(func(context.Context, []Reference, func() error) error { return denied }))
	require.ErrorIs(t, err, denied)
	assert.Zero(t, n)
	clock.advance(-time.Hour)
	n, err = s.Collect(ctx, approved)
	require.ErrorIs(t, err, ErrUnsafeGC)
	assert.Zero(t, n)
}

func TestCollectionRejectsLateOrHiddenFailureCallbacks(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	ref, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	clock.advance(24 * time.Hour)
	var later func() error
	_, err = s.Collect(ctx, guardFunc(func(_ context.Context, _ []Reference, fn func() error) error { later = fn; return nil }))
	require.ErrorIs(t, err, ErrUnsafeGC)
	require.ErrorIs(t, later(), ErrUnsafeGC)
	_, err = s.Lookup(ctx, ref.Block, ref.Kind)
	require.NoError(t, err)
	var held *Lease
	_, err = s.Collect(ctx, guardFunc(func(_ context.Context, _ []Reference, fn func() error) error {
		held, err = s.Acquire(ctx, ref)
		require.NoError(t, err)
		_ = fn()
		return nil // incorrectly discards the busy error
	}))
	require.ErrorIs(t, err, ErrBusy)
	require.NoError(t, held.Close())
}

func crashClose(t *testing.T, s *Store) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	require.NoError(t, s.closeHandles())
}

func TestTailRecoveryAndChecksums(t *testing.T) {
	for _, kind := range []string{"short-header", "partial-payload", "corrupt-header", "corrupt-payload"} {
		t.Run(kind, func(t *testing.T) {
			opts, _ := fixture(t)
			s := created(t, opts)
			ctx := context.Background()
			r, err := s.Append(ctx, envelope(opts, 1))
			require.NoError(t, err)
			crashClose(t, s)
			path := filepath.Join(opts.Path, segmentName(r.Segment, "raw"))
			f, err := os.OpenFile(path, os.O_RDWR, 0600)
			require.NoError(t, err)
			switch kind {
			case "short-header":
				_, err = f.WriteAt([]byte(frameMagic), r.FrameBytes)
			case "partial-payload":
				h, m, e := makeFrame(r.StoreID, envelope(opts, 2), r.FirstReceived)
				require.NoError(t, e)
				_, err = f.WriteAt(append(append(h, m...), 1), r.FrameBytes)
			case "corrupt-header":
				_, err = f.WriteAt([]byte{255}, 12)
			case "corrupt-payload":
				_, err = f.WriteAt([]byte{255}, r.FrameBytes-1)
			}
			require.NoError(t, err)
			require.NoError(t, f.Sync())
			require.NoError(t, f.Close())
			before, err := os.Stat(path)
			require.NoError(t, err)
			s2, err := Open(opts)
			if kind == "corrupt-header" || kind == "corrupt-payload" {
				require.ErrorIs(t, err, ErrCorrupt)
				after, e := os.Stat(path)
				require.NoError(t, e)
				assert.Equal(t, before.Size(), after.Size(), "corruption must not be truncated as an unfinished append")
				return
			}
			require.NoError(t, err)
			defer func() { _ = s2.Close() }()
			got, err := s2.Lookup(ctx, r.Block, r.Kind)
			require.NoError(t, err)
			assert.Equal(t, r, got)
			after, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, r.FrameBytes, after.Size())
		})
	}
}

func TestCrashAfterFsyncRecoversUnpublishedRecord(t *testing.T) {
	opts, _ := fixture(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowCrashHelper$")
	cmd.Env = append(os.Environ(), "INDEXER_WINDOW_CRASH_ROOT="+opts.Volume.Root)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	s, err := Open(opts)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ref, err := s.Lookup(context.Background(), envelope(opts, 1).Block, model.DatasetBodies)
	require.NoError(t, err)
	lease, err := s.Acquire(context.Background(), ref)
	require.NoError(t, err)
	defer func() { _ = lease.Close() }()
	got, err := lease.Envelope()
	require.NoError(t, err)
	assert.Equal(t, envelope(opts, 1).Payload, got.Payload)
}

func TestWindowCrashHelper(t *testing.T) {
	root := os.Getenv("INDEXER_WINDOW_CRASH_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	opts := Options{Volume: VolumeSpec{Root: root, ID: "fixture", AllowDirectory: true}, Path: filepath.Join(root, "chain"), Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 2, MaxSegments: 16}
	s, err := Create(opts)
	require.NoError(t, err)
	s.afterDataSync = func() { os.Exit(0) }
	_, err = s.Append(context.Background(), envelope(opts, 1))
	require.NoError(t, err)
	t.Fatal("crash hook was not called")
}

func TestGCMarkerRecoveryDoesNotResurrectDeletedInputs(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	r, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	require.NoError(t, writeJSON(s.root, segmentName(r.Segment, "gc"), gcMarker{StoreID: s.id, Segment: r.Segment}))
	require.NoError(t, s.root.Remove(segmentName(r.Segment, "idx")))
	crashClose(t, s)
	s2, err := Open(opts)
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	assert.Zero(t, s2.Stats().Records)
	_, err = s2.Lookup(ctx, r.Block, r.Kind)
	require.ErrorIs(t, err, ErrNotFound)
	r2, err := s2.Append(ctx, envelope(opts, 2))
	require.NoError(t, err)
	assert.Greater(t, r2.Segment, r.Segment)
}

func TestConcurrentAppendIsIdempotent(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Append(ctx, envelope(opts, 1)); results <- err }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, s.Stats().Records)
}

func TestConcurrentReadAppendAndCollection(t *testing.T) {
	opts, clock := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	ref, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	clock.advance(24 * time.Hour)
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 16; i++ {
			lease, err := s.Acquire(ctx, ref)
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrBusy) {
				continue
			}
			if err != nil {
				errs <- err
				return
			}
			e, err := lease.Envelope()
			if err == nil && !bytes.Equal(e.Payload, envelope(opts, 1).Payload) {
				err = ErrCorrupt
			}
			closeErr := lease.Close()
			if err != nil {
				errs <- err
				return
			}
			if closeErr != nil {
				errs <- closeErr
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for n := uint64(2); n <= 8; n++ {
			if _, err := s.Append(ctx, envelope(opts, n)); err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 16; i++ {
			if _, err := s.Collect(ctx, approved); err != nil && !errors.Is(err, ErrBusy) {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Zero(t, s.Stats().Readers)
	require.Zero(t, s.Stats().ReadReservedBytes)
	for n := uint64(2); n <= 8; n++ {
		_, err := s.Lookup(ctx, envelope(opts, n).Block, model.DatasetBodies)
		require.NoError(t, err, "new inputs must survive collection")
	}
}

func TestLogicalByteLimitDoesNotEvict(t *testing.T) {
	opts, _ := fixture(t)
	opts.MaxSegments = 128
	norm, err := opts.normalized()
	require.NoError(t, err)
	opts.MaxBytes = opts.SegmentBytes + norm.reservation()
	s := created(t, opts)
	ctx := context.Background()
	var last Reference
	for n := uint64(1); n <= 128; n++ {
		e := envelope(opts, n)
		e.Payload = bytes.Repeat([]byte{byte(n)}, 1024)
		r, err := s.Append(ctx, e)
		if errors.Is(err, ErrCapacity) {
			require.NotZero(t, last.Segment)
			assert.LessOrEqual(t, s.Stats().FileBytes, opts.MaxBytes)
			_, err = s.Lookup(ctx, last.Block, last.Kind)
			require.NoError(t, err)
			return
		}
		require.NoError(t, err)
		last = r
	}
	t.Fatal("byte limit failed to apply backpressure")
}

func TestSealedIndexCorruptionFailsClosed(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	ref, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	f, err := os.OpenFile(filepath.Join(opts.Path, segmentName(ref.Segment, "idx")), os.O_RDWR, 0600)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte{255}, indexHeader+41)
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	require.NoError(t, f.Close())
	_, err = Open(opts)
	require.ErrorIs(t, err, ErrCorrupt)
}

func TestWindowRefusesOversizeCancelledAndNoSpaceWrites(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	ctx2, cancel := context.WithCancel(ctx)
	cancel()
	_, err := s.Append(ctx2, envelope(opts, 1))
	require.ErrorIs(t, err, context.Canceled)
	e := envelope(opts, 1)
	e.Payload = bytes.Repeat([]byte{1}, int(opts.MaxPayloadBytes)+1)
	_, err = s.Append(ctx, e)
	require.Error(t, err)
	assert.Zero(t, s.Stats().Records)
	s.opts.MinFreeBytes = 1 << 62
	_, err = s.Append(ctx, envelope(opts, 1))
	require.ErrorIs(t, err, ErrCapacity)
	assert.Zero(t, s.Stats().Records)
	s.opts.MinFreeBytes = 0
	r, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.active.file.Close())
	_, err = s.Append(ctx, envelope(opts, 2))
	require.ErrorIs(t, err, ErrUnavailable)
	s.active.file = nil // already closed; simulate the failed writer being released
	_, err = s.Lookup(ctx, r.Block, r.Kind)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestMountedVolume(t *testing.T) {
	root := os.Getenv("INDEXER_WINDOW_MOUNT_ROOT")
	if root == "" {
		t.Skip("set to a disposable actual mount point for volume integration")
	}
	spec := VolumeSpec{Root: root, ID: "mounted-fixture"}
	require.NoError(t, InitializeVolume(spec))
	opts := Options{Volume: spec, Path: filepath.Join(root, "mounted-chain"), Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 512 << 20}
	s := created(t, opts)
	ref, err := s.Append(context.Background(), envelope(opts, 1))
	require.NoError(t, err)
	l, err := s.Acquire(context.Background(), ref)
	require.NoError(t, err)
	require.NoError(t, l.Close())
}

func TestReadOnlyMountedVolume(t *testing.T) {
	root := os.Getenv("INDEXER_WINDOW_READONLY_ROOT")
	if root == "" {
		t.Skip("requires the initialized fixture mounted read-only")
	}
	opts := Options{Volume: VolumeSpec{Root: root, ID: "mounted-fixture"}, Path: filepath.Join(root, "mounted-chain"), Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 512 << 20}
	_, err := Open(opts)
	require.Error(t, err, "read-only storage must not open a writer or fall back")
}

func TestStatsCountGrossAppendsOnly(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	ctx := context.Background()
	first, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	again, err := s.Append(ctx, envelope(opts, 1))
	require.NoError(t, err)
	require.Equal(t, first, again)
	second, err := s.Append(ctx, envelope(opts, 2))
	require.NoError(t, err)
	st := s.Stats()
	require.Equal(t, uint64(2), st.AppendedRecords)
	require.Equal(t, first.FrameBytes+second.FrameBytes, st.AppendedBytes)
}
