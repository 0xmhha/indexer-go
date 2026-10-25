package capture

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

const MaxStateValues = 128 << 10
const MaxStateChanges = 256 << 10
const MaxStateUndo = 128

// StateProjector is pure and bounded. Authenticate both headers and verify
// their exact selected proof commitments, then derive deterministic values and
// changes. It runs inside the source transaction; no nested DB calls or RPC.
type StateProjector func(model.StateProjectionDescriptor, SupplementCheckpoint, SupplementCheckpoint) (StateProjectionValues, error)
type StateProjectionValues struct{ Before, After, Changes json.RawMessage }
type StateProjectionCursor struct {
	Descriptor model.StateProjectionDescriptor `json:"descriptor"`
	Anchor     model.FeedBlock                 `json:"anchor"`
	PlanDigest common.Hash                     `json:"planDigest"`
	Epoch      uint64                          `json:"epoch"`
	Complete   *model.CaptureIdentity          `json:"complete"`
	HeadDigest common.Hash                     `json:"headDigest"`
	Sequence   uint64                          `json:"sequence"`
}
type StateProjectionHead struct {
	Input  *model.CaptureIdentity `json:"input"`
	Block  model.FeedBlock        `json:"block"`
	Values json.RawMessage        `json:"values"`
	Digest common.Hash            `json:"digest"`
}
type StateProjectionOutput struct {
	Identity      model.CaptureIdentity `json:"identity"`
	Definition    common.Hash           `json:"definition"`
	Before        model.FeedBlock       `json:"before"`
	BeforeDigest  common.Hash           `json:"beforeDigest"`
	AfterDigest   common.Hash           `json:"afterDigest"`
	Changes       json.RawMessage       `json:"changes"`
	Values        json.RawMessage       `json:"values"`
	ChangesDigest common.Hash           `json:"changesDigest"`
}
type StateProjectionUndo struct {
	Identity   model.CaptureIdentity `json:"identity"`
	Definition common.Hash           `json:"definition"`
	Before     StateProjectionHead   `json:"before"`
}
type StateProjectionEvent struct {
	Sequence uint64                 `json:"sequence"`
	Epoch    uint64                 `json:"epoch"`
	Kind     string                 `json:"kind"`
	Input    *model.CaptureIdentity `json:"input,omitempty"`
	Through  *model.FeedBlock       `json:"through"`
}

func statePrefix(d model.StateProjectionDescriptor) string {
	return fmt.Sprintf("state-projection/%x/", sha256.Sum256([]byte(d.Name)))
}
func stateInputKey(d model.StateProjectionDescriptor, kind string, id model.CaptureIdentity) string {
	return fmt.Sprintf("%s%s/%x/%x", statePrefix(d), kind, d.Digest(), rowOwner(id))
}
func stateCursor(tx port.CaptureReader, st State, d model.StateProjectionDescriptor) (c StateProjectionCursor, e error) {
	if d.Validate() != nil || d.Stream.FromHeight <= st.Anchor.Number {
		return c, port.ErrInvalidData
	}
	if e = getJSON(tx, statePrefix(d)+"cursor", &c); e != nil {
		return c, e
	}
	digest, _ := st.Plan.Digest()
	if !equalJSON(c.Descriptor, d) || c.Anchor != st.Anchor || c.PlanDigest != digest || c.Epoch == 0 || c.Epoch > st.Generation {
		return c, ErrConflict
	}
	if c.Complete != nil && (c.Complete.Block.Validate() != nil || c.Complete.Block.Chain != st.Anchor.Chain || c.Complete.Block.Number < d.Stream.FromHeight || c.HeadDigest == (common.Hash{}) || c.Sequence == 0 || c.Complete.Generation == 0 || c.Complete.Generation > st.Generation || c.Complete.PlanDigest != digest || c.Complete.PlanVersion != st.Plan.Version) {
		return c, ErrConflict
	}
	return c, nil
}
func validStateJSON(raw []byte, limit int) bool {
	if len(raw) == 0 || len(raw) > limit || !json.Valid(raw) {
		return false
	}
	b, e := json.Marshal(json.RawMessage(raw))
	return e == nil && string(b) == string(raw)
}
func validStateHead(h StateProjectionHead) bool {
	return h.Block.Validate() == nil && validStateJSON(h.Values, MaxStateValues) && h.Digest == sha256.Sum256(h.Values) && (h.Input == nil || h.Input.Block == h.Block && h.Input.Generation > 0 && h.Input.PlanVersion > 0 && h.Input.PlanDigest != (common.Hash{}))
}
func loadStateHead(tx port.CaptureReader, d model.StateProjectionDescriptor, c StateProjectionCursor) (*StateProjectionHead, error) {
	var h *StateProjectionHead
	e := getJSON(tx, statePrefix(d)+"head", &h)
	if e == port.ErrNotFound && c.HeadDigest == (common.Hash{}) && c.Complete == nil {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if h == nil {
		if c.HeadDigest != (common.Hash{}) || c.Complete != nil {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if !validStateHead(*h) || h.Digest != c.HeadDigest || !equalJSON(h.Input, c.Complete) || h.Block.Chain != c.Anchor.Chain {
		return nil, ErrConflict
	}
	return h, nil
}
func stateOutput(tx port.CaptureReader, d model.StateProjectionDescriptor, id model.CaptureIdentity) (out StateProjectionOutput, err error) {
	if err = getJSON(tx, stateInputKey(d, "output", id), &out); err != nil {
		return out, err
	}
	if out.Identity != id || out.Definition != d.Digest() || out.Before.Chain != id.Block.Chain || out.Before.Number == math.MaxUint64 || out.Before.Number+1 != id.Block.Number || out.Before.Validate() != nil || out.BeforeDigest == (common.Hash{}) || !validStateJSON(out.Values, MaxStateValues) || out.AfterDigest != sha256.Sum256(out.Values) || !validStateJSON(out.Changes, MaxStateChanges) || out.ChangesDigest != sha256.Sum256(out.Changes) {
		return out, ErrConflict
	}
	return out, nil
}
func stateUndo(tx port.CaptureReader, d model.StateProjectionDescriptor, h StateProjectionHead) (StateProjectionUndo, error) {
	var u StateProjectionUndo
	if h.Input == nil {
		return u, ErrConflict
	}
	if e := getJSON(tx, stateInputKey(d, "undo", *h.Input), &u); e != nil {
		return u, e
	}
	out, e := stateOutput(tx, d, *h.Input)
	if e != nil {
		return u, e
	}
	if u.Identity != *h.Input || u.Definition != d.Digest() || !validStateHead(u.Before) || u.Before.Block != out.Before || u.Before.Digest != out.BeforeDigest || out.AfterDigest != h.Digest {
		return u, ErrConflict
	}
	if u.Before.Input == nil && u.Before.Block.Number != d.Stream.FromHeight-1 {
		return u, ErrConflict
	}
	return u, nil
}
func emitState(tx port.CaptureWriter, d model.StateProjectionDescriptor, c *StateProjectionCursor, kind string, id *model.CaptureIdentity, through *model.FeedBlock) error {
	if c.Sequence == math.MaxUint64 {
		return port.ErrInvalidData
	}
	c.Sequence++
	return putJSON(tx, fmt.Sprintf("%sevents/%020d", statePrefix(d), c.Sequence), StateProjectionEvent{c.Sequence, c.Epoch, kind, id, through})
}

// InitializeStateProjection reconciles ONLY this worker's mutable head and
// undo/event namespace. A reset is atomic and walks at most 128 old inputs.
// Old fork outputs/undo stay historical. No projection writer undo is touched.
// On ErrCanonical, out.Epoch identifies the failed transaction's observed
// generation for retry/wait purposes; the returned cursor is not committed.
func (s *Store) InitializeStateProjection(ctx context.Context, d model.StateProjectionDescriptor) (out StateProjectionCursor, err error) {
	if d.Validate() != nil {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if d.Stream.FromHeight <= st.Anchor.Number {
			return port.ErrInvalidData
		}
		out, e = stateCursor(tx, st, d)
		if e == port.ErrNotFound {
			digest, _ := st.Plan.Digest()
			out = StateProjectionCursor{Descriptor: d, Anchor: st.Anchor, PlanDigest: digest, Epoch: st.Generation}
			return putJSON(tx, statePrefix(d)+"cursor", out)
		}
		if e != nil {
			return e
		}
		if out.Epoch == st.Generation {
			return nil
		}
		h, e := loadStateHead(tx, d, out)
		if e != nil {
			return e
		}
		out.Epoch = st.Generation
		undone := 0
		for h != nil {
			actual, e := canonicalID(tx, st, h.Block)
			if e == nil && h.Input != nil && actual != *h.Input {
				e = ErrConflict
			}
			if e == nil {
				break
			}
			if e != ErrGap && e != ErrConflict && e != port.ErrNotFound {
				return e
			}
			if h.Input == nil {
				h = nil
				break
			}
			if undone == MaxStateUndo {
				return ErrRebuildRequired
			}
			u, e := stateUndo(tx, d, *h)
			if e != nil {
				return e
			}
			old := *h.Input
			prior := u.Before.Block
			if e := emitState(tx, d, &out, "revert", &old, &prior); e != nil {
				return e
			}
			undone++
			h = &u.Before
		}
		out.Complete = nil
		out.HeadDigest = common.Hash{}
		if h != nil {
			out.Complete = h.Input
			out.HeadDigest = h.Digest
		}
		point := st.Anchor
		if h != nil {
			point = h.Block
		}
		if e := s.fresh(ctx, point); e != nil {
			return e
		}
		if e := putJSON(tx, statePrefix(d)+"head", h); e != nil {
			return e
		}
		if e := emitState(tx, d, &out, "reset", out.Complete, &point); e != nil {
			return e
		}
		return putJSON(tx, statePrefix(d)+"cursor", out)
	})
	return out, err
}

func projectionProofs(tx port.CaptureReader, st State, d model.StateProjectionDescriptor, id model.CaptureIdentity) (before, after SupplementCheckpoint, err error) {
	c, e := streamCursor(tx, st, d.Stream)
	if e == port.ErrNotFound {
		return before, after, ErrSupplementIncomplete
	}
	if e != nil {
		return before, after, e
	}
	if c.Epoch != st.Generation {
		return before, after, ErrSupplementIncomplete
	}
	if c.Complete == nil || c.Complete.Number < id.Block.Number {
		return before, after, ErrSupplementIncomplete
	}
	in, e := streamSupplement(tx, st, d.Stream, id.Block)
	if e != nil {
		return before, after, e
	}
	after = SupplementCheckpoint{Block: id.Block, Digest: in.Digest, Data: in.Data}
	if id.Block.Number == d.Stream.FromHeight {
		before, e = streamCheckpoint(tx, st, d.Stream, c)
	} else {
		previous, e0 := canonicalAtHeight(tx, st, id.Block.Number-1)
		if e0 != nil {
			return before, after, e0
		}
		p, e0 := streamSupplement(tx, st, d.Stream, previous.Block)
		if e0 != nil {
			return before, after, e0
		}
		before = SupplementCheckpoint{Block: previous.Block, Digest: p.Digest, Data: p.Data}
	}
	return before, after, e
}

// ApplyStateProjection atomically writes output + undo + current values +
// contiguous cursor + durable event. Every real attempt verifies both MPT
// inputs and performs ONE fresh check through the existing shared RPC pool.
func (s *Store) ApplyStateProjection(ctx context.Context, epoch uint64, d model.StateProjectionDescriptor, id model.CaptureIdentity, derive StateProjector) error {
	if d.Validate() != nil || id.Block.Chain != s.chain || id.Block.Number < d.Stream.FromHeight || derive == nil {
		return port.ErrInvalidData
	}
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		if e := checkProjection(tx, epoch, id); e != nil {
			return e
		}
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		c, e := stateCursor(tx, st, d)
		if e != nil {
			return e
		}
		if c.Epoch != epoch {
			return ErrStale
		}
		next := d.Stream.FromHeight
		if c.Complete != nil {
			if c.Complete.Block.Number == math.MaxUint64 {
				return ErrGap
			}
			next = c.Complete.Block.Number + 1
		}
		retry := c.Complete != nil && *c.Complete == id
		if !retry && id.Block.Number != next {
			return ErrGap
		}
		before, after, e := projectionProofs(tx, st, d, id)
		if e != nil {
			return e
		}
		var facts Input
		if e := getJSON(tx, factsKey(id), &facts); e != nil {
			return e
		}
		if facts.ParentHash != before.Block.Hash {
			return ErrGap
		}
		values, e := derive(d, before, after)
		if e != nil {
			return e
		}
		if !validStateJSON(values.Before, MaxStateValues) || !validStateJSON(values.After, MaxStateValues) || !validStateJSON(values.Changes, MaxStateChanges) {
			return port.ErrInvalidData
		}
		bh, ah := common.Hash(sha256.Sum256(values.Before)), common.Hash(sha256.Sum256(values.After))
		result := StateProjectionOutput{Identity: id, Definition: d.Digest(), Before: before.Block, BeforeDigest: bh, AfterDigest: ah, Values: values.After, Changes: values.Changes, ChangesDigest: sha256.Sum256(values.Changes)}
		head, e := loadStateHead(tx, d, c)
		if e != nil {
			return e
		}
		if retry {
			old, e := stateOutput(tx, d, id)
			if e != nil {
				return e
			}
			if !equalJSON(old, result) || head == nil || head.Digest != ah || string(head.Values) != string(values.After) {
				return ErrConflict
			}
			u, e := stateUndo(tx, d, *head)
			if e != nil {
				return e
			}
			if u.Before.Digest != bh || string(u.Before.Values) != string(values.Before) {
				return ErrConflict
			}
			return s.fresh(ctx, id.Block)
		}
		if head == nil {
			if c.Complete != nil || id.Block.Number != d.Stream.FromHeight {
				return ErrConflict
			}
			head = &StateProjectionHead{Block: before.Block, Values: values.Before, Digest: bh}
		}
		if head.Block != before.Block || head.Digest != bh || string(head.Values) != string(values.Before) {
			return ErrConflict
		}
		undo := StateProjectionUndo{Identity: id, Definition: d.Digest(), Before: *head}
		old, e := stateOutput(tx, d, id)
		if e == nil && !equalJSON(old, result) {
			return ErrConflict
		}
		if e != nil && e != port.ErrNotFound {
			return e
		}
		var priorUndo StateProjectionUndo
		e = getJSON(tx, stateInputKey(d, "undo", id), &priorUndo)
		if e == nil && !equalJSON(priorUndo, undo) {
			return ErrConflict
		}
		if e != nil && e != port.ErrNotFound {
			return e
		}
		if e := s.fresh(ctx, id.Block); e != nil {
			return e
		}
		if e := putJSON(tx, stateInputKey(d, "output", id), result); e != nil {
			return e
		}
		if e := putJSON(tx, stateInputKey(d, "undo", id), undo); e != nil {
			return e
		}
		afterHead := StateProjectionHead{Input: &id, Block: id.Block, Values: values.After, Digest: ah}
		if e := putJSON(tx, statePrefix(d)+"head", afterHead); e != nil {
			return e
		}
		c.Complete = &id
		c.HeadDigest = ah
		if e := emitState(tx, d, &c, "apply", &id, &id.Block); e != nil {
			return e
		}
		return putJSON(tx, statePrefix(d)+"cursor", c)
	})
}
