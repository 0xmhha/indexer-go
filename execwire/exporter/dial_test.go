//go:build darwin || linux

package exporter

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateDialAuthenticatesSameUIDAndRejectsUnsafePaths(t *testing.T) {
	dir, err := os.MkdirTemp("", "ex-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "s")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	client, err := dialExecution(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	peer, err := l.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = peer.Close() }()
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if raw, err := dialExecution(ctx, path); err == nil {
		_ = raw.Close()
		t.Fatal("unsafe parent admitted")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if raw, err := dialExecution(ctx, path); err == nil {
		_ = raw.Close()
		t.Fatal("unsafe socket admitted")
	}
}
