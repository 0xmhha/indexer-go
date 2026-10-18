package evmstate

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Run reads the canonical captured prefix as a durable queue. One bounded
// block is active; intake and existing projections run independently. Missing
// or pruned state is fatal, never skipped. The owner cancels/joins all tasks.
func (s *Supplier) Run(ctx context.Context, d model.SupplementStreamDescriptor, poll time.Duration) (err error) {
	// pgx may report a transport timeout when cancellation interrupts I/O.
	// Preserve that failure AND the owner's cancellation for cancel/join.
	defer func() {
		if ctx.Err() != nil && !errors.Is(err, ctx.Err()) {
			err = errors.Join(err, ctx.Err())
		}
	}()
	if _, e := selectionFor(d); e != nil {
		return e
	}
	if poll == 0 {
		poll = 50 * time.Millisecond
	}
	if poll < 20*time.Millisecond || poll > 5*time.Second {
		return model.ErrCaptureInvalid
	}
	for restarts := 0; ctx.Err() == nil; restarts++ {
		if restarts > 64 {
			return capture.ErrRebuildRequired
		}
		cursor, e := s.store.InitializeSupplementStream(ctx, d)
		if e != nil {
			return e
		}
		e = s.runEpoch(ctx, d, cursor, poll)
		if e == capture.ErrStale {
			continue
		}
		if e != capture.ErrCanonical {
			return e
		}
		// Let capture's existing supervisor authenticate/reset the fork. Never
		// forge an epoch or keep retrying uncached RPC on the obsolete branch.
		deadline := time.NewTimer(2 * time.Minute)
		tick := time.NewTicker(poll)
		changed := false
	wait:
		for {
			select {
			case <-ctx.Done():
				e = ctx.Err()
				break wait
			case <-deadline.C:
				break wait
			case <-tick.C:
				st, err := s.store.State(ctx)
				if err != nil {
					e = err
					break wait
				}
				if st.Generation != cursor.Epoch {
					changed = true
					break wait
				}
			}
		}
		deadline.Stop()
		tick.Stop()
		if !changed {
			return e
		}
	}
	return ctx.Err()
}
func (s *Supplier) runEpoch(ctx context.Context, d model.SupplementStreamDescriptor, c capture.SupplementCursor, poll time.Duration) error {
	sel, e := selectionFor(d)
	if e != nil {
		return e
	}
	if d.ParentCheckpoint {
		for {
			st, e := s.store.State(ctx)
			if e != nil {
				return e
			}
			if st.Generation != c.Epoch {
				return capture.ErrStale
			}
			if st.Through.Number >= d.FromHeight-1 {
				break
			}
			timer := time.NewTimer(poll)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if _, e := s.AcquireCheckpoint(ctx, c.Epoch, d); e != nil {
			return e
		}
	}
	next := d.FromHeight
	if c.Complete != nil {
		if c.Complete.Number == math.MaxUint64 {
			return model.ErrCaptureInvalid
		}
		id, e := s.store.CanonicalAt(ctx, c.Epoch, c.Complete.Number)
		if e != nil {
			return e
		}
		if _, e := s.Read(ctx, c.Epoch, id, sel); e != nil {
			return e
		}
		next = c.Complete.Number + 1
	}
	for ctx.Err() == nil {
		st, e := s.store.State(ctx)
		if e != nil {
			return e
		}
		if st.Generation != c.Epoch {
			return capture.ErrStale
		}
		if st.Through.Number < next {
			t := time.NewTimer(poll)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
			continue
		}
		id, e := s.store.CanonicalAt(ctx, c.Epoch, next)
		if e != nil {
			return e
		}
		if _, e := s.AcquireStream(ctx, c.Epoch, id, d); e != nil {
			return e
		}
		if next == math.MaxUint64 {
			return model.ErrCaptureInvalid
		}
		next++
	}
	return ctx.Err()
}
