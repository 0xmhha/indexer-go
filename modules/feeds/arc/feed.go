// Package arcfeed links the audited Arc testnet peer/codec to the capture
// lifecycle. It never starts an Arc node, execution client or Rust process.
package arcfeed

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
)

func New(ctx context.Context, cfg ethpeer.Config, fresh source.CanonicalReader) (*ethpeer.Client, error) {
	if cfg.Chain.ID != "5042002" || fresh == nil {
		return nil, chainfeed.ErrUnsupported
	}
	genesis, err := fresh.HashAt(ctx, 0)
	if err != nil {
		return nil, err
	}
	if genesis != cfg.Chain.GenesisHash {
		return nil, model.ErrCaptureInvalid
	}
	hash, err := fresh.HashAt(ctx, cfg.Checkpoint.Number)
	if err != nil {
		return nil, err
	}
	if hash != cfg.Checkpoint.Hash {
		return nil, capture.ErrCanonical
	}
	return ethpeer.New(cfg)
}

// Run supplies the two actual Tasks to the existing configured session. It
// supports only the audited encoded header/body/receipt plan. Other required
// inputs fail closed before any peer connection or filesystem initialization.
func Run(ctx context.Context, db port.CaptureDatabase, fresh source.CanonicalReader, peer ethpeer.Config, session capture.SessionConfig, opts capture.PipelineOptions) error {
	if peer.Chain != session.Anchor.Chain || peer.Checkpoint != session.Anchor {
		return model.ErrCaptureInvalid
	}
	if err := evmdata.Capabilities(peer.Chain).CheckPlan(peer.Chain, session.Plan); err != nil {
		return err
	}
	if len(session.Plan.Required) != 3 {
		return chainfeed.ErrUnsupported
	}
	if _, err := session.Storage.OptionsFor(session.Instance, peer.Chain); err != nil {
		return err
	}
	client, err := New(ctx, peer, fresh)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	receive, process, err := capture.PipelineTasks(client, evmdata.Verifier{Profile: arc.New(), Provenance: "arc.eth68/6e764023"}, opts)
	if err != nil {
		return err
	}
	return capture.RunConfiguredSession(ctx, db, fresh, session, receive, process)
}
