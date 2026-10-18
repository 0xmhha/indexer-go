package capture

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
)

func pause(ctx context.Context, interval time.Duration, wake <-chan window.Reference) error {
	t := time.NewTimer(interval)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wake:
		return nil
	case <-t.C:
		return nil
	}
}

func waitDeletion(ctx context.Context, enabled bool, fn func() error) error {
	for {
		err := fn()
		if !enabled || err != ErrDeletePending {
			return err
		}
		if err := pause(ctx, 20*time.Millisecond, nil); err != nil {
			return err
		}
	}
}

// Peel context-only wrappers, but never reduce a joined rollback/I/O failure
// to its canonical symptom. A retained orphan may be above the new RPC head.
func hintCause(err error) error {
	for {
		one, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		err = one.Unwrap()
	}
}

// Try a complete, unambiguous raw hint without a speculative RPC for a future
// height. Commit still checks fresh inside its generation transaction. Multiple
// forks or a fresh-rejected hint select by RPC/Lookup, so a retained orphan can
// neither poison restart order nor cause repeated speculative commits.
func processCanonical(ctx context.Context, raw *window.Store, facts *Store, adapter chainfeed.Adapter, assembler Assembler, opts PipelineOptions, wake <-chan window.Reference) error {
	var candidate model.FeedBlock
	var nextRead time.Time
	var height uint64
	resolve := false
	for {
		state, err := facts.State(ctx)
		if err != nil {
			return err
		}
		if state.Through.Number == math.MaxUint64 {
			return ErrGap
		}
		n := state.Through.Number + 1
		if height != n {
			height, resolve = n, false
			candidate = model.FeedBlock{}
			nextRead = time.Time{}
		}
		var refs []window.Reference
		if !resolve {
			kinds := make([]model.DatasetKind, len(state.Plan.Required))
			for i, req := range state.Plan.Required {
				kinds[i] = req.Kind
			}
			refs, err = raw.Candidate(ctx, n, kinds)
			switch {
			case err == nil:
				candidate = refs[0].Block
			case errors.Is(err, window.ErrAmbiguous):
				resolve = true
			case errors.Is(err, window.ErrNotFound):
				// Wakeups may coalesce; durable window metadata is authoritative
				// for availability even after restart or a full channel.
			default:
				return err
			}
		}
		if resolve && !time.Now().Before(nextRead) {
			hash, err := facts.canonical.HashAt(ctx, n)
			if err != nil && hintCause(err) != source.ErrNotFound {
				return err
			}
			candidate = model.FeedBlock{Chain: state.Anchor.Chain, Number: n, Hash: hash}
			nextRead = time.Now().Add(opts.ResolveInterval)
		}
		if resolve && candidate.Hash != (common.Hash{}) {
			for _, req := range state.Plan.Required {
				ref, err := raw.Lookup(ctx, candidate, req.Kind)
				if errors.Is(err, window.ErrNotFound) {
					break
				}
				if err != nil {
					return err
				}
				refs = append(refs, ref)
			}
		}
		if len(refs) != len(state.Plan.Required) {
			if err := pause(ctx, opts.PollInterval, wake); err != nil {
				return err
			}
			continue
		}
		recordRawQueue(ctx, refs)
		assembleSpan := feedtelemetry.From(ctx).Start(feedtelemetry.Assemble)
		in, err := assembler.Assemble(ctx, raw, state, refs)
		assembleSpan.End(err)
		if err != nil {
			return err
		}
		if in.Manifest.Identity.Block != candidate || in.Manifest.Identity.Generation != state.Generation {
			return model.ErrCaptureInvalid
		}
		commitSpan := feedtelemetry.From(ctx).Start(feedtelemetry.CaptureCommit)
		commitErr := waitDeletion(ctx, opts.WaitDeletion, func() error { return facts.Commit(ctx, in) })
		commitSpan.End(commitErr)
		cause := hintCause(commitErr)
		if !resolve && (cause == ErrCanonical || cause == source.ErrNotFound) {
			// A rejected/now-future hint rolls back before canonical selection.
			// Joined I/O/rollback/cancellation errors still stop.
			resolve = true
			candidate = model.FeedBlock{}
			nextRead = time.Time{}
			continue
		}
		if commitErr != nil {
			return commitErr
		}
		feedtelemetry.From(ctx).Complete(feedtelemetry.Captured, in.Manifest.Identity)

		if observer, ok := adapter.(interface {
			ObserveCaptured(*window.Store, []window.Reference) error
		}); ok {
			if err := observer.ObserveCaptured(raw, refs); err != nil {
				return err
			}
		}
		if opts.OnCaptured != nil {
			opts.OnCaptured(in.Manifest.Identity)
		}
		candidate = model.FeedBlock{}
	}
}
