package spool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectMetadataDoesNotRecoverCreateOrACK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	if _, err := InspectMetadata(t.Context(), path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created path")
	}
	o, _ := fixture(t)
	s, err := Open(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(o.Path, stateName))
	if err != nil {
		t.Fatal(err)
	}
	out, err := InspectMetadata(t.Context(), o.Path)
	if err != nil || out.Committed != o.Anchor {
		t.Fatal(out, err)
	}
	after, err := os.ReadFile(filepath.Join(o.Path, stateName))
	if err != nil || string(before) != string(after) {
		t.Fatal("inspection modified manifest", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = InspectMetadata(ctx, o.Path); err != context.Canceled {
		t.Fatal(err)
	}
}
