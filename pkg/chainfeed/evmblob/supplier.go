package evmblob

import (
	"bytes"
	"context"
	"crypto/sha256"
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
)

type CapturedReader interface {
	Read(context.Context, model.CaptureIdentity) (capture.Input, error)
	CopyPayload(context.Context, model.CaptureIdentity, model.DatasetKind, io.Writer) error
	ValidateProjection(context.Context, uint64, model.CaptureIdentity) error
	ReadSupplement(context.Context, uint64, model.CaptureIdentity, string, common.Hash) (capture.Supplement, error)
}

// Provider owns a chain-specific relay/beacon/peer transport. It MUST honor
// ctx; Close on the returned reader MUST be safe concurrently with Read and
// unblock it. An HTTP adapter must use the chain's existing shared 5 HTTP/s
// pool, including retries. No default endpoint or transport is installed here.
type Provider interface {
	Open(context.Context, model.FeedBlock, []Reference) (io.ReadCloser, error)
}
type Supplier struct {
	store    *capture.Store
	profile  chains.BinaryProfile
	provider Provider
}

func New(store *capture.Store, p chains.BinaryProfile, provider Provider) (*Supplier, error) {
	if store == nil || p == nil || provider == nil {
		return nil, port.ErrInvalidData
	}
	if e := checkProfile(p); e != nil {
		return nil, e
	}
	return &Supplier{store: store, profile: p, provider: provider}, nil
}

var ErrUnsupported = errors.New("profile has no supported full blob source")

func checkProfile(p chains.BinaryProfile) error {
	capability, e := chains.BlobCapabilityOf(p)
	if e != nil {
		return e
	}
	if capability.Status == model.BlobUnsupported {
		return fmt.Errorf("%w: %s", ErrUnsupported, capability.Reason)
	}
	return nil
}

type storedBundle struct {
	Version    uint64
	Identity   model.CaptureIdentity
	Definition Definition
	Response   []byte
}
type Result struct {
	Epoch        uint64                `json:"epoch"`
	Identity     model.CaptureIdentity `json:"identity"`
	Definition   Definition            `json:"definition"`
	Verification string                `json:"verification"`
	Coverage     string                `json:"coverage"`
	References   []Reference           `json:"references"`
	Response     Response              `json:"-"`
}

func capturedReferences(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity) ([]Reference, error) {
	if e := r.ValidateProjection(ctx, epoch, id); e != nil {
		return nil, e
	}
	in, e := r.Read(ctx, id)
	if e != nil {
		return nil, e
	}
	if len(in.Manifest.Datasets) != 3 {
		return nil, ErrBlob
	}
	data := map[model.DatasetKind][]byte{}
	for _, v := range in.Manifest.Datasets {
		if v.Codec != evmdata.Codec(v.Kind) || v.Verification != model.VerificationCommitment || v.Status != model.DatasetPersisted || v.Coverage.Scope != model.CoverageComplete {
			return nil, ErrBlob
		}
	}
	for _, v := range in.Payloads {
		data[v.Kind] = v.Bytes
	}
	for _, v := range in.Objects {
		if v.Ref.Size > 8<<20 {
			return nil, ErrBlob
		}
		var b bytes.Buffer
		if e := r.CopyPayload(ctx, id, v.Kind, &b); e != nil {
			return nil, e
		}
		data[v.Kind] = b.Bytes()
	}
	b, _, e := evmdata.Decode(p, id.Block, data[model.DatasetHeaders], data[model.DatasetBodies], data[model.DatasetReceipts])
	if e != nil {
		return nil, e
	}
	if b.ParentHash != in.ParentHash {
		return nil, ErrBlob
	}
	return References(b)
}
func decodeStored(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor, raw []byte) (Result, error) {
	var out Result
	def, e := DefinitionFor(d)
	if e != nil {
		return out, e
	}
	if p == nil || p.ID() != def.Profile || len(raw) == 0 || len(raw) > capture.MaxSupplementBytes {
		return out, ErrBlob
	}
	// Bound the outer list before decoding the nested byte string.
	f, e := splitList(raw, 4)
	if e != nil || len(f) != 4 {
		return out, ErrBlob
	}
	var b storedBundle
	if e = rlp.DecodeBytes(raw, &b); e != nil || b.Version != 1 || b.Identity != id || b.Definition != def {
		return out, ErrBlob
	}
	response, e := Decode(b.Response)
	if e != nil {
		return out, e
	}
	refs, e := capturedReferences(ctx, r, p, epoch, id)
	if e != nil {
		return out, e
	}
	span := feedtelemetry.From(ctx).Start(feedtelemetry.BlobVerify)
	e = Verify(ctx, response, id.Block, refs)
	span.End(e)
	if e != nil {
		return out, e
	}
	if e = r.ValidateProjection(ctx, epoch, id); e != nil {
		return out, e
	}
	return result(epoch, id, def, refs, response), nil
}
func result(epoch uint64, id model.CaptureIdentity, def Definition, refs []Reference, response Response) Result {
	return Result{Epoch: epoch, Identity: id, Definition: def, Verification: "kzg_full_blob_and_captured_transaction", Coverage: "all_eip4844_blob_references", References: refs, Response: response}
}
func ReadSnapshot(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor) (Result, error) {
	if r == nil || p == nil || id.Block.Number < d.FromHeight {
		return Result{}, port.ErrInvalidData
	}
	if e := checkProfile(p); e != nil {
		return Result{}, e
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

// Acquire verifies a whole block before atomically recording input, receipt,
// cursor and event. Retry revalidates stored bytes without opening the provider.
// Both paths still perform the Store's uncached fresh canonical check at commit.
func (s *Supplier) Acquire(ctx context.Context, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor) (out Result, err error) {
	span := feedtelemetry.From(ctx).Start(feedtelemetry.BlobAcquire)
	defer func() { span.End(err) }()
	def, e := DefinitionFor(d)
	if e != nil || def.Profile != s.profile.ID() || id.Block.Number < d.FromHeight {
		return out, ErrBlob
	}
	if _, e = s.store.SupplementStreamStatus(ctx, d); e != nil {
		return out, e
	}
	prior, e := s.store.ReadSupplement(ctx, epoch, id, Codec, d.SelectionHash)
	if e == nil {
		out, e = decodeStored(ctx, s.store, s.profile, epoch, id, d, prior.Data)
		if e != nil {
			return out, e
		}
		if e = s.commit(ctx, epoch, d, prior); e != nil {
			return Result{}, e
		}
		return out, nil
	}
	if !errors.Is(e, port.ErrNotFound) {
		return out, e
	}
	refs, e := capturedReferences(ctx, s.store, s.profile, epoch, id)
	if e != nil {
		return out, e
	}
	var raw []byte
	if len(refs) == 0 {
		// Proven absence in the captured body is distinct from an unavailable
		// provider. No network call is needed for a block without type-3 blobs.
		raw, e = rlp.EncodeToBytes(Response{Version: 1, Block: id.Block, Entries: []Entry{}})
	} else {
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		readSpan := feedtelemetry.From(ctx).Start(feedtelemetry.BlobSupply)
		raw, e = readProvider(requestCtx, s.provider, id.Block, refs)
		readSpan.End(e)
		cancel()
	}
	if e != nil {
		if ctx.Err() != nil {
			return out, errors.Join(ErrUnavailable, e, ctx.Err())
		}
		if stale := s.store.ValidateProjection(ctx, epoch, id); stale != nil {
			return out, errors.Join(e, stale)
		}
		return out, errors.Join(ErrUnavailable, e)
	}
	response, e := Decode(raw)
	if e != nil {
		return out, e
	}
	verify := feedtelemetry.From(ctx).Start(feedtelemetry.BlobVerify)
	e = Verify(ctx, response, id.Block, refs)
	verify.End(e)
	if e != nil {
		return out, e
	}
	data, e := rlp.EncodeToBytes(storedBundle{Version: 1, Identity: id, Definition: def, Response: raw})
	if e != nil {
		return out, e
	}
	if len(data) > capture.MaxSupplementBytes {
		return out, ErrBlob
	}
	in := capture.Supplement{Identity: id, Codec: Codec, Selection: d.SelectionHash, Digest: sha256.Sum256(data), Data: data}
	if e = s.commit(ctx, epoch, d, in); e != nil {
		return out, e
	}
	if e = s.store.ValidateProjection(ctx, epoch, id); e != nil {
		return out, e
	}
	return result(epoch, id, def, refs, response), nil
}
func (s *Supplier) commit(ctx context.Context, epoch uint64, d model.SupplementStreamDescriptor, in capture.Supplement) error {
	span := feedtelemetry.From(ctx).Start(feedtelemetry.BlobCommit)
	e := s.store.CommitStreamSupplement(ctx, epoch, d, in)
	span.End(e)
	return e
}

func readProvider(ctx context.Context, p Provider, block model.FeedBlock, refs []Reference) ([]byte, error) {
	body, e := p.Open(ctx, block, append([]Reference(nil), refs...))
	if e != nil {
		if body != nil {
			e = errors.Join(e, body.Close())
		}
		return nil, e
	}
	if body == nil {
		return nil, ErrUnavailable
	}
	// Cancellation closes and joins the callback before the normal Close.
	done := make(chan struct{})
	var closeErr error
	stop := context.AfterFunc(ctx, func() { closeErr = body.Close(); close(done) })
	raw, e := io.ReadAll(io.LimitReader(body, MaxResponseBytes+1))
	if !stop() {
		<-done
		e = errors.Join(e, closeErr)
	} else {
		e = errors.Join(e, body.Close())
	}
	if len(raw) > MaxResponseBytes {
		e = errors.Join(e, ErrBlob)
	}
	if ctx.Err() != nil {
		e = errors.Join(e, ctx.Err())
	}
	return raw, e
}

type Blob struct {
	Reference Reference `json:"reference"`
	Data      Entry     `json:"data"`
}
type Page struct {
	Epoch        uint64                `json:"epoch"`
	Identity     model.CaptureIdentity `json:"identity"`
	Selection    common.Hash           `json:"selection"`
	Definition   Definition            `json:"definition"`
	Verification string                `json:"verification"`
	Coverage     string                `json:"coverage"`
	Total        int                   `json:"total"`
	Blobs        []Blob                `json:"blobs"`
	Next         *uint32               `json:"next"`
}

// ReadPage performs no RPC. Callers must pin epoch, exact identity and selection
// for every page. A reset cannot turn this into a different fork's next page.
func ReadPage(ctx context.Context, r CapturedReader, p chains.BinaryProfile, epoch uint64, id model.CaptureIdentity, d model.SupplementStreamDescriptor, after uint32, limit int) (Page, error) {
	var page Page
	if limit < 1 || limit > 4 {
		return page, port.ErrInvalidData
	}
	out, e := ReadSnapshot(ctx, r, p, epoch, id, d)
	if e != nil {
		return page, e
	}
	if uint64(after) > uint64(len(out.References)) {
		return page, port.ErrInvalidData
	}
	end := min(uint64(after)+uint64(limit), uint64(len(out.References)))
	page = Page{Epoch: epoch, Identity: id, Selection: d.SelectionHash, Definition: out.Definition, Verification: out.Verification, Coverage: out.Coverage, Total: len(out.References), Blobs: []Blob{}}
	for i := uint64(after); i < end; i++ {
		page.Blobs = append(page.Blobs, Blob{Reference: out.References[i], Data: out.Response.Entries[i]})
	}
	if end < uint64(len(out.References)) {
		v := uint32(end)
		page.Next = &v
	}
	return page, nil
}
