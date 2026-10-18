package window

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
)

type storeMeta struct {
	Version int
	ID      common.Hash
	Chain   model.FeedChain
	Next    uint64
	Kind    string `json:",omitempty"`
}
type activeSegment struct {
	segment
	file  *os.File
	refs  []Reference
	dirty bool // unpublished append bytes; protected by Store.mu
}

type Store struct {
	mu                  sync.Mutex
	gcMu                sync.Mutex
	opts                Options
	id                  common.Hash
	next                uint64
	volume              *os.Root
	volumeDir           *os.File
	volumeInfo          os.FileInfo
	budgetLockInfo      os.FileInfo
	root                *os.Root
	dir                 *os.File
	dirInfo             os.FileInfo
	lock                *os.File
	segments            []*segment // bounded segment summaries, never all retained records
	active              *activeSegment
	used                int64
	readers             int
	readBytes           int64
	fault               error
	closed              bool
	lastClock           time.Time
	clockUnsafe         bool
	afterDataSync       func() // crash boundary used by subprocess tests
	afterRotateStep     func(string)
	afterGCIntent       func()
	afterGCUnlink       func()
	afterGCCompletion   func()
	checkRecoveryIntent func(DeletionRecord) error // optional coordinated Open, before any recovery unlink
	kind                string                     // empty: raw window; objects-v1: permanent streaming objects
}

// Create is explicit initialization. Open never creates a missing store.
func Create(opts Options) (*Store, error) { return openStore(opts, true, nil) }
func Open(opts Options) (*Store, error)   { return openStore(opts, false, nil) }

// OpenChecked revalidates durable .gc approvals with the DB coordinator under
// the process writer lock BEFORE recovery unlinks any remaining file. The
// synchronous callback must not call window methods. A nil check is rejected;
// standalone Open retains its historical preapproved-marker behavior.
func OpenChecked(opts Options, check func(DeletionRecord) error) (*Store, error) {
	if check == nil {
		return nil, ErrUnsafeGC
	}
	return openStore(opts, false, check)
}

func openStore(opts Options, create bool, check func(DeletionRecord) error) (_ *Store, err error) {
	s, err := openArea(opts, create, "", check)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.closeHandles())
		}
	}()
	if err = s.recover(); err != nil {
		return nil, err
	}
	s.lastClock = s.opts.Now()
	for _, seg := range s.segments {
		if seg.Newest > s.lastClock.UnixNano() {
			s.clockUnsafe = true
		}
	}
	if s.active != nil && s.active.Newest > s.lastClock.UnixNano() {
		s.clockUnsafe = true
	}
	if err = s.guard(); err != nil {
		return nil, err
	}
	return s, nil
}

// openArea shares the pinned directory, mount identity, writer lock and durable
// volume quota contract. Each storage format has its own explicit marker kind.
func openArea(opts Options, create bool, kind string, check func(DeletionRecord) error) (_ *Store, err error) {
	opts, err = opts.normalized()
	if err != nil {
		return nil, err
	}
	s := &Store{opts: opts, checkRecoveryIntent: check, kind: kind}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.closeHandles())
		}
	}()
	s.volume, err = os.OpenRoot(opts.Volume.Root)
	if err != nil {
		return nil, err
	}
	s.volumeDir, err = s.volume.Open(".")
	if err != nil {
		return nil, err
	}
	s.volumeInfo, err = s.volumeDir.Stat()
	if err != nil {
		return nil, err
	}
	if err = checkMount(s.volumeDir, opts.Volume); err != nil {
		return nil, err
	}
	if err = s.checkMarker(); err != nil {
		return nil, err
	}
	if opts.Volume.MaxBytes > 0 {
		s.budgetLockInfo, err = s.volume.Lstat(budgetLock)
		if err != nil {
			return nil, err
		}
	}
	if err = s.claimQuota(false, create); err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(opts.Volume.Root, opts.Path)
	if create {
		if err = mkdirWithin(s.volume, rel); err != nil {
			return nil, err
		}
	}
	s.root, err = s.volume.OpenRoot(rel)
	if err != nil {
		return nil, err
	}
	s.dir, err = s.root.Open(".")
	if err != nil {
		return nil, err
	}
	s.dirInfo, err = s.dir.Stat()
	if err != nil {
		return nil, err
	}
	if same, e := sameDevice(s.volumeDir, s.dir); e != nil || !same {
		return nil, fmt.Errorf("%w: window is on another filesystem: %v", ErrUnavailable, e)
	}
	s.lock, err = openRegular(s.root, "LOCK", os.O_RDWR|os.O_CREATE)
	if err != nil {
		return nil, err
	}
	if err = lockExclusive(s.lock); err != nil {
		return nil, err
	}
	if create {
		entries, e := s.dir.ReadDir(-1)
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			if entry.Name() != "LOCK" {
				return nil, fmt.Errorf("window: refusing to initialize nonempty store")
			}
		}
		if _, err = rand.Read(s.id[:]); err != nil {
			return nil, err
		}
		s.next = 1
		if err = s.writeMeta(); err != nil {
			return nil, err
		}
	} else {
		data, e := readSmall(s.root, "STORE.json", maxMeta)
		if e != nil {
			return nil, e
		}
		var meta storeMeta
		if json.Unmarshal(data, &meta) != nil || meta.Version != 1 || meta.Chain != opts.Chain || meta.ID == (common.Hash{}) || meta.Next == 0 || meta.Kind != kind {
			return nil, ErrConflict
		}
		s.id, s.next = meta.ID, meta.Next
	}
	if err = s.claimQuota(true, create); err != nil {
		return nil, err
	}
	if err = s.guard(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) checkMarker() error {
	data, err := readSmall(s.volume, ".indexer-volume.json", maxMeta)
	if err != nil {
		return err
	}
	var marker volumeMarker
	if json.Unmarshal(data, &marker) != nil || marker != markerFor(s.opts.Volume) {
		return ErrConflict
	}
	return nil
}

func (s *Store) guard() error {
	if s.closed {
		return ErrClosed
	}
	if s.fault != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, s.fault)
	}
	volumeInfo, err := os.Stat(s.opts.Volume.Root)
	if err == nil && !os.SameFile(volumeInfo, s.volumeInfo) {
		err = ErrConflict
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(s.opts.Path)
		if err == nil && !os.SameFile(info, s.dirInfo) {
			err = ErrConflict
		}
	}
	if err == nil {
		err = checkMount(s.volumeDir, s.opts.Volume)
	}
	if err == nil {
		err = s.checkMarker()
	}
	if err == nil && s.opts.Volume.MaxBytes > 0 {
		var info os.FileInfo
		info, err = s.volume.Lstat(budgetLock)
		if err == nil && !os.SameFile(info, s.budgetLockInfo) {
			err = ErrConflict
		}
	}
	if err != nil {
		return s.poison(err)
	}
	return nil
}

func (s *Store) poison(err error) error {
	s.fault = err
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func (s *Store) observeTime() time.Time {
	now := s.opts.Now()
	if !s.lastClock.IsZero() {
		wall := now.Round(0).Sub(s.lastClock.Round(0))
		elapsed := now.Sub(s.lastClock)
		if wall < 0 || wall-elapsed > 5*time.Second || elapsed-wall > 5*time.Second {
			s.clockUnsafe = true
		}
	}
	s.lastClock = now
	return now
}

func (s *Store) writeMeta() error {
	return s.writeMetaUsing(writeAtomic)
}

func (s *Store) writeMetaUsing(write func(*os.Root, string, []byte) error) error {
	data, err := json.Marshal(storeMeta{Version: 1, ID: s.id, Chain: s.opts.Chain, Next: s.next, Kind: s.kind})
	if err != nil {
		return err
	}
	var oldSize int64
	if info, err := s.root.Stat("STORE.json"); err == nil {
		oldSize = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := write(s.root, "STORE.json", data); err != nil {
		return err
	}
	s.used += int64(len(data)) - oldSize
	return nil
}

func (s *Store) room(n int64) error {
	if n < 0 || s.used > s.opts.MaxBytes-s.opts.reservation() || n > s.opts.MaxBytes-s.opts.reservation()-s.used {
		return ErrCapacity
	}
	free, err := freeBytes(s.dir)
	if err != nil {
		return s.poison(err)
	}
	reserve := s.opts.reservation()
	if s.opts.Volume.MaxBytes > 0 {
		reserve += VolumeOverheadBytes
	}
	if free < uint64(s.minFree()) || uint64(n+reserve) > free-uint64(s.minFree()) {
		return ErrCapacity
	}
	return nil
}

func (s *Store) startSegment() error {
	if len(s.segments) >= s.opts.MaxSegments || s.next == ^uint64(0) {
		return ErrCapacity
	}
	id := s.next
	s.next++
	if err := s.writeMeta(); err != nil {
		return s.poison(err)
	}
	return s.createReservedSegment(id)
}

// STORE.json has durably reserved id before a raw directory entry is created.
func (s *Store) createReservedSegment(id uint64) error {
	f, err := openRegular(s.root, segmentName(id, "raw"), os.O_RDWR|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return s.poison(err)
	}
	if err = syncDir(s.root); err != nil {
		_ = f.Close()
		return s.poison(err)
	}
	s.active = &activeSegment{segment: segment{ID: id, MinHeight: ^uint64(0)}, file: f}
	return nil
}

// Append returns only after raw bytes and the directory entry are durable.
// Duplicate input retains its original timestamp; conflicting bytes or codec
// fail without replacing the old record. No DB/projection ACK is emitted.
func (s *Store) Append(ctx context.Context, e chainfeed.Envelope) (Reference, error) {
	region := trace.StartRegion(ctx, "window.append")
	defer region.End()
	if trace.IsEnabled() {
		trace.Logf(ctx, "window.input", "%d %s", e.Block.Number, e.Kind)
	}
	lock := trace.StartRegion(ctx, "window.lock")
	s.mu.Lock()
	lock.End()
	defer s.mu.Unlock()
	var ref Reference
	err := s.withVolumeWrite(ctx, func() error { var err error; ref, err = s.appendLocked(ctx, e, true); return err })
	if err != nil {
		return Reference{}, err
	}
	return ref, nil
}

// AppendWait retains and retries one immutable input on capacity pressure.
// Processing/GC can run while this caller waits. No eviction, RPC retry or
// replacement input is performed; cancellation returns without an ACK.
func (s *Store) AppendWait(ctx context.Context, e chainfeed.Envelope) (Reference, error) {
	for {
		ref, err := s.Append(ctx, e)
		if !errors.Is(err, ErrCapacity) {
			return ref, err
		}
		if err := waitSpace(ctx); err != nil {
			return Reference{}, err
		}
	}
}

func (s *Store) appendLocked(ctx context.Context, e chainfeed.Envelope, flush bool) (Reference, error) {
	if err := ctx.Err(); err != nil {
		return Reference{}, err
	}
	if err := e.Validate(chainfeed.ReadLimits{MaxPayloadBytes: s.opts.MaxPayloadBytes}); err != nil {
		return Reference{}, err
	}
	if len(e.Codec) > 128 || e.Block.Chain != s.opts.Chain {
		return Reference{}, ErrConflict
	}
	if err := s.guard(); err != nil {
		return Reference{}, err
	}
	if ref, err := s.lookup(e.Block, e.Kind); err == nil {
		if ref.Digest != sha256.Sum256(e.Payload) || ref.Codec != e.Codec {
			return Reference{}, ErrConflict
		}
		return ref, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Reference{}, s.poison(err)
	}
	received := s.observeTime().UnixNano()
	if received <= 0 {
		return Reference{}, ErrConflict
	}
	header, meta, err := makeFrame(s.id, e, received)
	if err != nil {
		return Reference{}, err
	}
	n := int64(len(header) + len(meta) + len(e.Payload))
	if err = s.room(n); err != nil {
		return Reference{}, err
	}
	var reserved uint64
	if s.active != nil && (s.active.DataBytes+n > s.opts.SegmentBytes || len(s.active.refs) >= s.opts.MaxSegmentRecords) {
		if err = s.syncPending(ctx); err != nil {
			return Reference{}, err
		}
		rotation := trace.StartRegion(ctx, "window.seal")
		reserved, err = s.sealAndReserve(true)
		rotation.End()
		if err != nil {
			return Reference{}, err
		}
	}
	if s.active == nil {
		create := trace.StartRegion(ctx, "window.create")
		if reserved == 0 {
			err = s.startSegment()
		} else {
			err = s.createReservedSegment(reserved)
			if err == nil {
				s.rotationStep("nextCreated")
			}
		}
		create.End()
		if err != nil {
			return Reference{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return Reference{}, err
	}
	a := s.active
	offset := a.DataBytes
	for _, data := range [][]byte{header, meta, e.Payload} {
		written, e := a.file.WriteAt(data, offset)
		if e != nil {
			return Reference{}, s.poison(e)
		}
		if written != len(data) {
			return Reference{}, s.poison(io.ErrShortWrite)
		}
		offset += int64(written)
	}
	a.dirty = true
	if flush {
		if err = s.syncPending(ctx); err != nil {
			return Reference{}, err
		}
	}
	ref := Reference{StoreID: s.id, Block: e.Block, Kind: e.Kind, Codec: e.Codec, Segment: a.ID, Offset: a.DataBytes, FrameBytes: n, PayloadBytes: int64(len(e.Payload)), Digest: sha256.Sum256(e.Payload), FirstReceived: received}
	a.refs = append(a.refs, ref)
	a.DataBytes += n
	a.Count = len(a.refs)
	if e.Block.Number < a.MinHeight {
		a.MinHeight = e.Block.Number
	}
	if e.Block.Number > a.MaxHeight {
		a.MaxHeight = e.Block.Number
	}
	if received > a.Newest {
		a.Newest = received
	}
	s.used += n
	return ref, nil
}

func (s *Store) seal() error {
	_, err := s.sealAndReserve(false)
	return err
}

func (s *Store) Seal(ctx context.Context) error {
	return s.sealChecked(ctx, false)
}

// SealExpired closes an idle active segment only when its newest record has
// reached retention. GC still rechecks age, leases and its DB permit. This
// avoids rotating small segments on every maintenance tick.
func (s *Store) SealExpired(ctx context.Context) error {
	return s.sealChecked(ctx, true)
}

func (s *Store) sealChecked(ctx context.Context, expiredOnly bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.guard(); err != nil {
		return err
	}
	if expiredOnly {
		now := s.observeTime()
		if s.clockUnsafe {
			return ErrUnsafeGC
		}
		if s.active == nil || s.active.Count == 0 || time.Unix(0, s.active.Newest).Add(s.opts.Retention).After(now) {
			return nil
		}
	}
	return s.withVolumeWrite(ctx, func() error {
		if s.opts.Volume.MaxBytes > 0 && s.active != nil && len(s.active.refs) > 0 {
			if err := s.sealRoom(); err != nil {
				return err
			}
		}
		return s.seal()
	})
}

func (s *Store) lookup(block model.FeedBlock, kind model.DatasetKind) (Reference, error) {
	if block.Chain != s.opts.Chain || block.Validate() != nil || !kind.Valid() {
		return Reference{}, ErrConflict
	}
	key := Reference{Block: block, Kind: kind}
	if s.active != nil {
		for _, ref := range s.active.refs {
			if compareRef(ref, key) == 0 {
				return ref, nil
			}
		}
	}
	for i := len(s.segments) - 1; i >= 0; i-- {
		seg := s.segments[i]
		if block.Number < seg.MinHeight || block.Number > seg.MaxHeight {
			continue
		}
		refs, err := s.indexRefs(seg)
		if err != nil {
			return Reference{}, err
		}
		j := sort.Search(len(refs), func(i int) bool { return compareRef(refs[i], key) >= 0 })
		if j < len(refs) && compareRef(refs[j], key) == 0 {
			return refs[j], nil
		}
	}
	return Reference{}, ErrNotFound
}

func (s *Store) Lookup(ctx context.Context, block model.FeedBlock, kind model.DatasetKind) (Reference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Reference{}, err
	}
	if err := s.guard(); err != nil {
		return Reference{}, err
	}
	ref, err := s.lookup(block, kind)
	if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrConflict) {
		return Reference{}, s.poison(err)
	}
	return ref, err
}

type Stats struct {
	FileBytes                                      int64
	Segments, Records, ActiveIndexRecords, Readers int
	ReadReservedBytes                              int64
	ClockUnsafe                                    bool
}

func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Stats{FileBytes: s.used, Segments: len(s.segments), Readers: s.readers, ReadReservedBytes: s.readBytes, ClockUnsafe: s.clockUnsafe}
	for _, seg := range s.segments {
		st.Records += seg.Count
	}
	if s.active != nil {
		st.Segments++
		st.Records += s.active.Count
		st.ActiveIndexRecords = len(s.active.refs)
	}
	return st
}

func (s *Store) closeHandles() error {
	var err error
	if s.active != nil && s.active.file != nil {
		err = errors.Join(err, s.active.file.Close())
		s.active = nil
	}
	for _, f := range []*os.File{s.lock, s.dir, s.volumeDir} {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
	}
	for _, r := range []*os.Root{s.root, s.volume} {
		if r != nil {
			err = errors.Join(err, r.Close())
		}
	}
	return err
}

func (s *Store) Close() error {
	s.gcMu.Lock()
	defer s.gcMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if s.readers > 0 {
		return ErrBusy
	}
	var err error
	if s.fault == nil {
		if e := s.guard(); e == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = s.withVolumeWrite(ctx, func() error {
				if s.opts.Volume.MaxBytes > 0 && s.active != nil && len(s.active.refs) > 0 {
					if err := s.sealRoom(); err != nil {
						return err
					}
				}
				return s.seal()
			})
			cancel()
		} else {
			err = e
		}
	}
	s.closed = true
	return errors.Join(err, s.closeHandles())
}

func (s *Store) entries() ([]os.DirEntry, error) {
	dir, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	var all []os.DirEntry
	for {
		batch, err := dir.ReadDir(128)
		all = append(all, batch...)
		if len(all) > 4*s.opts.MaxSegments+16 {
			return nil, ErrCapacity
		}
		if err == io.EOF {
			return all, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (s *Store) recount() error {
	entries, err := s.entries()
	if err != nil {
		return s.poison(err)
	}
	var total int64
	for _, e := range entries {
		info, err := s.root.Lstat(e.Name())
		if err != nil {
			return s.poison(err)
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<60-total {
			return s.poison(ErrCorrupt)
		}
		total += info.Size()
	}
	s.used = total
	return nil
}

func parseSegment(name string) (uint64, string, error) {
	parts := strings.Split(name, ".")
	if len(parts) < 2 || len(parts[0]) != 20 {
		return 0, "", ErrCorrupt
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || id == 0 {
		return 0, "", ErrCorrupt
	}
	return id, strings.Join(parts[1:], "."), nil
}

func (s *Store) recover() error {
	entries, err := s.entries()
	if err != nil {
		return err
	}
	// A durable GC marker records approval before unlink. Finish only those
	// deletions; neither an age threshold nor a missing index is approval.
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gc") {
			id, _, err := parseSegment(e.Name())
			if err != nil {
				return err
			}
			data, err := readSmall(s.root, e.Name(), maxMeta)
			if err != nil {
				return err
			}
			var marker gcMarker
			if json.Unmarshal(data, &marker) != nil || marker.StoreID != s.id || marker.Segment != id || id >= s.next {
				return ErrCorrupt
			}
			if s.checkRecoveryIntent != nil {
				if err := s.checkRecoveryIntent(DeletionRecord(marker)); err != nil {
					return err
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = s.withVolumeWrite(ctx, func() error { return s.finishDelete(marker) })
			cancel()
			if err != nil {
				return err
			}
		}
	}
	entries, err = s.entries()
	if err != nil {
		return err
	}
	raws := map[uint64]bool{}
	seals := map[uint64]segment{}
	for _, e := range entries {
		name := e.Name()
		if name == completedGC {
			data, err := readSmall(s.root, name, maxMeta)
			if err != nil {
				return err
			}
			var receipt gcMarker
			if json.Unmarshal(data, &receipt) != nil || receipt.StoreID != s.id || receipt.Segment == 0 || receipt.Segment >= s.next || receipt.ReferencesHash == (common.Hash{}) {
				return ErrCorrupt
			}
			continue
		}
		if name == completedGC+".tmp" {
			if err := s.root.Remove(name); err != nil {
				return err
			}
			continue
		}
		if name == "LOCK" || name == "STORE.json" {
			continue
		}
		if name == "STORE.json.tmp" {
			if err = s.root.Remove(name); err != nil {
				return err
			}
			continue
		}
		id, suffix, err := parseSegment(name)
		if err != nil || id >= s.next {
			return ErrCorrupt
		}
		switch suffix {
		case "raw":
			raws[id] = true
		case "idx":
		case "seal":
			data, err := readSmall(s.root, name, maxMeta)
			if err != nil {
				return err
			}
			var seg segment
			if json.Unmarshal(data, &seg) != nil || seg.ID != id || seg.Count < 1 || seg.Count > s.opts.MaxSegmentRecords || seg.DataBytes < 1 || seg.DataBytes > s.opts.SegmentBytes {
				return ErrCorrupt
			}
			seals[id] = seg
		case "idx.tmp", "seal.tmp", "gc.tmp":
			if err = s.root.Remove(name); err != nil {
				return err
			}
		default:
			return ErrCorrupt
		}
	}
	if len(raws) > s.opts.MaxSegments {
		return ErrCapacity
	}
	ids := make([]uint64, 0, len(raws))
	for id := range raws {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for id := range seals {
		if !raws[id] {
			return ErrCorrupt
		}
	}
	for i, id := range ids {
		f, err := openRegular(s.root, segmentName(id, "raw"), os.O_RDWR)
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return err
		}
		if seg, sealed := seals[id]; sealed {
			if err := f.Close(); err != nil {
				return err
			}
			if info.Size() != seg.DataBytes {
				return ErrCorrupt
			}
			refs, err := s.indexRefs(&seg)
			if err != nil {
				return err
			}
			if summarize(id, seg.DataBytes, refs, seg.IndexDigest) != seg {
				return ErrCorrupt
			}
			sort.Slice(refs, func(i, j int) bool { return refs[i].Offset < refs[j].Offset })
			var offset int64
			for _, r := range refs {
				if r.Offset != offset {
					return ErrCorrupt
				}
				offset += r.FrameBytes
			}
			if offset != seg.DataBytes {
				return ErrCorrupt
			}
			s.segments = append(s.segments, &seg)
			continue
		}
		if i != len(ids)-1 || info.Size() > s.opts.SegmentBytes {
			_ = f.Close()
			return ErrCorrupt
		}
		a := &activeSegment{segment: segment{ID: id}, file: f}
		s.active = a
		for a.DataBytes < info.Size() {
			r, _, partial, err := readFrame(f, a.DataBytes, info.Size(), s.opts, s.id, false)
			if err != nil {
				return err
			}
			if partial {
				if err = f.Truncate(a.DataBytes); err != nil {
					return err
				}
				if err = f.Sync(); err != nil {
					return err
				}
				break
			}
			if len(a.refs) >= s.opts.MaxSegmentRecords {
				return ErrCorrupt
			}
			r.Segment = id
			a.refs = append(a.refs, r)
			a.DataBytes += r.FrameBytes
		}
		summary := summarize(id, a.DataBytes, a.refs, common.Hash{})
		a.segment = summary
		// Duplicate keys in a recoverable tail are corruption, not another
		// opportunity to replace an immutable record.
		seen := map[string]bool{}
		for _, r := range a.refs {
			k := fmt.Sprint(r.Block.Number, r.Block.Hash, r.Kind)
			if seen[k] {
				return ErrCorrupt
			}
			seen[k] = true
		}
	}
	if err = syncDir(s.root); err != nil {
		return err
	}
	return s.recount()
}
