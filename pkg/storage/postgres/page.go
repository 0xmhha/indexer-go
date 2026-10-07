package postgres

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// A page cursor names its list and the position of the last item returned,
// so a cursor of one list is refused by another (port.Page).

func encodeCursor(list string, pos int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(list + "|" + strconv.FormatInt(pos, 10)))
}

// decodeCursor returns the position in cursor, 0 for an empty cursor.
func decodeCursor(list, cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, port.ErrInvalidCursor
	}
	name, pos, ok := strings.Cut(string(raw), "|")
	if !ok || name != list {
		return 0, port.ErrInvalidCursor
	}
	v, err := strconv.ParseInt(pos, 10, 64)
	if err != nil {
		return 0, port.ErrInvalidCursor
	}
	return v, nil
}
