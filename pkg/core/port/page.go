package port

import "errors"

// Page selects one page of a list (refactoring plan R1-5). A list returns
// its items in a fixed order and, with them, a cursor for the next page.
//
// After continues after the last item of the previous page: pass the cursor
// that page returned. Reading a page this way costs the same at any depth.
// Offset skips items from the start of the list; it is kept for clients
// that page by number and costs time proportional to the offset. Offset is
// ignored when After is set. Limit <= 0 selects the method's default limit.
type Page struct {
	After  string
	Limit  int
	Offset int
}

// FirstPage returns a page request for the first limit items.
func FirstPage(limit int) Page { return Page{Limit: limit} }

// ErrInvalidCursor is returned when Page.After is not a cursor the list
// returned (malformed, or from another list).
var ErrInvalidCursor = errors.New("storage: invalid page cursor")

// A list method returns the cursor of the next page next to its items: an
// empty cursor means the list has no more items. Cursors are opaque; they
// stay valid while items are appended to the list, so a client can resume
// after new data arrives.
