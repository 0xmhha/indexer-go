package fetch

import (
	"context"
	"errors"
	"fmt"
	"time"
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
// finalized block (the "finalized" block tag); ok is false when the node
// has not finalized a block (or does not report finality).
type FinalizedClient interface {
	GetFinalizedBlockNumber(ctx context.Context) (n uint64, ok bool, err error)
}

// noFinalizedWarnEvery throttles the warning while the node reports no
// finalized block.
const noFinalizedWarnEvery = time.Minute

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
		n, ok, err := fc.GetFinalizedBlockNumber(rctx)
		if err != nil {
			return 0, false, err
		}
		if !ok {
			// go-stablenet sets the finalized block only for blocks it
			// receives from peers, so it can have none after a restart or
			// while syncing: wait instead of failing.
			if time.Since(f.noFinalizedWarned) > noFinalizedWarnEvery {
				f.noFinalizedWarned = time.Now()
				f.logger.Warn("Node reports no finalized block yet; waiting (indexer.finality: finalized)")
			}
			return 0, false, nil
		}
		return n, true, nil
	default:
		return 0, false, fmt.Errorf("fetch: unknown finality policy %q", f.config.Finality)
	}
}

// CheckFinality verifies at startup that the finality policy can be
// served: the client must be able to read the finalized block. A node that
// has not finalized a block yet is accepted (the live loop waits and
// warns).
func (f *Fetcher) CheckFinality(ctx context.Context) error {
	if _, _, err := f.targetHead(ctx); err != nil {
		return fmt.Errorf("finality %q: %w", f.config.Finality, err)
	}
	return nil
}
