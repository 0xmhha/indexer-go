package window

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// DeletionGuard is supplied by the capture DB coordinator, not a feed or TTL
// timer. It must check persisted required-plan facts, permanent objects,
// finality/reorg policy and every raw-only consumer ACK for all records, and
// hold the current generation fence while calling deleteFiles synchronously.
// It must not call window methods from inside its database critical section.
// There is intentionally no default "allow" guard and no public MarkDone.
// A guard must not reenter Collect/RecoverDeletion/Close on this store.
type DeletionGuard interface {
	WithDeletePermit(context.Context, []Reference, func() error) error
}

// RecordDeletionGuard additionally binds the complete durable intent identity.
// The supplied synchronous, once-only callback succeeds only after the exact
// completion receipt is durable. Legacy guards retain their existing contract.
type RecordDeletionGuard interface {
	WithDeleteRecord(context.Context, DeletionRecord, []Reference, func() error) error
}

type gcMarker struct {
	StoreID        common.Hash
	Segment        uint64
	ReferencesHash common.Hash `json:",omitempty"`
}

const completedGC = "GC.completed.json"

// DeletionRecord is a durable raw approval's identity, not proof of current DB
// completeness. Match checks the exact reference set, including offsets/time.
type DeletionRecord gcMarker

func (r DeletionRecord) Match(refs []Reference) error {
	if len(refs) == 0 || len(refs) > 8192 || r.ReferencesHash == (common.Hash{}) {
		return ErrUnsafeGC
	}
	for _, ref := range refs {
		if ref.StoreID != r.StoreID || ref.Segment != r.Segment {
			return ErrConflict
		}
	}
	digest, err := referencesHash(refs)
	if err != nil {
		return err
	}
	if digest != r.ReferencesHash {
		return ErrConflict
	}
	return nil
}

func referencesHash(refs []Reference) (common.Hash, error) {
	// The encoded Reference fields and domain are part of the receipt ABI.
	// Changing that field set requires an explicit receipt version/migration.
	refs = append([]Reference(nil), refs...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].Offset < refs[j].Offset })
	h := sha256.New()
	_, _ = h.Write([]byte("indexer.window.gc.refs.v1\x00"))
	enc := json.NewEncoder(h)
	for _, r := range refs {
		if err := enc.Encode(r); err != nil {
			return common.Hash{}, err
		}
	}
	var digest common.Hash
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

// Collect deletes complete sealed segments only. A read lease acquired after
// candidate selection still blocks deletion. Guard acquisition happens before
// the store mutex to avoid reversing DB-coordinator/window lock order.
func (s *Store) Collect(ctx context.Context, guard DeletionGuard) (int, error) {
	if guard == nil {
		return 0, ErrUnsafeGC
	}
	s.gcMu.Lock()
	defer s.gcMu.Unlock()
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	if err := s.guard(); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	now := s.observeTime()
	if s.clockUnsafe {
		s.mu.Unlock()
		return 0, ErrUnsafeGC
	}
	var ids []uint64
	for _, seg := range s.segments {
		if seg.leases == 0 && !time.Unix(0, seg.Newest).Add(s.opts.Retention).After(now) {
			ids = append(ids, seg.ID)
		}
	}
	s.mu.Unlock()
	removed := 0
	for _, id := range ids {
		s.mu.Lock()
		seg := s.segmentFor(id)
		if seg == nil || seg.leases > 0 {
			s.mu.Unlock()
			continue
		}
		refs, err := s.indexRefs(seg)
		if err != nil {
			err = s.poison(err)
		}
		s.mu.Unlock()
		if err != nil {
			return removed, err
		}
		var callMu sync.Mutex
		open, called, deleted := true, false, false
		var deleteErr error
		remove := func() error {
			callMu.Lock()
			defer callMu.Unlock()
			if !open || called {
				return ErrUnsafeGC
			}
			called = true
			s.mu.Lock()
			defer s.mu.Unlock()
			deleteErr = s.deleteApproved(ctx, id, refs)
			if _, full := guard.(RecordDeletionGuard); full && deleteErr == nil {
				deleteErr = s.checkRecovery(ctx, refs)
			}
			deleted = deleteErr == nil
			return deleteErr
		}
		if full, ok := guard.(RecordDeletionGuard); ok {
			record, e := DescribeDeletion(refs)
			if e != nil {
				return removed, e
			}
			err = full.WithDeleteRecord(ctx, record, append([]Reference(nil), refs...), remove)
		} else {
			err = guard.WithDeletePermit(ctx, refs, remove)
		}
		callMu.Lock()
		open = false
		if deleted {
			removed++
		}
		if err == nil {
			if !called {
				err = ErrUnsafeGC
			} else {
				err = deleteErr
			}
		}
		callMu.Unlock()
		if err == ErrRetain && !called {
			continue
		}
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (s *Store) deleteApproved(ctx context.Context, id uint64, refs []Reference) error {
	return s.withVolumeWrite(ctx, func() error { return s.deleteApprovedLocked(ctx, id, refs) })
}
func (s *Store) deleteApprovedLocked(ctx context.Context, id uint64, refs []Reference) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.guard(); err != nil {
		return err
	}
	seg := s.segmentFor(id)
	if seg == nil {
		return ErrNotFound
	}
	if seg.leases > 0 {
		return ErrBusy
	}
	now := s.observeTime()
	if s.clockUnsafe || time.Unix(0, seg.Newest).Add(s.opts.Retention).After(now) {
		return ErrUnsafeGC
	}
	digest, err := referencesHash(refs)
	if err != nil {
		return err
	}
	marker := gcMarker{StoreID: s.id, Segment: id, ReferencesHash: digest}
	if err := writeJSON(s.root, segmentName(id, "gc"), marker); err != nil {
		return s.poison(err)
	}
	if s.afterGCIntent != nil {
		s.afterGCIntent()
	}
	if err := s.finishDelete(marker); err != nil {
		return s.poison(err)
	}
	for i, entry := range s.segments {
		if entry.ID == id {
			s.segments = append(s.segments[:i], s.segments[i+1:]...)
			break
		}
	}
	return s.recount()
}

func (s *Store) finishDelete(marker gcMarker) error {
	id := marker.Segment
	for _, suffix := range []string{"raw", "idx", "seal", "idx.tmp", "seal.tmp"} {
		if err := s.root.Remove(segmentName(id, suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if s.afterGCUnlink != nil {
			s.afterGCUnlink()
		}
	}
	if err := syncDir(s.root); err != nil {
		return err
	}
	// One bounded receipt is enough: the capture DB keeps at most one pending
	// deletion and Collect/RecoverDeletion serialize through gcMu. Never infer
	// completion from absence alone. Legacy markers can finish deletion, but
	// cannot prove the exact reference set to a DB recovery coordinator.
	if marker.ReferencesHash != (common.Hash{}) {
		if err := writeJSON(s.root, completedGC, marker); err != nil {
			return err
		}
	}
	if s.afterGCCompletion != nil {
		s.afterGCCompletion()
	}
	if err := s.root.Remove(segmentName(id, "gc")); err != nil {
		return err
	}
	return syncDir(s.root)
}

// RecoverDeletion joins a durable DB permit with this exclusively locked raw
// store. authorize must recheck the exact DB token/generation and required
// facts, and complete must durably clear that token. Neither callback may
// call window methods. gcMu excludes an active Collect and Close, including
// across distinct capture coordinators sharing this Store; the filesystem
// writer lock excludes other processes. Callbacks are synchronous.
func (s *Store) RecoverDeletion(ctx context.Context, refs []Reference, authorize, complete func() error) error {
	if authorize == nil || complete == nil || len(refs) == 0 || len(refs) > s.opts.MaxSegmentRecords {
		return ErrUnsafeGC
	}
	s.gcMu.Lock()
	defer s.gcMu.Unlock()
	s.mu.Lock()
	err := s.checkRecovery(ctx, refs)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := authorize(); err != nil {
		return err
	}
	s.mu.Lock()
	err = s.checkRecovery(ctx, refs)
	if err == nil && s.segmentFor(refs[0].Segment) != nil {
		err = s.deleteApproved(ctx, refs[0].Segment, refs)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return complete()
}

func (s *Store) checkRecovery(ctx context.Context, refs []Reference) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.guard(); err != nil {
		return err
	}
	id := refs[0].Segment
	if id == 0 || id >= s.next || refs[0].StoreID != s.id {
		return ErrConflict
	}
	for _, r := range refs {
		if r.StoreID != s.id || r.Segment != id || r.Block.Chain != s.opts.Chain {
			return ErrConflict
		}
	}
	digest, err := referencesHash(refs)
	if err != nil {
		return err
	}
	if seg := s.segmentFor(id); seg != nil {
		if s.active != nil && s.active.ID == id {
			return ErrUnsafeGC
		}
		actual, err := s.indexRefs(seg)
		if err != nil {
			return s.poison(err)
		}
		got, err := referencesHash(actual)
		if err != nil {
			return err
		}
		if got != digest {
			return ErrConflict
		}
		return nil // lease/age/clock are rechecked by deleteApproved
	}
	data, err := readSmall(s.root, completedGC, maxMeta)
	if err != nil {
		return err
	}
	var receipt gcMarker
	if json.Unmarshal(data, &receipt) != nil || receipt.StoreID != s.id || receipt.Segment != id || receipt.ReferencesHash != digest {
		return ErrConflict
	}
	for _, suffix := range []string{"raw", "idx", "seal", "gc", "idx.tmp", "seal.tmp", "gc.tmp"} {
		if _, err := s.root.Lstat(segmentName(id, suffix)); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return ErrCorrupt
		}
	}
	return syncDir(s.root)
}
