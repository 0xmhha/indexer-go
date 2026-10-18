package storage

import (
	"bytes"
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/progress"
	"github.com/cockroachdb/pebble"
)

func init() { RegisterKeyspace("indexing-progress", ChainData, "/meta/indexing/") }
func (s *PebbleStorage) ReadIndexingPage(ctx context.Context, after string, limit int) (port.IndexingPage, error) {
	if e := s.ensureNotClosed(); e != nil {
		return port.IndexingPage{}, e
	}
	snap := s.db.NewSnapshot()
	defer func() { _ = snap.Close() }()
	get := func(k []byte) ([]byte, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		b, c, e := snap.Get(k)
		if errors.Is(e, pebble.ErrNotFound) {
			return nil, port.ErrNotFound
		}
		if e != nil {
			return nil, e
		}
		defer func() { _ = c.Close() }()
		if len(b) > progress.MaxValue {
			return nil, port.ErrInvalidData
		}
		return bytes.Clone(b), nil
	}
	scan := func(after string, n int) ([][2][]byte, error) {
		prefix := []byte(progress.StatePrefix)
		lower := prefix
		if after != "" {
			lower = append([]byte(progress.StatePrefix+after), 0)
		}
		it, e := snap.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: prefixUpperBound(prefix)})
		if e != nil {
			return nil, e
		}
		defer func() { _ = it.Close() }()
		rows := make([][2][]byte, 0, n)
		for it.First(); it.Valid() && len(rows) < n; it.Next() {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			if len(it.Key()) > len(prefix)+256 || len(it.Value()) > progress.MaxValue {
				return nil, port.ErrInvalidData
			}
			rows = append(rows, [2][]byte{bytes.Clone(it.Key()), bytes.Clone(it.Value())})
		}
		return rows, it.Error()
	}
	return progress.ReadPage(get, scan, after, limit)
}
