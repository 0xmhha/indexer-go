// Package facts replays verified execution inputs from the durable capture DB.
// Construction and HashAt/ValidateCommit use the supplied live canonical reader.
// It never reads blocks, receipts, state or missing history from a node.
package facts

import (
	"context"
	"errors"
	"fmt"
	"runtime/trace"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/ethereum/go-ethereum/common"
)

var ErrCoverage = errors.New("facts: required source coverage unavailable")

type Config struct {
	Store     *capture.Store
	Profile   chains.BinaryProfile   // must match the capturing chain module
	Canonical source.CanonicalReader // fresh, uncached; shared 5 HTTP/s pool
	Consumer  string                 // stable projection identity; independent of RawConsumers
}

// Source pins one capture generation. After Reset all old handles fail with
// capture.ErrStale: cancel/join, reconcile projection undo, then create a new
// handle. No cached input or generation silently follows a replacement fork.
type Source struct {
	cfg   Config
	chain model.FeedChain
	epoch uint64
	plan  model.CapturePlan
}

var _ source.Source = (*Source)(nil)
var _ source.BlockReader = (*Source)(nil)
var _ source.CommitValidator = (*Source)(nil)
var _ source.CommitAcknowledger = (*Source)(nil)

func New(ctx context.Context, cfg Config) (*Source, error) {
	if cfg.Store == nil || cfg.Profile == nil || cfg.Canonical == nil || cfg.Consumer == "" || len(cfg.Consumer) > 128 {
		return nil, errors.New("facts: store, binary profile, live canonical reader and consumer are required")
	}
	for _, r := range cfg.Consumer {
		if r < 33 || r > 126 {
			return nil, errors.New("facts: invalid consumer")
		}
	}
	st, e := cfg.Store.State(ctx)
	if e != nil {
		return nil, e
	}
	if len(st.Plan.Required) != 3 {
		return nil, fmt.Errorf("%w: only complete header/body/receipts can be replayed", ErrCoverage)
	}
	if e = evmdata.Capabilities(st.Anchor.Chain).CheckPlan(st.Anchor.Chain, st.Plan); e != nil {
		return nil, fmt.Errorf("%w: %w", ErrCoverage, e)
	}
	s := &Source{cfg: cfg, chain: st.Anchor.Chain, epoch: st.Generation, plan: st.Plan}
	for _, b := range []model.FeedBlock{{Chain: s.chain, Hash: s.chain.GenesisHash}, st.Anchor} {
		h, e := cfg.Canonical.HashAt(ctx, b.Number)
		if e != nil {
			return nil, e
		}
		if h != b.Hash {
			return nil, capture.ErrCanonical
		}
	}
	if _, e = s.current(ctx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *Source) Profile() chains.Profile { return s.cfg.Profile }
func (s *Source) Epoch() uint64           { return s.epoch }
func (s *Source) current(ctx context.Context) (capture.State, error) {
	st, e := s.cfg.Store.State(ctx)
	if e != nil {
		return st, e
	}
	if st.Generation != s.epoch {
		return st, capture.ErrStale
	}
	a, e := st.Plan.Digest()
	if e != nil {
		return st, e
	}
	b, e := s.plan.Digest()
	if e != nil {
		return st, e
	}
	if a != b || st.Anchor.Chain != s.chain {
		return st, capture.ErrConflict
	}
	return st, nil
}

// Head is captured, contiguous Through; it is never an observed network tip.
func (s *Source) Head(ctx context.Context) (uint64, error) {
	st, e := s.current(ctx)
	return st.Through.Number, e
}
func (s *Source) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	pre := trace.StartRegion(ctx, "feed.freshPreEpoch")
	_, e := s.current(ctx)
	pre.End()
	if e != nil {
		return common.Hash{}, e
	}
	rpc := trace.StartRegion(ctx, "feed.freshRPC")
	h, e := s.cfg.Canonical.HashAt(ctx, n)
	rpc.End()
	if e != nil {
		return common.Hash{}, e
	}
	post := trace.StartRegion(ctx, "feed.freshPostEpoch")
	_, e = s.current(ctx)
	post.End()
	if e != nil {
		return common.Hash{}, e
	}
	return h, nil
}
func (s *Source) identity(ctx context.Context, n uint64) (model.CaptureIdentity, error) {
	st, e := s.current(ctx)
	if e != nil {
		return model.CaptureIdentity{}, e
	}
	if n > st.Through.Number {
		return model.CaptureIdentity{}, source.ErrNotFound
	}
	if n <= st.Anchor.Number {
		return model.CaptureIdentity{}, fmt.Errorf("%w: bootstrap/older height %d has no captured input", ErrCoverage, n)
	}
	return s.cfg.Store.CanonicalAt(ctx, s.epoch, n)
}
func (s *Source) Block(ctx context.Context, n uint64) (*model.Block, error) {
	b, _, e := s.BlockWithReceipts(ctx, n)
	return b, e
}
func (s *Source) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	id, e := s.identity(ctx, n)
	if e != nil {
		return nil, nil, e
	}
	// Canonical metadata and bound permanent bytes share one snapshot. Decode
	// below still needs its independent epoch barrier, and the writer retains
	// its separate uncached live canonical check immediately before commit.
	in, e := s.cfg.Store.ReadProjectionInput(ctx, s.epoch, id)
	if errors.Is(e, capture.ErrProjectionInputLimit) {
		return nil, nil, fmt.Errorf("%w: %w", ErrCoverage, e)
	}
	if e != nil {
		return nil, nil, e
	}
	data := map[model.DatasetKind][]byte{}
	for _, evidence := range in.Manifest.Datasets {
		if evidence.Codec != evmdata.Codec(evidence.Kind) || evidence.Status == model.DatasetNotApplicable || evidence.Verification != model.VerificationCommitment {
			return nil, nil, fmt.Errorf("%w: dataset %s encoding/verification", ErrCoverage, evidence.Kind)
		}
	}
	for _, p := range in.Payloads {
		data[p.Kind] = p.Bytes
	}
	b, rs, e := evmdata.Decode(s.cfg.Profile, id.Block, data[model.DatasetHeaders], data[model.DatasetBodies], data[model.DatasetReceipts])
	if e != nil {
		return nil, nil, e
	}
	if b.ParentHash != in.ParentHash {
		return nil, nil, capture.ErrConflict
	}
	if e = s.cfg.Store.ValidateProjection(ctx, s.epoch, id); e != nil {
		return nil, nil, e
	}
	return b, rs, nil
}
func (s *Source) ValidateCommit(ctx context.Context, b *model.Block) error {
	region := trace.StartRegion(ctx, "feed.writerFresh")
	defer region.End()
	if trace.IsEnabled() && b != nil {
		trace.Logf(ctx, "feed.block", "%d", b.Number)
	}
	if b == nil {
		return model.ErrCaptureInvalid
	}
	plan, e := s.plan.Digest()
	if e != nil {
		return e
	}
	preflight := trace.StartRegion(ctx, "feed.freshPreflight")
	st, e := s.cfg.Store.ValidateProjectionBlock(ctx, s.epoch, plan, model.FeedBlock{Chain: s.chain, Number: b.Number, Hash: b.Hash})
	preflight.End()
	if e == capture.ErrGap {
		if b.Number <= st.Anchor.Number {
			return fmt.Errorf("%w: bootstrap/older height %d has no captured input", ErrCoverage, b.Number)
		}
		return source.ErrNotFound
	}
	if e != nil {
		return e
	}
	// The preflight already checked the pre-RPC epoch/scope. Retain one NEW
	// uncached live read and an independent post-RPC check, at the same writer
	// commit boundary. Never reuse a previous call's canonical answer.
	rpc := trace.StartRegion(ctx, "feed.freshRPC")
	h, e := s.cfg.Canonical.HashAt(ctx, b.Number)
	rpc.End()
	if e != nil {
		return e
	}
	post := trace.StartRegion(ctx, "feed.freshPostEpoch")
	_, e = s.current(ctx)
	post.End()
	if e != nil {
		return e
	}
	if h != b.Hash {
		return capture.ErrCanonical
	}
	return nil
}
func (s *Source) Acknowledge(ctx context.Context, b *model.Block) error {
	if b == nil {
		return model.ErrCaptureInvalid
	}
	id, e := s.identity(ctx, b.Number)
	if e != nil {
		return e
	}
	if id.Block.Hash != b.Hash {
		return capture.ErrCanonical
	}
	return s.cfg.Store.AckProjection(ctx, s.cfg.Consumer, s.epoch, id)
}

func (s *Source) RecoveryAnchor(ctx context.Context) (uint64, common.Hash, error) {
	st, e := s.current(ctx)
	if e != nil {
		return 0, common.Hash{}, e
	}
	return st.Anchor.Number, st.Anchor.Hash, nil
}

func (s *Source) ProjectionInput(ctx context.Context, b *model.Block) (model.ProjectionInput, error) {
	if b == nil {
		return model.ProjectionInput{}, model.ErrCaptureInvalid
	}
	st, e := s.current(ctx)
	if e != nil {
		return model.ProjectionInput{}, e
	}
	id, e := s.identity(ctx, b.Number)
	if e != nil {
		return model.ProjectionInput{}, e
	}
	if id.Block.Hash != b.Hash {
		return model.ProjectionInput{}, capture.ErrCanonical
	}
	return model.ProjectionInput{Epoch: s.epoch, Identity: id, Anchor: st.Anchor}, nil
}
