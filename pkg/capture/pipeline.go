package capture

import (
	"context"
	"errors"
	"io"
	"math"
	"runtime/trace"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/window"
)

type Assembler interface {
	Assemble(context.Context, *window.Store, State, []window.Reference) (Input, error)
}
type PipelineOptions struct {
	QueueReferences  int // bounded notification channel; durable work lives in window
	PollInterval     time.Duration
	MaxPayloadBytes  int64
	ResolveCanonical bool          // opt-in runtime: raw hint + commit fresh, resolve ambiguous/rejected forks
	ResolveInterval  time.Duration // refresh unresolved fork candidates (default 1s)
	WaitDeletion     bool          // retry only an active GC fence; cancellation still stops both workers
	// Called synchronously from the corresponding worker; must return promptly.
	OnReceived func(window.Reference)
	OnCaptured func(model.CaptureIdentity)
}

// PipelineTasks separates network/disk intake from verification/DB work.
// A full channel coalesces notifications, not durable work: Process pages the
// window until caught up. Neither a channel send nor raw fsync is a source ACK.
// Tasks are single-use and must run under RunSession (which joins both).
func PipelineTasks(adapter chainfeed.Adapter, assembler Assembler, opts PipelineOptions) (Task, Task, error) {
	if adapter == nil || assembler == nil {
		return nil, nil, model.ErrCaptureInvalid
	}
	if opts.QueueReferences == 0 {
		opts.QueueReferences = 256
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 100 * time.Millisecond
	}
	if opts.MaxPayloadBytes == 0 {
		opts.MaxPayloadBytes = 8 << 20
	}
	if opts.ResolveInterval == 0 {
		opts.ResolveInterval = time.Second
	}
	if opts.ResolveInterval < time.Millisecond || opts.ResolveInterval > time.Minute {
		return nil, nil, model.ErrCaptureInvalid
	}
	if opts.QueueReferences < 1 || opts.QueueReferences > 65536 || opts.PollInterval < time.Millisecond || opts.MaxPayloadBytes < 1 || opts.MaxPayloadBytes > 8<<20 {
		return nil, nil, model.ErrCaptureInvalid
	}
	queue := make(chan window.Reference, opts.QueueReferences)
	var closeOnce sync.Once
	closeAdapter := func() { closeOnce.Do(func() { _ = adapter.Close() }) }
	receive := func(ctx context.Context, raw *window.Store, facts *Store) error {
		defer closeAdapter()
		state, err := facts.State(ctx)
		if err != nil {
			return err
		}
		caps, err := adapter.Capabilities(ctx)
		if err != nil {
			return err
		}
		if err := caps.CheckPlan(state.Anchor.Chain, state.Plan); err != nil {
			return err
		}
		stream, err := adapter.Subscribe(ctx, state.Through, chainfeed.ReadLimits{MaxPayloadBytes: opts.MaxPayloadBytes})
		if err != nil {
			return err
		}
		defer func() { _ = stream.Close() }()
		recorder := feedtelemetry.From(ctx)
		recorder.StartIntake(state.Through)
		appended := raw.Stats()
		for {
			region := trace.StartRegion(ctx, "feed.next")
			nextSpan := feedtelemetry.From(ctx).Start(feedtelemetry.ReceiveNext)
			inputs, err := nextEnvelopes(ctx, stream, state.Anchor.Chain, opts.MaxPayloadBytes)
			nextSpan.End(err)
			if trace.IsEnabled() && err == nil {
				for _, e := range inputs {
					trace.Logf(ctx, "feed.input", "%d %s", e.Block.Number, e.Kind)
				}
			}
			region.End()
			if errors.Is(err, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			if err != nil {
				return err
			}
			for _, e := range inputs {
				if e.Kind == model.DatasetHeaders {
					feedtelemetry.From(ctx).Observe(e.Block)
				}
			}
			appendSpan := feedtelemetry.From(ctx).Start(feedtelemetry.RawAppend)
			var refs []window.Reference
			if len(inputs) == 1 {
				ref, appendErr := raw.AppendWait(ctx, inputs[0])
				err = appendErr
				refs = []window.Reference{ref}
			} else {
				refs, err = raw.AppendBatchWait(ctx, inputs)
			}
			appendSpan.End(err)
			if err != nil {
				return err
			}
			if recorder != nil {
				now := raw.Stats()
				recorder.Appended(now.AppendedRecords-appended.AppendedRecords, now.AppendedBytes-appended.AppendedBytes)
				appended = now
				for _, ref := range refs {
					recorder.Received(ref.Block, ref.Kind, time.Unix(0, ref.FirstReceived))
				}
			}
			for _, ref := range refs {
				if opts.OnReceived != nil {
					opts.OnReceived(ref)
				}
				select {
				case queue <- ref:
				default:
				}
			}
		}
	}
	process := func(ctx context.Context, raw *window.Store, facts *Store) error {
		initial, err := facts.State(ctx)
		if err != nil {
			return err
		}
		caps, err := adapter.Capabilities(ctx)
		if err != nil {
			return err
		}
		if err := caps.CheckPlan(initial.Anchor.Chain, initial.Plan); err != nil {
			return err
		}
		if opts.ResolveCanonical {
			return processCanonical(ctx, raw, facts, adapter, assembler, opts, queue)
		}
		var cursor window.Cursor
		var group []window.Reference
		for {
			state, err := facts.State(ctx)
			if err != nil {
				return err
			}
			if state.Through.Number == math.MaxUint64 {
				return ErrGap
			}
			refs, next, err := raw.Page(ctx, cursor, 256)
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if ref.Block.Number <= state.Through.Number {
					continue
				}
				if ref.Block.Number != state.Through.Number+1 {
					return ErrGap
				}
				if len(group) > 0 && ref.Block != group[0].Block {
					return ErrCanonical
				}
				wanted := false
				for _, req := range state.Plan.Required {
					if req.Kind == ref.Kind {
						wanted = true
						break
					}
				}
				if !wanted {
					return model.ErrCaptureInvalid
				}
				duplicate := false
				for _, old := range group {
					if old.Kind == ref.Kind {
						if old != ref {
							return ErrConflict
						}
						duplicate = true
						break
					}
				}
				if !duplicate {
					group = append(group, ref)
				}
				if len(group) != len(state.Plan.Required) {
					continue
				}
				recordRawQueue(ctx, group)
				assembleSpan := feedtelemetry.From(ctx).Start(feedtelemetry.Assemble)
				in, err := assembler.Assemble(ctx, raw, state, group)
				assembleSpan.End(err)
				if err != nil {
					return err
				}
				if in.Manifest.Identity.Block != ref.Block || in.Manifest.Identity.Generation != state.Generation {
					return model.ErrCaptureInvalid
				}
				commitSpan := feedtelemetry.From(ctx).Start(feedtelemetry.CaptureCommit)
				commitErr := waitDeletion(ctx, opts.WaitDeletion, func() error { return facts.Commit(ctx, in) })
				commitSpan.End(commitErr)
				if commitErr != nil {
					return commitErr
				}
				feedtelemetry.From(ctx).Complete(feedtelemetry.Captured, in.Manifest.Identity)

				if observer, ok := adapter.(interface {
					ObserveCaptured(*window.Store, []window.Reference) error
				}); ok {
					if err := observer.ObserveCaptured(raw, group); err != nil {
						return err
					}
				}
				if opts.OnCaptured != nil {
					opts.OnCaptured(in.Manifest.Identity)
				}
				group = nil
				state, err = facts.State(ctx)
				if err != nil {
					return err
				}
			}
			cursor = next
			if len(refs) > 0 {
				continue
			}
			timer := time.NewTimer(opts.PollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-queue:
				timer.Stop()
			case <-timer.C:
			}
		}
	}
	return receive, process, nil
}

// Ordinary adapters retain one-envelope intake. Batch adapters expose at most
// one already fetched block; no cross-block buffering or payload copies occur.
func nextEnvelopes(ctx context.Context, stream chainfeed.Stream, chain model.FeedChain, maxBytes int64) ([]chainfeed.Envelope, error) {
	var inputs []chainfeed.Envelope
	var err error
	if batch, ok := stream.(chainfeed.BatchStream); ok {
		inputs, err = batch.NextBatch(ctx)
	} else {
		var e chainfeed.Envelope
		e, err = stream.Next(ctx)
		inputs = []chainfeed.Envelope{e}
	}
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 || len(inputs) > chainfeed.MaxBatchEnvelopes {
		return nil, model.ErrCaptureInvalid
	}
	for i, e := range inputs {
		if err := e.Validate(chainfeed.ReadLimits{MaxPayloadBytes: maxBytes}); err != nil {
			return nil, err
		}
		if e.Block.Chain != chain || e.Block != inputs[0].Block {
			return nil, model.ErrCaptureInvalid
		}
		for _, prior := range inputs[:i] {
			if prior.Kind == e.Kind {
				return nil, model.ErrCaptureInvalid
			}
		}
	}
	return inputs, nil
}

// Raw queue age starts when the last required frame was received. It includes
// append/fsync, availability/canonical resolution and scheduling, not assembly.
func recordRawQueue(ctx context.Context, refs []window.Reference) {
	r := feedtelemetry.From(ctx)
	if r == nil {
		return
	}
	var latest int64
	for _, ref := range refs {
		if ref.FirstReceived > latest {
			latest = ref.FirstReceived
		}
	}
	now := time.Now()
	if latest > 0 && latest <= now.UnixNano() {
		r.Record(feedtelemetry.RawQueue, now.Sub(time.Unix(0, latest)))
	}
}
