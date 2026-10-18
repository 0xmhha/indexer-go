package window

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func managedOptions(root, name string) Options {
	return Options{Volume: VolumeSpec{Root: root, ID: "shared-fixture", AllowDirectory: true, MaxBytes: 5 << 20},
		Reservation: name, Path: filepath.Join(root, name), Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, MaxBytes: 2 << 20,
		SegmentBytes: 128 << 10, MaxPayloadBytes: 32 << 10, MaxSegmentRecords: 8, MaxSegments: 64}
}

func managedFixture(t *testing.T) Options {
	t.Helper()
	o := managedOptions(t.TempDir(), "arc")
	require.NoError(t, InitializeVolume(o.Volume))
	return o
}

func TestVolumeQuotasPersistAndBindStore(t *testing.T) {
	o := managedFixture(t)
	s := created(t, o)
	r, err := s.Append(context.Background(), envelope(o, 1))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	b := managedOptions(o.Volume.Root, "bsc")
	other := created(t, b)
	u, err := other.Usage(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 4<<20, u.VolumeAllocatedBytes, "closed stores still own disk bytes")
	c := managedOptions(o.Volume.Root, "third")
	_, err = Create(c)
	require.ErrorIs(t, err, ErrCapacity)
	_, err = os.Stat(c.Path)
	require.ErrorIs(t, err, os.ErrNotExist, "reserve before creating raw directories")
	s, err = Open(o)
	require.NoError(t, err)
	got, err := s.Lookup(context.Background(), r.Block, r.Kind)
	require.NoError(t, err)
	require.Equal(t, r, got)
	require.NoError(t, s.Close())
	for _, mutate := range []func(*Options){
		func(v *Options) { v.MaxBytes-- },
		func(v *Options) { v.Path += "-moved" },
		func(v *Options) { v.Chain.ID = "2" },
	} {
		v := o
		mutate(&v)
		_, err = Open(v)
		require.ErrorIs(t, err, ErrConflict)
	}
	// Even an empty directory cannot silently reuse a bound quota/store ID.
	require.NoError(t, os.Rename(o.Path, o.Path+"-offline"))
	_, err = Create(o)
	require.ErrorIs(t, err, ErrConflict)
	_, err = os.Stat(o.Path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestVolumeConcurrentReservation(t *testing.T) {
	o := managedFixture(t)
	created(t, o)
	start := make(chan struct{})
	type result struct {
		s   *Store
		err error
	}
	out := make(chan result, 2)
	for _, name := range []string{"bsc", "sepolia"} {
		go func(name string) {
			<-start
			s, err := Create(managedOptions(o.Volume.Root, name))
			out <- result{s, err}
		}(name)
	}
	close(start)
	winners := 0
	for i := 0; i < 2; i++ {
		r := <-out
		if r.err == nil {
			winners++
			require.NoError(t, r.s.Close())
		} else {
			require.ErrorIs(t, r.err, ErrCapacity)
		}
	}
	require.Equal(t, 1, winners)
}

func TestVolumeLedgerFailsClosed(t *testing.T) {
	for _, damage := range []string{"checksum", "missing-ledger", "missing-lock", "replaced-lock", "missing-marker"} {
		t.Run(damage, func(t *testing.T) {
			o := managedFixture(t)
			s, err := Create(o)
			require.NoError(t, err)
			switch damage {
			case "checksum":
				require.NoError(t, os.WriteFile(filepath.Join(o.Volume.Root, budgetFile), []byte(`{}`), 0600))
			case "missing-ledger":
				require.NoError(t, os.Remove(filepath.Join(o.Volume.Root, budgetFile)))
			case "missing-lock", "replaced-lock":
				require.NoError(t, os.Rename(filepath.Join(o.Volume.Root, budgetLock), filepath.Join(o.Volume.Root, "old-lock")))
				if damage == "replaced-lock" {
					require.NoError(t, os.WriteFile(filepath.Join(o.Volume.Root, budgetLock), nil, 0600))
				}
			case "missing-marker":
				require.NoError(t, os.Remove(filepath.Join(o.Volume.Root, ".indexer-volume.json")))
			}
			ref, err := s.Append(context.Background(), envelope(o, 1))
			require.Error(t, err)
			require.Zero(t, ref)
			_ = s.Close()
			require.Error(t, InitializeVolume(o.Volume), "must not rebuild lost accounting or marker")
			_, err = Open(o)
			require.Error(t, err)
		})
	}
}

func TestVolumeNoImplicitMigrationOrOpenReservation(t *testing.T) {
	o := managedFixture(t)
	_, err := Open(o)
	require.ErrorIs(t, err, ErrConflict)
	root, err := os.OpenRoot(o.Volume.Root)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	b, err := readBudget(root, o.Volume)
	require.NoError(t, err)
	require.Empty(t, b.Claims, "Open cannot allocate a new slot")
	legacy := o.Volume
	legacy.MaxBytes = 0
	require.ErrorIs(t, InitializeVolume(legacy), ErrConflict)
	require.NoError(t, os.Remove(filepath.Join(o.Volume.Root, ".indexer-volume.json")))
	require.ErrorIs(t, InitializeVolume(legacy), ErrConflict, "a lost marker cannot bypass a managed ledger")
	legacy.Root = t.TempDir()
	require.NoError(t, InitializeVolume(legacy))
	v2 := o.Volume
	v2.Root = legacy.Root
	require.ErrorIs(t, InitializeVolume(v2), ErrConflict)
}

func TestVolumeLockCancellationAndProcessCrash(t *testing.T) {
	o := managedFixture(t)
	s := created(t, o)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := volumeLock(context.Background(), s.volume, func() error {
		_, e := s.Append(ctx, envelope(o, 1))
		require.ErrorIs(t, e, context.DeadlineExceeded)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, s.Close())
	// The child dies while holding the global disk lock, just after fsync.
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagedCrashChild$")
	cmd.Env = append(os.Environ(), "INDEXER_MANAGED_CRASH_ROOT="+o.Volume.Root)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, string(output))
	require.Equal(t, 71, exit.ExitCode(), string(output))
	s2, err := Open(o)
	require.NoError(t, err)
	defer func() { require.NoError(t, s2.Close()) }()
	_, err = s2.Lookup(context.Background(), envelope(o, 1).Block, envelope(o, 1).Kind)
	require.NoError(t, err, "durable raw and the claim survive process death")
	u, err := s2.Usage(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2<<20, u.VolumeAllocatedBytes)
}

func TestManagedCrashChild(t *testing.T) {
	root := os.Getenv("INDEXER_MANAGED_CRASH_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	o := managedOptions(root, "arc")
	s, err := Open(o)
	require.NoError(t, err)
	s.afterDataSync = func() { os.Exit(71) }
	_, _ = s.Append(context.Background(), envelope(o, 1))
	t.Fatal("crash hook did not execute")
}

func TestVolumeReservationsAcrossProcesses(t *testing.T) {
	o := managedFixture(t)
	created(t, o)
	type child struct {
		cmd    *exec.Cmd
		gate   io.WriteCloser
		output bytes.Buffer
	}
	children := make([]*child, 0, 2)
	for _, name := range []string{"bsc", "sepolia"} {
		c := &child{cmd: exec.Command(os.Args[0], "-test.run=^TestManagedClaimChild$")}
		c.cmd.Env = append(os.Environ(), "INDEXER_MANAGED_CLAIM_ROOT="+o.Volume.Root, "INDEXER_MANAGED_CLAIM_NAME="+name)
		c.cmd.Stdout, c.cmd.Stderr = &c.output, &c.output
		var err error
		c.gate, err = c.cmd.StdinPipe()
		require.NoError(t, err)
		require.NoError(t, c.cmd.Start())
		children = append(children, c)
	}
	for _, c := range children {
		_, err := c.gate.Write([]byte{1})
		require.NoError(t, err)
		require.NoError(t, c.gate.Close())
	}
	winners := 0
	for _, c := range children {
		err := c.cmd.Wait()
		if err == nil {
			winners++
			continue
		}
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit, c.output.String())
		require.Equal(t, 72, exit.ExitCode(), c.output.String())
	}
	require.Equal(t, 1, winners, "only one process can reserve the last quota")
}

func TestManagedClaimChild(t *testing.T) {
	root := os.Getenv("INDEXER_MANAGED_CLAIM_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	var token [1]byte
	_, err := io.ReadFull(os.Stdin, token[:])
	require.NoError(t, err)
	s, err := Create(managedOptions(root, os.Getenv("INDEXER_MANAGED_CLAIM_NAME")))
	if errors.Is(err, ErrCapacity) {
		os.Exit(72)
	}
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestManagedMountedVolume(t *testing.T) {
	root := os.Getenv("INDEXER_WINDOW_MANAGED_MOUNT_ROOT")
	readonly := false
	if root == "" {
		root = os.Getenv("INDEXER_WINDOW_MANAGED_READONLY_ROOT")
		readonly = true
	}
	if root == "" {
		t.Skip("requires a disposable real bind mount")
	}
	o := managedOptions(root, "arc")
	o.Volume.AllowDirectory = false
	if readonly {
		_, err := Open(o)
		require.Error(t, err, "read-only managed storage must fail without fallback")
		return
	}
	require.NoError(t, InitializeVolume(o.Volume))
	s := created(t, o)
	ref, err := s.Append(context.Background(), envelope(o, 1))
	require.NoError(t, err)
	l, err := s.Acquire(context.Background(), ref)
	require.NoError(t, err)
	require.NoError(t, l.Close())
	u, err := s.Usage(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2<<20, u.VolumeAllocatedBytes)
}

func TestManagedPressureBackoffAndGC(t *testing.T) {
	o := managedFixture(t)
	// Reach both thresholds with a handful of durable frames. Keeping a
	// multi-megabyte fixture adds fsync load without another pressure case.
	o.MaxBytes = 240 << 10
	clock := &testClock{now: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	o.Now = clock.get
	s := created(t, o)
	ctx := context.Background()
	u, err := s.Usage(ctx)
	require.NoError(t, err)
	require.Equal(t, "normal", u.Severity, "allocating quota does not imply physical pressure")
	seen := map[string]bool{}
	var n uint64
	for n = 1; n < 200; n++ {
		e := envelope(o, n)
		e.Payload = bytes.Repeat([]byte("p"), int(o.MaxPayloadBytes))
		_, err = s.Append(ctx, e)
		u, usageErr := s.Usage(ctx)
		require.NoError(t, usageErr)
		seen[u.Severity] = true
		if errors.Is(err, ErrCapacity) {
			break
		}
		require.NoError(t, err)
	}
	require.Less(t, n, uint64(200))
	require.True(t, seen["warning"])
	require.True(t, seen["critical"])
	require.NoError(t, s.Seal(ctx), "sealing must fit the reserved index bytes at capacity")
	// A larger pending input needs more than the advisory Writable probe.
	// Retry it without retaining a global disk lock so GC can free space.
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e := envelope(o, n)
	e.Payload = bytes.Repeat([]byte("q"), int(o.MaxPayloadBytes))
	done := make(chan error, 1)
	go func() { _, e := s.AppendWait(waitCtx, e); done <- e }()
	select {
	case err := <-done:
		t.Fatalf("pending input escaped pressure: %v", err)
	default:
	}
	clock.advance(25 * time.Hour)
	removed, err := s.Collect(ctx, guardFunc(func(_ context.Context, _ []Reference, fn func() error) error { return fn() }))
	require.NoError(t, err)
	require.Positive(t, removed)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("intake did not resume after fenced GC")
	}
	u, err = s.Usage(ctx)
	require.NoError(t, err)
	require.Equal(t, "normal", u.Severity)
}

func TestManagedFreeSpaceFloorPausesWithoutPublishing(t *testing.T) {
	o := managedFixture(t)
	o.MinFreeBytes = 1 << 60
	s := created(t, o)
	u, err := s.Usage(context.Background())
	require.NoError(t, err)
	require.False(t, u.Writable)
	require.Equal(t, "critical", u.Severity)
	r, err := s.Append(context.Background(), envelope(o, 1))
	require.ErrorIs(t, err, ErrCapacity)
	require.Zero(t, r)
	require.Zero(t, s.Stats().Records)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); require.ErrorIs(t, s.WaitWritable(ctx), context.Canceled) }()
	cancel()
	wg.Wait()
}

func TestManagedInitialFreeSpaceAndImmutableBudget(t *testing.T) {
	o := managedOptions(t.TempDir(), "arc")
	v := o.Volume
	v.MinFreeBytes = 1 << 60
	require.ErrorIs(t, InitializeVolume(v), ErrCapacity)
	_, err := os.Stat(filepath.Join(v.Root, ".indexer-volume.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, InitializeVolume(o.Volume))
	require.NoError(t, InitializeVolume(o.Volume))
	for _, mutate := range []func(*VolumeSpec){func(v *VolumeSpec) { v.MaxBytes++ }, func(v *VolumeSpec) { v.MinFreeBytes++ }} {
		changed := o.Volume
		mutate(&changed)
		require.ErrorIs(t, InitializeVolume(changed), ErrConflict)
	}
}

func TestManagedCheckedRecoveryReleasesDiskLockForDB(t *testing.T) {
	o := managedFixture(t)
	clock := &testClock{now: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	o.Now = clock.get
	s := created(t, o)
	other := created(t, managedOptions(o.Volume.Root, "bsc"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := s.Append(ctx, envelope(o, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(ctx))
	clock.advance(25 * time.Hour)
	refs := []Reference{r}
	h, err := referencesHash(refs)
	require.NoError(t, err)
	require.NoError(t, writeJSON(s.root, segmentName(r.Segment, "gc"), gcMarker{StoreID: s.id, Segment: r.Segment, ReferencesHash: h}))
	crashClose(t, s)
	failure := errors.New("DB approval refused")
	authorize := func() error {
		// A DB callback may wait on another coordinator. It must not retain
		// this volume's disk lock and block independent stores meanwhile.
		_, err := other.Usage(ctx)
		return err
	}
	_, err = OpenChecked(o, func(record DeletionRecord) error {
		require.NoError(t, record.Match(refs))
		require.NoError(t, authorize())
		return failure
	})
	require.ErrorIs(t, err, failure)
	_, err = os.Stat(filepath.Join(o.Path, segmentName(r.Segment, "raw")))
	require.NoError(t, err, "DB authorization precedes unlink")
	reopened, err := OpenChecked(o, func(record DeletionRecord) error { return errors.Join(record.Match(refs), authorize()) })
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.NoError(t, reopened.RecoverDeletion(ctx, refs, authorize, authorize))
	u, err := reopened.Usage(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 4<<20, u.VolumeAllocatedBytes, "GC frees raw bytes, not the fixed reservation")
}
