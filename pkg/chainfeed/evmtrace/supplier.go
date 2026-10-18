package evmtrace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feedtelemetry"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

type BatchCaller interface {
	BatchCallContext(context.Context, []rpc.BatchElem) error
}
type CapturedReader interface {
	Read(context.Context, model.CaptureIdentity) (capture.Input, error)
	CopyPayload(context.Context, model.CaptureIdentity, model.DatasetKind, io.Writer) error
	ValidateProjection(context.Context, uint64, model.CaptureIdentity) error
	ReadSupplement(context.Context, uint64, model.CaptureIdentity, string, common.Hash) (capture.Supplement, error)
}
type Supplier struct {
	store   *capture.Store
	profile chains.BinaryProfile
	rpc     BatchCaller
}

func New(store *capture.Store, profile chains.BinaryProfile, client BatchCaller) (*Supplier, error) {
	if store == nil || profile == nil || client == nil {
		return nil, port.ErrInvalidData
	}
	return &Supplier{store: store, profile: profile, rpc: client}, nil
}

type Bundle struct {
	Version    uint32                `json:"version"`
	Identity   model.CaptureIdentity `json:"identity"`
	Definition Definition            `json:"definition"`
	Response   []byte                `json:"response"` // preserves exact bounded provider bytes
}
type Result struct {
	Epoch        uint64                `json:"epoch"`
	Identity     model.CaptureIdentity `json:"identity"`
	Definition   Definition            `json:"definition"`
	Verification string                `json:"verification"`
	Coverage     string                `json:"coverage"`
	Frames       []Frame               `json:"frames"`
}
type boundedResponse []byte

func (r *boundedResponse) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || len(raw) > MaxResponseBytes {
		return ErrTrace
	}
	*r = append((*r)[:0], raw...)
	return nil
}

func capturedBlock(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity) (*model.Block, []*model.Receipt, error) {
	if r == nil || p == nil {
		return nil, nil, port.ErrInvalidData
	}
	if e := r.ValidateProjection(ctx, epoch, id); e != nil {
		return nil, nil, e
	}
	in, e := r.Read(ctx, id)
	if e != nil {
		return nil, nil, e
	}
	if len(in.Manifest.Datasets) != 3 {
		return nil, nil, ErrTrace
	}
	data := map[model.DatasetKind][]byte{}
	for _, v := range in.Manifest.Datasets {
		if v.Codec != evmdata.Codec(v.Kind) || v.Verification != model.VerificationCommitment || v.Status != model.DatasetPersisted {
			return nil, nil, ErrTrace
		}
	}
	for _, v := range in.Payloads {
		data[v.Kind] = v.Bytes
	}
	for _, v := range in.Objects {
		if v.Ref.Size > 8<<20 {
			return nil, nil, ErrTrace
		}
		var b bytes.Buffer
		if e := r.CopyPayload(ctx, id, v.Kind, &b); e != nil {
			return nil, nil, e
		}
		data[v.Kind] = b.Bytes()
	}
	b, rs, e := evmdata.Decode(p, id.Block, data[model.DatasetHeaders], data[model.DatasetBodies], data[model.DatasetReceipts])
	if e != nil {
		return nil, nil, e
	}
	if b.ParentHash != in.ParentHash || len(b.Transactions) > MaxTransactions {
		return nil, nil, ErrTrace
	}
	// Pre-Byzantium post-state receipts do not authenticate a boolean status.
	// Reject rather than interpreting the model's absent status as a failure.
	receipts, e := evmdata.Items(data[model.DatasetReceipts], MaxTransactions)
	if e != nil {
		return nil, nil, e
	}
	for _, v := range receipts {
		kind, content, _, e := rlp.Split(v)
		if e != nil {
			return nil, nil, e
		}
		if kind == rlp.String {
			if len(content) < 2 {
				return nil, nil, ErrTrace
			}
			v = content[1:]
		}
		fields, e := evmdata.Items(v, 4)
		if e != nil || len(fields) != 4 {
			return nil, nil, ErrTrace
		}
		_, value, _, e := rlp.Split(fields[0])
		if e != nil || len(value) > 1 || len(value) == 1 && value[0] != 1 {
			return nil, nil, ErrTrace
		}
	}
	return b, rs, nil
}
func decodeStored(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor, raw []byte) (Result, error) {
	def, e := DefinitionFor(d)
	if e != nil {
		return Result{}, e
	}
	if p == nil || p.ID() != def.Profile {
		return Result{}, ErrTrace
	}
	if len(raw) == 0 || len(raw) > capture.MaxSupplementBytes {
		return Result{}, ErrTrace
	}
	var bundle Bundle
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&bundle); e != nil || bundle.Version != 1 || bundle.Identity != id || bundle.Definition != def {
		return Result{}, ErrTrace
	}
	if _, e = dec.Token(); e != io.EOF {
		return Result{}, ErrTrace
	}
	b, rs, e := capturedBlock(ctx, r, p, epoch, id)
	if e != nil {
		return Result{}, e
	}
	span := feedtelemetry.From(ctx).Start(feedtelemetry.TraceVerify)
	frames, e := Decode(bundle.Response, b, rs)
	span.End(e)
	if e != nil {
		return Result{}, e
	}
	if e = r.ValidateProjection(ctx, epoch, id); e != nil {
		return Result{}, e
	}
	return Result{Epoch: epoch, Identity: id, Definition: def, Verification: "provider_reported_root_consistency", Coverage: "reported_call_tree", Frames: frames}, nil
}

// ReadSnapshot is DB-only and rechecks all stored call roots, source bytes and
// epoch. A matching digest alone is not sufficient to trust stored semantics.
func ReadSnapshot(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor) (Result, error) {
	if r == nil || p == nil || id.Block.Number < d.FromHeight {
		return Result{}, port.ErrInvalidData
	}
	if _, e := DefinitionFor(d); e != nil {
		return Result{}, e
	}
	in, e := r.ReadSupplement(ctx, epoch, id, Codec, d.SelectionHash)
	if e != nil {
		return Result{}, e
	}
	return decodeStored(ctx, r, p, epoch, id, d, in.Data)
}

// Acquire uses only the caller's existing shared5 HTTP/s batch client. Hash
// selection is exact; fresh canonical remains mandatory in the source commit.
func (s *Supplier) Acquire(ctx context.Context, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor) (out Result, err error) {
	span := feedtelemetry.From(ctx).Start(feedtelemetry.TraceAcquire)
	defer func() { span.End(err) }()
	def, e := DefinitionFor(d)
	if e != nil || s.profile.ID() != def.Profile || id.Block.Number < d.FromHeight {
		return out, ErrTrace
	}
	if _, e := s.store.SupplementStreamStatus(ctx, d); e != nil {
		return out, e
	}
	prior, e := s.store.ReadSupplement(ctx, epoch, id, Codec, d.SelectionHash)
	if e == nil {
		out, e = decodeStored(ctx, s.store, s.profile, epoch, id, d, prior.Data)
		if e != nil {
			return out, e
		}
		e = s.commit(ctx, epoch, d, prior)
		if e != nil {
			return Result{}, e
		}
		return out, nil
	}
	if e != port.ErrNotFound {
		return out, e
	}
	b, rs, e := capturedBlock(ctx, s.store, s.profile, epoch, id)
	if e != nil {
		return out, e
	}
	var response boundedResponse
	opts := struct {
		Tracer       string `json:"tracer"`
		Timeout      string `json:"timeout"`
		Reexec       uint64 `json:"reexec"`
		TracerConfig struct {
			OnlyTopCall bool `json:"onlyTopCall"`
			WithLog     bool `json:"withLog"`
		} `json:"tracerConfig"`
	}{Tracer: "callTracer", Timeout: "5s"}
	batch := []rpc.BatchElem{{Method: "debug_traceBlockByHash", Args: []interface{}{id.Block.Hash, opts}, Result: &response}}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	call := feedtelemetry.From(ctx).Start(feedtelemetry.TraceBatch)
	e = s.rpc.BatchCallContext(requestCtx, batch)
	cancel()
	if e == nil {
		e = batch[0].Error
	}
	call.End(e)
	if e != nil {
		if ctx.Err() != nil {
			return out, errors.Join(fmt.Errorf("%w: %w", ErrUnavailable, e), ctx.Err())
		}
		if stale := s.store.ValidateProjection(ctx, epoch, id); stale == capture.ErrStale {
			return out, stale
		} else if stale != nil {
			return out, errors.Join(e, stale)
		}
		return out, fmt.Errorf("%w: %w", ErrUnavailable, e)
	}
	verify := feedtelemetry.From(ctx).Start(feedtelemetry.TraceVerify)
	frames, e := Decode(response, b, rs)
	verify.End(e)
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(Bundle{Version: 1, Identity: id, Definition: def, Response: response})
	if e != nil {
		return out, e
	}
	if len(raw) > capture.MaxSupplementBytes {
		return out, ErrTrace
	}
	in := capture.Supplement{Identity: id, Codec: Codec, Selection: d.SelectionHash, Digest: sha256.Sum256(raw), Data: raw}
	if e = s.commit(ctx, epoch, d, in); e != nil {
		return out, e
	}
	if e = s.store.ValidateProjection(ctx, epoch, id); e != nil {
		return out, e
	}
	return Result{Epoch: epoch, Identity: id, Definition: def, Verification: "provider_reported_root_consistency", Coverage: "reported_call_tree", Frames: frames}, nil
}
func (s *Supplier) commit(ctx context.Context, epoch uint64, d model.SupplementStreamDescriptor, in capture.Supplement) error {
	span := feedtelemetry.From(ctx).Start(feedtelemetry.TraceCommit)
	e := s.store.CommitStreamSupplement(ctx, epoch, d, in)
	span.End(e)
	return e
}

type Page struct {
	Selection    common.Hash           `json:"selection"`
	Epoch        uint64                `json:"epoch"`
	Identity     model.CaptureIdentity `json:"identity"`
	Definition   Definition            `json:"definition"`
	Verification string                `json:"verification"`
	Coverage     string                `json:"coverage"`
	Total        int                   `json:"total"`
	Frames       []Frame               `json:"frames"`
	Next         *uint32               `json:"next"`
}

func ReadPage(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor, after uint32, limit int) (Page, error) {
	var page Page
	if limit < 1 || limit > 256 {
		return page, port.ErrInvalidData
	}
	out, e := ReadSnapshot(ctx, r, p, epoch, id, d)
	if e != nil {
		return page, e
	}
	if uint64(after) > uint64(len(out.Frames)) {
		return page, port.ErrInvalidData
	}
	end := min(uint64(after)+uint64(limit), uint64(len(out.Frames)))
	page = Page{Selection: d.SelectionHash, Epoch: out.Epoch, Identity: out.Identity, Definition: out.Definition, Verification: out.Verification, Coverage: out.Coverage, Total: len(out.Frames), Frames: append([]Frame{}, out.Frames[int(after):int(end)]...)}
	if end < uint64(len(out.Frames)) {
		v := uint32(end)
		page.Next = &v
	}
	return page, nil
}
