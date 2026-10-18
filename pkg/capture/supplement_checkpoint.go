package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// SupplementCheckpoint is the exact parent state of a tracked range. Its
// codec-owned bytes include an authenticated header and proofs; the bootstrap
// anchor need not pretend to have a captured body/receipts/manifest.
type SupplementCheckpoint struct {
	Block  model.FeedBlock `json:"block"`
	Digest common.Hash     `json:"digest"`
	Data   []byte          `json:"data"`
}

func checkpointKey(d model.SupplementStreamDescriptor, b model.FeedBlock) string {
	return fmt.Sprintf("%scheckpoint/%s", supplementPrefix(d.Name), b.Hash.Hex())
}
func validCheckpoint(in SupplementCheckpoint) bool {
	return in.Block.Validate() == nil && len(in.Data) > 0 && len(in.Data) <= MaxSupplementBytes && in.Digest == sha256.Sum256(in.Data)
}
func checkpointAt(tx port.CaptureReader, st State, d model.SupplementStreamDescriptor) (out SupplementCheckpoint, err error) {
	if d.Validate() != nil || !d.ParentCheckpoint || d.FromHeight <= st.Anchor.Number {
		return out, port.ErrInvalidData
	}
	var id model.CaptureIdentity
	var e error
	if d.FromHeight-1 == st.Anchor.Number {
		id, e = canonicalID(tx, st, st.Anchor)
	} else {
		id, e = canonicalAtHeight(tx, st, d.FromHeight-1)
	}
	if e != nil {
		return out, e
	}
	if e := getJSON(tx, checkpointKey(d, id.Block), &out); e != nil {
		return out, e
	}
	if out.Block != id.Block || !validCheckpoint(out) {
		return out, ErrConflict
	}
	return out, nil
}
func streamCheckpoint(tx port.CaptureReader, st State, d model.SupplementStreamDescriptor, c SupplementCursor) (SupplementCheckpoint, error) {
	out, e := checkpointAt(tx, st, d)
	if e != nil {
		return out, e
	}
	if c.Parent == nil || *c.Parent != out.Block {
		return out, ErrSupplementIncomplete
	}
	return out, nil
}
func (s *Store) ReadSupplementCheckpoint(ctx context.Context, epoch uint64, d model.SupplementStreamDescriptor) (out SupplementCheckpoint, err error) {
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if st.Generation != epoch {
			return ErrStale
		}
		c, e := streamCursor(tx, st, d)
		if e != nil {
			return e
		}
		if c.Epoch != epoch {
			return ErrStale
		}
		out, e = streamCheckpoint(tx, st, d, c)
		return e
	})
	return out, err
}

// Checkpoint, cursor parent and event are atomic. Even duplicate retry verifies
// the codec and fresh canonicality again. No fact event/ACK/Through is advanced.
func (s *Store) CommitSupplementCheckpoint(ctx context.Context, epoch uint64, d model.SupplementStreamDescriptor, in SupplementCheckpoint) error {
	if d.Validate() != nil || !d.ParentCheckpoint || !validCheckpoint(in) || in.Block.Chain != s.chain || in.Block.Number != d.FromHeight-1 {
		return port.ErrInvalidData
	}
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if e := mutable(st, epoch); e != nil {
			return e
		}
		if _, e := canonicalID(tx, st, in.Block); e != nil {
			return e
		}
		c, e := streamCursor(tx, st, d)
		if e != nil {
			return e
		}
		if c.Epoch != epoch {
			return ErrStale
		}
		var prior SupplementCheckpoint
		e = getJSON(tx, checkpointKey(d, in.Block), &prior)
		if e != nil && e != port.ErrNotFound {
			return e
		}
		if e == nil && (prior.Block != in.Block || prior.Digest != in.Digest || !bytes.Equal(prior.Data, in.Data)) {
			return ErrConflict
		}
		if e := s.verifyTracked(d, in.Block, in.Data); e != nil {
			return e
		}
		if e := s.fresh(ctx, in.Block); e != nil {
			return e
		}
		if c.Parent != nil {
			if *c.Parent != in.Block || e != nil {
				return ErrConflict
			}
			return nil
		}
		if c.Sequence == math.MaxUint64 {
			return port.ErrInvalidData
		}
		if e := putJSON(tx, checkpointKey(d, in.Block), in); e != nil {
			return e
		}
		b := in.Block
		c.Parent = &b
		c.Sequence++
		if e := putJSON(tx, supplementPrefix(d.Name)+"cursor", c); e != nil {
			return e
		}
		return putJSON(tx, fmt.Sprintf("%sevents/%020d", supplementPrefix(d.Name), c.Sequence), SupplementEvent{c.Sequence, epoch, "checkpoint", &b})
	})
}
