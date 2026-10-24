package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportNeverOverwritesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	out := filepath.Join(dir, "existing.ndjson")
	if err := os.WriteFile(cfg, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), "genesis", cfg, "", "", "", out); !os.IsExist(err) {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "preserve" {
		t.Fatal("existing file changed", err)
	}
}
