package window

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
)

const objectHeader = 4096

// ObjectOptions reserves a separate permanent area on an explicitly initialized
// volume. MaxBytes includes staging; MaxObjectBytes bounds streaming work.
// Objects never expire by window retention. Only a DB-fenced orphan collector
// may delete them. The fixed quota survives Close/process death.
type ObjectOptions struct {
	Volume                                 VolumeSpec
	Path, Reservation                      string
	Chain                                  model.FeedChain
	MaxBytes, MinFreeBytes, MaxObjectBytes int64
	MaxObjects                             int
}

// ObjectRef names immutable bytes, not an arbitrary filesystem path or URI.
// IDs are random per upload: retiring an abandoned upload never poisons a later
// upload of identical content. Digest is SHA-256 of the exact encoded bytes.
type ObjectRef struct {
	StoreID common.Hash `json:"storeID"`
	ID      common.Hash `json:"id"`
	Digest  common.Hash `json:"digest"`
	Size    int64       `json:"size"`
}

func (r ObjectRef) Validate() error {
	if r.StoreID == (common.Hash{}) || r.ID == (common.Hash{}) || r.Digest == (common.Hash{}) || r.Size < 0 || r.Size > 16<<30 {
		return ErrCorrupt
	}
	return nil
}
func objectName(id common.Hash) string { return hex.EncodeToString(id[:]) + ".object" }
func objectID(name, suffix string) (common.Hash, bool) {
	var h common.Hash
	if !strings.HasSuffix(name, suffix) || len(name) != 64+len(suffix) {
		return h, false
	}
	b, e := hex.DecodeString(strings.TrimSuffix(name, suffix))
	if e != nil || len(b) != 32 {
		return h, false
	}
	copy(h[:], b)
	return h, h != (common.Hash{})
}

type Objects struct {
	area            *Store
	maxSize         int64
	maxCount, count int
}

func CreateObjects(o ObjectOptions) (*Objects, error) { return openObjects(o, true) }
func OpenObjects(o ObjectOptions) (*Objects, error)   { return openObjects(o, false) }
func openObjects(o ObjectOptions, create bool) (_ *Objects, err error) {
	if o.MaxObjectBytes == 0 {
		o.MaxObjectBytes = 1 << 30
	}
	if o.MaxObjects == 0 {
		o.MaxObjects = 1_000_000
	}
	if o.MaxObjectBytes < 1 || o.MaxObjectBytes > 16<<30 || o.MaxObjects < 1 || o.MaxObjects > 1_000_000 {
		return nil, ErrCapacity
	}
	a, e := openArea(Options{Volume: o.Volume, Path: o.Path, Reservation: o.Reservation, Chain: o.Chain, MaxBytes: o.MaxBytes, MinFreeBytes: o.MinFreeBytes, SegmentBytes: 9 << 20, MaxSegmentRecords: 1}, create, "objects-v1", nil)
	if e != nil {
		return nil, e
	}
	s := &Objects{area: a, maxSize: o.MaxObjectBytes, maxCount: o.MaxObjects}
	defer func() {
		if err != nil {
			err = errors.Join(err, a.Close())
		}
	}()
	err = s.recover()
	if err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Objects) ID() common.Hash                          { return s.area.id }
func (s *Objects) Chain() model.FeedChain                   { return s.area.opts.Chain }
func (s *Objects) Close() error                             { return s.area.Close() }
func (s *Objects) Usage(ctx context.Context) (Usage, error) { return s.area.Usage(ctx) }
func objectMetadata(f *os.File) (ObjectRef, error) {
	var r ObjectRef
	b := make([]byte, objectHeader)
	if _, err := io.ReadFull(f, b); err != nil {
		return r, err
	}
	n := binary.BigEndian.Uint32(b[:4])
	if n == 0 || n > objectHeader-4 {
		return r, ErrCorrupt
	}
	if err := json.Unmarshal(b[4:4+n], &r); err != nil {
		return r, ErrCorrupt
	}
	if err := r.Validate(); err != nil {
		return r, err
	}
	st, err := f.Stat()
	if err != nil {
		return r, err
	}
	if st.Size() != objectHeader+r.Size {
		return r, ErrCorrupt
	}
	return r, nil
}
func (s *Objects) recover() error {
	a := s.area
	d, err := a.root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	a.used = 0
	entries := 0
	for {
		es, e := d.ReadDir(64)
		if e != nil && e != io.EOF {
			return e
		}
		for _, v := range es {
			entries++
			if entries > s.maxCount+4 {
				return ErrCapacity
			}
			name := v.Name()
			fi, e := a.root.Lstat(name)
			if e != nil {
				return e
			}
			if !fi.Mode().IsRegular() {
				return ErrCorrupt
			}
			if _, ok := objectID(name, ".tmp"); ok {
				if e := a.root.Remove(name); e != nil {
					return e
				}
				continue
			}
			if name == "LOCK" || name == "STORE.json" {
				a.used += fi.Size()
				continue
			}
			id, ok := objectID(name, ".object")
			if !ok {
				return ErrCorrupt
			}
			f, e := openRegular(a.root, name, os.O_RDONLY)
			if e != nil {
				return e
			}
			r, e := objectMetadata(f)
			e = errors.Join(e, f.Close())
			if e != nil {
				return e
			}
			if r.ID != id || r.StoreID != a.id || r.Size > s.maxSize {
				return ErrConflict
			}
			a.used += fi.Size()
			s.count++
			if s.count > s.maxCount {
				return ErrCapacity
			}
		}
		if e == io.EOF {
			break
		}
	}
	if err := syncDir(a.root); err != nil {
		return err
	}
	if a.used > a.opts.MaxBytes-a.opts.reservation() {
		return ErrCapacity
	}
	return a.guard()
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

// Put checks declared length and digest while streaming through a 32 KiB
// buffer. It fsyncs data and the rename directory before returning a reference.
// Caller-owned readers must honor cancellation themselves if a Read blocks.
func (s *Objects) Put(ctx context.Context, digest common.Hash, size int64, input io.Reader) (ObjectRef, error) {
	a := s.area
	a.mu.Lock()
	defer a.mu.Unlock()
	var r ObjectRef
	if input == nil || digest == (common.Hash{}) || size < 0 || size > s.maxSize {
		return r, ErrCapacity
	}
	if err := a.guard(); err != nil {
		return r, err
	}
	err := a.withVolumeWrite(ctx, func() (result error) {
		if s.count >= s.maxCount {
			return ErrCapacity
		}
		if err := a.room(size + objectHeader); err != nil {
			return err
		}
		r = ObjectRef{StoreID: a.id, Digest: digest, Size: size}
		if _, e := rand.Read(r.ID[:]); e != nil {
			return e
		}
		tmp := strings.TrimSuffix(objectName(r.ID), ".object") + ".tmp"
		f, e := openRegular(a.root, tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if e != nil {
			return e
		}
		renamed := false
		defer func() {
			if !renamed {
				if err := errors.Join(a.root.Remove(tmp), syncDir(a.root)); err != nil {
					result = errors.Join(result, a.poison(err))
				}
			}
		}()
		b := make([]byte, objectHeader)
		meta, e := json.Marshal(r)
		if e != nil {
			_ = f.Close()
			return e
		}
		binary.BigEndian.PutUint32(b[:4], uint32(len(meta)))
		copy(b[4:], meta)
		_, e = f.Write(b)
		h := sha256.New()
		var n int64
		if e == nil {
			n, e = io.CopyBuffer(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, input}, size+1), make([]byte, 32<<10))
		}
		if e == nil && (n != size || common.BytesToHash(h.Sum(nil)) != digest) {
			e = ErrCorrupt
		}
		if e == nil {
			e = ctx.Err()
		}
		if e == nil {
			e = f.Sync()
		}
		e = errors.Join(e, f.Close())
		if e != nil {
			return e
		}
		if e = a.guard(); e != nil {
			return e
		}
		if e = renameFlat(a.root, tmp, objectName(r.ID)); e != nil {
			return a.poison(e)
		}
		renamed = true
		a.used += objectHeader + size
		s.count++
		if e = syncDir(a.root); e != nil {
			return a.poison(e)
		}
		return a.guard()
	})
	if err != nil {
		return ObjectRef{}, err
	}
	return r, nil
}

// Copy verifies the exact descriptor and digest using bounded memory. A writer
// must not commit derived output until Copy succeeds (a corrupt stream may have
// delivered a prefix). Missing media/bytes are errors, never empty datasets.
func (s *Objects) Copy(ctx context.Context, r ObjectRef, w io.Writer) error {
	a := s.area
	a.mu.Lock()
	defer a.mu.Unlock()
	if w == nil || r.Validate() != nil || r.StoreID != a.id || r.Size > s.maxSize {
		return ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.guard(); err != nil {
		return err
	}
	f, err := openRegular(a.root, objectName(r.ID), os.O_RDONLY)
	if err != nil {
		return err
	}
	got, err := objectMetadata(f)
	if err == nil && got != r {
		err = ErrConflict
	}
	if err == nil {
		h := sha256.New()
		n, e := io.CopyBuffer(io.MultiWriter(w, h), contextReader{ctx, io.LimitReader(f, r.Size)}, make([]byte, 32<<10))
		err = e
		if err == nil && (n != r.Size || common.BytesToHash(h.Sum(nil)) != r.Digest) {
			err = ErrCorrupt
		}
	}
	return errors.Join(err, f.Close(), a.guard())
}
func (s *Objects) Verify(ctx context.Context, r ObjectRef) error { return s.Copy(ctx, r, io.Discard) }

// Candidates scans a bounded directory batch at a time and returns up to 256
// references in ID order. It does not retain a whole-volume index in RAM.
func (s *Objects) Candidates(ctx context.Context, after common.Hash, limit int) ([]ObjectRef, error) {
	a := s.area
	a.mu.Lock()
	defer a.mu.Unlock()
	if limit < 1 || limit > 256 {
		return nil, ErrCapacity
	}
	if err := a.guard(); err != nil {
		return nil, err
	}
	d, e := a.root.Open(".")
	if e != nil {
		return nil, e
	}
	defer func() { _ = d.Close() }()
	ids := make([]string, 0, limit+64)
	for {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		es, e := d.ReadDir(64)
		if e != nil && e != io.EOF {
			return nil, e
		}
		for _, v := range es {
			id, ok := objectID(v.Name(), ".object")
			if ok && strings.Compare(id.Hex(), after.Hex()) > 0 {
				ids = append(ids, v.Name())
			}
		}
		sort.Strings(ids)
		if len(ids) > limit {
			ids = ids[:limit]
		}
		if e == io.EOF {
			break
		}
	}
	out := make([]ObjectRef, 0, len(ids))
	for _, name := range ids {
		f, e := openRegular(a.root, name, os.O_RDONLY)
		if e != nil {
			return nil, e
		}
		r, e := objectMetadata(f)
		e = errors.Join(e, f.Close())
		if e != nil {
			return nil, e
		}
		if r.StoreID != a.id || name != objectName(r.ID) {
			return nil, ErrConflict
		}
		out = append(out, r)
	}
	return out, a.guard()
}

// ObjectDeletionGuard must durably forbid all future DB references before
// invoking deleteFile. No mutex is held during authorization (DB -> area lock
// order is shared with capture Commit). A failed/uncertain DB commit must NOT
// invoke the callback. Retired IDs remain permanently forbidden.
type ObjectDeletionGuard interface {
	WithObjectDeletePermit(context.Context, ObjectRef, func() error) error
}

func (s *Objects) Collect(ctx context.Context, r ObjectRef, guard ObjectDeletionGuard) error {
	if guard == nil || r.Validate() != nil || r.StoreID != s.ID() {
		return ErrUnsafeGC
	}
	return guard.WithObjectDeletePermit(ctx, r, func() error {
		a := s.area
		a.mu.Lock()
		defer a.mu.Unlock()
		if e := a.guard(); e != nil {
			return e
		}
		return a.withVolumeWrite(ctx, func() error {
			f, e := openRegular(a.root, objectName(r.ID), os.O_RDONLY)
			if errors.Is(e, os.ErrNotExist) {
				return errors.Join(syncDir(a.root), a.guard())
			}
			if e != nil {
				return e
			}
			got, e := objectMetadata(f)
			e = errors.Join(e, f.Close())
			if e != nil {
				return e
			}
			if got != r {
				return ErrConflict
			}
			if e = a.root.Remove(objectName(r.ID)); e != nil {
				return e
			}
			a.used -= objectHeader + r.Size
			s.count--
			if e = syncDir(a.root); e != nil {
				return a.poison(e)
			}
			return a.guard()
		})
	})
}
