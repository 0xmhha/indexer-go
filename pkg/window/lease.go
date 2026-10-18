package window

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
)

// Lease owns the read buffer until Close. Bytes are immutable and may not be
// retained after Close. The caller must include decoder allocations in its
// own worker budget; this is a bound on window-owned buffers, not Go RSS.
type Lease struct {
	mu      sync.Mutex
	s       *Store
	ref     Reference
	file    *os.File
	payload []byte
	closed  bool
}

func (s *Store) segmentFor(id uint64) *segment {
	if s.active != nil && s.active.ID == id {
		return &s.active.segment
	}
	for _, seg := range s.segments {
		if seg.ID == id {
			return seg
		}
	}
	return nil
}

// Acquire re-resolves the reference from the trusted index. Caller-supplied
// offsets/lengths cannot select another record or allocate beyond the limit.
// Budget exhaustion is backpressure, not an unbounded waiting goroutine.
func (s *Store) Acquire(ctx context.Context, ref Reference) (*Lease, error) {
	s.mu.Lock()
	lease, err := s.acquireLocked(ctx, ref)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// Payload I/O does not hold the append mutex. The lease and reservation
	// already prevent GC/Close and bound allocations across parallel readers.
	info, err := lease.file.Stat()
	var actual Reference
	var payload []byte
	var partial bool
	if err == nil {
		actual, payload, partial, err = readFrame(lease.file, ref.Offset, info.Size(), s.opts, s.id, true)
	}
	actual.Segment = ref.Segment
	s.mu.Lock()
	if err != nil || partial || actual != ref {
		if err == nil {
			err = ErrCorrupt
		}
		err = s.poison(err)
	} else {
		err = s.guard()
	}
	s.mu.Unlock()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	lease.payload = payload
	return lease, nil
}

func (s *Store) acquireLocked(ctx context.Context, ref Reference) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.guard(); err != nil {
		return nil, err
	}
	got, err := s.lookup(ref.Block, ref.Kind)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrConflict) {
			return nil, s.poison(err)
		}
		return nil, err
	}
	if got != ref {
		return nil, ErrConflict
	}
	if s.readers >= s.opts.MaxReaders || ref.FrameBytes > s.opts.ReadBudgetBytes-s.readBytes {
		return nil, ErrBusy
	}
	f, err := openRegular(s.root, segmentName(ref.Segment, "raw"), os.O_RDONLY)
	if err != nil {
		return nil, s.poison(err)
	}
	seg := s.segmentFor(ref.Segment)
	if seg == nil {
		_ = f.Close()
		return nil, ErrNotFound
	}
	seg.leases++
	s.readers++
	s.readBytes += ref.FrameBytes
	return &Lease{s: s, ref: ref, file: f}, nil
}

func (l *Lease) Envelope() (chainfeed.Envelope, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return chainfeed.Envelope{}, ErrClosed
	}
	return chainfeed.Envelope{Block: l.ref.Block, Kind: l.ref.Kind, Codec: l.ref.Codec, Payload: l.payload}, nil
}

func (l *Lease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	seg := l.s.segmentFor(l.ref.Segment)
	if seg == nil || seg.leases < 1 {
		return fmt.Errorf("%w: lease accounting", ErrCorrupt)
	}
	err := l.file.Close()
	seg.leases--
	l.s.readers--
	l.s.readBytes -= l.ref.FrameBytes
	l.payload = nil
	l.closed = true
	return err
}
