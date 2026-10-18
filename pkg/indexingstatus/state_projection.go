package indexingstatus

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

func projectionDescriptor(ctx context.Context, store any) (port.CaptureDatabase, model.ProjectionDescriptor, error) {
	r, ok := store.(port.IndexingReader)
	if !ok {
		return nil, model.ProjectionDescriptor{}, ErrUnavailable
	}
	db, ok := store.(port.CaptureDatabase)
	if !ok {
		return nil, model.ProjectionDescriptor{}, ErrUnavailable
	}
	page, e := r.ReadIndexingPage(ctx, "", 1)
	if e != nil {
		return nil, model.ProjectionDescriptor{}, e
	}
	if page.Descriptor == nil || page.Descriptor.StateChanges == nil {
		return nil, model.ProjectionDescriptor{}, ErrUnavailable
	}
	if page.Descriptor.ValidateStateChanges() != nil {
		return nil, model.ProjectionDescriptor{}, port.ErrInvalidData
	}
	if e := capture.CheckStateProjectionScope(ctx, db, *page.Descriptor); e != nil {
		return nil, model.ProjectionDescriptor{}, e
	}
	return db, *page.Descriptor, nil
}

type StateResult struct {
	Descriptor model.StateProjectionDescriptor `json:"descriptor"`
	Epoch      uint64                          `json:"epoch"`
	Head       capture.StateProjectionHead     `json:"head"`
}

func ReadState(ctx context.Context, store any) (out StateResult, err error) {
	db, d, e := projectionDescriptor(ctx, store)
	if e != nil {
		return out, e
	}
	out.Descriptor = *d.StateChanges
	out.Head, out.Epoch, err = capture.ReadStateProjectionHead(ctx, db, d.Anchor.Chain, *d.StateChanges)
	return out, err
}

type StateChangeResult struct {
	Epoch  uint64                        `json:"epoch"`
	Output capture.StateProjectionOutput `json:"output"`
}

func ReadStateChanges(ctx context.Context, store any, n uint64) (out StateChangeResult, err error) {
	db, d, e := projectionDescriptor(ctx, store)
	if e != nil {
		return out, e
	}
	out.Output, out.Epoch, err = capture.ReadStateProjectionOutput(ctx, db, d.Anchor.Chain, *d.StateChanges, n)
	return out, err
}

type StateEventsResult struct {
	Descriptor model.StateProjectionDescriptor `json:"descriptor"`
	Events     []capture.StateProjectionEvent  `json:"events"`
	Next       uint64                          `json:"next"`
}

func ReadStateEvents(ctx context.Context, store any, after uint64, limit int) (out StateEventsResult, err error) {
	db, d, e := projectionDescriptor(ctx, store)
	if e != nil {
		return out, e
	}
	out.Descriptor = *d.StateChanges
	out.Events, err = capture.ReadStateProjectionEvents(ctx, db, d.Anchor.Chain, *d.StateChanges, after, limit)
	out.Next = after
	if len(out.Events) > 0 {
		out.Next = out.Events[len(out.Events)-1].Sequence
	}
	return out, err
}

// ReadStateInput is explicitly historical, including reverted forks. It does
// not claim current completion; event consumers resolve exact event input IDs.
func ReadStateInput(ctx context.Context, store any, n, generation uint64, hash common.Hash) (out capture.StateProjectionOutput, err error) {
	db, d, e := projectionDescriptor(ctx, store)
	if e != nil {
		return out, e
	}
	if generation == 0 || hash == (common.Hash{}) || n < d.StateChanges.Stream.FromHeight {
		return out, port.ErrInvalidData
	}
	digest, e := d.Plan.Digest()
	if e != nil {
		return out, e
	}
	id := model.CaptureIdentity{Block: model.FeedBlock{Chain: d.Anchor.Chain, Number: n, Hash: hash}, Generation: generation, PlanVersion: d.Plan.Version, PlanDigest: digest}
	return capture.ReadStateProjectionInput(ctx, db, d.Anchor.Chain, *d.StateChanges, id)
}
