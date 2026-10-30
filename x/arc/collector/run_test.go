package collector

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

func ownedAdapterScript(t *testing.T, mode, marker, before string) string {
	t.Helper()
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(t.TempDir(), "adapter")
	script := "#!/bin/sh\n" + before + "\nexport INDEXER_ARC_BACKEND_HELPER=" + quote(mode) + " INDEXER_ARC_HELPER_READY=" + quote(marker) + " GORACE=atexit_sleep_ms=0\nexec " + quote(os.Args[0]) + " -test.run=^TestArcBackendHelper$ -- \"$@\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestArcCollectorProductionStartFiltersInheritedPrivateEnvironment(t *testing.T) {
	t.Setenv("INDEXER_ARC_PRIVATE_FIXTURE", "must-not-reach-child")
	binary := ownedAdapterScript(t, "idle", "", "test -z \"${INDEXER_ARC_PRIVATE_FIXTURE-}\" || exit 17")
	b, err := Start(t.Context(), Options{Executable: binary, DataDir: t.TempDir(), Chain: execwire.Chain{ID: "5042002", Genesis: execwire.Hash{1}}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("production adapter inherited private App environment: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestArcCollectorRefusesPendingNativeRetentionBeforeChild(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".execution-retention")
	if e := os.WriteFile(marker, []byte("pending"), 0600); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = os.Remove(marker) }()
	b, e := Start(t.Context(), Options{Executable: os.Args[0], DataDir: dir, Chain: execwire.Chain{ID: "5042002", Genesis: execwire.Hash{1}}, Timeout: time.Second})
	if b != nil {
		_ = b.Close()
	}
	if !errors.Is(e, nativestore.ErrPending) {
		t.Fatalf("pending native fence must reject before launching a child: %v", e)
	}
}

func TestArcCollectorOwnedAdapterAndAuthenticatedSocketJoin(t *testing.T) {
	ipc, err := os.MkdirTemp("", "arc-c-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(ipc) }()
	socket := filepath.Join(ipc, "s")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err = os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	stopListen := context.AfterFunc(ctx, func() { _ = l.Close() })
	defer stopListen()
	anchor := execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}
	chain := execwire.Chain{ID: "5042002", Genesis: anchor.Hash}
	serverDone := make(chan error, 1)
	go func() {
		raw, e := l.AcceptUnix()
		if e != nil {
			serverDone <- e
			return
		}
		conn := execwire.NewConn(raw)
		defer func() { _ = conn.Close() }()
		_, e = conn.Read(ctx)
		if e == nil {
			e = conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: anchor}})
		}
		if e == nil {
			_, e = conn.Read(ctx)
		}
		serverDone <- e
	}()
	marker := filepath.Join(t.TempDir(), "ready")
	binary := ownedAdapterScript(t, "idle", marker, "")
	config := Config{Native: Options{Executable: binary, DataDir: t.TempDir(), Chain: chain, Timeout: 5 * time.Second}, Anchor: anchor, SpoolDir: filepath.Join(t.TempDir(), "spool"), Socket: socket, Poll: time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, config) }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, err = os.Stat(marker); err == nil {
			break
		}
		select {
		case err = <-done:
			t.Fatalf("collector failed: %v", err)
		case <-tick.C:
		case <-timer.C:
			t.Fatal("collector did not start native reads")
		}
	}
	scratchBytes, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	scratch := string(scratchBytes)
	if !filepath.IsAbs(scratch) {
		t.Fatal("native scratch path was not isolated")
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatal("owned scratch survived joined child", err)
	}
	if err = <-serverDone; err == nil {
		t.Fatal("server survived collector cancellation")
	}
}

func TestArcCollectorRejectsInvalidPathsAndLimitsBeforeSourceCreation(t *testing.T) {
	native := t.TempDir()
	base := Config{Native: Options{Executable: "/missing", DataDir: native, Chain: execwire.Chain{ID: "5042002", Genesis: execwire.Hash{1}}}, Anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, SpoolDir: filepath.Join(native, "spool"), Socket: "/private/socket"}
	for _, mutate := range []func(*Config){func(*Config) {}, func(c *Config) { c.Native.Timeout = -time.Second }, func(c *Config) { c.Window = 65 }, func(c *Config) { c.MaxRecords = -1 }, func(c *Config) { c.MaxBytes = 1 }, func(c *Config) { c.Native.Chain.ID = "1" }, func(c *Config) { c.Anchor.Generation = 0 }, func(c *Config) { c.Native.DataDir = string(filepath.Separator) }} {
		c := base
		mutate(&c)
		if err := Run(t.Context(), c); err == nil {
			t.Fatal("unsafe config accepted")
		}
		if _, err := os.Stat(base.SpoolDir); !os.IsNotExist(err) {
			t.Fatal("invalid config created source data")
		}
	}
	path := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(path, []byte("not a program"), 0600); err != nil {
		t.Fatal(err)
	}
	o := base.Native
	o.Executable = path
	if b, err := Start(t.Context(), o); err == nil {
		_ = b.Close()
		t.Fatal("non executable accepted")
	}
}

func TestArcCollectorCleanupPreservesReplacedScratch(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "owned")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	old := path + "-original"
	if err = os.Rename(path, old); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "KEEP")
	if err = os.WriteFile(marker, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = removeScratch(path, info); err == nil {
		t.Fatal("replacement directory accepted as owned scratch")
	}
	if got, e := os.ReadFile(marker); e != nil || string(got) != "replacement" {
		t.Fatal("replacement data was removed")
	}
}
