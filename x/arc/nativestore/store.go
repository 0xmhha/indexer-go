// Package nativestore maintains a fenced offline copy of the pinned Arc native DB.
// Native layout operations are delegated to the independently verified adapter.
package nativestore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

var ErrUnsafe = errors.New("arc native store: unsafe or unsupported offline maintenance")
var ErrPending = errors.New("arc native store: offline retention is pinned; exact recovery/release required")
var ErrCapacity = errors.New("arc native store: insufficient reserved capacity; source preserved")

type Adapter interface {
	Lock(context.Context, string, retention.Request) (io.Closer, error)
	Prune(context.Context, string, retention.Permit) error
	Verify(context.Context, string, retention.Permit) error
}

type Options struct {
	Adapter      Adapter
	DataDir      string
	ReserveBytes uint64
}

const ownerName = ".execution-retention-copy"
const floorName = ".execution-retention-floor"

type fence struct {
	Version               uint16
	Permit                retention.Permit
	Phase                 string
	Original, Replacement fileIdentity
}
type copyOwner struct{ PermitDigest execwire.Hash }
type floor struct {
	Version uint16
	Permit  retention.Permit
}

type hooks struct {
	phase func(string) error
	free  func(string) (uint64, error)
}

func (h hooks) at(name string) error {
	if h.phase != nil {
		return h.phase(name)
	}
	return nil
}
func paths(o Options) (path, marker, stage, backup string, err error) {
	if !filepath.IsAbs(o.DataDir) || filepath.Clean(o.DataDir) == string(os.PathSeparator) || strings.Contains(filepath.Base(o.DataDir), ".execution-retention") {
		err = ErrUnsafe
		return
	}
	path = filepath.Clean(o.DataDir)
	info, e := os.Lstat(path)
	// After the first rename, same-permit recovery has no current path yet.
	// The sealed original/replacement identities below authorize that boundary.
	if errors.Is(e, os.ErrNotExist) {
		info, e = os.Lstat(filepath.Dir(path))
	}
	if e != nil {
		err = e
		return
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		err = ErrUnsafe
		return
	}
	if _, e = identity(filepath.Dir(path)); e != nil {
		err = e
		return
	}
	path = filepath.Clean(o.DataDir)
	parent := filepath.Dir(path)
	info, e = os.Lstat(parent)
	if e != nil {
		err = e
		return
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		err = ErrUnsafe
		return
	}
	if _, e = identity(parent); e != nil {
		err = e
		return
	}
	base := "." + filepath.Base(path) + ".execution-retention"
	marker = filepath.Join(parent, base)
	stage = marker + "-new"
	backup = marker + "-old"
	return
}
func CheckStart(dataDir string) error {
	// A detached matched snapshot carries its pending intent inside the copy.
	// Its archived inode journal is evidence, never destination start authority.
	if _, err := os.Lstat(filepath.Join(dataDir, ".execution-retention-pending")); err == nil {
		return ErrPending
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, e := os.Lstat(filepath.Join(filepath.Dir(dataDir), "."+filepath.Base(dataDir)+".execution-retention"))
	if e == nil {
		return ErrPending
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return nil
}
func Status(o Options) (p retention.Permit, phase string, err error) {
	_, marker, _, _, err := paths(o)
	if err != nil {
		return
	}
	var f fence
	err = readJSON(marker, &f)
	if err == nil && (f.Version != 1 || f.Permit.Validate() != nil) {
		err = ErrUnsafe
	}
	return f.Permit, f.Phase, err
}

// Compact creates a complete replacement under an exclusive offline DB lease.
// The original remains intact until replacement postchecks and durable swap
// intent. Explicit same-permit recovery finishes each boundary idempotently.
func Compact(ctx context.Context, o Options, p retention.Permit) error {
	return compact(ctx, o, p, hooks{})
}
func compact(ctx context.Context, o Options, p retention.Permit, h hooks) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if p.Validate() != nil || o.Adapter == nil {
		return ErrUnsafe
	}
	path, marker, stage, backup, e := paths(o)
	if e != nil {
		return e
	}
	lease, e := takeLease(o.DataDir)
	if e != nil {
		return e
	}
	defer func() { _ = lease.Close() }()
	var f fence
	e = readJSON(marker, &f)
	if errors.Is(e, os.ErrNotExist) {
		if _, e = os.Lstat(stage); !errors.Is(e, os.ErrNotExist) {
			return ErrUnsafe
		}
		if _, e = os.Lstat(backup); !errors.Is(e, os.ErrNotExist) {
			return ErrUnsafe
		}
		f = fence{Version: 1, Permit: p, Phase: "copy"}
		f.Original, e = identity(path)
		if e != nil {
			return e
		}
		// Verify native locks/identity before pinning the node operation.
		disk, e := o.Adapter.Lock(ctx, path, p.Request)
		if e != nil {
			return e
		}
		e = disk.Close()
		if e != nil {
			return e
		}
		if e = writeJSON(marker, f); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	if f.Version != 1 || f.Permit != p {
		return ErrUnsafe
	}
	if e = h.at("intent"); e != nil {
		return e
	}
	if f.Phase == "copy" {
		if !sameDir(path, f.Original) {
			return ErrUnsafe
		}
		source, e := o.Adapter.Lock(ctx, path, p.Request)
		if e != nil {
			return e
		}
		sourceOpen := true
		defer func() {
			if sourceOpen {
				_ = source.Close()
			}
		}()
		if _, e = os.Lstat(stage); e == nil {
			if e = ownedCandidate(stage, p); e != nil {
				return e
			}
			if e = removeTree(stage); e != nil {
				return e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		size, e := treeBytes(ctx, path)
		if e != nil {
			return e
		}
		probe := h.free
		if probe == nil {
			probe = freeBytes
		}
		free, e := probe(filepath.Dir(path))
		if e != nil {
			return e
		}
		reserve := o.ReserveBytes
		if reserve == 0 {
			reserve = 1 << 30
		}
		if size > (^uint64(0)-reserve)/2 || free < 2*size+reserve {
			return ErrCapacity
		}
		if e = os.Mkdir(stage, 0700); e != nil {
			return e
		}
		digest, e := retention.Digest(p)
		if e != nil {
			return e
		}
		if e = writeJSON(filepath.Join(stage, ownerName), copyOwner{PermitDigest: digest}); e != nil {
			return e
		}
		if e = copyTree(ctx, path, stage); e != nil {
			return e
		}
		if e = h.at("copy"); e != nil {
			return e
		}
		if e = o.Adapter.Prune(ctx, stage, p); e != nil {
			return e
		}

		if e = writeJSON(filepath.Join(stage, floorName), floor{Version: 1, Permit: p}); e != nil {
			return e
		}
		if e = verifyCandidate(ctx, o, stage, p); e != nil {
			return e
		}
		if e = syncTree(stage); e != nil {
			return e
		}
		if e = h.at("candidate"); e != nil {
			return e
		}
		f.Replacement, e = identity(stage)
		if e != nil {
			return e
		}
		e = source.Close()
		sourceOpen = false
		if e != nil {
			return e
		}
		f.Phase = "swap"
		if e = writeJSON(marker, f); e != nil {
			return e
		}
	}
	if f.Phase == "swap" {
		if e = h.at("swap_intent"); e != nil {
			return e
		}
		if sameDir(path, f.Original) {
			if _, e = os.Lstat(backup); !errors.Is(e, os.ErrNotExist) {
				return ErrUnsafe
			}
			if !sameDir(stage, f.Replacement) {
				return ErrUnsafe
			}
			if e = verifyCandidate(ctx, o, stage, p); e != nil {
				return e
			}
			if e = os.Rename(path, backup); e != nil {
				return e
			}
			if e = syncDir(filepath.Dir(path)); e != nil {
				return e
			}
		}
		if e = h.at("old_rename"); e != nil {
			return e
		}
		if _, e = os.Lstat(path); errors.Is(e, os.ErrNotExist) {
			if !sameDir(backup, f.Original) || !sameDir(stage, f.Replacement) {
				return ErrUnsafe
			}
			if e = os.Rename(stage, path); e != nil {
				return e
			}
			if e = syncDir(filepath.Dir(path)); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if !sameDir(path, f.Replacement) || !sameDir(backup, f.Original) {
			return ErrUnsafe
		}
		if e = h.at("new_rename"); e != nil {
			return e
		}
		if e = verifyCandidate(ctx, o, path, p); e != nil {
			return e
		}
		f.Phase = "cleanup"
		if e = writeJSON(marker, f); e != nil {
			return e
		}
	}
	if f.Phase == "cleanup" {
		if !sameDir(path, f.Replacement) {
			return ErrUnsafe
		}
		if e = verifyCandidate(ctx, o, path, p); e != nil {
			return e
		}
		if e = h.at("cleanup_intent"); e != nil {
			return e
		}
		if _, e = os.Lstat(backup); e == nil {
			if !sameDir(backup, f.Original) {
				return ErrUnsafe
			}
			if e = removeTree(backup); e != nil {
				return e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e = syncDir(filepath.Dir(path)); e != nil {
			return e
		}
		if e = h.at("cleanup"); e != nil {
			return e
		}
		f.Phase = "done"
		if e = writeJSON(marker, f); e != nil {
			return e
		}
	}
	if f.Phase != "done" || !sameDir(path, f.Replacement) {
		return ErrUnsafe
	}
	if e = verifyCandidate(ctx, o, path, p); e != nil {
		return e
	}
	return h.at("done")
}
func ownedCandidate(path string, p retention.Permit) error {
	if _, e := identity(path); e != nil {
		return e
	}
	var owner copyOwner
	e := readJSON(filepath.Join(path, ownerName), &owner)
	if errors.Is(e, os.ErrNotExist) {
		entries, e := os.ReadDir(path)
		if e == nil && len(entries) == 0 {
			return nil
		}
		return ErrUnsafe
	}
	digest, err := retention.Digest(p)
	if e != nil || err != nil || owner.PermitDigest != digest {
		return ErrUnsafe
	}
	return nil
}
func verifyCandidate(ctx context.Context, o Options, path string, p retention.Permit) error {
	if e := ownedCandidate(path, p); e != nil {
		return e
	}
	var record floor
	if e := readJSON(filepath.Join(path, floorName), &record); e != nil {
		return e
	}
	if record.Version != 1 || record.Permit != p {
		return ErrUnsafe
	}
	return o.Adapter.Verify(ctx, path, p)
}

func Release(ctx context.Context, o Options, p retention.Permit, r retention.Receipt, release retention.Release) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if release.Match(p, r) != nil || o.Adapter == nil {
		return ErrUnsafe
	}
	path, marker, _, _, e := paths(o)
	if e != nil {
		return e
	}
	lease, e := takeLease(o.DataDir)
	if e != nil {
		return e
	}
	defer func() { _ = lease.Close() }()
	var f fence
	e = readJSON(marker, &f)
	if errors.Is(e, os.ErrNotExist) {
		return verifyCandidate(ctx, o, path, p)
	}
	if e != nil {
		return e
	}
	if f.Version != 1 || f.Permit != p || f.Phase != "done" || !sameDir(path, f.Replacement) {
		return ErrPending
	}
	if e = verifyCandidate(ctx, o, path, p); e != nil {
		return e
	}
	if e = os.Remove(marker); e != nil {
		return e
	}
	return syncDir(o.DataDir)
}

// AcquireRun excludes an offline compactor throughout native node ownership.
// The caller has already created/locked the dedicated node directory, but has
// not opened eth services or started import yet.
func AcquireRun(dataDir string) (*os.File, error) {
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) == string(os.PathSeparator) {
		return nil, ErrUnsafe
	}
	if _, e := identity(filepath.Dir(dataDir)); e != nil {
		return nil, e
	}
	info, e := os.Lstat(filepath.Dir(dataDir))
	if e != nil {
		return nil, e
	}
	if info.Mode().Perm()&0022 != 0 {
		return nil, ErrUnsafe
	}
	lease, e := takeRunLease(dataDir)
	if e != nil {
		return nil, e
	}
	if e = CheckStart(dataDir); e != nil {
		_ = lease.Close()
		return nil, e
	}
	return lease, nil
}

// WithRetentionSnapshot keeps the native maintenance lease and native-library
// hold until the callback has copied the complete verified store. It requires
// the current completed COW journal; an archived permit alone is insufficient.
func WithRetentionSnapshot(ctx context.Context, o Options, p retention.Permit, copySnapshot func(string) error) (err error) {
	if p.Validate() != nil || o.Adapter == nil || copySnapshot == nil {
		return ErrUnsafe
	}
	path, marker, _, _, err := paths(o)
	if err != nil {
		return err
	}
	lease, err := takeLease(o.DataDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	var f fence
	if err = readJSON(marker, &f); err != nil {
		return err
	}
	if f.Version != 1 || f.Permit != p || f.Phase != "done" || !sameDir(path, f.Replacement) {
		return ErrPending
	}
	if err = verifyCandidate(ctx, o, path, p); err != nil {
		return err
	}
	hold, err := o.Adapter.Lock(ctx, path, p.Request)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, hold.Close()) }()
	if err = ctx.Err(); err != nil {
		return err
	}
	return errors.Join(copySnapshot(path), ctx.Err())
}
