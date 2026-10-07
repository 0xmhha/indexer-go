package storage

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// unboundedPageChunk is how many entries scanPageOrAll reads at a time when
// a page has no limit.
const unboundedPageChunk = 1024

// scanPageOrAll is scanPage for lists where page.Limit <= 0 means no limit:
// such a page is read in chunks that each continue after the previous one,
// and holds every item after page.After (or page.Offset). A page with a
// limit is read by scanPage.
func (s *PebbleStorage) scanPageOrAll(ctx context.Context, lower, upper []byte, reverse bool, page port.Page, match func(key, value []byte) bool) ([]pageEntry, string, error) {
	if page.Limit > 0 {
		return s.scanPage(ctx, lower, upper, reverse, page, page.Limit, match)
	}
	var all []pageEntry
	for {
		entries, next, err := s.scanPage(ctx, lower, upper, reverse, page, unboundedPageChunk, match)
		if err != nil {
			return nil, "", err
		}
		all = append(all, entries...)
		if next == "" {
			return all, "", nil
		}
		page = port.Page{After: next}
	}
}

// scanLoadedPage reads one page of a list whose index entries in
// [lower, upper) point at records stored elsewhere. load reads the record of
// an entry; it returns ok false to leave the entry out of the list (a
// missing record, or one a filter rejects), so Offset and limit count only
// the entries that are items. An error from load ends the scan. limit <= 0
// reads every item (scanPageOrAll).
func scanLoadedPage[T any](ctx context.Context, s *PebbleStorage, lower, upper []byte, reverse bool, page port.Page, limit int, load func(ctx context.Context, key, value []byte) (item T, ok bool, err error)) ([]T, string, error) {
	loaded := make(map[string]T)
	var loadErr error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	match := func(key, value []byte) bool {
		item, ok, err := load(ctx, key, value)
		if err != nil {
			loadErr = err
			cancel() // scanPage stops at the next entry
			return false
		}
		if ok {
			loaded[string(key)] = item
		}
		return ok
	}
	page.Limit = limit
	entries, next, err := s.scanPageOrAll(ctx, lower, upper, reverse, page, match)
	if loadErr != nil {
		return nil, "", loadErr
	}
	if err != nil {
		return nil, "", err
	}
	items := make([]T, len(entries))
	for i, e := range entries {
		items[i] = loaded[string(e.Key)]
	}
	return items, next, nil
}
