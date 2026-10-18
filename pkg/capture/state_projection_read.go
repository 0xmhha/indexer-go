package capture

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type StateProjectionStatus struct {
	Descriptor model.StateProjectionDescriptor `json:"descriptor"`
	Epoch      uint64                          `json:"epoch"`
	Status     string                          `json:"status"`
	Complete   *model.BlockPoint               `json:"complete"`
	Backlog    uint64                          `json:"backlog"`
	Sequence   uint64                          `json:"sequence"`
}

// CheckStateProjectionScope rejects a stale projection descriptor before API
// reads. Source anchor/plan are immutable; resets change only the epoch, which
// each actual read fences again in its own snapshot.
func CheckStateProjectionScope(ctx context.Context, db port.CaptureDatabase, d model.ProjectionDescriptor) error {
	if db == nil || d.Version != 1 || d.Anchor.Validate() != nil || d.Plan.Validate() != nil || d.StateChanges == nil || d.ValidateStateChanges() != nil {
		return port.ErrInvalidData
	}
	return db.CaptureView(ctx, d.Anchor.Chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		actual, e := st.Plan.Digest()
		if e != nil {
			return e
		}
		expected, e := d.Plan.Digest()
		if e != nil {
			return e
		}
		if st.Anchor != d.Anchor || actual != expected {
			return ErrConflict
		}
		return nil
	})
}

func stateProjectionStatus(tx port.CaptureReader, st State, d model.StateProjectionDescriptor) (out StateProjectionStatus, err error) {
	out = StateProjectionStatus{Descriptor: d, Epoch: st.Generation, Status: "not_started"}
	if d.Validate() != nil || d.Stream.FromHeight <= st.Anchor.Number {
		return out, ErrConflict
	}
	if st.Through.Number >= d.Stream.FromHeight {
		out.Backlog = st.Through.Number - d.Stream.FromHeight + 1
	}
	c, e := stateCursor(tx, st, d)
	if e == port.ErrNotFound {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	out.Sequence = c.Sequence
	if c.Epoch != st.Generation {
		out.Status = "stale"
		return out, nil
	}
	if c.Complete == nil {
		out.Status = "waiting"
		return out, nil
	}
	if e := checkProjection(tx, st.Generation, *c.Complete); e != nil {
		return out, e
	}
	out.Complete = &model.BlockPoint{Height: c.Complete.Block.Number, Hash: c.Complete.Block.Hash}
	out.Backlog = st.Through.Number - c.Complete.Block.Number
	out.Status = "lagging"
	if out.Backlog == 0 {
		out.Status = "caught_up"
	}
	return out, nil
}
func (s *Store) StateProjectionStatus(ctx context.Context, d model.StateProjectionDescriptor) (out StateProjectionStatus, err error) {
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		out, e = stateProjectionStatus(tx, st, d)
		return e
	})
	return out, err
}
func readProjectionCursor(tx port.CaptureReader, d model.StateProjectionDescriptor) (StateProjectionCursor, error) {
	st, e := loadState(tx)
	if e != nil {
		return StateProjectionCursor{}, e
	}
	c, e := stateCursor(tx, st, d)
	if e != nil {
		return c, e
	}
	if c.Epoch != st.Generation {
		return c, ErrStale
	}
	if c.Complete != nil {
		if e := checkProjection(tx, st.Generation, *c.Complete); e != nil {
			return c, e
		}
	}
	return c, nil
}
func ReadStateProjectionHead(ctx context.Context, db port.CaptureDatabase, chain model.FeedChain, d model.StateProjectionDescriptor) (out StateProjectionHead, epoch uint64, err error) {
	if db == nil || chain.Validate() != nil || d.Validate() != nil {
		return out, 0, port.ErrInvalidData
	}
	err = db.CaptureView(ctx, chain, func(tx port.CaptureReader) error {
		c, e := readProjectionCursor(tx, d)
		if e != nil {
			return e
		}
		if c.Anchor.Chain != chain {
			return ErrConflict
		}
		epoch = c.Epoch
		if c.Complete == nil {
			return port.ErrNotFound
		}
		h, e := loadStateHead(tx, d, c)
		if e == port.ErrNotFound {
			return ErrConflict
		}
		if e != nil {
			return e
		}
		if h == nil {
			return ErrConflict
		}
		output, e := stateOutput(tx, d, *c.Complete)
		if e == port.ErrNotFound {
			return ErrConflict
		}
		if e != nil {
			return e
		}
		if output.AfterDigest != h.Digest || string(output.Values) != string(h.Values) {
			return ErrConflict
		}
		out = *h
		return nil
	})
	return out, epoch, err
}
func ReadStateProjectionOutput(ctx context.Context, db port.CaptureDatabase, chain model.FeedChain, d model.StateProjectionDescriptor, n uint64) (out StateProjectionOutput, epoch uint64, err error) {
	if db == nil || chain.Validate() != nil || d.Validate() != nil {
		return out, 0, port.ErrInvalidData
	}
	err = db.CaptureView(ctx, chain, func(tx port.CaptureReader) error {
		c, e := readProjectionCursor(tx, d)
		if e != nil {
			return e
		}
		if c.Anchor.Chain != chain {
			return ErrConflict
		}
		epoch = c.Epoch
		if c.Complete == nil || n < d.Stream.FromHeight || n > c.Complete.Block.Number {
			return port.ErrNotFound
		}
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		id, e := canonicalAtHeight(tx, st, n)
		if e != nil {
			return e
		}
		out, e = stateOutput(tx, d, id)
		if e == port.ErrNotFound {
			return ErrConflict
		}
		return e
	})
	return out, epoch, err
}

// Events retain old epochs and revert inputs for replay. Consumers commit
// their own sink updates and event sequence atomically; delivery is at least
// once. No raw ACK, source finality or existing writer outbox is advanced.
func ReadStateProjectionEvents(ctx context.Context, db port.CaptureDatabase, chain model.FeedChain, d model.StateProjectionDescriptor, after uint64, limit int) (out []StateProjectionEvent, err error) {
	if db == nil || chain.Validate() != nil || d.Validate() != nil || limit < 1 || limit > 256 {
		return nil, port.ErrInvalidData
	}
	out = []StateProjectionEvent{}
	err = db.CaptureView(ctx, chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		c, e := stateCursor(tx, st, d)
		if e != nil {
			return e
		}
		if c.Anchor.Chain != chain {
			return ErrConflict
		}
		if after > c.Sequence {
			return port.ErrInvalidData
		}
		for n := after; n < c.Sequence && len(out) < limit; {
			n++
			var ev StateProjectionEvent
			if e := getJSON(tx, fmt.Sprintf("%sevents/%020d", statePrefix(d), n), &ev); e != nil {
				if e == port.ErrNotFound {
					return ErrConflict
				}
				return e
			}
			if ev.Sequence != n || ev.Epoch == 0 || ev.Kind != "apply" && ev.Kind != "revert" && ev.Kind != "reset" {
				return ErrConflict
			}
			out = append(out, ev)
		}
		return nil
	})
	return out, err
}

// Historical input is explicit: it never claims current canonical completion.
func ReadStateProjectionInput(ctx context.Context, db port.CaptureDatabase, chain model.FeedChain, d model.StateProjectionDescriptor, id model.CaptureIdentity) (out StateProjectionOutput, err error) {
	if db == nil || chain.Validate() != nil || d.Validate() != nil || id.Block.Chain != chain {
		return out, port.ErrInvalidData
	}
	err = db.CaptureView(ctx, chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if _, e := stateCursor(tx, st, d); e != nil {
			return e
		}
		out, e = stateOutput(tx, d, id)
		return e
	})
	return out, err
}
