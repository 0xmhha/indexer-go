package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

func TestArcCollectorCommandProofSelectionBeforeStart(t *testing.T) {
	d := t.TempDir()
	_ = os.Chmod(d, 0700)
	p := filepath.Join(d, "selection.json")
	good := `{"targets":[{"address":"0x0000000000000000000000000000000000000001","code":true,"slots":[]}]}`
	if e := os.WriteFile(p, []byte(good), 0600); e != nil {
		t.Fatal(e)
	}
	hash := "0x0100000000000000000000000000000000000000000000000000000000000000"
	args := []string{"--chain-id", "5042002", "--genesis", hash, "--anchor-height", "0", "--anchor-hash", hash, "--generation", "1", "--native-executable", "/build/arc", "--native-datadir", "/node", "--spool-dir", "/private/spool", "--socket", "/private/indexer.sock", "--state-proofs", p}
	called := false
	var output bytes.Buffer
	start := func(_ context.Context, c collector.Config) error {
		called = true
		if c.Native.StateProofs == nil || len(c.Native.StateProofs.Targets) != 1 || !c.Native.StateProofs.Targets[0].Code {
			t.Fatal("selection lost")
		}
		return nil
	}
	if e := run(t.Context(), args, &output, start); e != nil || !called {
		t.Fatal(e)
	}
	called = false
	_ = os.Chmod(p, 0644)
	if e := run(t.Context(), args, &output, start); e == nil || called {
		t.Fatal("unsafe selection started collector", e)
	}
}

func TestExplicitCollectorConfigNoOverride(t *testing.T) {
	data, err := os.ReadFile("../../../../configs/execution/preflight/synthetic-arc/native.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "collector.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	started := 0
	start := func(_ context.Context, c collector.Config) error {
		started++
		if c.Native.DataDir != "/synthetic/native" {
			t.Fatal("config was overridden")
		}
		return nil
	}
	if err = run(context.Background(), []string{"--config", path}, io.Discard, start); err != nil || started != 1 {
		t.Fatal(err, started)
	}
	if err = run(context.Background(), []string{"--config", path, "--window", "9"}, io.Discard, start); err == nil || started != 1 {
		t.Fatal("mixed overrides accepted")
	}
}

func TestCollectorJSONRequiresExplicitAnchorHeight(t *testing.T) {
	data, err := os.ReadFile("../../../../configs/execution/preflight/synthetic-arc/native.json")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err = json.Unmarshal(data, &obj); err != nil {
		t.Fatal(err)
	}
	anchor := obj["Anchor"].(map[string]any)
	// A valid height-zero tuple must still require an explicitly declared 0.
	anchor["Height"] = float64(0)
	anchor["Hash"] = obj["Native"].(map[string]any)["Chain"].(map[string]any)["Genesis"]
	delete(anchor, "Height")
	data, err = json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "collector.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	started := false
	err = run(context.Background(), []string{"--config", path}, io.Discard, func(context.Context, collector.Config) error { started = true; return nil })
	if err == nil || started {
		t.Fatal("missing height silently became a genesis start")
	}
}
