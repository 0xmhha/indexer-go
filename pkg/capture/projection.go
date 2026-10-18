package capture

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime/trace"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ProjectionReceipt records AFTER the projection writer's durable commit.
// It is an exact per-input receipt, not a contiguous projection cursor and
// never a raw-consumer ACK, source FactsReady or permission to collect raw.
// The projection DB's cursor/undo/outbox remain owned by its single writer.
type ProjectionReceipt struct {
	Consumer string                `json:"consumer"`
	Epoch    uint64                `json:"epoch"`
	Identity model.CaptureIdentity `json:"identity"`
}

func projectionKey(consumer string, epoch uint64, id model.CaptureIdentity) string {
	return fmt.Sprintf("projection/%x/%020d/%x", sha256.Sum256([]byte(consumer)), epoch, rowOwner(id))
}
func validProjectionConsumer(c string) bool {
	if len(c) == 0 || len(c) > 128 {
		return false
	}
	for _, r := range c {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
func checkProjection(tx port.CaptureReader, epoch uint64, id model.CaptureIdentity) error {
	_, _, e := projectionInput(tx, epoch, id)
	return e
}
func projectionInput(tx port.CaptureReader, epoch uint64, id model.CaptureIdentity) (State, Input, error) {
	var in Input
	st, e := loadState(tx)
	if e != nil {
		return st, in, e
	}
	if epoch != st.Generation {
		return st, in, ErrStale
	}
	actual, e := canonicalID(tx, st, id.Block)
	if e != nil {
		return st, in, e
	}
	if actual != id {
		return st, in, ErrStale
	}
	if e = getJSON(tx, factsKey(id), &in); e != nil {
		return st, in, e
	}
	if in.Manifest.Identity != id {
		return st, in, ErrConflict
	}
	return st, in, st.Plan.ValidateComplete(in.Manifest)
}

// ValidateProjection checks the generation and exact canonical captured
// identity. Read-only DB consumers may continue during a raw deletion fence.
// Fresh canonicality is separately checked by the source before commit.
func (s *Store) ValidateProjection(ctx context.Context, epoch uint64, id model.CaptureIdentity) error {
	if id.Block.Chain != s.chain {
		return port.ErrInvalidData
	}
	return s.projectionView(ctx, s.chain, func(tx port.CaptureReader) error {
		tx, e := projectionMetadata(tx, id)
		if e != nil {
			return e
		}
		return checkProjection(tx, epoch, id)
	})
}

func (s *Store) projectionView(ctx context.Context, chain model.FeedChain, fn func(port.CaptureReader) error) error {
	if views, ok := s.db.(port.CaptureProjectionViewer); ok {
		return views.CaptureProjectionView(ctx, chain, fn)
	}
	return s.db.CaptureView(ctx, chain, fn)
}
func (s *Store) AckProjection(ctx context.Context, consumer string, epoch uint64, id model.CaptureIdentity) error {
	region := trace.StartRegion(ctx, "feed.ack")
	defer region.End()
	if trace.IsEnabled() {
		trace.Logf(ctx, "feed.block", "%d", id.Block.Number)
	}
	if !validProjectionConsumer(consumer) || epoch == 0 || id.Block.Chain != s.chain {
		return port.ErrInvalidData
	}
	update := s.db.CaptureUpdate
	if receipts, ok := s.db.(port.CaptureReceipter); ok {
		update = receipts.CaptureReceipt
	}
	return update(ctx, s.chain, func(tx port.CaptureWriter) error {
		if e := checkProjection(tx, epoch, id); e != nil {
			return e
		}
		receipt := ProjectionReceipt{Consumer: consumer, Epoch: epoch, Identity: id}
		var prior ProjectionReceipt
		e := getJSON(tx, projectionKey(consumer, epoch, id), &prior)
		if e == nil {
			if prior != receipt {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		return putJSON(tx, projectionKey(consumer, epoch, id), receipt)
	})
}

// ProjectionAck returns historical evidence for this exact epoch/input. A
// receipt can survive reorg and does not assert current canonicality.
func (s *Store) ProjectionAck(ctx context.Context, consumer string, epoch uint64, id model.CaptureIdentity) (ProjectionReceipt, error) {
	var receipt ProjectionReceipt
	if !validProjectionConsumer(consumer) || epoch == 0 || id.Block.Chain != s.chain {
		return receipt, port.ErrInvalidData
	}
	err := s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		if e := getJSON(tx, projectionKey(consumer, epoch, id), &receipt); e != nil {
			return e
		}
		if receipt != (ProjectionReceipt{Consumer: consumer, Epoch: epoch, Identity: id}) {
			return ErrConflict
		}
		return nil
	})
	return receipt, err
}
