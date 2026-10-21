package evmstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/rpc"
)

func (s *Supplier) capturedHeader(ctx context.Context, id model.CaptureIdentity) ([]byte, error) {
	in, e := s.store.Read(ctx, id)
	if e != nil {
		return nil, e
	}
	for _, p := range in.Payloads {
		if p.Kind == model.DatasetHeaders {
			if len(p.Bytes) > 64<<10 {
				return nil, ErrProof
			}
			return p.Bytes, nil
		}
	}
	for _, p := range in.Objects {
		if p.Kind == model.DatasetHeaders {
			if p.Ref.Size > 64<<10 {
				return nil, ErrProof
			}
			var b bytes.Buffer
			if e := s.store.CopyPayload(ctx, id, p.Kind, &b); e != nil {
				return nil, e
			}
			return b.Bytes(), nil
		}
	}
	return nil, port.ErrNotFound
}

// Verifier is pure and can run inside CaptureUpdate, including with a single
// PostgreSQL connection. Each proof authenticates its own header against the
// exact canonical block hash before verifying the selection's MPT commitments.
func Verifier(profile chains.BinaryProfile) capture.SupplementVerifier {
	return func(d model.SupplementStreamDescriptor, b model.FeedBlock, data []byte) error {
		_, e := verifiedCheckpoint(profile, d, b, data)
		return e
	}
}
func verifiedCheckpoint(profile chains.BinaryProfile, d model.SupplementStreamDescriptor, block model.FeedBlock, data []byte) ([]Account, error) {
	sel, e := selectionFor(d)
	if e != nil {
		return nil, e
	}
	h, ok := profile.(chains.HeaderCodec)
	if !ok {
		return nil, ErrProof
	}
	var bundle Bundle
	if e := json.Unmarshal(data, &bundle); e != nil {
		return nil, e
	}
	b, e := h.DecodeHeaderRLP(bundle.Header)
	if e != nil {
		return nil, e
	}
	if b.Number != block.Number || b.Hash != block.Hash {
		return nil, ErrProof
	}
	return Verify(b.StateRoot, sel, bundle)
}

type checkpointHeader json.RawMessage

func (b *checkpointHeader) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || len(raw) > 4<<20 || bytes.Equal(raw, []byte("null")) {
		return ErrProof
	}
	*b = append((*b)[:0], raw...)
	return nil
}

// AcquireCheckpoint supports both captured parents and the bootstrap anchor.
// At the anchor only its header is fetched, in the SAME hash-bound proof batch.
// No source manifest/body/state trie is invented for that bootstrap block.
func (s *Supplier) AcquireCheckpoint(ctx context.Context, epoch uint64, d model.SupplementStreamDescriptor) (capture.SupplementCheckpoint, error) {
	var out capture.SupplementCheckpoint
	sel, e := selectionFor(d)
	if e != nil || !d.ParentCheckpoint {
		return out, ErrProof
	}
	st, e := s.store.State(ctx)
	if e != nil {
		return out, e
	}
	if st.Generation != epoch {
		return out, capture.ErrStale
	}
	prior, e := s.store.ReadSupplementCheckpoint(ctx, epoch, d)
	if e == nil {
		start := time.Now()
		_, e := verifiedCheckpoint(s.profile, d, prior.Block, prior.Data)
		s.verify.record(start, e)
		feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofVerify, time.Since(start), e)
		if e != nil {
			return out, e
		}
		return prior, nil // durable recovery is DB-only, even after RPC pruning
	}
	if e != port.ErrNotFound && e != capture.ErrSupplementIncomplete {
		return out, e
	}
	var id model.CaptureIdentity
	if d.FromHeight-1 == st.Anchor.Number {
		id.Block = st.Anchor
	} else {
		id, e = s.store.CanonicalAt(ctx, epoch, d.FromHeight-1)
		if e != nil {
			return out, e
		}
	}
	var header []byte
	var raw checkpointHeader
	var extra []rpc.BatchElem
	if id.Block == st.Anchor {
		extra = []rpc.BatchElem{{Method: "eth_getBlockByHash", Args: []interface{}{id.Block.Hash, false}, Result: &raw}}
	} else {
		header, e = s.capturedHeader(ctx, id)
		if e != nil {
			return out, e
		}
	}
	bundle, e := s.requestBundle(ctx, id.Block, sel, extra...)
	if e != nil {
		now, err := s.store.State(ctx)
		if err != nil {
			return out, err
		}
		if now.Generation != epoch {
			return out, capture.ErrStale
		}
		return out, e
	}
	if len(extra) > 0 {
		h, ok := s.profile.(chains.HeaderCodec)
		if !ok {
			return out, ErrProof
		}
		header, e = h.EncodeHeaderJSON(json.RawMessage(raw))
		if e != nil {
			return out, e
		}
	}
	bundle.Header = header
	data, e := json.Marshal(bundle)
	if e != nil || len(data) > MaxBundleBytes {
		return out, ErrProof
	}
	out = capture.SupplementCheckpoint{Block: id.Block, Digest: sha256.Sum256(data), Data: data}
	start := time.Now()
	_, e = verifiedCheckpoint(s.profile, d, out.Block, data)
	s.verify.record(start, e)
	feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofVerify, time.Since(start), e)
	if e != nil {
		return out, e
	}
	start = time.Now()
	e = s.store.CommitSupplementCheckpoint(ctx, epoch, d, out)
	s.commit.record(start, e)
	feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofCommit, time.Since(start), e)
	if e != nil {
		return out, e
	}
	return out, nil
}

// TransitionState deliberately has no capture manifest identity: a bootstrap
// checkpoint authenticates state, without claiming a captured block body.
type TransitionState struct {
	Epoch     uint64          `json:"epoch"`
	Block     model.FeedBlock `json:"block"`
	Selection Selection       `json:"selection"`
	Accounts  []Account       `json:"accounts"`
}

// ReadTransition returns exact selected before/after states using DB bytes
// only. It requires the range's checkpoint and contiguous verified completion.
// It does not synthesize internal calls or all changes within the block.
func (s *Supplier) ReadTransition(ctx context.Context, epoch uint64, d model.SupplementStreamDescriptor, height uint64) (before, after TransitionState, err error) {
	if !d.ParentCheckpoint || height < d.FromHeight {
		return before, after, ErrProof
	}
	status, e := s.store.SupplementStreamStatus(ctx, d)
	if e != nil {
		return before, after, e
	}
	if status.Epoch != epoch {
		return before, after, capture.ErrStale
	}
	if status.Complete == nil || status.Complete.Height < height {
		return before, after, capture.ErrSupplementIncomplete
	}
	sel, e := selectionFor(d)
	if e != nil {
		return before, after, e
	}
	if height == d.FromHeight {
		cp, e := s.store.ReadSupplementCheckpoint(ctx, epoch, d)
		if e != nil {
			return before, after, e
		}
		accounts, e := verifiedCheckpoint(s.profile, d, cp.Block, cp.Data)
		if e != nil {
			return before, after, e
		}
		before = TransitionState{Epoch: epoch, Block: cp.Block, Selection: sel, Accounts: accounts}
	} else {
		id, e := s.store.CanonicalAt(ctx, epoch, height-1)
		if e != nil {
			return before, after, e
		}
		result, err := s.Read(ctx, epoch, id, sel)
		e = err
		if e != nil {
			return before, after, e
		}
		before = TransitionState{Epoch: epoch, Block: id.Block, Selection: result.Selection, Accounts: result.Accounts}
	}
	id, e := s.store.CanonicalAt(ctx, epoch, height)
	if e != nil {
		return before, after, e
	}
	result, e := s.Read(ctx, epoch, id, sel)
	if e != nil {
		return before, after, e
	}
	after = TransitionState{Epoch: epoch, Block: id.Block, Selection: result.Selection, Accounts: result.Accounts}
	return before, after, nil
}

// ReadTransition is also available to DB-only downstream code with no client.
func ReadTransition(ctx context.Context, store *capture.Store, profile chains.BinaryProfile, epoch uint64, d model.SupplementStreamDescriptor, height uint64) (TransitionState, TransitionState, error) {
	s := Supplier{store: store, profile: profile}
	if store == nil || profile == nil {
		return TransitionState{}, TransitionState{}, port.ErrInvalidData
	}
	return s.ReadTransition(ctx, epoch, d, height)
}
