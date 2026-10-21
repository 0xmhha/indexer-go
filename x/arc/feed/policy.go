package arcfeed

import (
	"context"
	"strings"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/x/arc"
)

// FinalitySource must be the SAME live source backed by the existing 5 HTTP/s
// pool. This is an explicit node-trust mode, not Malachite certificate checking.
type FinalitySource interface {
	source.CanonicalReader
	source.FinalizedReader
}

type rpcFinality struct {
	reader    source.FinalizedReader
	chain     model.FeedChain
	authority string
}

// TrustedRPCFinality selects the audited Arc finalized-tag trust mode. Authority
// is a stable public operator label, not a URL/key; the mode/label is DB-pinned.
func TrustedRPCFinality(reader source.FinalizedReader, chain model.FeedChain, authority string) (capture.FinalityPolicy, error) {
	if reader == nil || chain.Validate() != nil || chain.ID != "5042002" || len(authority) < 1 || len(authority) > 64 {
		return nil, chainfeed.ErrUnsupported
	}
	if err := validateAuthority(authority); err != nil {
		return nil, err
	}
	return rpcFinality{reader: reader, chain: chain, authority: authority}, nil
}
func validateAuthority(authority string) error {
	if len(authority) < 1 || len(authority) > 64 {
		return chainfeed.ErrUnsupported
	}
	for _, c := range authority {
		valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("._-", c)
		if !valid {
			return chainfeed.ErrUnsupported
		}
	}
	return nil
}
func (p rpcFinality) ID() string { return "arc.rpc-finalized/6e764023/" + p.authority }
func (p rpcFinality) Checkpoint(ctx context.Context, chain model.FeedChain) (model.FeedBlock, error) {
	if chain != p.chain {
		return model.FeedBlock{}, chainfeed.ErrUnsupported
	}
	n, hash, err := p.reader.FinalizedCheckpoint(ctx)
	return model.FeedBlock{Chain: chain, Number: n, Hash: hash}, err
}

// RunManaged explicitly enables supervision with the same canonical/finalized
// source. Empty authority is reorg-only. GC additionally needs runtime.Collect.
// Existing Run remains the acquisition baseline; neither entry activates CLI.
func RunManaged(ctx context.Context, db port.CaptureDatabase, fresh FinalitySource, peer ethpeer.Config, session capture.SessionConfig, runtime capture.RuntimeOptions, authority string) error {
	if peer.Chain != session.Anchor.Chain || peer.Checkpoint != session.Anchor {
		return model.ErrCaptureInvalid
	}
	if err := evmdata.Capabilities(peer.Chain).CheckPlan(peer.Chain, session.Plan); err != nil {
		return err
	}
	if len(session.Plan.Required) != 3 || fresh == nil || runtime.Finality != nil {
		return chainfeed.ErrUnsupported
	}
	if authority != "" {
		policy, err := TrustedRPCFinality(fresh, peer.Chain, authority)
		if err != nil {
			return err
		}
		runtime.Finality = policy
	}
	factory := func(ctx context.Context) (chainfeed.Adapter, capture.Assembler, error) {
		client, err := New(ctx, peer, fresh)
		if err != nil {
			return nil, nil, err
		}
		return client, evmdata.Verifier{Profile: arc.New(), Provenance: "arc.eth68/6e764023"}, nil
	}
	return capture.RunConfiguredFeed(ctx, db, fresh, session, factory, runtime)
}
