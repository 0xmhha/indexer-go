package indexingstatus

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

type TraceScope struct {
	Epoch     uint64
	Hash      common.Hash
	Selection common.Hash
}

func ReadTraces(ctx context.Context, store any, n uint64, after uint32, limit int, expected ...TraceScope) (out evmtrace.Page, err error) {
	if limit < 1 || limit > 256 || len(expected) > 1 || after > 0 && len(expected) != 1 {
		return out, port.ErrInvalidData
	}
	if len(expected) == 1 && (expected[0].Epoch == 0 || expected[0].Hash == (common.Hash{}) || expected[0].Selection == (common.Hash{})) {
		return out, port.ErrInvalidData
	}
	r, ok := store.(port.IndexingReader)
	if !ok {
		return out, ErrUnavailable
	}
	db, ok := store.(port.CaptureDatabase)
	if !ok {
		return out, ErrUnavailable
	}
	page, e := r.ReadIndexingPage(ctx, "", 1)
	if e == port.ErrInvalidData {
		return out, capture.ErrConflict
	}
	if e != nil {
		return out, e
	}
	if page.Descriptor == nil || page.Descriptor.CallTraces == nil {
		return out, ErrUnavailable
	}
	d := *page.Descriptor
	def, e := evmtrace.DefinitionFor(*d.CallTraces)
	if e != nil {
		return out, capture.ErrConflict
	}
	p, ok := chains.Lookup(def.Profile)
	if !ok {
		return out, ErrUnavailable
	}
	profile, ok := p.(chains.BinaryProfile)
	if !ok {
		return out, ErrUnavailable
	}
	cap, e := capture.ReadStatus(ctx, db, d, page)
	if e == port.ErrInvalidData {
		return out, capture.ErrConflict
	}
	if e != nil {
		return out, e
	}
	if len(expected) == 1 && (expected[0].Epoch != cap.State.Generation || expected[0].Selection != d.CallTraces.SelectionHash) {
		return out, capture.ErrStale
	}
	if cap.CallTraces == nil {
		return out, ErrUnavailable
	}
	if cap.CallTraces.Status == "stale" {
		return out, capture.ErrStale
	}
	if n < d.CallTraces.FromHeight || cap.CallTraces.Complete == nil || n > cap.CallTraces.Complete.Height {
		return out, port.ErrNotFound
	}
	reader, e := capture.NewReader(db, d.Anchor.Chain, nil)
	if e != nil {
		return out, e
	}
	id, e := reader.CanonicalAt(ctx, cap.State.Generation, n)
	if e == port.ErrNotFound {
		return out, capture.ErrConflict
	}
	if e != nil {
		return out, e
	}
	if len(expected) == 1 && expected[0].Hash != id.Block.Hash {
		return out, capture.ErrStale
	}
	out, e = evmtrace.ReadPage(ctx, reader, profile, cap.State.Generation, id, *d.CallTraces, after, limit)
	// A completed stream cannot legitimately lose its captured/proof input.
	if e == port.ErrNotFound {
		return out, capture.ErrConflict
	}
	return out, e
}
