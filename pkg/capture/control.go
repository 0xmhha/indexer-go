package capture

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
)

var ErrRebuildRequired = errors.New("capture: reorg exceeds retained policy; explicit rebuild required")
var ErrFinalityConflict = errors.New("capture: finalized checkpoint contradicted")

// FinalityPolicy authenticates a chain-specific finalized canonical prefix.
// ID must identify its trust mode/authority/rules, durably pinned before GC.
// HashAt alone or confirmations are insufficient implementations of this port.
type FinalityPolicy interface {
	ID() string
	Checkpoint(context.Context, model.FeedChain) (model.FeedBlock, error)
}

type ControlStatus struct {
	Generation uint64
	Finalized  *model.FeedBlock
	Collected  int
	Blocked    error
}

// CanonicalAt reads the CURRENT capture pointer, bounded by its contiguous
// cursor. It is not a live canonical oracle or an immutable-fork lookup.
func (s *Store) CanonicalAt(ctx context.Context, generation, n uint64) (model.CaptureIdentity, error) {
	var id model.CaptureIdentity
	err := s.projectionView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if st.Generation != generation {
			return ErrStale
		}
		if n < st.Anchor.Number || n > st.Through.Number {
			return ErrGap
		}
		if n == st.Anchor.Number {
			id, err = canonicalID(tx, st, st.Anchor)
			return err
		}
		if err := getJSON(tx, heightKey(n), &id); err != nil {
			return err
		}
		if id.Block.Chain != s.chain || id.Block.Number != n {
			return ErrConflict
		}
		return nil
	})
	return id, err
}

func (s *Store) bindFinality(ctx context.Context, policy string) error {
	if len(policy) < 1 || len(policy) > 256 || strings.TrimSpace(policy) != policy {
		return port.ErrInvalidData
	}
	// Policy IDs are write-once. Steady maintenance must not create a durable
	// no-op transaction each tick; first binding still rechecks under the lock.
	state, err := s.State(ctx)
	if err != nil {
		return err
	}
	if state.FinalityPolicy == policy {
		return mutable(state, state.Generation)
	}
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if err := mutable(st, st.Generation); err != nil {
			return err
		}
		if st.FinalityPolicy == policy {
			return nil
		}
		if st.FinalityPolicy != "" || st.Finalized != nil {
			return ErrConflict
		}
		st.FinalityPolicy = policy
		return putJSON(tx, "state", st)
	})
}

// CommonAncestor searches only a bounded captured suffix, never below the
// bootstrap/finalized floor. The supervisor calls it again AFTER joining all
// workers, and Reset checks its chosen ancestor fresh inside the transaction.
func (s *Store) CommonAncestor(ctx context.Context, maxDepth uint64) (model.FeedBlock, bool, error) {
	st, err := s.State(ctx)
	if err != nil {
		return model.FeedBlock{}, false, err
	}
	if maxDepth < 1 || maxDepth > 4096 {
		return model.FeedBlock{}, false, port.ErrInvalidData
	}
	floor := st.Anchor.Number
	if st.Finalized != nil {
		if err := s.fresh(ctx, *st.Finalized); err != nil {
			if errors.Is(err, ErrCanonical) {
				return model.FeedBlock{}, false, errors.Join(ErrFinalityConflict, err)
			}
			return model.FeedBlock{}, false, err
		}
		floor = st.Finalized.Number
	}
	for n, depth := st.Through.Number, uint64(0); ; n, depth = n-1, depth+1 {
		id, err := s.CanonicalAt(ctx, st.Generation, n)
		if err != nil {
			return model.FeedBlock{}, false, err
		}
		err = s.fresh(ctx, id.Block)
		if err == nil {
			return id.Block, depth != 0, nil
		}
		if !errors.Is(err, ErrCanonical) && !errors.Is(err, source.ErrNotFound) {
			return model.FeedBlock{}, false, err
		}
		if n == floor {
			if st.Finalized != nil {
				return model.FeedBlock{}, false, ErrFinalityConflict
			}
			return model.FeedBlock{}, false, ErrRebuildRequired
		}
		if depth == maxDepth {
			return model.FeedBlock{}, false, ErrRebuildRequired
		}
	}
}

// ControlStep is a serialized maintenance tick. The finality policy is an
// explicit trust dependency; unavailable finality never falls back or collects.
func ControlStep(ctx context.Context, raw *window.Store, facts *Store, policy FinalityPolicy, collect bool, maxDepth uint64) (ControlStatus, bool, error) {
	var result ControlStatus
	if facts == nil || (collect && (policy == nil || raw == nil)) {
		return result, false, port.ErrInvalidData
	}
	readCtx := port.WithRPCMaintenance(ctx)
	_, changed, err := facts.CommonAncestor(readCtx, maxDepth)
	if err != nil || changed {
		return result, changed, err
	}
	st, err := facts.State(ctx)
	if err != nil {
		return result, false, err
	}
	result.Generation, result.Finalized = st.Generation, st.Finalized
	if policy == nil {
		if collect {
			return result, false, port.ErrInvalidData
		}
		return result, false, nil
	}
	if err := facts.bindFinality(ctx, policy.ID()); err != nil {
		return result, false, err
	}
	cp, err := policy.Checkpoint(readCtx, st.Anchor.Chain)
	if errors.Is(err, source.ErrFinalityUnavailable) {
		result.Blocked = err
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if cp.Validate() != nil || cp.Chain != st.Anchor.Chain {
		return result, false, ErrFinalityConflict
	}
	if st.Finalized != nil && (cp.Number < st.Finalized.Number || (cp.Number == st.Finalized.Number && cp.Hash != st.Finalized.Hash)) {
		return result, false, ErrFinalityConflict
	}
	if err := facts.fresh(readCtx, cp); err != nil {
		if errors.Is(err, ErrCanonical) {
			return result, false, errors.Join(ErrFinalityConflict, err)
		}
		return result, false, fmt.Errorf("finality checkpoint: %w", err)
	}
	if cp.Number < st.Anchor.Number {
		return result, false, nil
	}
	id, err := facts.CanonicalAt(ctx, st.Generation, min(cp.Number, st.Through.Number))
	if err != nil {
		return result, false, err
	}
	if cp.Number == id.Block.Number && cp != id.Block {
		return result, false, ErrFinalityConflict
	}
	if st.Finalized == nil || id.Block.Number > st.Finalized.Number {
		if err := facts.Finalize(ctx, st.Generation, id.Block); err != nil {
			return result, false, err
		}
		result.Finalized = &id.Block
	}
	if collect {
		if raw == nil {
			return result, false, port.ErrInvalidData
		}
		if err := raw.SealExpired(ctx); err != nil {
			return result, false, err
		}
		blocked := false
		result.Collected, err = raw.Collect(ctx, waitingGuard{facts: facts, blocked: &blocked})
		if blocked {
			result.Blocked = errGCWaiting
		}
	}
	return result, false, err
}

var errGCWaiting = errors.New("capture: GC waiting for facts/finality/raw ACK/required state")

// Preflight classifies ordinary lag separately from durable deletion/corruption
// errors. The real guard rechecks every byte/ACK in its fenced transaction.
type waitingGuard struct {
	facts   *Store
	blocked *bool
}

func (g waitingGuard) WithDeletePermit(ctx context.Context, refs []window.Reference, remove func() error) error {
	err := g.facts.db.CaptureView(ctx, g.facts.chain, func(tx port.CaptureReader) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if st.Finalized == nil {
			return errGCWaiting
		}
		for _, ref := range refs {
			if ref.Block.Number > st.Finalized.Number || ref.Block.Number > st.Through.Number {
				return errGCWaiting
			}
			id, err := canonicalID(tx, st, ref.Block)
			if errors.Is(err, ErrConflict) || errors.Is(err, ErrGap) {
				return errGCWaiting
			} // retained orphan: no deletion authority
			if err != nil {
				return err
			}
			if d := st.RequiredSupplement; d != nil && ref.Block.Number >= d.FromHeight-1 {
				if d.Validate() != nil || !d.Required || d.FromHeight <= st.Anchor.Number {
					return ErrConflict
				}
				c, e := streamCursor(tx, st, *d)
				if e == port.ErrNotFound {
					return errGCWaiting
				}
				if e != nil {
					return e
				}
				if c.Epoch != st.Generation || c.Parent == nil || ref.Block.Number >= d.FromHeight && (c.Complete == nil || c.Complete.Number < ref.Block.Number) {
					return errGCWaiting
				}
			}
			for _, consumer := range st.RawConsumers {
				_, err := tx.Get([]byte(ackKey(id, consumer)))
				if errors.Is(err, port.ErrNotFound) {
					return errGCWaiting
				}
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == errGCWaiting {
		*g.blocked = true
		return window.ErrRetain
	}
	if err != nil {
		return err
	}
	return g.facts.WithDeletePermit(ctx, refs, remove)
}
