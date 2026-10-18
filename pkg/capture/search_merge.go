package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

type searchPosting struct {
	prefix []byte
	it     *trie.Iterator
	hit    *SearchHit
}

func mergeSearchLists(ctx context.Context, tx port.CaptureReader, st State, t *trie.Trie, q SearchFilter, after *SearchPosition, limit int) ([]SearchHit, error) {
	canonical := map[uint64]model.CaptureIdentity{}
	advance := func(p *searchPosting) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		p.hit = nil
		if !p.it.Next() {
			if p.it.Err != nil {
				return errors.Join(ErrConflict, p.it.Err)
			}
			return nil
		}
		if len(p.it.Key) != len(p.prefix)+12 || !bytes.HasPrefix(p.it.Key, p.prefix) {
			return ErrConflict
		}
		n := binary.BigEndian.Uint64(p.it.Key[len(p.prefix):])
		i := binary.BigEndian.Uint32(p.it.Key[len(p.prefix)+8:])
		var loc LookupLocation
		if rlp.DecodeBytes(p.it.Value, &loc) != nil || n < q.From || n > q.To || i >= maxRows || loc.Height != n || loc.Index != i {
			return ErrConflict
		}
		point, ok := canonical[n]
		if !ok {
			var e error
			point, e = canonicalAtHeight(tx, st, n)
			if e != nil {
				return errors.Join(ErrConflict, e)
			}
			canonical[n] = point
		}
		if point.Block.Hash != loc.BlockHash {
			return ErrConflict
		}
		p.hit = &SearchHit{loc, SearchPosition{n, i}}
		return nil
	}
	var lists []*searchPosting
	for _, prefix := range q.drivers() {
		start := searchKey(prefix, q.From, 0)
		if after != nil {
			start = searchKey(prefix, after.Height, after.Index+1)
		}
		nit, e := t.NodeIteratorWithRange(start, searchKey(prefix, q.To, maxRows))
		if e != nil {
			return nil, errors.Join(ErrConflict, e)
		}
		p := &searchPosting{prefix: prefix, it: trie.NewIterator(nit)}
		if e = advance(p); e != nil {
			return nil, e
		}
		lists = append(lists, p)
	}
	out := []SearchHit{}
	for len(out) < limit {
		var next *SearchHit
		for _, p := range lists {
			if p.hit != nil && (next == nil || p.hit.Position.Height < next.Position.Height || p.hit.Position.Height == next.Position.Height && p.hit.Position.Index < next.Position.Index) {
				next = p.hit
			}
		}
		if next == nil {
			break
		}
		for _, p := range lists {
			if p.hit != nil && p.hit.Position == next.Position && p.hit.Location != next.Location {
				return nil, ErrConflict
			}
		}
		out = append(out, *next)
		if len(out) == limit {
			break
		}
		for _, p := range lists {
			if p.hit != nil && p.hit.Position == next.Position {
				if e := advance(p); e != nil {
					return nil, e
				}
			}
		}
	}
	return out, nil
}
