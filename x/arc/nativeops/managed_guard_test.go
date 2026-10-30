package nativeops

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/stretchr/testify/require"
)

// Managed ownership cannot be downgraded by calling the legacy node command.
// The child is only TestNativeHelper, never a node executable.
func TestNativeLegacyGuardRejectsManagedAndPartialStateBeforeChild(t *testing.T) {
	for _, name := range []string{
		".arc-native-import.identity.json", ".arc-native-import.head.json",
		".arc-native-import-receipts", ".arc-native-import-epochs",
		".arc-native-import.pending", ".arc-native-bootstrap.pending",
		".arc-native-bootstrap.json",
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := nativeFixture(t)
			require.NoError(t, os.WriteFile(filepath.Join(c.NativeDataDir, name), []byte("partial or malformed owner"), 0600))
			err := RunGuard(t.Context(), c.NativeExecutable, c.NativeDataDir, "arc-testnet", nil, nil, io.Discard, io.Discard)
			require.Error(t, err, "managed state must refuse the legacy writer")
			_, err = os.Stat(filepath.Join(c.NativeDataDir, "guard-ran"))
			require.ErrorIs(t, err, os.ErrNotExist, "rejection must happen before child startup")
		})
	}
	for _, shape := range []string{"directory", "broken-link"} {
		t.Run(shape, func(t *testing.T) {
			c, _ := nativeFixture(t)
			marker := filepath.Join(c.NativeDataDir, ".arc-native-import.identity.json")
			if shape == "directory" {
				require.NoError(t, os.Mkdir(marker, 0700))
			} else {
				require.NoError(t, os.Symlink("absent", marker))
			}
			require.Error(t, RunGuard(t.Context(), c.NativeExecutable, c.NativeDataDir, "arc-testnet", nil, nil, io.Discard, io.Discard))
			_, err := os.Stat(filepath.Join(c.NativeDataDir, "guard-ran"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestNativeLegacyGuardCannotRaceTheManagedSoleWriter(t *testing.T) {
	c, _ := nativeFixture(t)
	path := filepath.Join(filepath.Dir(c.NativeDataDir), "."+filepath.Base(c.NativeDataDir)+".arc-native-import-writer")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	err = RunGuard(t.Context(), c.NativeExecutable, c.NativeDataDir, "arc-testnet", nil, nil, io.Discard, io.Discard)
	require.Error(t, err, "the importer may hold its writer lock before it creates identity")
	_, err = os.Stat(filepath.Join(c.NativeDataDir, "guard-ran"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestNativeLegacyGuardKeepsSoleWriterUntilChildJoinedAndAllowsRO(t *testing.T) {
	c, _ := nativeFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	reader, writer := io.Pipe()
	done := make(chan struct{})
	var result error
	go func() {
		result = RunGuard(ctx, c.NativeExecutable, c.NativeDataDir, "arc-testnet", []string{"--wait"}, nil, writer, io.Discard)
		_ = writer.CloseWithError(result)
		close(done)
	}()
	defer func() {
		cancel()
		_ = reader.Close()
		<-done
	}()
	line, err := bufio.NewReader(reader).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ready\n", line)
	// Native RO ownership retains its existing shared run lease.
	run, err := nativestore.AcquireRun(c.NativeDataDir)
	require.NoError(t, err)
	require.NoError(t, run.Close())
	path := filepath.Join(filepath.Dir(c.NativeDataDir), "."+filepath.Base(c.NativeDataDir)+".arc-native-import-writer")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	require.True(t, errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN), "managed writer must be excluded until child exits")
	cancel()
	<-done
	require.ErrorIs(t, result, context.Canceled)
	_, err = os.Stat(filepath.Join(c.NativeDataDir, "guard-stopped"))
	require.NoError(t, err, "successful lock reacquisition requires joined owned child")
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
}
