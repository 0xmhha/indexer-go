package spool

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/0xmhha/indexer-go/execwire"
)

type Store struct {
	ops      fileOps
	options  Options
	path     string
	gate     chan struct{}
	done     chan struct{}
	once     sync.Once
	closeErr error
	lock     *os.File
	state    manifest
	entries  map[uint64]Entry
	recovery bool
}

const metadataReserve = 64 << 10
const stateName = ".exec-spool-state"
const lockName = ".exec-spool-lock"

func Open(ctx context.Context, o Options) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 4 << 30
	}
	if o.MaxRecords == 0 {
		o.MaxRecords = 262144
	}
	if o.Path == "" || o.MaxBytes <= metadataReserve || o.MaxRecords < 1 || o.MaxRecords > 1<<20 {
		return nil, ErrFull
	}
	if err := validateAnchor(o); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(o.Path)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("%w: directory ownership or permissions", ErrConflict)
	}
	initial := manifest{Version: 1, Chain: o.Chain, Anchor: o.Anchor, Committed: o.Anchor}
	state, err := readManifest(filepath.Join(path, stateName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	missing := errors.Is(err, os.ErrNotExist)
	if !missing && (state.Chain != o.Chain || state.Anchor != o.Anchor) {
		return nil, ErrConflict
	}
	s := &Store{options: o, path: path, gate: make(chan struct{}, 1), done: make(chan struct{}), entries: make(map[uint64]Entry), state: state, ops: defaultOps()}
	s.gate <- struct{}{}
	s.lock, err = takeLock(filepath.Join(path, lockName))
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = s.Close()
		}
	}()
	// The previous owner can finish between the first read and taking the lock.
	// Recovery must use the manifest observed under exclusive ownership.
	state, err = readManifest(filepath.Join(path, stateName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	missing = errors.Is(err, os.ErrNotExist)
	if !missing && (state.Chain != o.Chain || state.Anchor != o.Anchor) {
		return nil, ErrConflict
	}
	s.state = state
	if missing {
		names, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, item := range names {
			if item.Name() != lockName {
				return nil, fmt.Errorf("%w: data without identity manifest", ErrCorrupt)
			}
		}
		s.state = initial
		if err := s.saveState(ctx); err != nil {
			return nil, err
		}
	}
	if err := s.recover(ctx); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

func (s *Store) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return ErrClosed
	case <-s.gate:
	}
	select {
	case <-s.done:
		s.release()
		return ErrClosed
	default:
	}
	if err := ctx.Err(); err != nil {
		s.release()
		return err
	}
	return nil
}
func (s *Store) release() { s.gate <- struct{}{} }

func (s *Store) Append(ctx context.Context, r execwire.Result) (Entry, error) {
	if err := s.acquire(ctx); err != nil {
		return Entry{}, err
	}
	defer s.release()
	if s.recovery {
		return Entry{}, ErrRecovery
	}
	p, err := execwire.MarshalResult(r)
	if err != nil {
		return Entry{}, err
	}
	digest := execwire.Hash(sha256.Sum256(p))
	if old, ok := s.entries[r.Ref.Height]; ok {
		if old.Ref == r.Ref && old.Digest == digest {
			return old, nil
		}
		return Entry{}, ErrConflict
	}
	for _, old := range s.state.History {
		if old.Ref == r.Ref {
			if old.Digest != digest || old.Parent != r.Parent {
				return Entry{}, ErrConflict
			}
			return old, nil
		}
	}
	tail := s.state.Committed
	for _, e := range s.entries {
		if e.Ref.Height > tail.Height {
			tail = e.Ref
		}
	}
	if r.Ref.Generation != tail.Generation || r.Ref.Height != tail.Height+1 || r.Parent != tail.Hash {
		return Entry{}, ErrSequence
	}
	e := Entry{Ref: r.Ref, Parent: r.Parent, Digest: digest}
	header, err := recordBytes(s.options.Chain, e, p)
	if err != nil {
		return Entry{}, err
	}
	e.Size = uint64(len(header)) + uint64(len(p))
	used, records, _, err := s.usage()
	if err != nil {
		return Entry{}, err
	}
	if records >= s.options.MaxRecords || used > s.options.MaxBytes-metadataReserve || e.Size > s.options.MaxBytes-metadataReserve-used {
		return Entry{}, ErrFull
	}
	if err := s.atomic(ctx, "result", recordName(e.Ref), header, p); err != nil {
		return Entry{}, err
	}
	s.entries[e.Ref.Height] = e
	return e, nil
}

func (s *Store) Read(ctx context.Context, ref execwire.Ref) (execwire.Result, error) {
	if err := s.acquire(ctx); err != nil {
		return execwire.Result{}, err
	}
	defer s.release()
	if s.recovery {
		return execwire.Result{}, ErrRecovery
	}
	e, ok := s.entries[ref.Height]
	if !ok || e.Ref != ref {
		return execwire.Result{}, os.ErrNotExist
	}
	return readResult(ctx, filepath.Join(s.path, recordName(ref)), s.options.Chain, e)
}

func (s *Store) Next(ctx context.Context) (Entry, bool, error) {
	if err := s.acquire(ctx); err != nil {
		return Entry{}, false, err
	}
	defer s.release()
	if s.recovery {
		return Entry{}, false, ErrRecovery
	}
	e, ok := s.entries[s.state.Committed.Height+1]
	return e, ok, nil
}

func (s *Store) Acknowledge(ctx context.Context, f execwire.Frame) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	if s.recovery {
		return ErrRecovery
	}
	if f.Kind != execwire.Committed || f.Header.Chain != s.options.Chain || len(f.Payload) != 0 {
		return ErrConflict
	}
	if err := validateACK(f); err != nil {
		return err
	}
	for _, old := range s.state.History {
		if old.Ref == f.Header.Ref && old.Digest == f.Header.Digest {
			return nil
		}
	}
	e, ok := s.entries[s.state.Committed.Height+1]
	if !ok || f.Header.Ref.Height != e.Ref.Height {
		return ErrSequence
	}
	if e.Ref != f.Header.Ref || e.Digest != f.Header.Digest {
		return ErrConflict
	}
	data, err := sealJSON(ackReceipt{Version: 1, Chain: s.options.Chain, Entry: e})
	if err != nil {
		return err
	}
	if err := s.atomic(ctx, "ack", ackName(e.Ref), data); err != nil {
		return err
	}
	if err := s.finishACK(ctx, e); err != nil {
		s.recovery = true
		return err
	}
	return nil
}

func (s *Store) Status(ctx context.Context) (Status, error) {
	if err := s.acquire(ctx); err != nil {
		return Status{}, err
	}
	defer s.release()
	used, _, quarantined, err := s.usage()
	return Status{Pending: len(s.entries), Bytes: used, Committed: s.state.Committed, Digest: s.state.Digest, Quarantined: quarantined}, err
}

func (s *Store) Close() error {
	s.once.Do(func() { close(s.done); <-s.gate; s.closeErr = releaseLock(s.lock) })
	return s.closeErr
}

func (s *Store) finishACK(ctx context.Context, e Entry) error {
	path := filepath.Join(s.path, recordName(e.Ref))
	if _, err := os.Lstat(path); err == nil {
		if err := s.ops.remove(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.ops.syncDir(s.path); err != nil {
		return err
	}
	if e.Ref.Height > s.state.Committed.Height {
		s.state.Committed = e.Ref
		s.state.Digest = e.Digest
		s.state.History = append(s.state.History, e)
		if len(s.state.History) > 128 {
			s.state.History = s.state.History[len(s.state.History)-128:]
		}
		if err := s.saveState(ctx); err != nil {
			return err
		}
	}
	delete(s.entries, e.Ref.Height)
	if err := s.ops.remove(filepath.Join(s.path, ackName(e.Ref))); err != nil {
		return err
	}
	return s.ops.syncDir(s.path)
}

func (s *Store) recover(ctx context.Context) error {
	names, err := os.ReadDir(s.path)
	if err != nil {
		return err
	}
	if len(names) > 2*s.options.MaxRecords+260 {
		return ErrFull
	}
	var receipts []ackReceipt
	var temporary []string
	for _, item := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := item.Name()
		if name == stateName || name == lockName {
			continue
		}
		path := filepath.Join(s.path, name)
		if err := regularPrivate(path); err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(name, ".tmp-result-"), strings.HasPrefix(name, ".tmp-state-"), strings.HasPrefix(name, ".tmp-ack-"):
			temporary = append(temporary, name)
		case strings.HasPrefix(name, ".quarantine-"):
		case strings.HasSuffix(name, ".result"):
			e, err := readEntry(path, s.options.Chain)
			if err != nil {
				return err
			}
			if recordName(e.Ref) != name || e.Ref.Generation != s.state.Committed.Generation {
				return ErrConflict
			}
			if _, ok := s.entries[e.Ref.Height]; ok {
				return ErrConflict
			}
			s.entries[e.Ref.Height] = e
		case strings.HasSuffix(name, ".ack"):
			var receipt ackReceipt
			if err := readSealedJSON(path, &receipt); err != nil {
				return err
			}
			if receipt.Version != 1 || receipt.Chain != s.options.Chain || ackName(receipt.Entry.Ref) != name {
				return ErrCorrupt
			}
			if err := validateReceipt(receipt); err != nil {
				return err
			}
			receipts = append(receipts, receipt)
		default:
			return fmt.Errorf("%w: unexpected spool file", ErrCorrupt)
		}
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Entry.Ref.Height < receipts[j].Entry.Ref.Height })
	for _, receipt := range receipts {
		e := receipt.Entry
		if old, ok := s.entries[e.Ref.Height]; ok && old != e {
			return ErrConflict
		}
		if e.Ref.Height > s.state.Committed.Height {
			if e.Ref.Generation != s.state.Committed.Generation || e.Ref.Height != s.state.Committed.Height+1 || e.Parent != s.state.Committed.Hash {
				return ErrSequence
			}
		} else {
			found := false
			for _, old := range s.state.History {
				if old == e {
					found = true
					break
				}
			}
			if !found {
				return ErrConflict
			}
		}
		if err := s.finishACK(ctx, e); err != nil {
			return err
		}
	}
	for height, e := range s.entries {
		if height > s.state.Committed.Height {
			continue
		}
		known := false
		for _, old := range s.state.History {
			if old == e {
				known = true
				break
			}
		}
		if !known {
			return ErrConflict
		}
		if err := s.ops.remove(filepath.Join(s.path, recordName(e.Ref))); err != nil {
			return err
		}
		delete(s.entries, height)
	}
	for _, name := range temporary {
		if err := s.ops.rename(filepath.Join(s.path, name), filepath.Join(s.path, ".quarantine-"+name)); err != nil {
			return err
		}
	}
	if err := s.ops.syncDir(s.path); err != nil {
		return err
	}
	heights := make([]uint64, 0, len(s.entries))
	for h := range s.entries {
		heights = append(heights, h)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	tail := s.state.Committed
	for _, height := range heights {
		e := s.entries[height]
		if e.Ref.Height != tail.Height+1 || e.Ref.Generation != tail.Generation || e.Parent != tail.Hash {
			return ErrSequence
		}
		tail = e.Ref
	}
	return nil
}
