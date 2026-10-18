// Package capture commits verified source inputs separately from projections.
// It does not decode peer data or infer state completeness from block bodies.
package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime/trace"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
)

var (
	ErrStale         = errors.New("capture: stale generation")
	ErrConflict      = errors.New("capture: conflicting immutable facts")
	ErrGap           = errors.New("capture: noncontiguous block or wrong parent")
	ErrCanonical     = errors.New("capture: fresh canonical hash mismatch")
	ErrDeletePending = errors.New("capture: durable deletion fence is pending")
)

const maxBlockBytes = 64 << 20

// Payload is an exact, verified, encoded dataset. Persisted/verified_empty
// evidence requires one payload (including a zero-length proven empty value).
// not_applicable requires no payload. Larger values use Input.Objects.
type Payload struct {
	Kind  model.DatasetKind
	Bytes []byte
}
type Input struct {
	Manifest   model.CaptureManifest `json:"manifest"`
	ParentHash common.Hash           `json:"parentHash"`
	Payloads   []Payload             `json:"-"`
	Objects    []ObjectPayload       `json:"objects,omitempty"`
	Normalized *RowManifest          `json:"normalized,omitempty"`
	Rows       []Row                 `json:"-"`
}

type State struct {
	Generation         uint64                            `json:"generation"`
	Plan               model.CapturePlan                 `json:"plan"`
	Anchor             model.FeedBlock                   `json:"anchor"`  // explicit bootstrap checkpoint; not captured facts
	Through            model.FeedBlock                   `json:"through"` // contiguous since Anchor, never max(observed height)
	Finalized          *model.FeedBlock                  `json:"finalized,omitempty"`
	FinalityPolicy     string                            `json:"finalityPolicy,omitempty"`
	RawConsumers       []string                          `json:"rawConsumers"`
	Sequence           uint64                            `json:"sequence"`
	PendingDelete      common.Hash                       `json:"pendingDelete"`
	RequiredSupplement *model.SupplementStreamDescriptor `json:"requiredSupplement,omitempty"`
}

// Event is the internal durable facts stream, independent of projection
// outbox entries/CommitAcknowledger. A reset invalidates the earlier generation.
type Event struct {
	Sequence uint64                `json:"sequence"`
	Kind     string                `json:"kind"`
	Identity model.CaptureIdentity `json:"identity"`
}

type Store struct {
	db               port.CaptureDatabase
	chain            model.FeedChain
	canonical        source.CanonicalReader
	objects          *window.Objects
	verifySupplement SupplementVerifier
}

// New requires a live, uncached canonical reader. The RPC composition must
// retain the existing per-chain shared 5 HTTP/s pool; no new client is made.
func New(db port.CaptureDatabase, chain model.FeedChain, fresh source.CanonicalReader) (*Store, error) {
	if err := chain.Validate(); err != nil {
		return nil, err
	}
	if db == nil || fresh == nil {
		return nil, port.ErrInvalidData
	}
	return &Store{db: db, chain: chain, canonical: fresh}, nil
}

func (s *Store) fresh(ctx context.Context, b model.FeedBlock) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if b.Chain != s.chain {
		return port.ErrInvalidData
	}
	h, err := s.canonical.HashAt(ctx, b.Number)
	if err != nil {
		return err
	}
	if h != b.Hash {
		return ErrCanonical
	}
	return nil
}

// Initialize is explicit and idempotent for exactly the same plan, bootstrap
// checkpoint and raw-only consumer set. It never claims to have captured the
// checkpoint's datasets. Changing these requires a separate migration.
func (s *Store) Initialize(ctx context.Context, plan model.CapturePlan, anchor model.FeedBlock, rawConsumers []string) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	consumers := append([]string{}, rawConsumers...)
	sort.Strings(consumers)
	if len(consumers) > 64 {
		return port.ErrInvalidData
	}
	for i, c := range consumers {
		if len(c) == 0 || len(c) > 128 || (i > 0 && consumers[i-1] == c) {
			return port.ErrInvalidData
		}
	}
	if err := s.fresh(ctx, model.FeedBlock{Chain: s.chain, Hash: s.chain.GenesisHash}); err != nil {
		return err
	}
	if err := s.fresh(ctx, anchor); err != nil {
		return err
	}
	return s.db.CaptureUpdate(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err == nil {
			a, _ := plan.Digest()
			b, _ := st.Plan.Digest()
			if a != b || st.Anchor != anchor || !equalJSON(st.RawConsumers, consumers) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}
		return putJSON(tx, "state", State{Generation: 1, Plan: plan, Anchor: anchor, Through: anchor, RawConsumers: consumers})
	})
}

func (s *Store) State(ctx context.Context) (State, error) {
	var st State
	// One coherent state read. In a projection transaction only generation
	// must stay pinned; append-only Through may advance after this callback.
	err := s.projectionView(ctx, s.chain, func(tx port.CaptureReader) error { var err error; st, err = loadState(tx); return err })
	return st, err
}

// Commit atomically stores every required dataset, its manifest, canonical
// pointer, contiguous cursor and FactsReady event. No ACK precedes success.
// The supplied evidence must already have been verified by its chain adapter;
// this layer verifies identity, completeness and exact payload digests only.
func (s *Store) Commit(ctx context.Context, in Input) error {
	region := trace.StartRegion(ctx, "feed.capture")
	defer region.End()
	if trace.IsEnabled() {
		trace.Logf(ctx, "feed.block", "%d", in.Manifest.Identity.Block.Number)
	}
	in.Manifest.Datasets = append([]model.DatasetEvidence(nil), in.Manifest.Datasets...)
	in.Objects = append([]ObjectPayload(nil), in.Objects...)
	sort.Slice(in.Manifest.Datasets, func(i, j int) bool { return in.Manifest.Datasets[i].Kind < in.Manifest.Datasets[j].Kind })
	sort.Slice(in.Objects, func(i, j int) bool { return in.Objects[i].Kind < in.Objects[j].Kind })
	if err := validatePayloads(in); err != nil {
		return err
	}
	if err := validateRows(in); err != nil {
		return err
	}
	update := s.db.CaptureUpdate
	if appendDB, ok := s.db.(port.CaptureAppender); ok {
		update = appendDB.CaptureAppend
	}
	return update(ctx, s.chain, func(tx port.CaptureWriter) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		id := in.Manifest.Identity
		if err := mutable(st, id.Generation); err != nil {
			return err
		}
		if id.Block.Chain != s.chain {
			return port.ErrInvalidData
		}
		if err := st.Plan.ValidateComplete(in.Manifest); err != nil {
			return err
		}
		if err := s.checkObjects(ctx, tx, in.Objects, true); err != nil {
			return err
		}
		// Check inside the generation transaction, immediately before writes.
		// A chain can still reorg after any RPC response; Reset fences work
		// and projections retain their own independent fresh check.
		if err := s.fresh(ctx, id.Block); err != nil {
			return err
		}
		key := factsKey(id)
		prev, err := tx.Get([]byte(key))
		if err == nil {
			enc, e := json.Marshal(in)
			if e != nil {
				return e
			}
			if !bytes.Equal(prev, enc) {
				return ErrConflict
			}
			for _, p := range in.Payloads {
				stored, e := tx.Get([]byte(payloadKey(id, p.Kind)))
				if e != nil {
					return e
				}
				if !bytes.Equal(stored, p.Bytes) {
					return ErrConflict
				}
			}
			return verifyRows(tx, id, in.Normalized) // identical retry emits no event
		}
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}
		if st.Through.Number == math.MaxUint64 || id.Block.Number != st.Through.Number+1 || in.ParentHash != st.Through.Hash {
			return ErrGap
		}
		for _, p := range in.Payloads {
			if err := tx.Put([]byte(payloadKey(id, p.Kind)), p.Bytes); err != nil {
				return err
			}
		}
		if err := storeRows(tx, id, in.Rows); err != nil {
			return err
		}
		if err := putJSON(tx, key, in); err != nil {
			return err
		}
		if err := putJSON(tx, heightKey(id.Block.Number), id); err != nil {
			return err
		}
		st.Through = id.Block
		return emit(tx, &st, "facts_ready", id)
	})
}

// Read returns an immutable captured input, including fork inputs retained
// after Reset. Canonicality/completeness for a projection is a separate check.
func (s *Store) Read(ctx context.Context, id model.CaptureIdentity) (Input, error) {
	var in Input
	if id.Block.Chain != s.chain {
		return in, port.ErrInvalidData
	}
	err := s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if err := getJSON(tx, factsKey(id), &in); err != nil {
			return err
		}
		if in.Manifest.Identity != id {
			return ErrConflict
		}
		if err := st.Plan.ValidateComplete(in.Manifest); err != nil {
			return err
		}
		for _, e := range in.Manifest.Datasets {
			if e.Status == model.DatasetNotApplicable {
				continue
			}
			if _, ok := objectFor(in, e.Kind); ok {
				continue
			}
			v, err := tx.Get([]byte(payloadKey(id, e.Kind)))
			if err != nil {
				return err
			}
			in.Payloads = append(in.Payloads, Payload{e.Kind, v})
		}
		if err := validatePayloads(in); err != nil {
			return err
		}
		return s.checkObjects(ctx, tx, in.Objects, false)
	})
	return in, err
}

// Events returns at most 256 contiguous internal events after a sequence.
func (s *Store) Events(ctx context.Context, after uint64, limit int) ([]Event, error) {
	if limit < 1 || limit > 256 {
		return nil, port.ErrInvalidData
	}
	out := []Event{}
	err := s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		st, err := loadState(tx)
		if err != nil {
			return err
		}
		if after > st.Sequence {
			return port.ErrInvalidData
		}
		for n := after; n < st.Sequence && len(out) < limit; {
			n++
			var e Event
			if err := getJSON(tx, eventKey(n), &e); err != nil {
				return err
			}
			if e.Sequence != n {
				return ErrConflict
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

func validatePayloads(in Input) error {
	if len(in.Payloads)+len(in.Objects) > 11 || len(in.Manifest.Datasets) > 11 {
		return port.ErrInvalidData
	}
	seen := map[model.DatasetKind]common.Hash{}
	total := 0
	for _, p := range in.Payloads {
		if _, ok := seen[p.Kind]; ok || !p.Kind.Valid() || len(p.Bytes) > 8<<20 {
			return port.ErrInvalidData
		}
		total += len(p.Bytes)
		if total > maxBlockBytes {
			return port.ErrInvalidData
		}
		seen[p.Kind] = sha256.Sum256(p.Bytes)
	}
	for _, p := range in.Objects {
		if _, ok := seen[p.Kind]; ok || !p.Kind.Valid() || p.Ref.Validate() != nil {
			return port.ErrInvalidData
		}
		seen[p.Kind] = p.Ref.Digest
	}
	for _, e := range in.Manifest.Datasets {
		if len(e.Codec) > 128 || len(e.Source) > 1024 || len(e.Reason) > 4096 {
			return port.ErrInvalidData
		}
		v, ok := seen[e.Kind]
		if e.Status == model.DatasetNotApplicable {
			if ok {
				return port.ErrInvalidData
			}
			continue
		}
		if !ok || v != e.Digest {
			return model.ErrCaptureIncomplete
		}
		delete(seen, e.Kind)
	}
	if len(seen) != 0 {
		return port.ErrInvalidData
	}
	return nil
}

func mutable(st State, g uint64) error {
	if st.Generation != g {
		return ErrStale
	}
	if st.PendingDelete != (common.Hash{}) {
		return ErrDeletePending
	}
	return nil
}
func loadState(tx port.CaptureReader) (State, error) {
	var st State
	err := getJSON(tx, "state", &st)
	return st, err
}
func getJSON(tx port.CaptureReader, k string, out any) error {
	v, err := tx.Get([]byte(k))
	if err != nil {
		return err
	}
	return json.Unmarshal(v, out)
}
func putJSON(tx port.CaptureWriter, k string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return tx.Put([]byte(k), b)
}
func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func factsKey(id model.CaptureIdentity) string {
	return fmt.Sprintf("facts/%s/%s/%020d", id.Block.Hash.Hex(), id.PlanDigest.Hex(), id.Generation)
}
func payloadKey(id model.CaptureIdentity, k model.DatasetKind) string {
	return factsKey(id) + "/" + string(k)
}
func heightKey(n uint64) string { return fmt.Sprintf("canonical/%020d", n) }
func eventKey(n uint64) string  { return fmt.Sprintf("events/%020d", n) }
func emit(tx port.CaptureWriter, st *State, kind string, id model.CaptureIdentity) error {
	if st.Sequence == math.MaxUint64 {
		return port.ErrInvalidData
	}
	st.Sequence++
	if err := putJSON(tx, eventKey(st.Sequence), Event{st.Sequence, kind, id}); err != nil {
		return err
	}
	return putJSON(tx, "state", st)
}
