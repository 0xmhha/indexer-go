package window

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const ownedRawKind = "owned-raw-v1"
const ownerName = "OWNER.json"
const maxOwnerState = 4096

// Owner is bounded intake control, never publication, ACK or deletion authority.
type Owner struct {
	Version uint8
	StoreID common.Hash
	Domain  string
	State   json.RawMessage
}

type ownerSeal struct {
	Data   json.RawMessage
	Digest common.Hash
}

func validOwnerDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 128 {
		return false
	}
	for _, c := range domain {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func validOwnerState(state []byte) bool {
	if len(state) < 2 || len(state) > maxOwnerState || state[0] != '{' || !json.Valid(state) {
		return false
	}
	canonical, err := json.Marshal(json.RawMessage(state))
	return err == nil && bytes.Equal(canonical, state)
}

// CreateOwned initializes a NEW managed raw window. Its kind prevents legacy
// capture from adopting it. A missing/partial control is never regenerated.
func CreateOwned(opts Options, domain string, state []byte) (*Store, error) {
	if !validOwnerDomain(domain) || !validOwnerState(state) {
		return nil, ErrConflict
	}
	return openOwned(opts, true, domain, state, nil)
}

// OpenOwnedChecked validates ownership before recovery may invoke any GC guard.
// The caller still supplies the authoritative current deletion coordinator.
func OpenOwnedChecked(opts Options, domain string, check func(DeletionRecord) error) (*Store, error) {
	if !validOwnerDomain(domain) || check == nil {
		return nil, ErrConflict
	}
	return openOwned(opts, false, domain, nil, check)
}
func openOwned(opts Options, create bool, domain string, state []byte, check func(DeletionRecord) error) (_ *Store, err error) {
	s, err := openArea(opts, create, ownedRawKind, check)
	if err != nil {
		return nil, err
	}
	s.ownerDomain = domain
	defer func() {
		if err != nil {
			err = errors.Join(err, s.closeHandles())
		}
	}()
	if create {
		if _, e := s.root.Lstat(ownerName); !errors.Is(e, os.ErrNotExist) {
			return nil, ErrConflict
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = s.writeOwner(ctx, state)
		cancel()
		if err != nil {
			return nil, err
		}
	}
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

func canonicalOwnerJSON(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return ErrCorrupt
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return ErrCorrupt
	}
	encoded, err := json.Marshal(dst)
	if err != nil || !bytes.Equal(data, encoded) {
		return ErrCorrupt
	}
	return nil
}
func (s *Store) readOwner() (Owner, error) {
	var owner Owner
	if s.kind != ownedRawKind || !validOwnerDomain(s.ownerDomain) {
		return owner, ErrConflict
	}
	f, err := openRegular(s.root, ownerName, os.O_RDONLY)
	if err != nil {
		return owner, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return owner, err
	}
	if err = ownerPrivateFile(info); err != nil || info.Size() > maxMeta {
		return owner, ErrCorrupt
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMeta+1))
	if err != nil {
		return owner, err
	}
	current, err := s.root.Lstat(ownerName)
	if err != nil || !os.SameFile(info, current) {
		return owner, ErrConflict
	}
	var seal ownerSeal
	if len(data) > maxMeta || canonicalOwnerJSON(data, &seal) != nil || sha256.Sum256(seal.Data) != seal.Digest || canonicalOwnerJSON(seal.Data, &owner) != nil || owner.Version != 1 || owner.StoreID != s.id || !validOwnerState(owner.State) {
		return Owner{}, ErrCorrupt
	}
	if owner.Domain != s.ownerDomain {
		return Owner{}, ErrConflict
	}
	return owner, nil
}

func (s *Store) ReadOwner(ctx context.Context) (Owner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Owner{}, err
	}
	if err := s.guard(); err != nil {
		return Owner{}, err
	}
	return s.readOwner()
}
func (s *Store) CompareOwner(ctx context.Context, previous, state []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.guard(); err != nil {
		return err
	}
	if !validOwnerState(previous) || !validOwnerState(state) {
		return ErrConflict
	}
	owner, err := s.readOwner()
	if err != nil {
		return err
	}
	if !bytes.Equal(owner.State, previous) {
		return ErrConflict
	}
	return s.writeOwner(ctx, state)
}

// The one bounded control and its temporary use the existing64KiB metadata
// reserve. Recount charges their actual bytes; no per-block files grow outside
// the reservation. A capacity failure preserves raw and the prior control.
func (s *Store) writeOwner(ctx context.Context, state []byte) error {
	return s.withVolumeWrite(ctx, func() error {
		owner := Owner{Version: 1, StoreID: s.id, Domain: s.ownerDomain, State: bytes.Clone(state)}
		data, err := json.Marshal(owner)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(ownerSeal{Data: data, Digest: sha256.Sum256(data)})
		if err != nil {
			return err
		}
		if len(encoded) > maxMeta || int64(len(encoded)) > s.opts.MaxBytes-s.used {
			return ErrCapacity
		}
		free, err := freeBytes(s.dir)
		if err != nil {
			return s.poison(err)
		}
		need := int64(len(encoded))
		if s.opts.Volume.MaxBytes > 0 {
			need += VolumeOverheadBytes
		}
		if free < uint64(s.minFree()) || uint64(need) > free-uint64(s.minFree()) {
			return ErrCapacity
		}
		if err = writeAtomic(s.root, ownerName, encoded); err != nil {
			return s.poison(err)
		}
		return s.recount()
	})
}

func (s *Store) recoverOwner() error {
	if s.kind != ownedRawKind {
		return nil
	}
	if _, err := s.readOwner(); err != nil {
		return err
	}
	s.ownerReady = true
	// A known atomic-rename temporary never advanced intake. Validate its file
	// boundary before discarding it; the exact last published owner remains.
	info, err := s.root.Lstat(ownerName + ".tmp")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if ownerPrivateFile(info) != nil || info.Size() > maxMeta {
		return ErrCorrupt
	}
	if err = s.root.Remove(ownerName + ".tmp"); err != nil {
		return err
	}
	return syncDir(s.root)
}
