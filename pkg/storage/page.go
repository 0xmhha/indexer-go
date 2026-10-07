package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Keyset pagination (refactoring plan R1-5). A list is a key range; a page
// cursor is the key of the last item returned, so the next page seeks to it
// instead of skipping every earlier item.

// encodeCursor returns the cursor that continues after key.
func encodeCursor(key []byte) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

// decodeCursor returns the key a cursor continues after. A cursor that is
// malformed or outside [lower, upper) (another list's) is
// port.ErrInvalidCursor.
func decodeCursor(cursor string, lower, upper []byte) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || bytes.Compare(key, lower) < 0 || (upper != nil && bytes.Compare(key, upper) >= 0) {
		return nil, port.ErrInvalidCursor
	}
	return key, nil
}

// pageLimit returns page.Limit, or def when it is not positive.
func pageLimit(page port.Page, def int) int {
	if page.Limit <= 0 {
		return def
	}
	return page.Limit
}

// pageEntry is one stored entry of a page; Key and Value are copies.
type pageEntry struct {
	Key, Value []byte
}

// scanPage reads one page of the entries in [lower, upper), in key order or
// in reverse. match decides whether an entry is an item of the list (nil
// keeps every entry). The page starts after page.After, or after skipping
// page.Offset items when there is no cursor, and holds up to limit items.
// The returned cursor continues after the last item, and is empty when no
// further item exists.
func (s *PebbleStorage) scanPage(ctx context.Context, lower, upper []byte, reverse bool, page port.Page, limit int, match func(key, value []byte) bool) ([]pageEntry, string, error) {
	var after []byte
	if page.After != "" {
		var err error
		if after, err = decodeCursor(page.After, lower, upper); err != nil {
			return nil, "", err
		}
	}
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	var valid bool
	switch {
	case after == nil && !reverse:
		valid = iter.First()
	case after == nil:
		valid = iter.Last()
	case !reverse:
		valid = iter.SeekGE(after)
		if valid && bytes.Equal(iter.Key(), after) {
			valid = iter.Next()
		}
	default:
		valid = iter.SeekLT(after)
	}

	skip := 0
	if after == nil {
		skip = page.Offset
	}
	entries := make([]pageEntry, 0, limit+1)
	for ; valid && len(entries) <= limit; valid = step(iter, reverse) {
		s.pageSteps.Add(1)
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		if match != nil && !match(iter.Key(), iter.Value()) {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		entries = append(entries, pageEntry{
			Key:   append([]byte(nil), iter.Key()...),
			Value: append([]byte(nil), iter.Value()...),
		})
	}
	if err := iter.Error(); err != nil {
		return nil, "", fmt.Errorf("iterator error: %w", err)
	}
	if len(entries) <= limit {
		return entries, "", nil
	}
	return entries[:limit], encodeCursor(entries[limit-1].Key), nil
}

// step moves the iterator one entry in the page's direction.
func step(iter *pebble.Iterator, reverse bool) bool {
	if reverse {
		return iter.Prev()
	}
	return iter.Next()
}
