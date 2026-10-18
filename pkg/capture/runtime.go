package capture

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// FeedFactory constructs a NEW single-use adapter/assembler for each epoch.
// It must clean up partial construction on error and must not own the DB/window.
type FeedFactory func(context.Context) (chainfeed.Adapter, Assembler, error)

type RuntimeOptions struct {
	Pipeline        PipelineOptions
	Finality        FinalityPolicy // nil is reorg-only; never grants deletion
	Collect         bool
	Consumers       map[string]RawConsumer // must exactly match the session's raw-only groups
	ControlInterval time.Duration          // default 10s, all RPCs still use the caller's shared pool
	MaxReorgDepth   uint64                 // default 128; at most 4096; finalized/bootstrap floor always wins
	MaxRestarts     int                    // default 8 per run, explicit bounded recovery
	RestartDelay    time.Duration
	OnControl       func(ControlStatus) // synchronous, must return promptly
}

var errReorgObserved = errors.New("capture: observed canonical reorg")

func (o RuntimeOptions) normalized(consumers []string) (RuntimeOptions, error) {
	if o.ControlInterval == 0 {
		o.ControlInterval = 10 * time.Second
	}
	if o.MaxReorgDepth == 0 {
		o.MaxReorgDepth = 128
	}
	if o.MaxRestarts == 0 {
		o.MaxRestarts = 8
	}
	if o.RestartDelay == 0 {
		o.RestartDelay = 100 * time.Millisecond
	}
	if o.ControlInterval < time.Millisecond || o.ControlInterval > time.Hour || o.MaxReorgDepth > 4096 || o.MaxRestarts < 1 || o.MaxRestarts > 64 || o.RestartDelay < time.Millisecond || o.RestartDelay > time.Minute || (o.Collect && o.Finality == nil) || len(o.Consumers) != len(consumers) || len(consumers) > 8 {
		return o, port.ErrInvalidData
	}
	if o.Finality != nil {
		id := o.Finality.ID()
		if id == "" || len(id) > 256 || strings.TrimSpace(id) != id {
			return o, port.ErrInvalidData
		}
	}
	seen := map[string]bool{}
	for _, name := range consumers {
		if o.Consumers[name] == nil {
			return o, port.ErrInvalidData
		}
		if seen[name] {
			return o, port.ErrInvalidData
		}
		seen[name] = true
	}
	o.Pipeline.ResolveCanonical, o.Pipeline.WaitDeletion = true, true
	return o, nil
}

// RunFeedSession adds chain-policy maintenance and raw consumers to RunSession.
// Reorg recovery always happens AFTER cancel/join/Close of the entire epoch.
// Pending deletion is recovered by RunSession before any task can run.
func RunFeedSession(ctx context.Context, db port.CaptureDatabase, fresh source.CanonicalReader, session SessionOptions, factory FeedFactory, options RuntimeOptions) error {
	if factory == nil {
		return port.ErrInvalidData
	}
	opts, err := options.normalized(session.RawConsumers)
	if err != nil {
		return err
	}
	if _, err := session.Window.Normalize(); err != nil {
		return err
	}
	if err := session.Plan.Validate(); err != nil {
		return err
	}
	if session.Anchor.Validate() != nil || session.Anchor.Chain != session.Window.Chain {
		return port.ErrInvalidData
	}
	for restarts := 0; ; restarts++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Reject authority changes BEFORE opening the raw store: Open may
		// finish an old, durable GC intent under the previous policy.
		if err := checkRuntimePolicy(ctx, db, fresh, session.Window.Chain, opts.Finality); err != nil {
			return err
		}
		adapter, assembler, err := factory(ctx)
		if err != nil {
			return err
		}
		receive, process, err := PipelineTasks(adapter, assembler, opts.Pipeline)
		if err != nil {
			if adapter != nil {
				err = errors.Join(err, adapter.Close())
			}
			return err
		}
		err = RunSession(ctx, db, fresh, session, receive, supervised(process, opts))
		err = errors.Join(err, adapter.Close())
		if ctx.Err() != nil {
			return errors.Join(err, ctx.Err())
		}
		if !recoveryOnly(err) {
			return err
		}
		facts, e := sessionStore(db, session.Window.Chain, fresh, session.Objects)
		if e != nil {
			return errors.Join(err, e)
		}
		ancestor, changed, e := facts.CommonAncestor(ctx, opts.MaxReorgDepth)
		if e != nil {
			return errors.Join(err, e)
		}
		if !changed {
			return err
		} // bad peer/parent without a captured reorg is not an auto-reset
		if restarts >= opts.MaxRestarts {
			return errors.Join(ErrRebuildRequired, err)
		}
		state, e := facts.State(ctx)
		if e != nil {
			return errors.Join(err, e)
		}
		if e := facts.Reset(ctx, state.Generation, ancestor); e != nil {
			return errors.Join(err, e)
		}
		session.Create = false
		if err := pause(ctx, opts.RestartDelay, nil); err != nil {
			return err
		}
	}
}

func checkRuntimePolicy(ctx context.Context, db port.CaptureDatabase, fresh source.CanonicalReader, chain model.FeedChain, policy FinalityPolicy) error {
	facts, err := New(db, chain, fresh)
	if err != nil {
		return err
	}
	state, err := facts.State(ctx)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	id := ""
	if policy != nil {
		id = policy.ID()
	}
	if (state.FinalityPolicy != "" && state.FinalityPolicy != id) || (state.FinalityPolicy == "" && state.Finalized != nil && id != "") {
		return ErrConflict
	}
	return nil
}

// Only known reorg symptoms may trigger recovery. A joined I/O, corruption,
// finality or shutdown error must not disappear behind errors.Is(ErrCanonical).
func recoveryOnly(err error) bool {
	safe, reorg := recoveryCauses(err)
	return safe && reorg
}

func recoveryCauses(err error) (bool, bool) {
	if err == nil {
		return false, false
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		reorg := false
		for _, e := range many.Unwrap() {
			if e != nil {
				safe, cause := recoveryCauses(e)
				if !safe {
					return false, false
				}
				reorg = reorg || cause
			}
		}
		return true, reorg
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return recoveryCauses(one.Unwrap())
	}
	if err == context.Canceled {
		return true, false
	} // joined cancellation of workers after a reorg
	reorg := err == errReorgObserved || err == ErrCanonical || err == ErrGap
	return reorg, reorg
}

func supervised(process Task, opts RuntimeOptions) Task {
	return func(ctx context.Context, raw *window.Store, facts *Store) error {
		step := func(ctx context.Context) error {
			status, changed, err := ControlStep(ctx, raw, facts, opts.Finality, opts.Collect, opts.MaxReorgDepth)
			if opts.OnControl != nil {
				opts.OnControl(status)
			}
			if err != nil {
				return err
			}
			if changed {
				return errReorgObserved
			}
			return nil
		}
		if err := step(ctx); err != nil {
			return err
		}
		tasks := []Task{process, func(ctx context.Context, _ *window.Store, _ *Store) error {
			ticker := time.NewTicker(opts.ControlInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					if err := step(ctx); err != nil {
						return err
					}
				}
			}
		}}
		names := make([]string, 0, len(opts.Consumers))
		for name := range opts.Consumers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tasks = append(tasks, RawConsumerTask(name, opts.Consumers[name], 20*time.Millisecond))
		}
		child, cancel := context.WithCancel(ctx)
		defer cancel()
		results := make(chan error, len(tasks))
		for _, task := range tasks {
			go func() { results <- task(child, raw, facts) }()
		}
		errs := []error{<-results}
		cancel()
		for i := 1; i < len(tasks); i++ {
			if err := <-results; err != context.Canceled {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}
