package window

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

const budgetFile = ".indexer-budget.json"
const budgetLock = ".indexer-budget.lock"
const maxClaims = 256
const maxBudgetJSON = 256 << 10
const VolumeOverheadBytes int64 = 1 << 20 // ledger atomic replacement, marker and locks

type quota struct {
	Name     string
	Path     string // clean path relative to pinned volume root
	Chain    model.FeedChain
	MaxBytes int64
	StoreID  common.Hash // zero only while explicit initialization is incomplete
}
type budgetState struct {
	Version                int
	VolumeID               string
	MaxBytes, MinFreeBytes int64
	Claims                 []quota
	LockID                 common.Hash
	Checksum               common.Hash
}

func budgetHash(b budgetState) (common.Hash, error) {
	b.Checksum = common.Hash{}
	v, err := json.Marshal(b)
	if err != nil {
		return common.Hash{}, err
	}
	return sha256.Sum256(append([]byte("indexer.volume.budget.v1\x00"), v...)), nil
}
func saveBudget(root *os.Root, b budgetState) error {
	sort.Slice(b.Claims, func(i, j int) bool { return b.Claims[i].Name < b.Claims[j].Name })
	h, err := budgetHash(b)
	if err != nil {
		return err
	}
	b.Checksum = h
	v, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if len(v) > maxBudgetJSON {
		return ErrCapacity
	}
	return writeAtomic(root, budgetFile, v)
}
func readBudget(root *os.Root, spec VolumeSpec) (budgetState, error) {
	var b budgetState
	v, err := readSmall(root, budgetFile, maxBudgetJSON)
	if err != nil {
		return b, err
	}
	if json.Unmarshal(v, &b) != nil {
		return b, ErrCorrupt
	}
	h, err := budgetHash(b)
	if err != nil {
		return b, err
	}
	if b.Version != 1 || b.VolumeID != spec.ID || b.MaxBytes != spec.MaxBytes || b.MinFreeBytes != spec.MinFreeBytes || h != b.Checksum || len(b.Claims) > maxClaims {
		return b, ErrConflict
	}
	lockID, err := readSmall(root, budgetLock, 32)
	if err != nil {
		return b, err
	}
	if len(lockID) != 32 || b.LockID == (common.Hash{}) || common.BytesToHash(lockID) != b.LockID {
		return b, ErrConflict
	}
	var total int64
	for i, c := range b.Claims {
		if !validVolumeID(c.Name) || !filepath.IsLocal(c.Path) || c.Path == "." || filepath.Clean(c.Path) != c.Path || c.Chain.Validate() != nil || c.MaxBytes < 1 || c.MaxBytes > b.MaxBytes-VolumeOverheadBytes-total || (i > 0 && b.Claims[i-1].Name >= c.Name) {
			return b, ErrCorrupt
		}
		total += c.MaxBytes
		for _, p := range b.Claims[:i] {
			if pathOverlaps(p.Path, c.Path) || (c.StoreID != (common.Hash{}) && p.StoreID == c.StoreID) {
				return b, ErrCorrupt
			}
		}
	}
	return b, nil
}
func pathOverlaps(a, b string) bool {
	sep := string(filepath.Separator)
	return a == b || strings.HasPrefix(a, strings.TrimRight(b, sep)+sep) || strings.HasPrefix(b, strings.TrimRight(a, sep)+sep)
}

// A separate file description per operation makes flock exclusion work both
// between Store instances in one process and across processes/containers.
// No lock is retained during network/DB calls or after the operation returns.
func volumeLock(ctx context.Context, root *os.Root, fn func() error) error {
	f, err := openRegular(root, budgetLock, os.O_RDWR)
	if err != nil {
		return err
	}
	for {
		if err = ctx.Err(); err != nil {
			return errors.Join(err, f.Close())
		}
		err = lockExclusive(f)
		if !errors.Is(err, ErrLocked) {
			break
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return errors.Join(ctx.Err(), f.Close())
		case <-timer.C:
		}
	}
	if err != nil {
		return errors.Join(err, f.Close())
	}
	// A replaced lock path cannot silently establish a second lock domain.
	fi, err := f.Stat()
	if err == nil {
		var pi os.FileInfo
		pi, err = root.Lstat(budgetLock)
		if err == nil && !os.SameFile(fi, pi) {
			err = ErrConflict
		}
	}
	if err == nil {
		err = fn()
	}
	return errors.Join(err, f.Close())
}
func initializeBudget(root *os.Root, spec VolumeSpec) error {
	f, err := openRegular(root, budgetLock, os.O_RDWR)
	if errors.Is(err, os.ErrNotExist) {
		if _, e := root.Lstat(budgetFile); !errors.Is(e, os.ErrNotExist) {
			return ErrConflict
		}
		f, err = openRegular(root, budgetLock, os.O_RDWR|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return err
		}
		var id [32]byte
		_, err = rand.Read(id[:])
		if err == nil {
			_, err = f.Write(id[:])
		}
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close(), syncDir(root))
		if err != nil {
			return err
		}
		f, err = openRegular(root, budgetLock, os.O_RDWR)
	}
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return volumeLock(c, root, func() error {
		b, err := readBudget(root, spec)
		if err == nil {
			// Only an empty ledger can precede publication of the marker.
			// A missing marker over live reservations is not initialization.
			if len(b.Claims) != 0 {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		free, err := freeBytes(dir)
		err = errors.Join(err, dir.Close())
		if err != nil {
			return err
		}
		if uint64(spec.MaxBytes)+uint64(spec.MinFreeBytes) > free {
			return ErrCapacity
		}
		id, err := readSmall(root, budgetLock, 32)
		if err != nil {
			return err
		}
		if len(id) != 32 || common.BytesToHash(id) == (common.Hash{}) {
			return ErrCorrupt
		}
		return saveBudget(root, budgetState{Version: 1, VolumeID: spec.ID, MaxBytes: spec.MaxBytes, MinFreeBytes: spec.MinFreeBytes, Claims: []quota{}, LockID: common.BytesToHash(id)})
	})
}

func (s *Store) claimQuota(bind, create bool) error {
	if s.opts.Volume.MaxBytes == 0 {
		return nil
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return volumeLock(c, s.volume, func() error {
		if err := s.checkMarker(); err != nil {
			return err
		}
		b, err := readBudget(s.volume, s.opts.Volume)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.opts.Volume.Root, s.opts.Path)
		if err != nil {
			return err
		}
		var total int64
		if bind {
			for _, q := range b.Claims {
				if q.Name != s.opts.Reservation && q.StoreID == s.id {
					return ErrConflict
				}
			}
		}
		for i, q := range b.Claims {
			total += q.MaxBytes
			if q.Name == s.opts.Reservation {
				if q.Path != rel || q.Chain != s.opts.Chain || q.MaxBytes != s.opts.MaxBytes || (bind && q.StoreID != (common.Hash{}) && q.StoreID != s.id) {
					return ErrConflict
				}
				if !bind && create && q.StoreID != (common.Hash{}) {
					return ErrConflict
				}
				if bind && q.StoreID == (common.Hash{}) {
					b.Claims[i].StoreID = s.id
					return saveBudget(s.volume, b)
				}
				return nil
			}
			if pathOverlaps(q.Path, rel) {
				return ErrConflict
			}
		}
		if bind || !create {
			return ErrConflict
		}
		if len(b.Claims) >= maxClaims || s.opts.MaxBytes > b.MaxBytes-VolumeOverheadBytes-total {
			return ErrCapacity
		}
		// Persist BEFORE creating directories/bytes. Close/crash never releases
		// a claim: raw data may still exist. No PID/TTL based reclamation.
		b.Claims = append(b.Claims, quota{Name: s.opts.Reservation, Path: rel, Chain: s.opts.Chain, MaxBytes: s.opts.MaxBytes})
		return saveBudget(s.volume, b)
	})
}

func (s *Store) withVolumeWrite(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.opts.Volume.MaxBytes == 0 {
		return fn()
	}
	if err := s.guard(); err != nil {
		return err
	}
	return volumeLock(ctx, s.volume, func() error {
		if err := s.guard(); err != nil {
			return err
		}
		b, err := readBudget(s.volume, s.opts.Volume)
		if err != nil {
			return s.poison(err)
		}
		rel, _ := filepath.Rel(s.opts.Volume.Root, s.opts.Path)
		for _, q := range b.Claims {
			if q.Name == s.opts.Reservation {
				if q.Path != rel || q.Chain != s.opts.Chain || q.MaxBytes != s.opts.MaxBytes || q.StoreID != s.id {
					return s.poison(ErrConflict)
				}
				return fn()
			}
		}
		return s.poison(ErrConflict)
	})
}

type Usage struct {
	Reservation                              string
	FileBytes, LimitBytes, IndexReserveBytes int64
	VolumeAllocatedBytes, VolumeLimitBytes   int64 // static quotas, including offline stores
	FreeBytes                                uint64
	MinFreeBytes                             int64
	Severity                                 string // normal, warning (80%), critical (95%)
	Writable                                 bool
	// AppendedBytes counts new frames since this handle was opened; unlike
	// FileBytes, collection does not lower it (Stats.AppendedBytes).
	AppendedBytes int64
}

// Usage reads current physical headroom and the durable allocation ledger.
// Quotas are logical bytes, not filesystem preallocation; other applications
// can consume space independently. The same volume lock serializes cooperating
// writers' free-space checks with their bounded disk writes, not RPC/processing.
func (s *Store) Usage(ctx context.Context) (Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := Usage{Reservation: s.opts.Reservation, FileBytes: s.used, LimitBytes: s.opts.MaxBytes, IndexReserveBytes: s.opts.reservation(), Severity: "normal", AppendedBytes: s.appendedBytes}
	err := s.withVolumeWrite(ctx, func() error {
		if err := s.guard(); err != nil {
			return err
		}
		u.MinFreeBytes = s.minFree()
		free, err := freeBytes(s.dir)
		if err != nil {
			return s.poison(err)
		}
		u.FreeBytes = free
		if s.opts.Volume.MaxBytes > 0 {
			b, err := readBudget(s.volume, s.opts.Volume)
			if err != nil {
				return err
			}
			u.VolumeLimitBytes = b.MaxBytes
			for _, q := range b.Claims {
				u.VolumeAllocatedBytes += q.MaxBytes
			}
		}
		roomErr := s.room(0)
		if roomErr != nil && !errors.Is(roomErr, ErrCapacity) {
			return roomErr
		}
		u.Writable = roomErr == nil
		used := float64(u.FileBytes+u.IndexReserveBytes) / float64(u.LimitBytes)
		if used >= 0.8 {
			u.Severity = "warning"
		}
		if used >= 0.95 || !u.Writable {
			u.Severity = "critical"
		}
		if u.VolumeLimitBytes > 0 && free >= uint64(u.MinFreeBytes) {
			margin := free - uint64(u.MinFreeBytes)
			if margin <= uint64(u.VolumeLimitBytes/5) && u.Severity == "normal" {
				u.Severity = "warning"
			}
			if margin <= uint64(u.VolumeLimitBytes/20) {
				u.Severity = "critical"
			}
		}
		return nil
	})
	return u, err
}

func (s *Store) minFree() int64 {
	if s.opts.Volume.MinFreeBytes > s.opts.MinFreeBytes {
		return s.opts.Volume.MinFreeBytes
	}
	return s.opts.MinFreeBytes
}

func (s *Store) sealRoom() error {
	if s.active == nil || len(s.active.refs) == 0 {
		return nil
	}
	// Only the current index/metadata must fit at shutdown. Requiring the
	// full NEXT-append reserve would prevent closing an otherwise valid store.
	n := 2*(int64(indexHeader)+int64(len(s.active.refs))*indexEntry) + 2*maxMeta
	if n > s.opts.MaxBytes-s.used {
		return ErrCapacity
	}
	free, err := freeBytes(s.dir)
	if err != nil {
		return s.poison(err)
	}
	if free < uint64(s.minFree()) || uint64(n+VolumeOverheadBytes) > free-uint64(s.minFree()) {
		return ErrCapacity
	}
	return nil
}

// WaitWritable backs off acquisition without stopping a processor which can
// drain durable raw work and complete GC. It never evicts data or creates a
// polling goroutine. Writable is advisory: an individual payload can require
// more bytes. AppendWait retries that exact payload with bounded backoff.
func (s *Store) WaitWritable(ctx context.Context) error {
	for {
		u, err := s.Usage(ctx)
		if err != nil {
			return err
		}
		if u.Writable {
			return nil
		}
		if err := waitSpace(ctx); err != nil {
			return err
		}
	}
}

func waitSpace(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
