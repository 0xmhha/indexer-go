package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/x/arc/collector"
)

func TestArcCollectorCommandHelpAndExplicitAnchor(t *testing.T) {
	var output bytes.Buffer
	called := false
	start := func(context.Context, collector.Config) error { called = true; return nil }
	if err := run(t.Context(), []string{"--help"}, &output, start); err != nil || called || !bytes.Contains(output.Bytes(), []byte("anchor-height")) {
		t.Fatalf("help: %v", err)
	}
	if err := run(t.Context(), nil, &output, start); err == nil || called {
		t.Fatal("incomplete command started collection")
	}
	hash := "0x0100000000000000000000000000000000000000000000000000000000000000"
	args := []string{"--chain-id", "5042002", "--genesis", hash, "--anchor-height", "0", "--anchor-hash", hash, "--generation", "1", "--native-executable", "/build/arc", "--native-datadir", "/node", "--spool-dir", "/private/spool", "--socket", "/private/indexer.sock"}
	if err := run(t.Context(), args, &output, func(_ context.Context, c collector.Config) error {
		called = true
		if c.Native.Chain.ID != "5042002" || c.Anchor.Height != 0 || c.Anchor.Generation != 1 || c.MaxBytes != 4<<30 || c.MaxRecords != 262144 || c.Window != 8 {
			t.Fatal("defaults/identity changed")
		}
		return nil
	}); err != nil || !called {
		t.Fatalf("explicit command: %v", err)
	}
}
