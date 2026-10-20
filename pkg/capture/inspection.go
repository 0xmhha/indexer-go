package capture

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
)

// CheckpointReader deliberately exposes no update, startup or recovery method.
type CheckpointReader interface {
	CaptureView(context.Context, model.FeedChain, func(port.CaptureReader) error) error
}

// WindowBinding decodes the existing app/feed-window/v1 record. Store identity
// is separately pinned by the window volume ledger, not this path binding.
type WindowBinding struct{ Volume, Root, Path, Reservation string }

type RawACKObservation struct {
	Block    model.FeedBlock `json:"block"`
	Consumer string          `json:"consumer"`
	Status   string          `json:"status"`
}

type CheckpointSnapshot struct {
	State         State                  `json:"state"`
	Binding       *WindowBinding         `json:"windowBinding"`
	Pending       *DeleteIntent          `json:"-"`
	PendingRecord *window.DeletionRecord `json:"pendingRecord"`
	PendingCount  int                    `json:"pendingReferenceCount"`
	RawACKs       []RawACKObservation    `json:"rawACKs"`
	ACKScope      string                 `json:"ackScope"`
	ACKTruncated  bool                   `json:"ackTruncated"`
}

// ReadCheckpoint reads one DB snapshot. ACKs cover at most the first 16 unique
// pending-deletion blocks, or Through when no deletion is pending. They are
// neither a contiguous raw-consumer cursor nor projection processing receipts.
func ReadCheckpoint(ctx context.Context, db CheckpointReader, chain model.FeedChain) (out CheckpointSnapshot, err error) {
	if db == nil || chain.Validate() != nil {
		return out, port.ErrInvalidData
	}
	out.RawACKs = []RawACKObservation{}
	err = db.CaptureView(ctx, chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		if st.Generation == 0 || st.Plan.Validate() != nil || st.Anchor.Validate() != nil || st.Through.Validate() != nil || st.Anchor.Chain != chain || st.Through.Chain != chain || st.Through.Number < st.Anchor.Number || len(st.RawConsumers) > 64 {
			return ErrConflict
		}
		for i, c := range st.RawConsumers {
			if len(c) == 0 || len(c) > 128 || i > 0 && st.RawConsumers[i-1] >= c {
				return ErrConflict
			}
		}
		canonical := func(block model.FeedBlock) (model.CaptureIdentity, error) {
			id, e := canonicalID(tx, st, block)
			if e != nil {
				return id, e
			}
			digest, _ := st.Plan.Digest()
			if block.Validate() != nil || id.Generation == 0 || id.Generation > st.Generation || id.PlanDigest != digest || id.PlanVersion != st.Plan.Version {
				return id, ErrConflict
			}
			return id, nil
		}
		if _, e = canonical(st.Through); e != nil {
			return e
		}
		if st.Finalized != nil {
			if _, e = canonical(*st.Finalized); e != nil {
				return e
			}
		}
		out.State = st
		var binding WindowBinding
		e = getJSON(tx, "app/feed-window/v1", &binding)
		if e == nil {
			rel, re := filepath.Rel(binding.Root, binding.Path)
			if binding.Volume == "" || binding.Reservation == "" || !filepath.IsAbs(binding.Root) || !filepath.IsAbs(binding.Path) || re != nil || rel == "." || !filepath.IsLocal(rel) {
				return ErrConflict
			}
			out.Binding = &binding
		} else if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		blocks := []model.FeedBlock{}
		out.ACKScope = "through_only"
		if st.PendingDelete != (common.Hash{}) {
			var intent DeleteIntent
			if e = getJSON(tx, "deletion_intent", &intent); e != nil {
				return e
			}
			if intent.Token != st.PendingDelete || intent.Generation != st.Generation {
				return ErrConflict
			}
			record, e := window.DescribeDeletion(intent.References)
			if e != nil {
				return e
			}
			out.Pending, out.PendingRecord, out.PendingCount = &intent, &record, len(intent.References)
			out.ACKScope = "pending_deletion_blocks"
			seen := map[model.FeedBlock]bool{}
			for _, ref := range intent.References {
				if ref.Block.Chain != chain {
					return ErrConflict
				}
				if seen[ref.Block] {
					continue
				}
				seen[ref.Block] = true
				if len(blocks) == 16 {
					out.ACKTruncated = true
					continue
				}
				blocks = append(blocks, ref.Block)
			}
		} else if st.Through.Number > st.Anchor.Number {
			blocks = append(blocks, st.Through)
		}
		for _, block := range blocks {
			id, e := canonical(block)
			if e != nil {
				return e
			}
			for _, consumer := range st.RawConsumers {
				ack := RawACKObservation{Block: block, Consumer: consumer, Status: "missing"}
				b, e := tx.Get([]byte(ackKey(id, consumer)))
				if e == nil {
					if len(b) != 1 || b[0] != 1 {
						return ErrConflict
					}
					ack.Status = "acknowledged"
				} else if !errors.Is(e, port.ErrNotFound) {
					return e
				}
				out.RawACKs = append(out.RawACKs, ack)
			}
		}
		return ctx.Err()
	})
	if err != nil {
		return CheckpointSnapshot{}, err
	}
	return out, nil
}
