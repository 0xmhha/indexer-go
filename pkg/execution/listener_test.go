//go:build darwin || linux

package execution

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func executionSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ei-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func TestReceiverPrivateUnixReconnectAndOwnedCleanup(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	r, result, _ := receiverFixture(t, &controlledBackend{})
	path := executionSocket(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, path) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Unix listener did not join")
		}
	}()
	for range 2 {
		var raw net.Conn
		require.Eventually(t, func() bool {
			var err error
			raw, err = net.DialTimeout("unix", path, 50*time.Millisecond)
			return err == nil
		}, time.Second, time.Millisecond)
		conn := execwire.NewConn(raw)
		defer func() { _ = conn.Close() }()
		call, finish := context.WithTimeout(ctx, time.Second)
		require.NoError(t, conn.Write(call, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: execwire.Ref{Generation: 1, Hash: result.Parent}}}))
		_, err := conn.Read(call)
		finish()
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		done <- err
	case <-time.After(3 * time.Second):
		t.Fatal("listener shutdown stalled")
	}
	_, err = os.Lstat(path)
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestReceiverPrivateUnixDoesNotRemoveReplacedSocket(t *testing.T) {
	r, _, _ := receiverFixture(t, &controlledBackend{})
	path := executionSocket(t)
	l, err := listenExecution(path)
	require.NoError(t, err)
	defer func(owned *executionListener) { _ = owned.Close() }(l)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(path, []byte("replacement"), 0600))
	require.Error(t, l.Close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(data))
	_ = r
}

func TestReceiverPrivateUnixPreservesExistingPaths(t *testing.T) {
	r, _, _ := receiverFixture(t, &controlledBackend{})
	path := executionSocket(t)
	require.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
	require.Error(t, r.Run(t.Context(), path))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	path = executionSocket(t)
	require.NoError(t, os.Chmod(filepath.Dir(path), 0755))
	require.Error(t, r.Run(t.Context(), path))
	_, err = os.Lstat(path)
	require.True(t, errors.Is(err, os.ErrNotExist))
	require.Error(t, r.Run(t.Context(), "relative.sock"))
}

func TestReceiverSocketPathPublishedOnlyAfterReadyAndNeverReplaces(t *testing.T) {
	path := executionSocket(t)
	l, err := listenExecutionReady(path, func() error {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("public socket path exposed before ready")
		}
		return nil
	})
	require.NoError(t, err)
	defer func(owned *executionListener) { _ = owned.Close() }(l)
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	c, err := net.DialTimeout("unix", path, time.Second)
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.NoError(t, l.Close())
	l, err = listenExecutionReady(path, func() error { return os.WriteFile(path, []byte("preserve concurrent owner"), 0600) })
	require.Error(t, err)
	require.Nil(t, l)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("preserve concurrent owner"), data)
}
