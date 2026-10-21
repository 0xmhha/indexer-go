package evmstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

var ErrUnavailable = errors.New("evmstate: hash-bound state supply unavailable")

// BatchCaller is borrowed from the chain's EXISTING shared 5 HTTP/s client.
// There is no dialer, alternate endpoint, unbounded queue or latest fallback.
type BatchCaller interface {
	BatchCallContext(context.Context, []rpc.BatchElem) error
}

type Supplier struct {
	store                    *capture.Store
	profile                  chains.BinaryProfile
	rpc                      BatchCaller
	rpcBatch, verify, commit stageCounter
}

func New(store *capture.Store, profile chains.BinaryProfile, client BatchCaller) (*Supplier, error) {
	if store == nil || profile == nil || client == nil {
		return nil, port.ErrInvalidData
	}
	return &Supplier{store: store.WithSupplementVerifier(Verifier(profile)), profile: profile, rpc: client}, nil
}

// Result contains state at the exact END of this block for the exact tracked
// selection. It is neither a list of all changes nor an EVM execution witness.
type Result struct {
	Epoch     uint64                `json:"epoch"`
	Identity  model.CaptureIdentity `json:"identity"`
	Selection Selection             `json:"selection"`
	Accounts  []Account             `json:"accounts"`
}

func (s *Supplier) block(ctx context.Context, epoch uint64, id model.CaptureIdentity) (*model.Block, error) {
	if e := s.store.ValidateProjection(ctx, epoch, id); e != nil {
		return nil, e
	}
	in, e := s.store.Read(ctx, id)
	if e != nil {
		return nil, e
	}
	data := map[model.DatasetKind][]byte{}
	if len(in.Manifest.Datasets) != 3 {
		return nil, ErrProof
	}
	for _, d := range in.Manifest.Datasets {
		if d.Codec != evmdata.Codec(d.Kind) || d.Verification != model.VerificationCommitment || d.Status != model.DatasetPersisted {
			return nil, ErrProof
		}
	}
	for _, p := range in.Payloads {
		data[p.Kind] = p.Bytes
	}
	for _, p := range in.Objects {
		if p.Ref.Size > 8<<20 {
			return nil, ErrProof
		}
		var b bytes.Buffer
		if e := s.store.CopyPayload(ctx, id, p.Kind, &b); e != nil {
			return nil, e
		}
		data[p.Kind] = b.Bytes()
	}
	b, _, e := evmdata.Decode(s.profile, id.Block, data[model.DatasetHeaders], data[model.DatasetBodies], data[model.DatasetReceipts])
	if e != nil {
		return nil, e
	}
	if b.ParentHash != in.ParentHash {
		return nil, capture.ErrConflict
	}
	return b, nil
}

type boundedCode hexutil.Bytes

func (b *boundedCode) UnmarshalJSON(raw []byte) error {
	if len(raw) < 4 || len(raw) > MaxCodeBytes*2+4 {
		return ErrProof
	}
	var value hexutil.Bytes
	if e := json.Unmarshal(raw, &value); e != nil || len(value) > MaxCodeBytes {
		return ErrProof
	}
	*b = boundedCode(value)
	return nil
}

// Acquire fetches every selected proof/code in one HTTP batch using an
// EIP-1898 blockHash + requireCanonical selector. Every member must succeed.
// Verification precedes the durable source transaction and its fresh check.
// An already durable retry uses the DB bytes and still performs that check.
func (s *Supplier) Acquire(ctx context.Context, epoch uint64, id model.CaptureIdentity, selection Selection) (Result, error) {
	return s.acquire(ctx, epoch, id, selection, nil)
}

func (s *Supplier) AcquireStream(ctx context.Context, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor) (Result, error) {
	sel, e := selectionFor(d)
	if e != nil {
		return Result{}, e
	}
	r := feedtelemetry.From(ctx)
	started := time.Now()
	span := r.Start(feedtelemetry.ProofAcquire)
	out, err := s.acquire(ctx, epoch, id, sel, &d)
	span.End(err)
	if err == nil {
		r.QueueAge(feedtelemetry.Captured, feedtelemetry.ProofQueue, id, started)
		r.Complete(feedtelemetry.Proved, id)
	}
	return out, err
}

func (s *Supplier) acquire(ctx context.Context, epoch uint64, id model.CaptureIdentity, selection Selection, stream *model.SupplementStreamDescriptor) (Result, error) {
	selected, digest, e := selection.Normalize()
	if e != nil {
		return Result{}, e
	}
	b, e := s.block(ctx, epoch, id)
	if e != nil {
		return Result{}, e
	}
	prior, e := s.store.ReadSupplement(ctx, epoch, id, Codec, digest)
	if e == nil {
		start := time.Now()
		out, e := verifiedResult(epoch, id, b, selected, prior.Data)
		s.verify.record(start, e)
		feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofVerify, time.Since(start), e)
		if e != nil {
			return Result{}, e
		}
		if e = s.commitSupplement(ctx, epoch, prior, stream); e != nil {
			return Result{}, e
		}
		if e = s.store.ValidateProjection(ctx, epoch, id); e != nil {
			return Result{}, e
		}
		return out, nil
	}
	if e != port.ErrNotFound {
		return Result{}, e
	}
	if s.rpc == nil {
		return Result{}, ErrUnavailable
	}
	bundle, e := s.requestBundle(ctx, id.Block, selected)
	if e != nil {
		if stale := s.store.ValidateProjection(ctx, epoch, id); stale != nil {
			return Result{}, stale
		}
		return Result{}, e
	}
	if stream != nil && stream.ParentCheckpoint {
		bundle.Header, e = s.capturedHeader(ctx, id)
		if e != nil {
			return Result{}, e
		}
	}
	start := time.Now()
	accounts, e := Verify(b.StateRoot, selected, bundle)
	s.verify.record(start, e)
	feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofVerify, time.Since(start), e)
	if e != nil {
		return Result{}, e
	}
	data, e := json.Marshal(bundle)
	if e != nil {
		return Result{}, e
	}
	if len(data) > MaxBundleBytes {
		return Result{}, ErrProof
	}
	supp := capture.Supplement{Identity: id, Codec: Codec, Selection: digest, Digest: sha256.Sum256(data), Data: data}
	if e = s.commitSupplement(ctx, epoch, supp, stream); e != nil {
		return Result{}, e
	}
	if e := s.store.ValidateProjection(ctx, epoch, id); e != nil {
		return Result{}, e
	}
	return Result{Epoch: epoch, Identity: id, Selection: selected, Accounts: accounts}, nil
}

func (s *Supplier) commitSupplement(ctx context.Context, epoch uint64, in capture.Supplement, stream *model.SupplementStreamDescriptor) error {
	start := time.Now()
	var e error
	if stream == nil {
		e = s.store.CommitSupplement(ctx, epoch, in)
	} else {
		e = s.store.CommitStreamSupplement(ctx, epoch, *stream, in)
	}
	s.commit.record(start, e)
	feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofCommit, time.Since(start), e)
	return e
}

func verifiedResult(epoch uint64, id model.CaptureIdentity, block *model.Block, selected Selection, raw []byte) (Result, error) {
	if len(raw) == 0 || len(raw) > MaxBundleBytes {
		return Result{}, ErrProof
	}
	var bundle Bundle
	if e := json.Unmarshal(raw, &bundle); e != nil {
		return Result{}, e
	}
	accounts, e := Verify(block.StateRoot, selected, bundle)
	if e != nil {
		return Result{}, e
	}
	return Result{Epoch: epoch, Identity: id, Selection: selected, Accounts: accounts}, nil
}

// Read performs no RPC, re-verifies stored proof/code against the captured
// profile-verified header, and fences the epoch again after decoding. Missing
// addresses/slots or an uncaptured block remain errors, never zero values.
func (s *Supplier) Read(ctx context.Context, epoch uint64, id model.CaptureIdentity, selection Selection) (Result, error) {
	selected, digest, e := selection.Normalize()
	if e != nil {
		return Result{}, e
	}
	b, e := s.block(ctx, epoch, id)
	if e != nil {
		return Result{}, e
	}
	in, e := s.store.ReadSupplement(ctx, epoch, id, Codec, digest)
	if e != nil {
		return Result{}, e
	}
	start := time.Now()
	out, e := verifiedResult(epoch, id, b, selected, in.Data)
	s.verify.record(start, e)
	feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofVerify, time.Since(start), e)
	if e != nil {
		return Result{}, e
	}
	if e = s.store.ValidateProjection(ctx, epoch, id); e != nil {
		return Result{}, e
	}
	return out, nil
}

// ReadSnapshot is the read-only composition for downstream workers/API code;
// no RPC dependency, client creation or fresh network call is possible here.
func ReadSnapshot(ctx context.Context, store *capture.Store, profile chains.BinaryProfile, epoch uint64, id model.CaptureIdentity, selection Selection) (Result, error) {
	if store == nil || profile == nil {
		return Result{}, port.ErrInvalidData
	}
	s := Supplier{store: store, profile: profile}
	return s.Read(ctx, epoch, id, selection)
}

func (s *Supplier) requestBundle(ctx context.Context, block model.FeedBlock, selected Selection, extra ...rpc.BatchElem) (Bundle, error) {
	if s.rpc == nil {
		return Bundle{}, ErrUnavailable
	}
	bundle := Bundle{Version: 1, Selection: selected, Evidence: make([]Evidence, len(selected.Targets))}
	codes := make([]boundedCode, len(selected.Targets))
	batch := append(make([]rpc.BatchElem, 0, len(extra)+2*len(selected.Targets)), extra...)
	selector := rpc.BlockNumberOrHashWithHash(block.Hash, true)
	for i, t := range selected.Targets {
		keys := make([]string, len(t.Slots))
		for j, k := range t.Slots {
			keys[j] = k.Hex()
		}
		batch = append(batch, rpc.BatchElem{Method: "eth_getProof", Args: []interface{}{t.Address, keys, selector}, Result: &bundle.Evidence[i].Proof})
		if t.Code {
			batch = append(batch, rpc.BatchElem{Method: "eth_getCode", Args: []interface{}{t.Address, selector}, Result: &codes[i]})
		}
	}
	start := time.Now()
	e := s.rpc.BatchCallContext(ctx, batch)
	if e == nil {
		for _, member := range batch {
			if member.Error != nil {
				e = member.Error
				break
			}
		}
	}
	s.rpcBatch.record(start, e)
	feedtelemetry.From(ctx).RecordResult(feedtelemetry.ProofBatch, time.Since(start), e)
	if e != nil {
		return Bundle{}, fmt.Errorf("%w: %w", ErrUnavailable, e)
	}
	for i, t := range selected.Targets {
		if t.Code {
			code := hexutil.Bytes(codes[i])
			bundle.Evidence[i].Code = &code
		}
	}

	return bundle, nil
}
