package spool

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

const gcIntentName = ".exec-spool-gc"
const gcReceiptName = ".exec-spool-gc-completed"
const maxGCFiles = 128

type gcFile struct {
	Name   string
	Digest execwire.Hash
	Size   uint64
}
type gcIntent struct {
	Version uint16
	Permit  retention.Permit
	Files   []gcFile
}
type gcCompletion struct {
	Permit  retention.Permit
	Receipt retention.Receipt
}

func (s *Store) sourceDigest() (execwire.Hash, error) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	if err := enc.Encode(s.state); err != nil {
		return execwire.Hash{}, err
	}
	heights := make([]uint64, 0, len(s.entries))
	for n := range s.entries {
		heights = append(heights, n)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	for _, n := range heights {
		if err := enc.Encode(s.entries[n]); err != nil {
			return execwire.Hash{}, err
		}
	}
	var out execwire.Hash
	copy(out[:], h.Sum(nil))
	return out, nil
}
func (s *Store) retentionRequest(head execwire.Ref, root execwire.Hash, keep uint64) (retention.Request, error) {
	if keep == 0 {
		keep = retention.DefaultKeep
	}
	digest, e := s.sourceDigest()
	if e != nil {
		return retention.Request{}, e
	}
	tail := s.state.Committed
	for _, entry := range s.entries {
		if entry.Ref.Height > tail.Height {
			tail = entry.Ref
		}
	}
	r := retention.Request{Version: 1, Chain: s.state.Chain, Anchor: s.state.Anchor, ACK: s.state.Committed, Tail: tail, Head: head, HeadRoot: root, SourceDigest: digest, KeepBlocks: keep}
	if r.ACK.Height > keep {
		r.Through = r.ACK.Height - keep
	}
	return r, r.Validate()
}
func (s *Store) RetentionRequest(ctx context.Context, head execwire.Ref, root execwire.Hash, keep uint64) (retention.Request, error) {
	if err := s.acquire(ctx); err != nil {
		return retention.Request{}, err
	}
	defer s.release()
	if s.recovery || s.maintenance || s.maintenanceOnly {
		return retention.Request{}, ErrRecovery
	}
	return s.retentionRequest(head, root, keep)
}

// InspectRetention reads existing sealed source metadata under its ownership
// lock, without initialization, ACK/transition recovery, deletion or repair.
func InspectRetention(ctx context.Context, o Options, head execwire.Ref, root execwire.Hash, keep uint64) (retention.Request, error) {
	if err := ctx.Err(); err != nil {
		return retention.Request{}, err
	}
	info, err := os.Lstat(o.Path)
	if err != nil {
		return retention.Request{}, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return retention.Request{}, ErrConflict
	}
	if err = regularPrivate(filepath.Join(o.Path, lockName)); err != nil {
		return retention.Request{}, err
	}
	lock, err := takeLock(filepath.Join(o.Path, lockName))
	if err != nil {
		return retention.Request{}, err
	}
	defer func() { _ = releaseLock(lock) }()
	state, err := readManifest(filepath.Join(o.Path, stateName))
	if err != nil {
		return retention.Request{}, err
	}
	if state.Chain != o.Chain || state.Anchor != o.Anchor {
		return retention.Request{}, ErrConflict
	}
	s := &Store{path: o.Path, state: state, entries: map[uint64]Entry{}}
	if err = s.checkMaintenance(nil); err != nil {
		return retention.Request{}, err
	}
	names, err := os.ReadDir(o.Path)
	if err != nil {
		return retention.Request{}, err
	}
	if len(names) > 2*(1<<20)+260 {
		return retention.Request{}, ErrFull
	}
	for _, item := range names {
		if err = ctx.Err(); err != nil {
			return retention.Request{}, err
		}
		name := item.Name()
		path := filepath.Join(o.Path, name)
		if err = regularPrivate(path); err != nil {
			return retention.Request{}, err
		}
		switch {
		case name == stateName || name == lockName || name == gcReceiptName || strings.HasPrefix(name, ".quarantine-"):
		case strings.HasSuffix(name, ".result"):
			e, err := readEntry(path, o.Chain)
			if err != nil {
				return retention.Request{}, err
			}
			if recordName(e.Ref) != name || e.Ref.Generation != state.Committed.Generation {
				return retention.Request{}, ErrConflict
			}
			s.entries[e.Ref.Height] = e
		default:
			return retention.Request{}, ErrRecovery
		}
	}
	heights := make([]uint64, 0, len(s.entries))
	for n := range s.entries {
		heights = append(heights, n)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	tail := state.Committed
	for _, n := range heights {
		e := s.entries[n]
		if e.Ref.Height != tail.Height+1 || e.Parent != tail.Hash {
			return retention.Request{}, ErrSequence
		}
		tail = e.Ref
	}
	return s.retentionRequest(head, root, keep)
}
func (s *Store) checkMaintenance(permit *retention.Permit) error {
	var done gcCompletion
	err := readSealedJSON(filepath.Join(s.path, gcReceiptName), &done)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && done.Receipt.Match(done.Permit) != nil {
		return ErrCorrupt
	}
	var intent gcIntent
	err = readSealedJSON(filepath.Join(s.path, gcIntentName), &intent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = validateGCIntent(intent); err != nil {
		return err
	}
	if permit == nil {
		return ErrRecovery
	}
	if intent.Permit != *permit {
		return ErrConflict
	}
	if _, err = os.Lstat(filepath.Join(s.path, transitionName)); !errors.Is(err, os.ErrNotExist) {
		return ErrRecovery
	}
	s.maintenance = true
	return nil
}
func validateGCIntent(j gcIntent) error {
	if j.Version != 1 || j.Permit.Validate() != nil || len(j.Files) > maxGCFiles {
		return ErrCorrupt
	}
	prev := ""
	for _, f := range j.Files {
		if f.Name <= prev || filepath.Base(f.Name) != f.Name || !strings.HasPrefix(f.Name, ".quarantine-") || f.Digest == (execwire.Hash{}) || f.Size == 0 || f.Size > execwire.MaxBlockBytes+execwire.MaxHeaderBytes+12 {
			return ErrCorrupt
		}
		prev = f.Name
	}
	return nil
}
func hashGCFile(ctx context.Context, path string) (execwire.Hash, uint64, error) {
	if err := regularPrivate(path); err != nil {
		return execwire.Hash{}, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return execwire.Hash{}, 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, execwire.MaxBlockBytes+execwire.MaxHeaderBytes+13))
	if err != nil {
		return execwire.Hash{}, 0, err
	}
	if n <= 0 || n > execwire.MaxBlockBytes+execwire.MaxHeaderBytes+12 {
		return execwire.Hash{}, 0, ErrCorrupt
	}
	if err = ctx.Err(); err != nil {
		return execwire.Hash{}, 0, err
	}
	var digest execwire.Hash
	copy(digest[:], h.Sum(nil))
	return digest, uint64(n), nil
}
func (s *Store) gcCandidates(ctx context.Context, p retention.Permit) ([]gcFile, error) {
	names, err := os.ReadDir(s.path)
	if err != nil {
		return nil, err
	}
	var files []gcFile
	for _, item := range names {
		name := item.Name()
		if !strings.HasPrefix(name, ".quarantine-") || (!strings.HasSuffix(name, ".result") && !strings.HasSuffix(name, ".ack")) {
			continue
		}
		path := filepath.Join(s.path, name)
		var e Entry
		if strings.HasSuffix(name, ".result") {
			e, err = readEntry(path, s.state.Chain)
			if err == nil && name != ".quarantine-"+recordName(e.Ref) {
				err = ErrCorrupt
			}
		} else {
			var a ackReceipt
			err = readSealedJSON(path, &a)
			e = a.Entry
			if err == nil && (a.Version != 1 || a.Chain != s.state.Chain || validateReceipt(a) != nil || name != ".quarantine-"+ackName(e.Ref)) {
				err = ErrCorrupt
			}
		}
		if err != nil {
			return nil, err
		}
		if e.Ref.Generation >= p.Request.ACK.Generation || e.Ref.Height > p.Request.Through {
			continue
		}
		if e.Ref.Height <= p.Request.Anchor.Height {
			return nil, ErrCorrupt
		}
		if strings.HasSuffix(name, ".result") {
			if _, err = readResult(ctx, path, s.state.Chain, e); err != nil {
				return nil, err
			}
		}
		digest, size, err := hashGCFile(ctx, path)
		if err != nil {
			return nil, err
		}
		files = append(files, gcFile{Name: name, Digest: digest, Size: size})
		if len(files) == maxGCFiles {
			break
		}
	}
	return files, nil
}

// CollectRetention holds the source operation gate (the file-read lease) and
// process lock throughout node postchecks and deletion. The intent remains
// pinned even after success until a DB-issued Release is supplied explicitly.
func (s *Store) CollectRetention(ctx context.Context, p retention.Permit, node func(context.Context) error) (out retention.Receipt, err error) {
	if err = s.acquire(ctx); err != nil {
		return
	}
	defer s.release()
	if p.Validate() != nil || node == nil {
		return out, retention.ErrUnsafe
	}
	r, e := s.retentionRequest(p.Request.Head, p.Request.HeadRoot, p.Request.KeepBlocks)
	if e != nil {
		return out, ErrConflict
	}
	if r != p.Request {
		return out, ErrConflict
	}
	var j gcIntent
	e = readSealedJSON(filepath.Join(s.path, gcIntentName), &j)
	if errors.Is(e, os.ErrNotExist) {
		if s.recovery {
			return out, ErrRecovery
		}
		files, e := s.gcCandidates(ctx, p)
		if e != nil {
			return out, e
		}
		j = gcIntent{Version: 1, Permit: p, Files: files}
		data, e := sealJSON(j)
		if e != nil {
			return out, e
		}
		if len(data) > metadataReserve-4096 {
			return out, ErrFull
		}
		used, _, _, e := s.usage()
		if e != nil {
			return out, e
		}
		if used > s.options.MaxBytes || uint64(len(data)+2048) > s.options.MaxBytes-used {
			return out, ErrFull
		}
		s.maintenance = true
		if e = s.atomic(ctx, "gc", gcIntentName, data); e != nil {
			s.recovery = true
			return out, e
		}
	} else if e != nil {
		return out, e
	}
	if validateGCIntent(j) != nil || j.Permit != p {
		return out, ErrConflict
	}
	s.maintenance = true
	var complete gcCompletion
	e = readSealedJSON(filepath.Join(s.path, gcReceiptName), &complete)
	if e == nil && complete.Permit == p {
		if complete.Receipt.Match(p) != nil {
			return out, ErrCorrupt
		}
		if err = node(ctx); err != nil {
			return out, err
		}
		return complete.Receipt, nil
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return out, e
	}
	// Missing files are accepted only under this exact persisted intent.
	for _, f := range j.Files {
		digest, size, e := hashGCFile(ctx, filepath.Join(s.path, f.Name))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return out, e
		}
		if digest != f.Digest || size != f.Size {
			return out, ErrConflict
		}
	}
	if err = node(ctx); err != nil {
		return out, err
	}
	for _, f := range j.Files {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if e = s.ops.remove(filepath.Join(s.path, f.Name)); e != nil && !errors.Is(e, os.ErrNotExist) {
			return out, e
		}
		out.Files++
		out.Bytes += f.Size
	}
	if err = s.ops.syncDir(s.path); err != nil {
		return out, err
	}
	digest, e := retention.Digest(p)
	if e != nil {
		return out, e
	}
	out.Version, out.PermitDigest, out.Node, out.Spool = 1, digest, true, true
	data, e := sealJSON(gcCompletion{Permit: p, Receipt: out})
	if e != nil {
		return out, e
	}
	if err = s.atomic(ctx, "gc", gcReceiptName, data); err != nil {
		return retention.Receipt{}, err
	}
	return out, nil
}

func (s *Store) ReleaseRetention(ctx context.Context, p retention.Permit, release retention.Release) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	var done gcCompletion
	if err := readSealedJSON(filepath.Join(s.path, gcReceiptName), &done); err != nil {
		return err
	}
	if done.Permit != p || release.Match(p, done.Receipt) != nil {
		return ErrConflict
	}
	var j gcIntent
	err := readSealedJSON(filepath.Join(s.path, gcIntentName), &j)
	if errors.Is(err, os.ErrNotExist) {
		return s.ops.syncDir(s.path)
	}
	if err != nil {
		return err
	}
	if validateGCIntent(j) != nil || j.Permit != p {
		return ErrConflict
	}
	for _, f := range j.Files {
		if _, err = os.Lstat(filepath.Join(s.path, f.Name)); !errors.Is(err, os.ErrNotExist) {
			return ErrConflict
		}
	}
	if err = s.ops.remove(filepath.Join(s.path, gcIntentName)); err != nil {
		return err
	}
	return s.ops.syncDir(s.path)
}

// RetentionCompletion reads an exact persisted completion; callers must still
// verify the node and DB-issued release before reopening source ownership.
func (s *Store) RetentionCompletion(ctx context.Context, p retention.Permit) (retention.Receipt, error) {
	if err := s.acquire(ctx); err != nil {
		return retention.Receipt{}, err
	}
	defer s.release()
	var done gcCompletion
	if err := readSealedJSON(filepath.Join(s.path, gcReceiptName), &done); err != nil {
		return retention.Receipt{}, err
	}
	if done.Permit != p || done.Receipt.Match(p) != nil {
		return retention.Receipt{}, ErrConflict
	}
	return done.Receipt, nil
}

// ResumeCapacity is a read-only capacity observation under exclusive source
// ownership. It does not finish ACK/deletion recovery or certify input integrity.
func ResumeCapacity(ctx context.Context, o Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := regularPrivate(filepath.Join(o.Path, lockName)); err != nil {
		return err
	}
	lock, err := takeLock(filepath.Join(o.Path, lockName))
	if err != nil {
		return err
	}
	defer func() { _ = releaseLock(lock) }()
	m, err := readManifest(filepath.Join(o.Path, stateName))
	if err != nil {
		return err
	}
	if m.Chain != o.Chain || m.Anchor != o.Anchor {
		return ErrConflict
	}
	for _, name := range []string{gcIntentName, transitionName} {
		if _, err = os.Lstat(filepath.Join(o.Path, name)); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return ErrRecovery
		}
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 4 << 30
	}
	if o.MaxRecords == 0 {
		o.MaxRecords = 262144
	}
	if o.MaxBytes <= metadataReserve || o.MaxRecords < 1 {
		return ErrFull
	}
	s := &Store{path: o.Path}
	used, records, _, err := s.usage()
	if err != nil {
		return err
	}
	if used >= o.MaxBytes-o.MaxBytes/5 || records >= o.MaxRecords-o.MaxRecords/5 {
		return ErrFull
	}
	return ctx.Err()
}
