package postgres

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// A page cursor names its list and holds the sort key of the last item
// returned, so a list continues after it with a keyset query, and a cursor
// of one list is refused by another (port.Page).

// encodeCursor returns the cursor of list after the item with sort key
// parts.
func encodeCursor(list string, parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(list + "|" + strings.Join(parts, "|")))
}

// decodeCursor returns the n parts of a cursor of list, nil for an empty
// cursor.
func decodeCursor(list, cursor string, n int) ([]string, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, port.ErrInvalidCursor
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != n+1 || parts[0] != list {
		return nil, port.ErrInvalidCursor
	}
	return parts[1:], nil
}

// pageLimit returns the rows a page reads: one more than it returns, to
// know whether a next page exists; 0 means no limit (defaultAll).
func pageLimit(page port.Page, defaultAll bool) int {
	switch {
	case page.Limit > 0:
		return page.Limit
	case defaultAll:
		return 0
	default:
		return defaultPageLimit
	}
}

// limitClause returns the LIMIT and OFFSET of a page read: limit+1 rows,
// after Offset rows unless the page continues after a cursor.
func limitClause(page port.Page, limit int) string {
	s := ""
	if page.After == "" && page.Offset > 0 {
		s += " OFFSET " + itoa(page.Offset)
	}
	if limit > 0 {
		s += " LIMIT " + itoa(limit+1)
	}
	return s
}

// trimPage cuts rows to limit and reports whether a next page exists.
func trimPage[T any](rows []T, limit int) ([]T, bool) {
	if limit > 0 && len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}

// keyCol is one sort column of a list: its SQL expression, the type of its
// cursor value (kindInt, kindNumeric, kindBytes) and its direction. A column
// the select list also reads with a cast (balance::text) must be qualified
// with its table: ORDER BY resolves a bare name to the output column, which
// would sort the text.
type keyCol struct {
	expr string
	kind byte
	desc bool
}

const (
	kindInt     = 'i' // integer, cursor value in decimal
	kindNumeric = 'n' // numeric, cursor value in decimal
	kindBytes   = 'b' // bytea, cursor value in hex
)

// param returns the SQL of cursor value $n of the column's type.
func (k keyCol) param(n int) string {
	p := "$" + itoa(n)
	switch k.kind {
	case kindNumeric:
		return p + "::numeric"
	case kindBytes:
		return "decode(" + p + ", 'hex')"
	default:
		return p + "::bigint"
	}
}

// listQuery is a list read a page at a time with a keyset cursor.
type listQuery[T any] struct {
	list       string   // names the list in its cursors
	sql        string   // SELECT ... FROM ... WHERE <filter>
	args       []any    // arguments of sql
	keys       []keyCol // sort key, unique per item
	defaultAll bool     // Limit <= 0 returns every item
	scan       pgx.RowToFunc[T]
	keyOf      func(T) []string // the sort key of an item, as cursor values
}

// run reads one page: the items after the cursor (or after Offset items),
// in key order, and the cursor of the next page.
func (lq listQuery[T]) run(ctx context.Context, q querier, page port.Page) ([]T, string, error) {
	after, err := decodeCursor(lq.list, page.After, len(lq.keys))
	if err != nil {
		return nil, "", err
	}
	sql, args := lq.sql, append([]any{}, lq.args...)
	if after != nil {
		// (k1 > v1) OR (k1 = v1 AND k2 > v2) OR ..., with < for descending
		// columns.
		var or []string
		for i := range lq.keys {
			var and []string
			for j := 0; j <= i; j++ {
				k := lq.keys[j]
				args = append(args, after[j])
				op := "="
				if j == i {
					op = ">"
					if k.desc {
						op = "<"
					}
				}
				and = append(and, k.expr+" "+op+" "+k.param(len(args)))
			}
			or = append(or, "("+strings.Join(and, " AND ")+")")
		}
		sql += " AND (" + strings.Join(or, " OR ") + ")"
	}
	order := make([]string, len(lq.keys))
	for i, k := range lq.keys {
		order[i] = k.expr
		if k.desc {
			order[i] += " DESC"
		}
	}
	limit := pageLimit(page, lq.defaultAll)
	sql += " ORDER BY " + strings.Join(order, ", ") + limitClause(page, limit)
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", err
	}
	items, err := pgx.CollectRows(rows, lq.scan)
	if err != nil {
		return nil, "", err
	}
	items, more := trimPage(items, limit)
	if !more {
		return items, "", nil
	}
	return items, encodeCursor(lq.list, lq.keyOf(items[len(items)-1])...), nil
}
