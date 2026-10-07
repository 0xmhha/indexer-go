package porttest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// listPage reads one page of a list.
type listPage[T any] func(page port.Page) ([]T, string, error)

// checkPaging checks the Page contract of a list whose items, in list
// order, are want (at least three):
//   - a page as large as the list returns all items and no cursor;
//   - following cursors page by page returns every item once, in order, and
//     the last page returns no cursor;
//   - Offset skips items from the start, and is ignored when After is set;
//   - a malformed cursor is port.ErrInvalidCursor.
//
// key turns an item into a comparable value for messages and equality.
func checkPaging[T any, K comparable](t *testing.T, want []T, key func(T) K, list listPage[T]) {
	t.Helper()
	require.GreaterOrEqual(t, len(want), 3, "the fixture must hold at least three items")
	keys := func(items []T) []K {
		out := make([]K, len(items))
		for i, it := range items {
			out[i] = key(it)
		}
		return out
	}
	wantKeys := keys(want)

	all, next, err := list(port.Page{Limit: len(want) + 5})
	require.NoError(t, err)
	assert.Equal(t, wantKeys, keys(all), "the whole list in order")
	assert.Empty(t, next, "no cursor after the last page")

	for _, size := range []int{1, 2, len(want) - 1} {
		t.Run(fmt.Sprintf("Cursor%d", size), func(t *testing.T) {
			var got []K
			page := port.Page{Limit: size}
			for i := 0; ; i++ {
				require.Less(t, i, len(want)+2, "cursor paging does not end")
				items, next, err := list(page)
				require.NoError(t, err)
				assert.LessOrEqual(t, len(items), size)
				got = append(got, keys(items)...)
				if next == "" {
					break
				}
				assert.Len(t, items, size, "only the last page may be short")
				page = port.Page{After: next, Limit: size}
			}
			assert.Equal(t, wantKeys, got)
		})
	}

	items, _, err := list(port.Page{Limit: 2, Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, wantKeys[1:3], keys(items), "Offset skips items from the start")

	items, _, err = list(port.Page{Limit: 1, Offset: len(want)})
	require.NoError(t, err)
	assert.Empty(t, items, "offset past the end")

	_, next, err = list(port.Page{Limit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, next)
	items, _, err = list(port.Page{After: next, Limit: 1, Offset: 99})
	require.NoError(t, err)
	assert.Equal(t, wantKeys[1:2], keys(items), "Offset is ignored when After is set")

	_, _, err = list(port.Page{After: "not a cursor!", Limit: 1})
	assert.ErrorIs(t, err, port.ErrInvalidCursor)
}

// checkCursorFromOtherList checks that a cursor one list returned is
// rejected by another list (other) of the same kind.
func checkCursorFromOtherList[T any](t *testing.T, list, other listPage[T]) {
	t.Helper()
	_, next, err := list(port.Page{Limit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, next, "the list must have more than one item")
	_, _, err = other(port.Page{After: next, Limit: 1})
	assert.ErrorIs(t, err, port.ErrInvalidCursor)
}
