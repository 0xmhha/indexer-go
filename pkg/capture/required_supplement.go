package capture

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var ErrSupplementIncomplete = errors.New("capture: required supplement or parent checkpoint incomplete")

// SupplementVerifier is a pure, bounded codec verifier: it MUST authenticate
// the encoded header against block.Hash and verify the exact selection's
// commitments. No DB calls, RPCs or mutations are allowed inside this callback.
// Missing composition fails closed, including on another Store/GC recovery.
type SupplementVerifier func(model.SupplementStreamDescriptor, model.FeedBlock, []byte) error

// WithSupplementVerifier returns a new handle; compose before starting workers.
func (s *Store) WithSupplementVerifier(v SupplementVerifier) *Store {
	out := *s
	out.verifySupplement = v
	return &out
}

// BindRequiredSupplement permanently pins the required tracked-state contract
// BEFORE first intake or Open/GC recovery. Existing sources need an explicit
// migration/new DB; adding/removing/renaming requirements is never inferred.
func (s *Store) BindRequiredSupplement(ctx context.Context, d *model.SupplementStreamDescriptor) error {
	if d != nil && (d.Validate() != nil || !d.Required) {
		return port.ErrInvalidData
	}
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if st.RequiredSupplement != nil {
			if !equalJSON(st.RequiredSupplement, d) {
				return ErrConflict
			}
			return nil
		}
		if d == nil {
			return nil
		}
		if d.FromHeight <= st.Anchor.Number {
			return port.ErrInvalidData
		}
		if e := mutable(st, st.Generation); e != nil {
			return e
		}
		if st.Sequence != 0 || st.Through != st.Anchor || st.Finalized != nil {
			return ErrConflict
		}
		copy := *d
		st.RequiredSupplement = &copy
		return putJSON(tx, "state", st)
	})
}

func (s *Store) verifyTracked(d model.SupplementStreamDescriptor, b model.FeedBlock, raw []byte) error {
	if s.verifySupplement == nil {
		return ErrSupplementIncomplete
	}
	return s.verifySupplement(d, b, raw)
}

// Verify every input actually needed for this transition in the SAME source
// transaction as the deletion fence. Cursor/receipts alone never authorize GC.
// Data remains permanent; this gate grants no raw consumer ACK or finality.
func (s *Store) requiredDeletion(tx port.CaptureReader, st State, b model.FeedBlock, checked map[uint64]bool) error {
	d := st.RequiredSupplement
	if d == nil {
		return nil
	}
	if d.Validate() != nil || !d.Required || d.FromHeight <= st.Anchor.Number {
		return ErrConflict
	}
	if b.Number < d.FromHeight-1 {
		return nil
	}
	c, e := streamCursor(tx, st, *d)
	if e != nil {
		return e
	}
	if c.Epoch != st.Generation {
		return ErrStale
	}
	if !checked[d.FromHeight-1] {
		cp, e := streamCheckpoint(tx, st, *d, c)
		if e != nil {
			return e
		}
		if e := s.verifyTracked(*d, cp.Block, cp.Data); e != nil {
			return e
		}
		checked[cp.Block.Number] = true
	}
	if b.Number < d.FromHeight {
		return nil
	}
	if c.Complete == nil || c.Complete.Number < b.Number {
		return ErrSupplementIncomplete
	}
	for n := max(d.FromHeight, b.Number-1); n <= b.Number; n++ {
		if !checked[n] {
			id, e := canonicalAtHeight(tx, st, n)
			if e != nil {
				return e
			}
			in, e := streamSupplement(tx, st, *d, id.Block)
			if e != nil {
				return e
			}
			if e := s.verifyTracked(*d, id.Block, in.Data); e != nil {
				return e
			}
			checked[n] = true
		}
		if n == b.Number {
			break
		} // uint64 overflow
	}
	return nil
}

// RequiredSupplementReady exposes completion of the explicit tracked scope,
// separately from the base block plan and state-free projections.
func (s *Store) RequiredSupplementReady(ctx context.Context, epoch uint64, b model.FeedBlock) error {
	return s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if st.Generation != epoch {
			return ErrStale
		}
		if st.RequiredSupplement == nil || b.Number < st.RequiredSupplement.FromHeight {
			return ErrSupplementIncomplete
		}
		if _, e := canonicalID(tx, st, b); e != nil {
			return e
		}
		return s.requiredDeletion(tx, st, b, make(map[uint64]bool))
	})
}
