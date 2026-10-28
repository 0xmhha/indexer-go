package metadata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepeatedSamplesDetectIdentityContentAndAbsentMarkerChanges(t *testing.T) {
	for _, change := range []string{"bytes", "inode", "missing", "directory"} {
		t.Run(change, func(t *testing.T) {
			dir, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "record")
			if change != "missing" {
				if e = os.WriteFile(path, []byte("old"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			r := New(t.Context())
			_, e = r.File(path)
			if e != nil && !os.IsNotExist(e) {
				t.Fatal(e)
			}
			switch change {
			case "bytes":
				e = os.WriteFile(path, []byte("new"), 0600)
			case "inode":
				e = os.Rename(path, path+".old")
				if e == nil {
					e = os.WriteFile(path, []byte("old"), 0600)
				}
			case "missing":
				e = os.WriteFile(path, []byte("new"), 0600)
			case "directory":
				e = os.Rename(dir, dir+".old")
				if e == nil {
					defer func() { _ = os.Remove(dir); _ = os.Rename(dir+".old", dir) }()
					e = os.Mkdir(dir, 0700)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if !errors.Is(r.Verify(), ErrChanged) {
				t.Fatal("changed metadata produced stable observation")
			}
		})
	}
}
func TestStrictPrivateFilesBoundsAndCancellation(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "record")
	if e = os.WriteFile(path, []byte("{}"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = New(t.Context()).File(path); !errors.Is(e, ErrUnsafe) {
		t.Fatal("public file accepted", e)
	}
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		r := New(t.Context())
		if _, e = r.File(path); e != nil {
			t.Fatal(e)
		}
		if e = r.Verify(); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(path, []byte(strings.Repeat(" ", (1<<20)+1)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = New(t.Context()).File(path); e == nil {
		t.Fatal("oversized file accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := New(ctx)
	if _, e = r.File(path); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e = r.Verify(); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	r = New(t.Context())
	for range 32 {
		if e = r.Directory(dir); e != nil {
			t.Fatal(e)
		}
	}
	if e = r.Directory(dir); !errors.Is(e, ErrUnsafe) {
		t.Fatal("unbounded reads")
	}
}
