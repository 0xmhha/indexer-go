package capture

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

type SupplementCursor struct {
	Descriptor model.SupplementStreamDescriptor `json:"descriptor"`
	Anchor     model.FeedBlock                  `json:"anchor"`
	PlanDigest common.Hash                      `json:"planDigest"`
	Epoch      uint64                           `json:"epoch"`
	Complete   *model.FeedBlock                 `json:"complete"`
	Parent     *model.FeedBlock                 `json:"parent,omitempty"`
	Sequence   uint64                           `json:"sequence"`
}
type SupplementEvent struct {
	Sequence uint64           `json:"sequence"`
	Epoch    uint64           `json:"epoch"`
	Kind     string           `json:"kind"`
	Complete *model.FeedBlock `json:"complete"`
}
type supplementReceipt struct {
	Identity model.CaptureIdentity `json:"identity"`
	Digest   common.Hash           `json:"digest"`
}

func supplementPrefix(name string) string {
	return fmt.Sprintf("supplement-stream/%x/", sha256.Sum256([]byte(name)))
}
func streamReceiptKey(name string, id model.CaptureIdentity) string {
	return fmt.Sprintf("%sinput/%x", supplementPrefix(name), rowOwner(id))
}
func streamCursor(tx port.CaptureReader, st State, d model.SupplementStreamDescriptor) (c SupplementCursor, e error) {
	if e = d.Validate(); e != nil {
		return c, e
	}
	if d.FromHeight <= st.Anchor.Number {
		return c, port.ErrInvalidData
	}
	if e = getJSON(tx, supplementPrefix(d.Name)+"cursor", &c); e != nil {
		return c, e
	}
	plan, _ := st.Plan.Digest()
	if c.Epoch == 0 || c.Epoch > st.Generation || c.Anchor != st.Anchor || c.PlanDigest != plan || !equalJSON(c.Descriptor, d) || c.Complete != nil && (c.Complete.Validate() != nil || c.Complete.Chain != st.Anchor.Chain || c.Complete.Number < d.FromHeight) {
		return c, ErrConflict
	}
	if c.Complete != nil && c.Sequence == 0 {
		return c, ErrConflict
	}
	if c.Parent != nil && (!d.ParentCheckpoint || c.Parent.Validate() != nil || c.Parent.Chain != st.Anchor.Chain || c.Parent.Number != d.FromHeight-1 || c.Sequence == 0) {
		return c, ErrConflict
	}
	return c, nil
}
func streamReceipt(tx port.CaptureReader, st State, d model.SupplementStreamDescriptor, b model.FeedBlock) (model.CaptureIdentity, supplementReceipt, error) {
	id, e := canonicalID(tx, st, b)
	if e != nil {
		return id, supplementReceipt{}, e
	}
	var receipt supplementReceipt
	if e = getJSON(tx, streamReceiptKey(d.Name, id), &receipt); e != nil {
		return id, receipt, e
	}
	if receipt.Identity != id || receipt.Digest == (common.Hash{}) {
		return id, receipt, ErrConflict
	}
	return id, receipt, nil
}
func streamInput(tx port.CaptureReader, st State, d model.SupplementStreamDescriptor, b model.FeedBlock) error {
	_, e := streamSupplement(tx, st, d, b)
	return e
}
func streamSupplement(tx port.CaptureReader, st State, d model.SupplementStreamDescriptor, b model.FeedBlock) (Supplement, error) {
	var in Supplement
	id, receipt, e := streamReceipt(tx, st, d, b)
	if e != nil {
		return in, e
	}
	if e = getJSON(tx, supplementKey(id, d.Codec, d.SelectionHash), &in); e != nil {
		return in, e
	}
	if receipt.Identity != id || in.Identity != id || in.Codec != d.Codec || in.Selection != d.SelectionHash || receipt.Digest != in.Digest || !validSupplement(in) {
		return in, ErrConflict
	}
	return in, nil
}

// InitializeSupplementStream pins name/selection/start permanently. Reset
// reuses only an exact canonical receipt prefix (bounded 128-height search).
func (s *Store) InitializeSupplementStream(ctx context.Context, d model.SupplementStreamDescriptor) (out SupplementCursor, err error) {
	if d.Validate() != nil {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if d.FromHeight <= st.Anchor.Number {
			return port.ErrInvalidData
		}
		if d.Required && !equalJSON(st.RequiredSupplement, &d) {
			return ErrConflict
		}
		out, e = streamCursor(tx, st, d)
		if e == port.ErrNotFound {
			plan, _ := st.Plan.Digest()
			out = SupplementCursor{Descriptor: d, Anchor: st.Anchor, PlanDigest: plan, Epoch: st.Generation}
		} else if e != nil {
			return e
		} else if out.Epoch == st.Generation {
			return nil
		} else {
			if out.Sequence == math.MaxUint64 {
				return port.ErrInvalidData
			}
			var complete *model.FeedBlock
			if out.Complete != nil {
				n := min(out.Complete.Number, st.Through.Number)
				for checked := 0; n >= d.FromHeight && checked < 128; checked++ {
					id, e := canonicalAtHeight(tx, st, n)
					if e != nil {
						return e
					}
					e = streamInput(tx, st, d, id.Block)
					if e == nil {
						b := id.Block
						complete = &b
						break
					}
					if e != port.ErrNotFound {
						return e
					}
					n-- // FromHeight is nonzero.
				}
			}
			out.Complete, out.Epoch = complete, st.Generation
			if d.ParentCheckpoint {
				out.Parent = nil
				if st.Through.Number >= d.FromHeight-1 {
					cp, e := checkpointAt(tx, st, d)
					if e == nil {
						b := cp.Block
						out.Parent = &b
					} else if e != port.ErrNotFound {
						return e
					}
				}
				if out.Parent == nil {
					out.Complete = nil
				}
			}
			out.Sequence++
			if e := putJSON(tx, fmt.Sprintf("%sevents/%020d", supplementPrefix(d.Name), out.Sequence), SupplementEvent{out.Sequence, out.Epoch, "reset", out.Complete}); e != nil {
				return e
			}
		}
		return putJSON(tx, supplementPrefix(d.Name)+"cursor", out)
	})
	return out, err
}

// CommitStreamSupplement atomically stores optional input, its exact receipt,
// contiguous cursor and own event, with ONE fresh check. It grants no raw ACK.
func (s *Store) CommitStreamSupplement(ctx context.Context, epoch uint64, d model.SupplementStreamDescriptor, in Supplement) error {
	if d.Validate() != nil || !validSupplement(in) || in.Identity.Block.Chain != s.chain || in.Codec != d.Codec || in.Selection != d.SelectionHash {
		return port.ErrInvalidData
	}
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		if e := checkProjection(tx, epoch, in.Identity); e != nil {
			return e
		}
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		c, e := streamCursor(tx, st, d)
		if e != nil {
			return e
		}
		if c.Epoch != epoch {
			return ErrStale
		}
		if d.Required && !equalJSON(st.RequiredSupplement, &d) {
			return ErrConflict
		}
		if d.ParentCheckpoint {
			cp, e := streamCheckpoint(tx, st, d, c)
			if e != nil {
				return e
			}
			if e := s.verifyTracked(d, cp.Block, cp.Data); e != nil {
				return e
			}
			if e := s.verifyTracked(d, in.Identity.Block, in.Data); e != nil {
				return e
			}
		}
		if c.Complete != nil && *c.Complete == in.Identity.Block {
			if e := streamInput(tx, st, d, *c.Complete); e != nil {
				return e
			}
			if e := sameSupplement(tx, in); e != nil {
				return e
			}
			return s.fresh(ctx, in.Identity.Block)
		}
		next := d.FromHeight
		if c.Complete != nil {
			if c.Complete.Number == math.MaxUint64 {
				return ErrGap
			}
			next = c.Complete.Number + 1
		}
		if in.Identity.Block.Number != next {
			return ErrGap
		}
		if e := sameSupplement(tx, in); e != nil && e != port.ErrNotFound {
			return e
		}
		if c.Sequence == math.MaxUint64 {
			return port.ErrInvalidData
		}
		if e := s.fresh(ctx, in.Identity.Block); e != nil {
			return e
		}
		if e := putJSON(tx, supplementKey(in.Identity, in.Codec, in.Selection), in); e != nil {
			return e
		}
		if e := putJSON(tx, streamReceiptKey(d.Name, in.Identity), supplementReceipt{in.Identity, in.Digest}); e != nil {
			return e
		}
		b := in.Identity.Block
		c.Complete = &b
		c.Sequence++
		if e := putJSON(tx, supplementPrefix(d.Name)+"cursor", c); e != nil {
			return e
		}
		return putJSON(tx, fmt.Sprintf("%sevents/%020d", supplementPrefix(d.Name), c.Sequence), SupplementEvent{c.Sequence, epoch, "input", c.Complete})
	})
}
func sameSupplement(tx port.CaptureReader, in Supplement) error {
	var prior Supplement
	if e := getJSON(tx, supplementKey(in.Identity, in.Codec, in.Selection), &prior); e != nil {
		return e
	}
	if !equalJSON(prior, in) {
		return ErrConflict
	}
	return nil
}

type SupplementStatus struct {
	Descriptor model.SupplementStreamDescriptor `json:"descriptor"`
	Epoch      uint64                           `json:"epoch"`
	Status     string                           `json:"status"`
	Complete   *model.BlockPoint                `json:"complete"`
	Backlog    uint64                           `json:"backlog"`
	Sequence   uint64                           `json:"sequence"`
	Parent     *model.BlockPoint                `json:"parent,omitempty"`
}

func supplementStatus(tx port.CaptureReader, st State, d model.SupplementStreamDescriptor) (out SupplementStatus, err error) {
	out = SupplementStatus{Descriptor: d, Epoch: st.Generation, Status: "not_started"}
	if d.Validate() != nil || d.FromHeight <= st.Anchor.Number {
		return out, ErrConflict
	}
	if st.Through.Number >= d.FromHeight {
		out.Backlog = st.Through.Number - d.FromHeight + 1
	}
	c, e := streamCursor(tx, st, d)
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
	if d.ParentCheckpoint {
		if c.Parent == nil {
			out.Status = "checkpoint_pending"
			return out, nil
		}
		if _, e := canonicalID(tx, st, *c.Parent); e != nil {
			return out, e
		}
		out.Parent = &model.BlockPoint{Height: c.Parent.Number, Hash: c.Parent.Hash}
	}
	if c.Complete == nil {
		if st.Through.Number < d.FromHeight {
			out.Status = "waiting"
		}
		return out, nil
	}
	// Status validates the atomic receipt, not multi-MiB proof bytes. Actual
	// consumers and restart recovery re-read and verify those bytes separately.
	if _, _, e := streamReceipt(tx, st, d, *c.Complete); e != nil {
		return out, e
	}
	out.Complete = &model.BlockPoint{Height: c.Complete.Number, Hash: c.Complete.Hash}
	out.Backlog = st.Through.Number - c.Complete.Number
	out.Status = "lagging"
	if out.Backlog == 0 {
		out.Status = "caught_up"
	}
	return out, nil
}
func (s *Store) SupplementStreamStatus(ctx context.Context, d model.SupplementStreamDescriptor) (out SupplementStatus, err error) {
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		out, e = supplementStatus(tx, st, d)
		return e
	})
	return out, err
}

func (s *Store) SupplementStreamEvents(ctx context.Context, d model.SupplementStreamDescriptor, after uint64, limit int) (out []SupplementEvent, err error) {
	if d.Validate() != nil || limit < 1 || limit > 256 {
		return nil, port.ErrInvalidData
	}
	out = []SupplementEvent{}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		c, e := streamCursor(tx, st, d)
		if e != nil {
			return e
		}
		if after > c.Sequence {
			return ErrConflict
		}
		for n := after; n < c.Sequence && len(out) < limit; {
			n++
			var event SupplementEvent
			if e := getJSON(tx, fmt.Sprintf("%sevents/%020d", supplementPrefix(d.Name), n), &event); e != nil {
				return e
			}
			if event.Sequence != n || event.Epoch == 0 || event.Kind != "input" && event.Kind != "reset" && event.Kind != "checkpoint" {
				return ErrConflict
			}
			out = append(out, event)
		}
		return nil
	})
	return out, err
}
