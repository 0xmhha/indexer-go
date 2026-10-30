package nativeops

import (
	"context"
	"fmt"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// RunGuard is an explicit operator command, never called by collection or GC.
// Only the standard native layout is supported. Managed first-import state
// refuses this path; the shared run and sole-writer leases last through child
// join. Tests substitute an owned protocol fixture.
func RunGuard(ctx context.Context, executable, datadir, chain string, args []string, in io.Reader, out, stderr io.Writer) error {
	if (chain != "arc-mainnet" && chain != "arc-testnet") || !filepath.IsAbs(executable) || !filepath.IsAbs(datadir) {
		return nativestore.ErrUnsafe
	}
	canonical, e := filepath.EvalSymlinks(datadir)
	if e != nil {
		return e
	}
	if canonical != filepath.Clean(datadir) {
		return nativestore.ErrUnsafe
	}
	for _, arg := range args {
		if arg == "--" || arg == "-d" || strings.HasPrefix(arg, "--datadir") || strings.HasPrefix(arg, "--chain") || strings.HasPrefix(arg, "--config") || strings.HasPrefix(arg, "--db.") || strings.HasPrefix(arg, "--static-files") {
			return fmt.Errorf("native path/config/chain override is unsupported: %s", arg)
		}
	}
	for _, name := range []string{"db", "rocksdb", "static_files"} {
		info, e := os.Lstat(filepath.Join(datadir, name))
		if e != nil {
			return e
		}
		if !info.IsDir() {
			return nativestore.ErrUnsafe
		}
	}
	info, e := os.Lstat(executable)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return nativestore.ErrUnsafe
	}
	lease, e := nativestore.AcquireUnmanagedRun(datadir)
	if e != nil {
		return e
	}
	defer func() { _ = lease.Close() }()
	command := append([]string{"node", "--datadir", datadir, "--chain", chain}, args...)
	cmd := exec.CommandContext(ctx, executable, command...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 30 * time.Second
	return cmd.Run()
}
