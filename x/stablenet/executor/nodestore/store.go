// Package nodestore maintains an offline copy of the pinned StableNet node DB.
// It never constructs a node, imports a peer block or executes the EVM.
package nodestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
)

var ErrUnsafe = errors.New("nodestore: unsafe or unsupported offline maintenance")
var ErrPending = errors.New("nodestore: offline retention is pinned; exact recovery/release required")
var ErrCapacity = errors.New("nodestore: insufficient reserved capacity; source preserved")

type Options struct {
	DataDir      string
	ReserveBytes uint64
}

const fenceName = ".execution-retention"
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
	if !filepath.IsAbs(o.DataDir) || filepath.Clean(o.DataDir) == string(os.PathSeparator) {
		err = ErrUnsafe
		return
	}
	info, e := os.Lstat(o.DataDir)
	if e != nil {
		err = e
		return
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		err = ErrUnsafe
		return
	}
	if _, e = identity(o.DataDir); e != nil {
		err = e
		return
	}
	instance := filepath.Join(o.DataDir, "indexer-stablenet")
	if _, e = identity(instance); e != nil {
		err = e
		return
	}
	path = filepath.Join(instance, "chaindata")
	marker = filepath.Join(o.DataDir, fenceName)
	stage = filepath.Join(instance, ".execution-retention-new")
	backup = filepath.Join(instance, ".execution-retention-old")
	return
}
func CheckStart(dataDir string) error {
	_, e := os.Lstat(filepath.Join(dataDir, fenceName))
	if e == nil {
		return ErrPending
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return nil
}
func Inspect(ctx context.Context, o Options, chain execwire.Chain, anchor execwire.Ref) (State, error) {
	path, _, _, _, e := paths(o)
	if e != nil {
		return State{}, e
	}
	if e = CheckStart(o.DataDir); e != nil {
		return State{}, e
	}
	if _, e = treeBytes(ctx, path); e != nil {
		return State{}, e
	}
	disk, e := openDisk(path, true)
	if e != nil {
		return State{}, e
	}
	st, e := checkDisk(ctx, disk, retention.Request{Chain: chain, Anchor: anchor})
	return st, errors.Join(e, disk.Close())
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
	if p.Validate() != nil {
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
		if _, e = os.Stat(filepath.Join(path, "ancient", "chain")); e != nil {
			return ErrUnsafe
		}
		f.Original, e = identity(path)
		if e != nil {
			return e
		}
		// Verify native locks/identity before pinning the node operation.
		disk, e := openDisk(path, true)
		if e != nil {
			return e
		}
		_, checkErr := checkDisk(ctx, disk, p.Request)
		e = errors.Join(checkErr, disk.Close())
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
		source, e := openDisk(path, true)
		if e != nil {
			return e
		}
		sourceOpen := true
		defer func() {
			if sourceOpen {
				_ = source.Close()
			}
		}()
		if _, e = checkDisk(ctx, source, p.Request); e != nil {
			return e
		}
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
		candidate, e := openDisk(stage, false)
		if e != nil {
			return e
		}
		pruneErr := pruneHot(ctx, candidate, p.Request)
		if pruneErr == nil {
			pruneErr = pruneStateHistory(ctx, candidate, stage, p.Request)
		}
		if pruneErr == nil {
			// Keep the native auxiliary transaction indexer above pruned bodies.
			// The producer does not use this lookup for replay or exported archive data.
			rawWriteTail(candidate, p.Request.Through+1)
			pruneErr = candidate.Compact(nil, nil)
		}
		e = errors.Join(pruneErr, candidate.Close())
		if e != nil {
			return e
		}
		if e = pruneCold(ctx, stage, p.Request); e != nil {
			return e
		}
		if e = writeJSON(filepath.Join(stage, floorName), floor{Version: 1, Permit: p}); e != nil {
			return e
		}
		if e = verifyCandidate(ctx, stage, p); e != nil {
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
		f.Phase = "swap"
		if e = writeJSON(marker, f); e != nil {
			return e
		}
		e = source.Close()
		sourceOpen = false
		if e != nil {
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
			if e = verifyCandidate(ctx, stage, p); e != nil {
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
		if e = verifyCandidate(ctx, path, p); e != nil {
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
		if e = verifyCandidate(ctx, path, p); e != nil {
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
	if e = verifyCandidate(ctx, path, p); e != nil {
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
func verifyCandidate(ctx context.Context, path string, p retention.Permit) error {
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
	disk, e := openDisk(path, true)
	if e != nil {
		return e
	}
	defer func() { _ = disk.Close() }()
	if _, e = checkDisk(ctx, disk, p.Request); e != nil {
		return e
	}
	return checkPayloadBounds(ctx, disk, p.Request)
}
func Release(ctx context.Context, o Options, p retention.Permit, r retention.Receipt, release retention.Release) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if release.Match(p, r) != nil {
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
		return verifyCandidate(ctx, path, p)
	}
	if e != nil {
		return e
	}
	if f.Version != 1 || f.Permit != p || f.Phase != "done" || !sameDir(path, f.Replacement) {
		return ErrPending
	}
	if e = verifyCandidate(ctx, path, p); e != nil {
		return e
	}
	if e = os.Remove(marker); e != nil {
		return e
	}
	return syncDir(o.DataDir)
}

// LookupWindow is enabled only on a compacted producer. A one-block native
// auxiliary lookup avoids background re-indexing of deliberately absent old
// bodies; all archive transaction queries are served by the indexer's DB.
func LookupWindow(dataDir string) (uint64, error) {
	var f floor
	e := readJSON(filepath.Join(dataDir, "indexer-stablenet", "chaindata", floorName), &f)
	if errors.Is(e, os.ErrNotExist) {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	if f.Version != 1 || f.Permit.Validate() != nil {
		return 0, ErrUnsafe
	}
	return 1, nil
}

func rawWriteTail(disk ethdb.Database, n uint64) { rawdb.WriteTxIndexTail(disk, n) }
func checkPayloadBounds(ctx context.Context, disk ethdb.Database, r retention.Request) error {
	for n := r.Anchor.Height + 1; n <= r.Through; n++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		hash := rawdb.ReadCanonicalHash(disk, n)
		if rawdb.ReadHeader(disk, hash, n) == nil || len(rawdb.ReadBodyRLP(disk, hash, n)) != 0 || len(rawdb.ReadReceiptsRLP(disk, hash, n)) != 0 {
			return ErrUnsafe
		}
	}
	// The first retained payload and exact replay tail must still be present.
	for _, n := range []uint64{0, r.Through + 1, r.Tail.Height, r.Head.Height} {
		hash := rawdb.ReadCanonicalHash(disk, n)
		if rawdb.ReadBlock(disk, hash, n) == nil {
			return ErrUnsafe
		}
	}
	return nil
}

// AcquireRun excludes an offline compactor throughout native node ownership.
// The caller has already created/locked the dedicated node directory, but has
// not opened eth services or started import yet.
func AcquireRun(dataDir string) (*os.File, error) {
	lease, e := takeLease(dataDir)
	if e != nil {
		return nil, e
	}
	if e = CheckStart(dataDir); e != nil {
		_ = lease.Close()
		return nil, e
	}
	return lease, nil
}

// CheckCapacity is a functional storage admission check, not a throughput
// measurement. Unobservable/over-budget storage stops the producer; it neither
// deletes raw input nor starts/restarts any process.
func CheckCapacity(ctx context.Context, dataDir string, budget, minFree uint64) error {
	if budget == 0 || minFree == 0 {
		return ErrUnsafe
	}
	if _, e := identity(dataDir); e != nil {
		return e
	}
	size, e := treeBytes(ctx, dataDir)
	if e != nil {
		return e
	}
	free, e := freeBytes(dataDir)
	if e != nil {
		return e
	}
	if size >= budget || free <= minFree {
		return ErrCapacity
	}
	return nil
}
