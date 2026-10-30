//go:build darwin || linux

package nativestore

import (
	"context"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func viewFixture(t *testing.T) (string, string) {
	t.Helper()
	o, _ := fixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(o.DataDir, ".arc-native-import.identity.json"), []byte("private identity fixture; parsed by the managed adapter"), 0600))
	return o.DataDir, filepath.Join(filepath.Dir(o.DataDir), "."+filepath.Base(o.DataDir)+".arc-native-import-view")
}
func TestManagedReadViewKeepsInheritedOFDButExplicitlyUnlocksEachOperation(t *testing.T) {
	dir, path := viewFixture(t)
	v, err := OpenReadView(t.Context(), dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, v.Close()) }()
	l, err := v.Acquire(t.Context())
	require.NoError(t, err)
	require.NoError(t, l.Validate())
	dup, err := unix.Dup(int(v.File().Fd()))
	require.NoError(t, err)
	defer func() { _ = unix.Close(dup) }()
	other, err := os.OpenFile(path, os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = other.Close() }()
	require.Error(t, unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	require.NoError(t, l.Close())
	require.ErrorIs(t, l.Validate(), os.ErrClosed, "released view must not validate bytes outside its SH lifetime")
	require.NoError(t, unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB), "parent unlocks even while inherited duplicate remains open")
	require.NoError(t, unix.Flock(int(other.Fd()), unix.LOCK_UN))
	require.NoError(t, v.Close())
	_, err = v.File().Stat()
	require.ErrorIs(t, err, os.ErrClosed)
	_, err = v.Acquire(t.Context())
	require.Error(t, err)
}
func TestManagedReadViewWriterTurnstileCancellationAndConcurrentCloseJoin(t *testing.T) {
	dir, path := viewFixture(t)
	v, err := OpenReadView(t.Context(), dir)
	require.NoError(t, err)
	first, err := v.Acquire(t.Context())
	require.NoError(t, err)
	turn, err := os.OpenFile(filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".arc-native-import-turn"), os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = turn.Close() }()
	require.NoError(t, unix.Flock(int(turn.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	other, err := os.OpenFile(path, os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = other.Close() }()
	require.Error(t, unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	require.NoError(t, first.Close())
	require.NoError(t, unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		l, err := v.Acquire(ctx)
		if l != nil {
			_ = l.Close()
		}
		result <- err
	}()
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	require.NoError(t, unix.Flock(int(other.Fd()), unix.LOCK_UN))
	require.NoError(t, unix.Flock(int(turn.Fd()), unix.LOCK_UN))
	active, err := v.Acquire(t.Context())
	require.NoError(t, err)
	closed := make(chan struct{})
	go func() { _ = v.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("closed inherited descriptor before active operation joined")
	default:
	}
	require.NoError(t, active.Close())
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("read view did not close after release")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _ = v.Close() })
	}
	wg.Wait()
}
func TestManagedReadViewRejectsPendingReplacedOrPublicLockAndKeepsData(t *testing.T) {
	for _, mode := range []string{"pending", "public", "replaced", "run-replaced"} {
		t.Run(mode, func(t *testing.T) {
			dir, path := viewFixture(t)
			v, err := OpenReadView(t.Context(), dir)
			require.NoError(t, err)
			defer func() { _ = v.Close() }()
			switch mode {
			case "pending":
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".arc-native-import.pending"), []byte("preserve"), 0600))
			case "public":
				require.NoError(t, os.Chmod(path, 0644))
			case "replaced":
				require.NoError(t, os.Rename(path, path+".keep"))
				require.NoError(t, os.WriteFile(path, []byte{}, 0600))
			case "run-replaced":
				run := v.run.Name()
				require.NoError(t, os.Rename(run, run+".keep"))
				require.NoError(t, os.WriteFile(run, nil, 0600))
			}
			l, err := v.Acquire(t.Context())
			if l != nil {
				_ = l.Close()
			}
			require.Error(t, err)
			b, err := os.ReadFile(filepath.Join(dir, "CURRENT"))
			require.NoError(t, err)
			require.Equal(t, "state and code", string(b))
		})
	}
}

func TestManagedReadViewWaitsForOwnedPublicationBeforeEvaluatingPending(t *testing.T) {
	for _, startup := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing-reader", true: "startup-reader"}[startup], func(t *testing.T) {
			dir, path := viewFixture(t)
			var v *ReadView
			var err error
			if !startup {
				v, err = OpenReadView(t.Context(), dir)
				require.NoError(t, err)
			}
			writer, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
			require.NoError(t, err)
			defer func() { _ = writer.Close() }()
			require.NoError(t, unix.Flock(int(writer.Fd()), unix.LOCK_EX|unix.LOCK_NB))
			pending := filepath.Join(dir, ".arc-native-import.pending")
			require.NoError(t, os.WriteFile(pending, []byte("active owned publication"), 0600))
			if startup {
				v, err = OpenReadView(t.Context(), dir)
				require.NoError(t, err, "construction opens no native provider and must wait for its first SH before judging a live publication")
			}
			defer func() { _ = v.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				lease, e := v.Acquire(ctx)
				if lease != nil {
					e = lease.Close()
				}
				done <- e
			}()
			turn, err := os.OpenFile(filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".arc-native-import-turn"), os.O_RDWR|os.O_CREATE, 0600)
			require.NoError(t, err)
			defer func() { _ = turn.Close() }()
			var early error
			earlyDone := false
			require.Eventually(t, func() bool {
				select {
				case e := <-done:
					early, earlyDone = e, true
					return true
				default:
				}
				if unix.Flock(int(turn.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
					return true
				}
				require.NoError(t, unix.Flock(int(turn.Fd()), unix.LOCK_UN))
				return false
			}, 5*time.Second, time.Millisecond)
			if earlyDone {
				t.Fatalf("reader evaluated pending before it could hold SH: %v", early)
			}
			require.NoError(t, os.Remove(pending))
			require.NoError(t, unix.Flock(int(writer.Fd()), unix.LOCK_UN))
			require.NoError(t, <-done, "completed publication must not terminate the managed collector")
		})
	}
}
