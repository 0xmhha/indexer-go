//go:build darwin || linux

package collector

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func operationView(t *testing.T) *nativestore.ReadView {
	t.Helper()
	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0700))
	dir := filepath.Join(parent, "native")
	require.NoError(t, os.Mkdir(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".arc-native-import.identity.json"), []byte("private fixture identity"), 0600))
	v, err := nativestore.OpenReadView(t.Context(), dir)
	require.NoError(t, err)
	return v
}
func TestManagedBackendCancelDuringDecodeJoinsChildBeforeViewUnlock(t *testing.T) {
	b, _ := helperBackend(t, "normal")
	v := operationView(t)
	b.readView = v
	t.Cleanup(func() { _ = v.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	result := make(chan error, 1)
	go func() {
		result <- b.operation(ctx, func(context.Context) error { close(entered); <-release; return nil })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("decode did not start")
	}
	cancel()
	select {
	case <-b.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not join owned child while decode was pending")
	}
	probe, err := os.OpenFile(v.File().Name(), os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = probe.Close() }()
	require.Error(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB), "SH must remain until complete decode exits even after child joined")
	unblock()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled decode cleanup deadlocked")
	}
	_, err = v.File().Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}
func TestManagedBackendCloseWaitsForCompleteDecodeBeforeDescriptorRelease(t *testing.T) {
	b, _ := helperBackend(t, "normal")
	v := operationView(t)
	b.readView = v
	t.Cleanup(func() { _ = v.Close() })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	result := make(chan error, 1)
	go func() {
		result <- b.operation(t.Context(), func(context.Context) error { close(entered); <-release; return nil })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("decoder did not begin")
	}
	closed := make(chan struct{})
	go func() { _ = b.Close(); close(closed) }()
	select {
	case <-b.done:
	case <-time.After(5 * time.Second):
		t.Fatal("owned child did not join")
	}
	select {
	case <-closed:
		t.Fatal("Close returned before the complete decode joined")
	default:
	}
	_, err := v.File().Stat()
	require.NoError(t, err, "read descriptor must outlive active validation")
	unblock()
	select {
	case err := <-result:
		require.Error(t, err, "closed operation must not return accepted bytes")
	case <-time.After(5 * time.Second):
		t.Fatal("decode cleanup deadlocked with Close")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish")
	}
	_, err = v.File().Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}
func TestManagedBackendOperationFailureDeregistersBeforeFatalClose(t *testing.T) {
	b, _ := helperBackend(t, "normal")
	v := operationView(t)
	b.readView = v
	t.Cleanup(func() { _ = v.Close() })
	want := errors.New("fixture invalid decoded epoch proof")
	result := make(chan error, 1)
	go func() { result <- b.operation(t.Context(), func(context.Context) error { return want }) }()
	select {
	case err := <-result:
		require.ErrorIs(t, err, want)
	case <-time.After(5 * time.Second):
		t.Fatal("operation fatal Close waited on its own lifetime")
	}
	select {
	case <-b.done:
	default:
		t.Fatal("invalid decoded operation left its owned child running")
	}
	_, err := v.File().Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}
