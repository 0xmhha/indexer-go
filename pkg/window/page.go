package window

import (
	"context"
	"sort"

	"github.com/ethereum/go-ethereum/common"
)

// Cursor is a durable raw append position, not a contiguous canonical block
// checkpoint. Capture DB cursors determine which jobs are complete after a
// restart. A collected cursor must explicitly recover via that database.
type Cursor struct {
	StoreID common.Hash
	Segment uint64
	Offset  int64 // byte immediately after the last returned frame
}

// Page returns at most 256 references in original segment/append order.
// References are hints: Acquire must pin a record before payload processing.
func (s *Store) Page(ctx context.Context, after Cursor, limit int) ([]Reference, Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, after, err
	}
	if err := s.guard(); err != nil {
		return nil, after, err
	}
	if limit < 1 || limit > 256 || after.Offset < 0 {
		return nil, after, ErrConflict
	}
	if after.StoreID != (common.Hash{}) && after.StoreID != s.id {
		return nil, after, ErrConflict
	}
	if after.StoreID == (common.Hash{}) && (after.Segment != 0 || after.Offset != 0) {
		return nil, after, ErrConflict
	}
	if after.Segment == 0 && after.Offset != 0 {
		return nil, after, ErrConflict
	}
	if after.Segment != 0 && s.segmentFor(after.Segment) == nil {
		return nil, after, ErrCursorExpired
	}
	segments := append([]*segment(nil), s.segments...)
	if s.active != nil {
		segments = append(segments, &s.active.segment)
	}
	items := make([]Reference, 0, limit)
	next := after
	next.StoreID = s.id
	for _, seg := range segments {
		if seg.ID < after.Segment {
			continue
		}
		var refs []Reference
		if s.active != nil && seg.ID == s.active.ID {
			refs = s.active.refs
		} else {
			var err error
			refs, err = s.indexRefs(seg)
			if err != nil {
				return nil, after, s.poison(err)
			}
			sort.Slice(refs, func(i, j int) bool { return refs[i].Offset < refs[j].Offset })
		}
		if seg.ID == after.Segment && after.Offset != 0 {
			found := false
			for _, r := range refs {
				if r.Offset+r.FrameBytes == after.Offset {
					found = true
					break
				}
			}
			if !found {
				return nil, after, ErrConflict
			}
		}
		for _, r := range refs {
			if seg.ID == after.Segment && r.Offset < after.Offset {
				continue
			}
			items = append(items, r)
			next = Cursor{s.id, r.Segment, r.Offset + r.FrameBytes}
			if len(items) == limit {
				return items, next, nil
			}
		}
	}
	return items, next, nil
}
