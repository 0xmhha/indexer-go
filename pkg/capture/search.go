package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"sort"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// SearchData is produced by a profile-verified execution adapter, outside the
// transaction. Addresses mean transaction sender/recipient or log emitter only.
type SearchTransaction struct {
	Hash common.Hash
	From common.Address
	To   *common.Address
}
type SearchLog struct {
	Address     common.Address
	Topics      []common.Hash
	Transaction uint32
}
type SearchData struct {
	Transactions []SearchTransaction
	Logs         []SearchLog
}
type SearchOutput struct {
	Version      uint32      `json:"version"`
	Root         common.Hash `json:"root"`
	InputDigest  common.Hash `json:"inputDigest"`
	Transactions uint32      `json:"transactions"`
	Logs         uint32      `json:"logs"`
}

// SearchFilter ANDs dimensions; alternatives within a dimension are ORed.
// Omitted topic positions are wildcards, including when searching the zero hash.
type SearchFilter struct {
	Kind      RowKind          `json:"kind"`
	From      uint64           `json:"from"`
	To        uint64           `json:"to"`
	Address   *common.Address  `json:"address,omitempty"`
	Role      string           `json:"role,omitempty"`
	Topics    [4]*common.Hash  `json:"topics"`
	Addresses []common.Address `json:"addresses,omitempty"`
	TopicAny  [][]common.Hash  `json:"topicAny,omitempty"`
	FromTime  *uint64          `json:"fromTime,omitempty"`
	ToTime    *uint64          `json:"toTime,omitempty"`
}
type SearchPosition struct {
	Height uint64 `json:"height"`
	Index  uint32 `json:"index"`
}
type SearchHit struct {
	Location LookupLocation `json:"location"`
	Position SearchPosition `json:"position"`
}
type SearchScope struct {
	Epoch      uint64      `json:"epoch"`
	EndHash    common.Hash `json:"endHash"`
	Root       common.Hash `json:"root"`
	Filter     common.Hash `json:"filter"`
	Definition string      `json:"definition"`
}

// SearchCandidatePage contains matches for the primary posting key only.
// Every secondary predicate still requires verified source-data evaluation.
type SearchCandidatePage struct {
	Snapshot   LookupSnapshot `json:"snapshot"`
	Scope      SearchScope    `json:"scope"`
	Candidates []SearchHit    `json:"hits"`
	Done       bool           `json:"done"`
}

const MaxSearchCandidates = 256
const MaxSearchAlternatives = 16
const MaxSearchTotalAlternatives = 64

func (q SearchFilter) Validate() error {
	if q.From > q.To || (q.Kind != RowLog && q.Kind != RowTransaction) {
		return port.ErrInvalidData
	}
	if q.FromTime != nil && q.ToTime != nil && *q.FromTime > *q.ToTime ||
		q.Address != nil && len(q.Addresses) != 0 || len(q.Addresses) > MaxSearchAlternatives ||
		len(q.TopicAny) != 0 && len(q.TopicAny) != 4 {
		return port.ErrInvalidData
	}
	total := len(q.Addresses)
	if q.Address != nil {
		total++
	}
	for i, topics := range q.TopicAny {
		if len(topics) > MaxSearchAlternatives || len(topics) > 0 && q.Topics[i] != nil {
			return port.ErrInvalidData
		}
		total += len(topics)
	}
	for _, topic := range q.Topics {
		if topic != nil {
			total++
		}
	}
	if total > MaxSearchTotalAlternatives {
		return port.ErrInvalidData
	}
	if q.Kind == RowLog && q.Role != "" {
		return port.ErrInvalidData
	}
	if q.Kind == RowTransaction {
		for _, topics := range q.TopicAny {
			if len(topics) != 0 {
				return port.ErrInvalidData
			}
		}
		for _, t := range q.Topics {
			if t != nil {
				return port.ErrInvalidData
			}
		}
		if q.Role != "" && q.Role != "from" && q.Role != "to" {
			return port.ErrInvalidData
		}
		if q.Role != "" && q.Address == nil && len(q.Addresses) == 0 {
			return port.ErrInvalidData
		}
	}
	return nil
}

// Normalize copies, sorts and deduplicates alternatives. A single alternative
// retains the original v1 digest, so existing exact-filter cursors still work.
func (q SearchFilter) Normalize() (SearchFilter, error) {
	if e := q.Validate(); e != nil {
		return q, e
	}
	q.Addresses = append([]common.Address(nil), q.Addresses...)
	sort.Slice(q.Addresses, func(i, j int) bool { return bytes.Compare(q.Addresses[i][:], q.Addresses[j][:]) < 0 })
	addresses := q.Addresses[:0]
	for _, a := range q.Addresses {
		if len(addresses) == 0 || addresses[len(addresses)-1] != a {
			addresses = append(addresses, a)
		}
	}
	q.Addresses = addresses
	if len(addresses) == 1 {
		a := addresses[0]
		q.Address, q.Addresses = &a, nil
	}
	anyTopics := make([][]common.Hash, 4)
	hasAny := false
	for i, values := range q.TopicAny {
		v := append([]common.Hash(nil), values...)
		sort.Slice(v, func(i, j int) bool { return bytes.Compare(v[i][:], v[j][:]) < 0 })
		unique := v[:0]
		for _, h := range v {
			if len(unique) == 0 || unique[len(unique)-1] != h {
				unique = append(unique, h)
			}
		}
		if len(unique) == 1 {
			h := unique[0]
			q.Topics[i] = &h
		} else if len(unique) > 1 {
			anyTopics[i] = unique
			hasAny = true
		}
	}
	q.TopicAny = nil
	if hasAny {
		q.TopicAny = anyTopics
	}
	return q, nil
}
func (q SearchFilter) Digest() common.Hash {
	q, e := q.Normalize()
	if e != nil {
		return common.Hash{}
	}
	b, _ := json.Marshal(q)
	return sha256.Sum256(b)
}

// Primary contains only the posting dimension; source readers recheck it even
// when another predicate (including time) rejects the candidate.
func (q SearchFilter) Primary() SearchFilter {
	p := SearchFilter{Kind: q.Kind, Address: q.Address, Addresses: q.Addresses}
	if q.Kind == RowTransaction || q.Address != nil || len(q.Addresses) > 0 {
		return p
	}
	for i, t := range q.Topics {
		if t != nil {
			p.Topics[i] = t
			return p
		}
		if len(q.TopicAny) == 4 && len(q.TopicAny[i]) > 0 {
			p.TopicAny = make([][]common.Hash, 4)
			p.TopicAny[i] = q.TopicAny[i]
			return p
		}
	}
	return p
}

// drivers selects at most 16 ordered lists for one dimension. The bounded
// merge keeps a single height/ordinal cursor and never sorts full history.
func (q SearchFilter) drivers() [][]byte {
	p := q.Primary()
	addressPrefix := byte('a')
	if q.Kind == RowTransaction {
		addressPrefix = 'p'
	}
	if p.Address != nil {
		return [][]byte{append([]byte{addressPrefix}, p.Address[:]...)}
	}
	if len(p.Addresses) > 0 {
		out := make([][]byte, 0, len(p.Addresses))
		for _, a := range p.Addresses {
			out = append(out, append([]byte{addressPrefix}, a[:]...))
		}
		return out
	}
	for i, t := range p.Topics {
		if t != nil {
			return [][]byte{append([]byte{byte('0' + i)}, t[:]...)}
		}
		if len(p.TopicAny) == 4 && len(p.TopicAny[i]) > 0 {
			out := make([][]byte, 0, len(p.TopicAny[i]))
			for _, h := range p.TopicAny[i] {
				out = append(out, append([]byte{byte('0' + i)}, h[:]...))
			}
			return out
		}
	}
	if q.Kind == RowTransaction {
		return [][]byte{{'x'}}
	}
	return [][]byte{{'l'}}
}
func searchKey(prefix []byte, n uint64, i uint32) []byte {
	k := append([]byte(nil), prefix...)
	var b [12]byte
	binary.BigEndian.PutUint64(b[:8], n)
	binary.BigEndian.PutUint32(b[8:], i)
	return append(k, b[:]...)
}
func searchMarker(n uint64) []byte {
	var k [9]byte
	k[0] = 'M'
	binary.BigEndian.PutUint64(k[1:], n)
	return k[:]
}
func searchOutput(tx port.CaptureReader, def string, id model.CaptureIdentity) (out SearchOutput, err error) {
	var w WorkerOutput
	if e := getJSON(tx, workerOutputKey(model.SourceSearchWorker, def, id), &w); e != nil {
		return out, errors.Join(ErrConflict, e)
	}
	if w.Identity != id || w.Definition != def || len(w.Data) > MaxWorkerOutput {
		return out, ErrConflict
	}
	dec := json.NewDecoder(bytes.NewReader(w.Data))
	dec.DisallowUnknownFields()
	if dec.Decode(&out) != nil || out.Version != 1 || out.Root == (common.Hash{}) || out.Root == types.EmptyRootHash || out.InputDigest == (common.Hash{}) || out.Transactions > maxRows || out.Logs > maxRows {
		return out, ErrConflict
	}
	return out, nil
}
func loadSearch(tx port.CaptureReader, d model.SourceSearchDescriptor, epoch uint64) (out LookupSnapshot, c WorkerCursor, err error) {
	st, e := loadState(tx)
	if e != nil {
		return out, c, e
	}
	if st.Generation != epoch {
		return out, c, ErrStale
	}
	if e = getJSON(tx, workerPrefix(model.SourceSearchWorker)+"cursor", &c); e != nil {
		return out, c, e
	}
	if !validWorkerCursor(c) || c.Name != model.SourceSearchWorker || c.Definition != d.Definition() || c.Anchor != st.Anchor {
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
	w, e := searchOutput(tx, d.Definition(), id)
	if e != nil {
		return out, c, e
	}
	if e = checkSearchRoot(tx, w.Root, id.Block); e != nil {
		return out, c, e
	}
	out.Root, out.Identity = w.Root, id
	return out, c, nil
}
func checkSearchRoot(tx port.CaptureReader, root common.Hash, b model.FeedBlock) error {
	t, e := immutableTrie(tx, root, "search/v1")
	if e != nil {
		return e
	}
	value, e := t.Get(searchMarker(b.Number))
	if e != nil {
		return errors.Join(ErrConflict, e)
	}
	if !bytes.Equal(value, b.Hash[:]) {
		return ErrConflict
	}
	return nil
}
func (s *Store) CommitSearch(ctx context.Context, d model.SourceSearchDescriptor, epoch uint64, id model.CaptureIdentity, data SearchData) error {
	if d.Validate() != nil || id.Block.Chain != s.chain || len(data.Transactions) > maxRows || len(data.Logs) > maxRows {
		return port.ErrInvalidData
	}
	seen := map[common.Hash]bool{}
	for _, tx := range data.Transactions {
		if tx.Hash == (common.Hash{}) || seen[tx.Hash] {
			return port.ErrInvalidData
		}
		seen[tx.Hash] = true
	}
	for _, log := range data.Logs {
		if len(log.Topics) > 4 || int(log.Transaction) >= len(data.Transactions) {
			return port.ErrInvalidData
		}
	}
	if len(data.Transactions) == 0 {
		data.Transactions = nil
	}
	if len(data.Logs) == 0 {
		data.Logs = nil
	}
	encoded, _ := json.Marshal(struct {
		ID   model.CaptureIdentity
		Data SearchData
	}{id, data})
	digest := common.Hash(sha256.Sum256(encoded))
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		if e := checkProjection(tx, epoch, id); e != nil {
			return e
		}
		snap, c, e := loadSearch(tx, d, epoch)
		if e != nil {
			return e
		}
		if c.Through == id.Block {
			out, e := searchOutput(tx, d.Definition(), id)
			if e != nil {
				return e
			}
			if out.InputDigest != digest || out.Transactions != uint32(len(data.Transactions)) || out.Logs != uint32(len(data.Logs)) {
				return ErrConflict
			}
			raw, _ := json.Marshal(out)
			return s.commitWorker(ctx, tx, model.SourceSearchWorker, d.Definition(), epoch, id, raw)
		}
		if c.Through.Number == math.MaxUint64 || id.Block.Number != c.Through.Number+1 {
			return ErrGap
		}
		t, e := immutableTrie(tx, snap.Root, "search/v1")
		if e != nil {
			return e
		}
		if e = t.Update(searchMarker(id.Block.Number), id.Block.Hash[:]); e != nil {
			return e
		}
		add := func(prefix []byte, i uint32) error {
			if e := ctx.Err(); e != nil {
				return e
			}
			key := searchKey(prefix, id.Block.Number, i)
			value, _ := rlp.EncodeToBytes(LookupLocation{id.Block.Number, id.Block.Hash, i})
			return t.Update(key, value)
		}
		for i, v := range data.Transactions {
			if e = add([]byte{'x'}, uint32(i)); e != nil {
				return e
			}
			if e = add(append([]byte{'p'}, v.From[:]...), uint32(i)); e != nil {
				return e
			}
			if v.To != nil && *v.To != v.From {
				if e = add(append([]byte{'p'}, v.To[:]...), uint32(i)); e != nil {
					return e
				}
			}
		}
		for i, v := range data.Logs {
			if e = add([]byte{'l'}, uint32(i)); e != nil {
				return e
			}
			if e = add(append([]byte{'a'}, v.Address[:]...), uint32(i)); e != nil {
				return e
			}
			for pos, topic := range v.Topics {
				if e = add(append([]byte{byte('0' + pos)}, topic[:]...), uint32(i)); e != nil {
					return e
				}
			}
		}
		root, nodes := t.Commit(false)
		if e = storeImmutableNodes(tx, "search/v1", nodes); e != nil {
			return e
		}
		out := SearchOutput{1, root, digest, uint32(len(data.Transactions)), uint32(len(data.Logs))}
		raw, _ := json.Marshal(out)
		return s.commitWorker(ctx, tx, model.SourceSearchWorker, d.Definition(), epoch, id, raw)
	})
}
func (s *Store) SearchSnapshot(ctx context.Context, d model.SourceSearchDescriptor, epoch uint64) (out LookupSnapshot, err error) {
	if d.Validate() != nil || epoch == 0 {
		return out, port.ErrInvalidData
	}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		tx = newImmutableReads(ctx, tx, false)
		var e error
		out, _, e = loadSearch(tx, d, epoch)
		return e
	})
	return
}

// ScanSearch merges authenticated posting lists in (height,ordinal) order.
// Each page is a bounded set of candidates, not a claim of full-filter matches.
func (s *Store) ScanSearch(ctx context.Context, d model.SourceSearchDescriptor, epoch uint64, q SearchFilter, after *SearchPosition, limit int) (out SearchCandidatePage, err error) {
	q, err = q.Normalize()
	if d.Validate() != nil || epoch == 0 || err != nil || limit < 1 || limit > MaxSearchCandidates || after != nil && (after.Height < q.From || after.Height > q.To || after.Index >= maxRows) {
		return out, port.ErrInvalidData
	}
	out.Candidates = []SearchHit{}
	err = s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		tx = newImmutableReads(ctx, tx, true)
		snap, _, e := loadSearch(tx, d, epoch)
		if e != nil {
			return e
		}
		out.Snapshot = snap
		if q.From <= snap.Anchor.Number {
			return port.ErrInvalidData
		}
		if q.To > snap.Through.Number {
			return port.ErrNotFound
		}
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		id, e := canonicalAtHeight(tx, st, q.To)
		if e != nil {
			return errors.Join(ErrConflict, e)
		}
		w, e := searchOutput(tx, d.Definition(), id)
		if e != nil {
			return e
		}
		if e = checkSearchRoot(tx, w.Root, id.Block); e != nil {
			return e
		}
		out.Scope = SearchScope{epoch, id.Block.Hash, w.Root, q.Digest(), d.Definition()}
		t, e := immutableTrie(tx, w.Root, "search/v1")
		if e != nil {
			return e
		}
		out.Candidates, e = mergeSearchLists(ctx, tx, st, t, q, after, limit)
		if e != nil {
			return e
		}
		// A full candidate page may need an empty final page; no extra speculative
		// node read can hide corruption or consume an unreturned candidate.
		out.Done = len(out.Candidates) < limit
		return nil
	})
	return
}
func (r *Reader) SearchSnapshot(ctx context.Context, d model.SourceSearchDescriptor, epoch uint64) (LookupSnapshot, error) {
	return r.store.SearchSnapshot(ctx, d, epoch)
}
func (r *Reader) ScanSearch(ctx context.Context, d model.SourceSearchDescriptor, epoch uint64, q SearchFilter, after *SearchPosition, limit int) (SearchCandidatePage, error) {
	return r.store.ScanSearch(ctx, d, epoch, q, after, limit)
}
