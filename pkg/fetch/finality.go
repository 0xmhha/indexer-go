package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// Progress is how far the live loop has looked: the highest block the node
// offered under the finality policy at its last poll (refactoring plan
// R6-3: a project's API tells "not indexed yet" from "indexing is behind").
// ok is false before the first successful poll.
func (f *Fetcher) Progress() (target uint64, ok bool) {
	return f.lastTarget.Load(), f.polled.Load()
}

// progressPoll is how often the live loop's progress is read from the node
// besides the loop's own polls: a batch that keeps failing retries without
// polling, and the progress must not go stale meanwhile.
var progressPoll = time.Second

// followTarget records the target head every progressPoll until ctx ends.
func (f *Fetcher) followTarget(ctx context.Context) {
	t := time.NewTicker(progressPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if target, ok, err := f.readTargetHead(ctx, false); err == nil && ok {
				f.noteTarget(target)
			}
		}
	}
}

// noteTarget records a poll's target head.
func (f *Fetcher) noteTarget(target uint64) {
	f.lastTarget.Store(target)
	f.polled.Store(true)
}

// Progress providers are found by the storage they index into, so an API
// serving that storage finds the live loop's progress.
var progress sync.Map // storage -> *Fetcher

// AttachProgress makes f the progress of the storage store.
func AttachProgress(store any, f *Fetcher) { progress.Store(store, f) }

// DetachProgress removes the progress of store.
func DetachProgress(store any) { progress.Delete(store) }

// ProgressOf returns the live loop's progress for store (see Progress);
// attached is false when no live loop indexes into it in this process.
func ProgressOf(store any) (target uint64, ok, attached bool) {
	v, found := progress.Load(store)
	if !found {
		return 0, false, false
	}
	target, ok = v.(*Fetcher).Progress()
	return target, ok, true
}

// noFinalizedWarnEvery throttles the warning while the node reports no
// finalized block.
const noFinalizedWarnEvery = time.Minute

// targetHead returns the highest block the live loop may index under the
// finality policy. ok is false when no block qualifies yet (fewer blocks
// than the required confirmations).
func (f *Fetcher) targetHead(ctx context.Context) (target uint64, ok bool, err error) {
	return f.readTargetHead(ctx, true)
}

// readTargetHead is targetHead; warn reports a node without a finalized
// block (only the live loop does, so the warning's state has one writer).
func (f *Fetcher) readTargetHead(ctx context.Context, warn bool) (target uint64, ok bool, err error) {
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
			if warn && time.Since(f.noFinalizedWarned) > noFinalizedWarnEvery {
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
