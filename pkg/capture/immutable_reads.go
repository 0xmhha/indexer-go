package capture

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const immutableReadBytes = 4 << 20
const immutableReadEntries = 4096

type immutableReadEntry struct {
	value    []byte
	err      error
	expanded bool
}

// One cache belongs to one CaptureView callback. Only immutable, hash-checked
// node bytes enter it; metadata/cursors/fresh checks always reach the reader.
type immutableReads struct {
	port.CaptureReader
	ctx           context.Context
	nodes         map[string]*immutableReadEntry
	used          int
	prefetchNodes bool
}

func newImmutableReads(ctx context.Context, tx port.CaptureReader, prefetch bool) *immutableReads {
	return &immutableReads{CaptureReader: tx, ctx: ctx, prefetchNodes: prefetch, nodes: map[string]*immutableReadEntry{}}
}
func validImmutableNode(value []byte, h common.Hash) bool {
	return len(value) > 0 && len(value) <= 1<<20 && crypto.Keccak256Hash(value) == h
}
func (r *immutableReads) remember(key string, value []byte, err error) *immutableReadEntry {
	cost := len(key) + len(value) + 64
	if len(r.nodes) >= immutableReadEntries || r.used+cost > immutableReadBytes {
		return nil
	}
	e := &immutableReadEntry{value: append([]byte(nil), value...), err: err}
	r.nodes[key] = e
	r.used += cost
	return e
}
func (r *immutableReads) node(prefix string, h common.Hash) ([]byte, error) {
	if e := r.ctx.Err(); e != nil {
		return nil, e
	}
	key := string(immutableNodeKey(prefix, h))
	e, ok := r.nodes[key]
	if !ok {
		value, err := r.Get([]byte(key))
		if err != nil {
			return nil, errors.Join(ErrConflict, err)
		}
		if !validImmutableNode(value, h) {
			return nil, ErrConflict
		}
		e = r.remember(key, value, nil)
		if e == nil {
			return value, nil
		} // Cache full: validated on-demand read.
	}
	if e.err != nil {
		return nil, e.err
	}
	if !e.expanded {
		e.expanded = true
		if err := r.prefetch(prefix, e.value); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), e.value...), nil
}
func (r *immutableReads) prefetch(prefix string, value []byte) error {
	batch, ok := r.CaptureReader.(port.CaptureBatchReader)
	if !r.prefetchNodes || !ok || len(r.nodes) >= immutableReadEntries || r.used >= immutableReadBytes {
		return nil
	}
	keys := [][]byte{}
	hashes := map[string]common.Hash{}
	for _, h := range immutableChildHashes(value) {
		key := immutableNodeKey(prefix, h)
		if _, known := r.nodes[string(key)]; known {
			continue
		}
		if _, duplicate := hashes[string(key)]; duplicate {
			continue
		}
		keys = append(keys, key)
		hashes[string(key)] = h
	}
	if len(keys) == 0 {
		return nil
	}
	values, err := batch.GetMany(keys)
	if err != nil {
		return errors.Join(ErrConflict, err)
	}
	for key := range values {
		if _, wanted := hashes[key]; !wanted {
			return ErrConflict
		}
	}
	for key, h := range hashes {
		v, found := values[key]
		var err error
		if !found {
			err = errors.Join(ErrConflict, port.ErrNotFound)
		} else if !validImmutableNode(v, h) {
			err = ErrConflict
		}
		// An unrelated sibling need not exist for this query. A required
		// missing/corrupt node still fails when traversed, never as absence.
		if err != nil {
			v = nil
		}
		r.remember(key, v, err)
	}
	return nil
}

// Only direct hash references in branch/extension nodes are prefetched.
// Leaf values (even 32-byte ones) are never interpreted as child hashes.
// Embedded nodes are <32 bytes and therefore cannot contain hash references.
func immutableChildHashes(blob []byte) []common.Hash {
	list, rest, err := rlp.SplitList(blob)
	if err != nil || len(rest) != 0 {
		return nil
	}
	items := [][]byte{}
	for len(list) != 0 {
		_, _, rest, err := rlp.Split(list)
		if err != nil || len(items) == 17 {
			return nil
		}
		items = append(items, list[:len(list)-len(rest)])
		list = rest
	}
	var refs [][]byte
	switch len(items) {
	case 17:
		refs = items[:16]
	case 2:
		path, _, err := rlp.SplitString(items[0])
		if err != nil || len(path) == 0 || path[0]>>4 > 3 || path[0]&0x20 != 0 {
			return nil
		}
		refs = items[1:]
	default:
		return nil
	}
	out := []common.Hash{}
	for _, ref := range refs {
		b, _, err := rlp.SplitString(ref)
		if err == nil && len(b) == common.HashLength {
			out = append(out, common.BytesToHash(b))
		}
	}
	return out
}
