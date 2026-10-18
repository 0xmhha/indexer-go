package indexingstatus

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

const MaxSearchDecodedBlocks = 16
const MaxSearchCursorBytes = 1024

type SearchCursor struct {
	Version uint32                 `json:"version"`
	Scope   capture.SearchScope    `json:"scope"`
	After   capture.SearchPosition `json:"after"`
}
type SearchRow struct {
	Identity model.CaptureIdentity `json:"identity"`
	Row      capture.Row           `json:"row"`
}
type SourceSearchPage struct {
	Snapshot          capture.LookupSnapshot `json:"snapshot"`
	Scope             capture.SearchScope    `json:"scope"`
	Rows              []SearchRow            `json:"rows"`
	Codec             string                 `json:"codec"`
	Next              string                 `json:"next"`
	Done              bool                   `json:"done"`
	ScannedCandidates int                    `json:"scannedCandidates"`
	DecodedBlocks     int                    `json:"decodedBlocks"`
}

func DecodeSearchCursor(raw string) (out SearchCursor, err error) {
	if len(raw) == 0 || len(raw) > MaxSearchCursorBytes {
		return out, port.ErrInvalidData
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		return out, port.ErrInvalidData
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&out) != nil || out.Version != 1 || out.Scope.Epoch == 0 || out.Scope.Definition == "" || len(out.Scope.Definition) > 128 || out.Scope.Root == ([32]byte{}) || out.Scope.EndHash == ([32]byte{}) || out.Scope.Filter == ([32]byte{}) {
		return out, port.ErrInvalidData
	}
	if _, e = dec.Token(); e != io.EOF {
		return out, port.ErrInvalidData
	}
	canonical, _ := json.Marshal(out)
	if base64.RawURLEncoding.EncodeToString(canonical) != raw {
		return out, port.ErrInvalidData
	}
	return out, nil
}
func encodeSearchCursor(scope capture.SearchScope, pos capture.SearchPosition) string {
	b, _ := json.Marshal(SearchCursor{1, scope, pos})
	return base64.RawURLEncoding.EncodeToString(b)
}
func txMatches(q capture.SearchFilter, tx *model.Transaction) bool {
	if q.Address == nil && len(q.Addresses) == 0 {
		return true
	}
	addresses := q.Addresses
	if q.Address != nil {
		addresses = []common.Address{*q.Address}
	}
	from, to := false, false
	for _, a := range addresses {
		from = from || tx.From == a
		to = to || tx.To != nil && *tx.To == a
	}
	if q.Role == "from" {
		return from
	}
	if q.Role == "to" {
		return to
	}
	return from || to
}
func logMatches(q capture.SearchFilter, log *model.Log) bool {
	if q.Address != nil && *q.Address != log.Address {
		return false
	}
	if len(q.Addresses) > 0 {
		matched := false
		for _, a := range q.Addresses {
			matched = matched || log.Address == a
		}
		if !matched {
			return false
		}
	}
	for i, t := range q.Topics {
		if t != nil && (len(log.Topics) <= i || log.Topics[i] != *t) {
			return false
		}
	}
	for i, topics := range q.TopicAny {
		if len(topics) == 0 {
			continue
		}
		if len(log.Topics) <= i {
			return false
		}
		matched := false
		for _, h := range topics {
			matched = matched || h == log.Topics[i]
		}
		if !matched {
			return false
		}
	}
	return true
}

// ReadSearch checks index locations and all requested predicates against
// permanent, profile-verified source data. It never reads RPC/raw/projected rows.
func ReadSearch(ctx context.Context, store any, q capture.SearchFilter, cursor string, limit int) (out SourceSearchPage, err error) {
	q, err = q.Normalize()
	if err != nil || limit < 1 || limit > 256 {
		return out, port.ErrInvalidData
	}
	var pinned *SearchCursor
	if cursor != "" {
		c, e := DecodeSearchCursor(cursor)
		if e != nil {
			return out, e
		}
		if c.Scope.Filter != q.Digest() {
			return out, capture.ErrStale
		}
		pinned = &c
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
	if page.Descriptor == nil || page.Descriptor.SourceSearch == nil {
		return out, ErrUnavailable
	}
	d := *page.Descriptor
	if d.ValidateSourceSearch() != nil {
		return out, capture.ErrConflict
	}
	p, ok := chains.Lookup(d.SourceSearch.Profile)
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
	if pinned != nil && (pinned.Scope.Epoch != epoch || pinned.Scope.Definition != d.SourceSearch.Definition()) {
		return out, capture.ErrStale
	}
	reader, e := capture.NewReader(db, d.Anchor.Chain, nil)
	if e != nil {
		return out, e
	}
	var after *capture.SearchPosition
	if pinned != nil {
		after = &pinned.After
	}
	candidates, e := reader.ScanSearch(ctx, *d.SourceSearch, epoch, q, after, capture.MaxSearchCandidates)
	if e != nil {
		return out, e
	}
	if pinned != nil && candidates.Scope != pinned.Scope {
		return out, capture.ErrStale
	}
	out = SourceSearchPage{Snapshot: candidates.Snapshot, Scope: candidates.Scope, Rows: []SearchRow{}, Codec: capture.RowCodec(q.Kind)}
	var current *model.Block
	var logs []*model.Log
	var id model.CaptureIdentity
	var last *capture.SearchPosition
	total := 0
	consumed := 0
	for _, hit := range candidates.Candidates {
		if current == nil || current.Number != hit.Location.Height {
			if out.DecodedBlocks == MaxSearchDecodedBlocks {
				break
			}
			id, e = reader.CanonicalAt(ctx, epoch, hit.Location.Height)
			if e != nil {
				return out, errors.Join(capture.ErrConflict, e)
			}
			var receipts []*model.Receipt
			current, receipts, e = evmdata.ReadCaptured(ctx, reader, profile, epoch, id)
			if e != nil {
				return out, errors.Join(capture.ErrConflict, e)
			}
			logs = nil
			for _, r := range receipts {
				logs = append(logs, r.Logs...)
			}
			out.DecodedBlocks++
		}
		if current.Hash != hit.Location.BlockHash {
			return out, capture.ErrConflict
		}
		i := hit.Location.Index
		var value []byte
		matched := false
		if q.Kind == capture.RowTransaction {
			if int(i) >= len(current.Transactions) {
				return out, capture.ErrConflict
			}
			tx := current.Transactions[i]
			if tx.Opaque {
				return out, capture.ErrConflict
			}
			driver := q.Primary()
			if !txMatches(driver, tx) {
				return out, capture.ErrConflict
			}
			matched = txMatches(q, tx) && (q.FromTime == nil || current.Time >= *q.FromTime) && (q.ToTime == nil || current.Time <= *q.ToTime)
			if matched {
				value, e = model.EncodeTransaction(tx)
			}
		} else {
			if int(i) >= len(logs) {
				return out, capture.ErrConflict
			}
			log := logs[i]
			// The driver must match even when a secondary predicate does not.
			driver := q.Primary()
			if !logMatches(driver, log) {
				return out, capture.ErrConflict
			}
			matched = logMatches(q, log) && (q.FromTime == nil || current.Time >= *q.FromTime) && (q.ToTime == nil || current.Time <= *q.ToTime)
			if matched {
				value, e = json.Marshal(log)
			}
		}
		if e != nil {
			return out, errors.Join(capture.ErrConflict, e)
		}
		if len(value) > 8<<20 {
			return out, capture.ErrConflict
		}
		if matched && total+len(value) > 8<<20 {
			break
		}
		pos := hit.Position
		last = &pos
		consumed++
		out.ScannedCandidates++
		if matched {
			total += len(value)
			out.Rows = append(out.Rows, SearchRow{id, capture.Row{Kind: q.Kind, Index: i, Value: value}})
		}
		if len(out.Rows) == limit {
			break
		}
	}
	out.Done = candidates.Done && consumed == len(candidates.Candidates)
	if !out.Done {
		if last == nil {
			return out, capture.ErrConflict
		}
		out.Next = encodeSearchCursor(out.Scope, *last)
	}
	end, e := reader.CanonicalAt(ctx, epoch, q.To)
	if e != nil {
		return out, errors.Join(capture.ErrConflict, e)
	}
	if end.Block.Hash != out.Scope.EndHash {
		return out, capture.ErrStale
	}
	if e = reader.ValidateProjection(ctx, epoch, end); e != nil {
		return out, e
	}
	return out, nil
}
