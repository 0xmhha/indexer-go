package indexingstatus

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

type SourceScope struct {
	Epoch      uint64      `json:"epoch"`
	Hash       common.Hash `json:"hash"`
	Definition string      `json:"definition"`
}
type SourceRequest struct {
	Height      *uint64
	Hash        common.Hash
	Transaction bool
	Kind        capture.RowKind
	After       uint32
	Limit       int
	Expected    *SourceScope
}
type SourcePage struct {
	capture.LookupSnapshot
	Scope    *SourceScope           `json:"scope,omitempty"`
	Identity *model.CaptureIdentity `json:"identity,omitempty"`
	Found    bool                   `json:"found"`
	Kind     capture.RowKind        `json:"kind"`
	Codec    string                 `json:"codec"`
	Rows     []capture.Row          `json:"rows"`
	Next     uint32                 `json:"next"`
	Done     bool                   `json:"done"`
}

// ReadSource uses only permanent source storage and a committed derived index.
// Absence applies exclusively to (anchor, indexedThrough], never all history.
// Model rows retain their versioned RLP bytes; JSON encoding emits base64.
func ReadSource(ctx context.Context, store any, q SourceRequest) (out SourcePage, err error) {
	if q.Limit < 1 || q.Limit > 256 || capture.RowCodec(q.Kind) == "" || q.After > 0 && q.Expected == nil || (q.Height == nil) == (q.Hash == (common.Hash{})) || q.Transaction && (q.Height != nil || q.Kind != capture.RowTransaction && q.Kind != capture.RowReceipt && q.Kind != capture.RowLog) {
		return out, port.ErrInvalidData
	}
	if q.Expected != nil && (q.Expected.Epoch == 0 || q.Expected.Hash == (common.Hash{}) || q.Expected.Definition == "") {
		return out, port.ErrInvalidData
	}
	ir, ok := store.(port.IndexingReader)
	if !ok {
		return out, ErrUnavailable
	}
	db, ok := store.(port.CaptureDatabase)
	if !ok {
		return out, ErrUnavailable
	}
	page, e := ir.ReadIndexingPage(ctx, "", 1)
	if e != nil {
		if errors.Is(e, port.ErrInvalidData) {
			return out, capture.ErrConflict
		}
		return out, e
	}
	if page.Descriptor == nil || page.Descriptor.SourceLookup == nil {
		return out, ErrUnavailable
	}
	d := *page.Descriptor
	if d.ValidateSourceLookup() != nil {
		return out, capture.ErrConflict
	}
	p, ok := chains.Lookup(d.SourceLookup.Profile)
	if !ok {
		return out, ErrUnavailable
	}
	profile, ok := p.(chains.BinaryProfile)
	if !ok {
		return out, ErrUnavailable
	}
	status, e := capture.ReadStatus(ctx, db, d, page)
	if e != nil {
		if errors.Is(e, port.ErrInvalidData) {
			return out, capture.ErrConflict
		}
		return out, e
	}
	epoch := status.State.Generation
	if q.Expected != nil && (q.Expected.Epoch != epoch || q.Expected.Definition != d.SourceLookup.Definition()) {
		return out, capture.ErrStale
	}
	reader, e := capture.NewReader(db, d.Anchor.Chain, nil)
	if e != nil {
		return out, e
	}
	var location *capture.LookupLocation
	if q.Height != nil {
		out.LookupSnapshot, e = reader.LookupSnapshot(ctx, *d.SourceLookup, epoch)
		if e != nil {
			return out, e
		}
		if *q.Height <= out.Anchor.Number || *q.Height > out.Through.Number {
			return out, port.ErrNotFound
		}
		id, e := reader.CanonicalAt(ctx, epoch, *q.Height)
		if e != nil {
			return out, errors.Join(capture.ErrConflict, e)
		}
		location = &capture.LookupLocation{Height: *q.Height, BlockHash: id.Block.Hash}
	} else {
		kind := capture.LookupBlock
		if q.Transaction {
			kind = capture.LookupTransaction
		}
		result, e := reader.FindLookup(ctx, *d.SourceLookup, epoch, kind, q.Hash)
		if e != nil {
			return out, e
		}
		out.LookupSnapshot = result.LookupSnapshot
		location = result.Location
	}
	out.Kind, out.Codec, out.Rows = q.Kind, capture.RowCodec(q.Kind), []capture.Row{}
	if location == nil {
		if q.Expected != nil {
			return out, capture.ErrStale
		}
		if out.LookupSnapshot.Identity.Block.Number > out.Anchor.Number {
			if e = reader.ValidateProjection(ctx, epoch, out.LookupSnapshot.Identity); e != nil {
				return out, e
			}
		}
		out.Done = true
		return out, nil
	}
	id, e := reader.CanonicalAt(ctx, epoch, location.Height)
	if e != nil {
		return out, errors.Join(capture.ErrConflict, e)
	}
	if id.Block.Hash != location.BlockHash {
		return out, capture.ErrConflict
	}
	if q.Expected != nil && q.Expected.Hash != id.Block.Hash {
		return out, capture.ErrStale
	}
	b, rs, e := evmdata.ReadCaptured(ctx, reader, profile, epoch, id)
	if e != nil {
		return out, errors.Join(capture.ErrConflict, e)
	}
	if q.Transaction && (int(location.Index) >= len(b.Transactions) || b.Transactions[location.Index].Hash != q.Hash) {
		return out, capture.ErrConflict
	}
	rows, e := evmdata.SourceRows(b, rs)
	if e != nil {
		return out, errors.Join(capture.ErrConflict, e)
	}
	var filtered []capture.Row
	var logStart, logEnd uint32
	if q.Transaction && q.Kind == capture.RowLog {
		for i := uint32(0); i < location.Index; i++ {
			logStart += uint32(len(rs[i].Logs))
		}
		logEnd = logStart + uint32(len(rs[location.Index].Logs))
	}
	for _, row := range rows {
		if row.Kind != q.Kind {
			continue
		}
		if q.Transaction {
			switch q.Kind {
			case capture.RowTransaction, capture.RowReceipt:
				if row.Index != location.Index {
					continue
				}
			case capture.RowLog:
				// Log row indexes are global to the block; pagination offsets are local
				// to this transaction's selected log list.
				if row.Index < logStart || row.Index >= logEnd {
					continue
				}
			}
		}
		filtered = append(filtered, row)
	}
	if uint64(q.After) > uint64(len(filtered)) {
		return out, port.ErrInvalidData
	}
	total := 0
	next := int(q.After)
	for next < len(filtered) && len(out.Rows) < q.Limit {
		row := filtered[next]
		if total+len(row.Value) > 8<<20 {
			break
		}
		total += len(row.Value)
		out.Rows = append(out.Rows, row)
		next++
	}
	if next == int(q.After) && next < len(filtered) {
		return out, capture.ErrConflict
	}
	out.Next, out.Done = uint32(next), next == len(filtered)
	out.Found = true
	out.Identity = &id
	out.Scope = &SourceScope{Epoch: epoch, Hash: id.Block.Hash, Definition: d.SourceLookup.Definition()}
	if e = reader.ValidateProjection(ctx, epoch, id); e != nil {
		return out, e
	}
	if out.LookupSnapshot.Identity.Block.Number > out.Anchor.Number {
		if e = reader.ValidateProjection(ctx, epoch, out.LookupSnapshot.Identity); e != nil {
			return out, e
		}
	}
	return out, nil
}
