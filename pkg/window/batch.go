package window

import (
	"context"
	"errors"
	"runtime/trace"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// AppendBatch shares data syncs across at most three datasets of one block.
// No reference is returned or visible to concurrent readers until its bytes
// are durable. On failure, a durable prefix may remain, just as with separate
// appends; retry checks that prefix's immutable identities/digests normally.
// This never acknowledges capture/projection or grants deletion authority.
func (s *Store) AppendBatch(ctx context.Context, inputs []chainfeed.Envelope) ([]Reference, error) {
	region := trace.StartRegion(ctx, "window.appendBatch")
	defer region.End()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(inputs) == 0 || len(inputs) > chainfeed.MaxBatchEnvelopes {
		return nil, model.ErrCaptureInvalid
	}
	for i, e := range inputs {
		if err := e.Validate(chainfeed.ReadLimits{MaxPayloadBytes: s.opts.MaxPayloadBytes}); err != nil {
			return nil, err
		}
		if e.Block.Chain != s.opts.Chain || e.Block != inputs[0].Block || len(e.Codec) > 128 {
			return nil, ErrConflict
		}
		for _, prior := range inputs[:i] {
			if prior.Kind == e.Kind {
				return nil, ErrConflict
			}
		}
	}
	if trace.IsEnabled() {
		trace.Logf(ctx, "window.input", "%d batch", inputs[0].Block.Number)
	}
	lock := trace.StartRegion(ctx, "window.lock")
	s.mu.Lock()
	lock.End()
	defer s.mu.Unlock()
	refs := make([]Reference, 0, len(inputs))
	err := s.withVolumeWrite(ctx, func() error {
		for _, e := range inputs {
			ref, err := s.appendLocked(ctx, e, false)
			if err != nil {
				// Preserve a completed prefix even on capacity/cancellation.
				// A sync/identity failure poisons the store and is never retried
				// as ordinary capacity pressure.
				return errors.Join(err, s.syncPending(ctx))
			}
			refs = append(refs, ref)
		}
		return s.syncPending(ctx)
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

func (s *Store) AppendBatchWait(ctx context.Context, inputs []chainfeed.Envelope) ([]Reference, error) {
	for {
		refs, err := s.AppendBatch(ctx, inputs)
		if !capacityOnly(err) {
			return refs, err
		}
		if err := waitSpace(ctx); err != nil {
			return nil, err
		}
	}
}

func capacityOnly(err error) bool {
	if err == ErrCapacity {
		return true
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		items := many.Unwrap()
		if len(items) == 0 {
			return false
		}
		for _, item := range items {
			if !capacityOnly(item) {
				return false
			}
		}
		return true
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return capacityOnly(one.Unwrap())
	}
	return false
}

// The Store mutex and volume write lock remain held. Readers cannot observe
// pending refs, and rotation cannot seal an index for unsynced bytes.
func (s *Store) syncPending(ctx context.Context) error {
	a := s.active
	if a == nil || !a.dirty {
		return nil
	}
	region := trace.StartRegion(ctx, "window.dataSync")
	err := a.file.Sync()
	region.End()
	if err != nil {
		return s.poison(err)
	}
	a.dirty = false
	if s.afterDataSync != nil {
		s.afterDataSync()
	}
	return s.guard()
}
