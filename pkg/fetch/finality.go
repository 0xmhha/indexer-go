package fetch

import (
	"context"
	"errors"
	"fmt"
)

// Finality policies (indexer.finality).
const (
	FinalityHead          = "head"          // index the newest block; roll back reorganizations
	FinalityConfirmations = "confirmations" // stay Confirmations blocks behind the head
	FinalityFinalized     = "finalized"     // index up to the node's finalized block
)

// ErrNoFinalized means the node does not report a finalized block.
var ErrNoFinalized = errors.New("fetch: node does not report a finalized block")

// FinalizedClient is implemented by clients that can read the node's
// finalized block (the "finalized" block tag).
type FinalizedClient interface {
	GetFinalizedBlockNumber(ctx context.Context) (uint64, error)
}

// targetHead returns the highest block the live loop may index under the
// finality policy. ok is false when no block qualifies yet (fewer blocks
// than the required confirmations).
func (f *Fetcher) targetHead(ctx context.Context) (target uint64, ok bool, err error) {
	switch f.config.Finality {
	case "", FinalityHead:
		head, err := f.latestBlockNumber(ctx)
		return head, err == nil, err
	case FinalityConfirmations:
		head, err := f.latestBlockNumber(ctx)
		if err != nil {
			return 0, false, err
		}
		if head < f.config.Confirmations {
			return 0, false, nil
		}
		return head - f.config.Confirmations, true, nil
	case FinalityFinalized:
		fc, isFC := f.client.(FinalizedClient)
		if !isFC {
			return 0, false, fmt.Errorf("%w: client cannot read it", ErrNoFinalized)
		}
		rctx, cancel := f.rpcCtx(ctx)
		defer cancel()
		n, err := fc.GetFinalizedBlockNumber(rctx)
		if err != nil {
			return 0, false, err
		}
		return n, true, nil
	default:
		return 0, false, fmt.Errorf("fetch: unknown finality policy %q", f.config.Finality)
	}
}

// CheckFinality verifies at startup that the node supports the finality
// policy, so an unsupported "finalized" tag stops the indexer instead of
// leaving the live loop retrying forever.
func (f *Fetcher) CheckFinality(ctx context.Context) error {
	if _, _, err := f.targetHead(ctx); err != nil {
		return fmt.Errorf("finality %q: %w", f.config.Finality, err)
	}
	return nil
}
