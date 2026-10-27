package nativestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

type fileIdentity struct{ Device, Inode uint64 }

func sameDir(path string, id fileIdentity) bool {
	got, e := identity(path)
	return e == nil && got == id
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

type sealed struct {
	Data   json.RawMessage
	Digest execwire.Hash
}

func readJSON(path string, v any) error {
	if e := regular(path); e != nil {
		return e
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	data, e := io.ReadAll(io.LimitReader(f, retention.MaxArtifact+1))
	if e != nil {
		return e
	}
	if len(data) > retention.MaxArtifact {
		return ErrUnsafe
	}
	var envelope sealed
	if json.Unmarshal(data, &envelope) != nil || execwire.Hash(sha256.Sum256(envelope.Data)) != envelope.Digest || json.Unmarshal(envelope.Data, v) != nil {
		return ErrUnsafe
	}
	canonical, e := json.Marshal(v)
	if e != nil || !bytes.Equal(canonical, envelope.Data) {
		return ErrUnsafe
	}
	outer, e := json.Marshal(envelope)
	if e != nil || !bytes.Equal(outer, data) {
		return ErrUnsafe
	}
	return nil
}
func writeJSON(path string, v any) error {
	blob, e := json.Marshal(v)
	if e != nil {
		return e
	}
	data, e := json.Marshal(sealed{Data: blob, Digest: execwire.Hash(sha256.Sum256(blob))})
	if e != nil {
		return e
	}
	if len(data) > retention.MaxArtifact {
		return ErrUnsafe
	}
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".retention-")
	if e != nil {
		return e
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, e = f.Write(data); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if _, e = os.Lstat(path); e == nil {
		if e = regular(path); e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return syncDir(dir)
}
func treeBytes(ctx context.Context, path string) (uint64, error) {
	var total uint64
	count := 0
	e := filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		count++
		if count > 1<<20 {
			return ErrUnsafe
		}
		if d.IsDir() {
			_, e = identity(p)
			return e
		}
		if e = regular(p); e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Size() < 0 || uint64(info.Size()) > ^uint64(0)-total {
			return ErrUnsafe
		}
		total += uint64(info.Size())
		return nil
	})
	return total, e
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func copyTree(ctx context.Context, source, dest string) error {
	return copyTreeWithOwner(ctx, source, dest, false)
}
func copyTreeWithOwner(ctx context.Context, source, dest string, preserveOwner bool) error {
	return filepath.WalkDir(source, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			if _, e = identity(path); e != nil {
				return e
			}
			if rel == "." {
				return nil
			}
			return os.Mkdir(target, 0700)
		}
		if rel == ownerName && !preserveOwner {
			return nil
		}
		if e = regular(path); e != nil {
			return e
		}
		from, e := os.Open(path)
		if e != nil {
			return e
		}
		defer func() { _ = from.Close() }()
		to, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(to, contextReader{ctx: ctx, r: from})
		return errors.Join(copyErr, to.Sync(), to.Close())
	})
}

// Callers prove ownership/identity using the durable operation journal before
// entering here. Reject links and foreign owners before removing any file.
func removeTree(path string) error {
	if _, e := treeBytes(context.Background(), path); e != nil {
		return e
	}
	return os.RemoveAll(path)
}
func syncTree(path string) error {
	var dirs []string
	if e := filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	}); e != nil {
		return e
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if e := syncDir(dirs[i]); e != nil {
			return e
		}
	}
	return nil
}
