package capture

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Reset starts a new generation at a captured common ancestor (or the explicit
// bootstrap anchor). It keeps immutable fork facts/events. Heights above the
// new cursor cannot authorize reads/GC even while old pointers remain on disk.
func (s *Store) Reset(ctx context.Context, generation uint64, ancestor model.FeedBlock) error {
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if err := mutable(st, generation); err != nil {
			return err
		}
		if generation == math.MaxUint64 || ancestor.Number < st.Anchor.Number || ancestor.Number > st.Through.Number {
			return ErrGap
		}
		if st.Finalized != nil && ancestor.Number < st.Finalized.Number {
			return ErrConflict
		}
		if _, err := canonicalID(tx, st, ancestor); err != nil {
			return err
		}
		if err := s.fresh(ctx, ancestor); err != nil {
			return err
		}
		st.Generation++
		st.Through = ancestor
		digest, _ := st.Plan.Digest()
		return emit(tx, &st, "capture_reset", model.CaptureIdentity{Block: ancestor, Generation: st.Generation, PlanVersion: st.Plan.Version, PlanDigest: digest})
	})
}

// Finalize accepts a checkpoint ALREADY authenticated by the chain-specific
// finality policy. This is a trusted internal composition API, not an RPC tag
// parser: matching a fresh hash alone does not prove consensus finality.
// ControlStep supplies the explicitly selected, durably pinned chain policy.
// Direct callers remain trusted and must provide equivalent authentication.
func (s *Store) Finalize(ctx context.Context, generation uint64, block model.FeedBlock) error {
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if err := mutable(st, generation); err != nil {
			return err
		}
		if st.Finalized != nil && block.Number < st.Finalized.Number {
			return ErrConflict
		}
		if _, err := canonicalID(tx, st, block); err != nil {
			return err
		}
		if err := s.fresh(ctx, block); err != nil {
			return err
		}
		st.Finalized = &block
		return putJSON(tx, "state", st)
	})
}

// AckRaw is only for configured consumers which need raw-window bytes. The
// caller invokes it AFTER its own durable work. It cannot acknowledge another
// generation or an uncaptured fork. DB-only projection lag needs no raw ACK.
func (s *Store) AckRaw(ctx context.Context, id model.CaptureIdentity, consumer string) error {
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		// A retained common ancestor can belong to an older generation.
		// Its exact canonical identity below still has to match.
		if err := mutable(st, st.Generation); err != nil {
			return err
		}
		known := false
		for _, c := range st.RawConsumers {
			known = known || c == consumer
		}
		if !known {
			return port.ErrInvalidData
		}
		got, err := canonicalID(tx, st, id.Block)
		if err != nil {
			return err
		}
		if got != id {
			return ErrStale
		}
		if _, err := tx.Get([]byte(factsKey(id))); err != nil {
			return err
		}
		return tx.Put([]byte(ackKey(id, consumer)), []byte{1})
	})
}

func ackKey(id model.CaptureIdentity, c string) string {
	return fmt.Sprintf("ack/%s/%020d/%x", id.Block.Hash.Hex(), id.Generation, sha256.Sum256([]byte(c)))
}

func canonicalID(tx port.CaptureReader, st State, b model.FeedBlock) (model.CaptureIdentity, error) {
	var id model.CaptureIdentity
	if b.Chain != st.Anchor.Chain || b.Number < st.Anchor.Number || b.Number > st.Through.Number {
		return id, ErrGap
	}
	if b.Number == st.Anchor.Number {
		if b != st.Anchor {
			return id, ErrConflict
		}
		digest, _ := st.Plan.Digest()
		return model.CaptureIdentity{Block: b, Generation: st.Generation, PlanVersion: st.Plan.Version, PlanDigest: digest}, nil
	}
	if err := getJSON(tx, heightKey(b.Number), &id); err != nil {
		return id, err
	}
	if id.Block != b {
		return id, ErrConflict
	}
	return id, nil
}
