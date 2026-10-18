package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// Immutable trie roots represent exact canonical indexed prefixes. Reorgs
// select a retained root through the ordinary worker reset; no mutable hash
// mappings or feature undo records can leak results from an abandoned fork.
const LookupBlock byte = 'b'
const LookupTransaction byte = 't'

type LookupLocation struct {
	Height    uint64      `json:"height"`
	BlockHash common.Hash `json:"blockHash"`
	Index     uint32      `json:"index"`
}
type LookupOutput struct {
	Version      uint32      `json:"version"`
	Root         common.Hash `json:"root"`
	InputDigest  common.Hash `json:"inputDigest"`
	Transactions uint32      `json:"transactions"`
}
type LookupSnapshot struct {
	Epoch           uint64                `json:"epoch"`
	Anchor          model.FeedBlock       `json:"anchor"`
	Through         model.FeedBlock       `json:"indexedThrough"`
	CapturedThrough model.FeedBlock       `json:"capturedThrough"`
	Root            common.Hash           `json:"root"`
	Identity        model.CaptureIdentity `json:"-"`
}
type LookupResult struct {
	LookupSnapshot
	Found    bool            `json:"found"`
	Location *LookupLocation `json:"location,omitempty"`
}

func lookupTrie(tx port.CaptureReader, root common.Hash) (*trie.Trie, error) {
	return immutableTrie(tx, root, "lookup/v1")
}
func lookupKey(kind byte, h common.Hash) []byte { return append([]byte{kind}, h[:]...) }
func lookupOutput(tx port.CaptureReader, def string, id model.CaptureIdentity) (out LookupOutput, err error) {
	var w WorkerOutput
	if err = getJSON(tx, workerOutputKey(model.SourceLookupWorker, def, id), &w); err != nil {
		return out, errors.Join(ErrConflict, err)
	}
	if w.Identity != id || w.Definition != def || len(w.Data) > MaxWorkerOutput {
		return out, ErrConflict
	}
	dec := json.NewDecoder(bytes.NewReader(w.Data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&out); err != nil {
		return out, ErrConflict
	}
	if out.Version != 1 || out.Root == (common.Hash{}) || out.Root == types.EmptyRootHash || out.InputDigest == (common.Hash{}) || out.Transactions > maxRows {
		return out, ErrConflict
	}
	return out, nil
}
func loadLookup(tx port.CaptureReader, d model.SourceLookupDescriptor, epoch uint64) (out LookupSnapshot, c WorkerCursor, err error) {
	st, e := loadState(tx)
	if e != nil {
		return out, c, e
	}
	if st.Generation != epoch {
		return out, c, ErrStale
	}
	if e = getJSON(tx, workerPrefix(model.SourceLookupWorker)+"cursor", &c); e != nil {
		return out, c, e
	}
	if !validWorkerCursor(c) || c.Name != model.SourceLookupWorker || c.Definition != d.Definition() || c.Anchor != st.Anchor {
		return out, c, ErrConflict
	}
	if c.Epoch != epoch {
		return out, c, ErrStale
	}
	if c.Through.Number > st.Through.Number {
		return out, c, ErrConflict
	}
	out = LookupSnapshot{Epoch: epoch, Anchor: st.Anchor, Through: c.Through, CapturedThrough: st.Through, Root: types.EmptyRootHash}
	if c.Through == st.Anchor {
		return out, c, nil
	}
	id, e := canonicalID(tx, st, c.Through)
	if e != nil {
		return out, c, errors.Join(ErrConflict, e)
	}
	if e = checkProjection(tx, epoch, id); e != nil {
		return out, c, e
	}
	w, e := lookupOutput(tx, d.Definition(), id)
	if e != nil {
		return out, c, e
	}
	if _, e = lookupTrie(tx, w.Root); e != nil {
		return out, c, e
	}
	out.Root, out.Identity = w.Root, id
	return out, c, nil
}

// CommitLookup receives only profile-verified ordered hashes from the EVM
// adapter. Trie nodes, root, contiguous cursor and worker event share one durable
// transaction and the same uncached canonical check as other worker commits.
func (s *Store) CommitLookup(ctx context.Context, d model.SourceLookupDescriptor, epoch uint64, id model.CaptureIdentity, hashes []common.Hash) error {
	if d.Validate() != nil || len(hashes) > maxRows || id.Block.Chain != s.chain {
		return port.ErrInvalidData
	}
	seen := map[common.Hash]bool{}
	for _, h := range hashes {
		if h == (common.Hash{}) || seen[h] {
			return port.ErrInvalidData
		}
		seen[h] = true
	}
	encoded, _ := json.Marshal(struct {
		ID     model.CaptureIdentity
		Hashes []common.Hash
	}{id, hashes})
	digest := common.Hash(sha256.Sum256(encoded))
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		if e := checkProjection(tx, epoch, id); e != nil {
			return e
		}
		snap, c, e := loadLookup(tx, d, epoch)
		if e != nil {
			return e
		}
		if c.Through == id.Block {
			prior, e := lookupOutput(tx, d.Definition(), id)
			if e != nil {
				return e
			}
			if prior.InputDigest != digest || prior.Transactions != uint32(len(hashes)) {
				return ErrConflict
			}
			data, _ := json.Marshal(prior)
			return s.commitWorker(ctx, tx, model.SourceLookupWorker, d.Definition(), epoch, id, data)
		}
		if c.Through.Number == math.MaxUint64 || id.Block.Number != c.Through.Number+1 {
			return ErrGap
		}
		t, e := lookupTrie(tx, snap.Root)
		if e != nil {
			return e
		}
		add := func(kind byte, h common.Hash, i uint32) error {
			key := lookupKey(kind, h)
			old, e := t.Get(key)
			if e != nil {
				return errors.Join(ErrConflict, e)
			}
			if len(old) > 0 {
				return ErrConflict
			}
			value, e := rlp.EncodeToBytes(LookupLocation{id.Block.Number, id.Block.Hash, i})
			if e != nil {
				return e
			}
			return t.Update(key, value)
		}
		if e = add(LookupBlock, id.Block.Hash, 0); e != nil {
			return e
		}
		for i, h := range hashes {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if e = add(LookupTransaction, h, uint32(i)); e != nil {
				return e
			}
		}
		root, nodes := t.Commit(false)
		if e = storeImmutableNodes(tx, "lookup/v1", nodes); e != nil {
			return e
		}

		out := LookupOutput{1, root, digest, uint32(len(hashes))}
		data, _ := json.Marshal(out)
		return s.commitWorker(ctx, tx, model.SourceLookupWorker, d.Definition(), epoch, id, data)
	})
}
func (s *Store) LookupSnapshot(ctx context.Context, d model.SourceLookupDescriptor, epoch uint64) (out LookupSnapshot, err error) {
	if d.Validate() != nil || epoch == 0 {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		tx = newImmutableReads(ctx, tx, false)
		var e error
		out, _, e = loadLookup(tx, d, epoch)
		return e
	})
	return
}
func (s *Store) FindLookup(ctx context.Context, d model.SourceLookupDescriptor, epoch uint64, kind byte, h common.Hash) (out LookupResult, err error) {
	if d.Validate() != nil || epoch == 0 || (kind != LookupBlock && kind != LookupTransaction) || h == (common.Hash{}) {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		tx = newImmutableReads(ctx, tx, false)
		snap, _, e := loadLookup(tx, d, epoch)
		if e != nil {
			return e
		}
		out.LookupSnapshot = snap
		t, e := lookupTrie(tx, snap.Root)
		if e != nil {
			return e
		}
		b, e := t.Get(lookupKey(kind, h))
		if e != nil {
			return errors.Join(ErrConflict, e)
		}
		if len(b) == 0 {
			return nil
		}
		var loc LookupLocation
		if e = rlp.DecodeBytes(b, &loc); e != nil || loc.Height <= snap.Anchor.Number || loc.Height > snap.Through.Number || loc.Index >= maxRows || kind == LookupBlock && (loc.Index != 0 || loc.BlockHash != h) {
			return ErrConflict
		}
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		id, e := canonicalAtHeight(tx, st, loc.Height)
		if e != nil {
			return errors.Join(ErrConflict, e)
		}
		if id.Block.Hash != loc.BlockHash {
			return ErrConflict
		}
		out.Found = true
		out.Location = &loc
		return nil
	})
	return
}
func (r *Reader) LookupSnapshot(ctx context.Context, d model.SourceLookupDescriptor, epoch uint64) (LookupSnapshot, error) {
	return r.store.LookupSnapshot(ctx, d, epoch)
}
func (r *Reader) FindLookup(ctx context.Context, d model.SourceLookupDescriptor, epoch uint64, kind byte, h common.Hash) (LookupResult, error) {
	return r.store.FindLookup(ctx, d, epoch, kind, h)
}
