package ingress

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// RawDeletion binds one complete segment to stopped-owner observations.
// Epochs are observations, not substitutes for publication, ACK or an E3 lease.
type RawDeletion struct {
	Version                                uint8
	OwnerDigest, SourceDigest, ACKDigest   execwire.Hash
	VolumeID, Reservation                  string
	NativeEpoch, NativeReadEpoch, RawEpoch uint64
	Record                                 window.DeletionRecord
	References                             []window.Reference
}

type RawDeletionTicket struct {
	Permit    retention.Permit
	Evidence  RawDeletion
	Completed bool
}

type RawDeletionStore interface {
	WithExecutionRawDeletion(context.Context, retention.Permit, RawDeletion, func(context.Context) error) error
	CheckExecutionRawDeletion(context.Context, retention.Permit, RawDeletion, window.DeletionRecord) error
	ExecutionRawDeletion(context.Context, execwire.Hash) (RawDeletionTicket, error)
}

func (r RawDeletion) Validate(p retention.Permit) error {
	if p.Validate() != nil || r.Version != 1 || r.OwnerDigest == (execwire.Hash{}) || r.SourceDigest != p.Request.SourceDigest || r.ACKDigest == (execwire.Hash{}) || !validWindowName(r.VolumeID) || r.Reservation != "" && !validWindowName(r.Reservation) || r.Record.Segment > math.MaxInt64 || r.Record.Match(r.References) != nil {
		return retention.ErrUnsafe
	}
	for _, n := range []uint64{r.NativeEpoch, r.NativeReadEpoch, r.RawEpoch} {
		if n == 0 || n > math.MaxInt64 {
			return retention.ErrUnsafe
		}
	}
	if p.Request.ACK != p.Visible || p.Request.Tail != p.Visible || p.Request.Head.Height != p.Visible.Height || p.Request.Head.Hash != p.Visible.Hash || p.Request.HeadRoot != p.Root {
		return retention.ErrUnsafe
	}
	for _, ref := range r.References {
		if ref.Block.Chain.ID != p.Request.Chain.ID || execwire.Hash(ref.Block.Chain.GenesisHash) != p.Request.Chain.Genesis || ref.Block.Number <= p.Request.Anchor.Height || ref.Block.Number > p.Request.Through {
			return window.ErrRetain
		}
	}
	actual, err := window.DescribeDeletion(r.References)
	if err != nil || actual != r.Record {
		return retention.ErrUnsafe
	}
	return nil
}

func (r RawDeletion) Bytes(p retention.Permit) ([]byte, execwire.Hash, error) {
	if err := r.Validate(p); err != nil {
		return nil, execwire.Hash{}, err
	}
	b, err := json.Marshal(struct {
		Permit   retention.Permit
		Evidence RawDeletion
	}{p, r})
	if err != nil || len(b) > 8<<20 {
		return nil, execwire.Hash{}, retention.ErrUnsafe
	}
	return b, execwire.Hash(sha256.Sum256(append([]byte("indexer.ingress.raw-deletion.v1\x00"), b...))), nil
}
