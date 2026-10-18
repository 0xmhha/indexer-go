package capture

import (
	"context"
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
)

type SessionOptions struct {
	RequiredSupplement *model.SupplementStreamDescriptor
	SupplementVerifier SupplementVerifier
	Window             window.Options
	Objects            *window.Objects // optional borrowed permanent area; caller closes after join
	Plan               model.CapturePlan
	Anchor             model.FeedBlock
	RawConsumers       []string
	// Create explicitly initializes an empty raw window. Missing storage on
	// restart is never recreated, and volume initialization remains separate.
	Create        bool
	UsageInterval time.Duration      // zero defaults to 10s for managed volumes
	OnUsage       func(window.Usage) // optional, synchronous; must return promptly
	OnObjectUsage func(window.Usage)
}

// Task owns its streams/leases and must release them and return when ctx is
// cancelled, including joining goroutines it spawned. It may not close raw or
// db. Production acquisition/assembly and chain finality policies are
// separate, explicitly supplied composition. An intake Task can use AppendWait
// to retain one input during capacity pressure while processing continues.
type Task func(ctx context.Context, raw *window.Store, facts *Store) error

// RunSession opens/checks storage and recovers the exact pending deletion
// BEFORE either worker starts. The DB is borrowed, the window is owned. When
// either worker returns, cancel and join BOTH (and the managed-volume monitor)
// before closing the window and
// releasing its process lock. Durable raw/facts cursors survive cancellation;
// this function makes no new RPC client, background GC or implicit restart.
func RunSession(ctx context.Context, db port.CaptureDatabase, fresh source.CanonicalReader, opts SessionOptions, receive, process Task) error {
	if receive == nil || process == nil {
		return port.ErrInvalidData
	}
	if opts.UsageInterval < 0 {
		return port.ErrInvalidData
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	facts, err := sessionStore(db, opts.Window.Chain, fresh, opts.Objects)
	if err != nil {
		return err
	}
	facts = facts.WithSupplementVerifier(opts.SupplementVerifier)
	// Validate the DB/config before creating any directory. Initialize also
	// preserves fresh genesis/checkpoint validation through the shared reader.
	if err := facts.Initialize(ctx, opts.Plan, opts.Anchor, opts.RawConsumers); err != nil {
		return err
	}
	if err := facts.BindRequiredSupplement(ctx, opts.RequiredSupplement); err != nil {
		return err
	}
	if opts.Objects != nil {
		u, e := opts.Objects.Usage(ctx)
		if e != nil {
			return e
		}
		if opts.OnObjectUsage != nil {
			opts.OnObjectUsage(u)
		}
	}
	var raw *window.Store
	if opts.Create {
		st, e := facts.State(ctx)
		if e != nil {
			return e
		}
		if st.PendingDelete != (common.Hash{}) {
			return ErrDeletePending
		}
		raw, err = window.Create(opts.Window)
		if err == nil {
			if e := facts.RecoverDeletion(ctx, raw); e != nil {
				return errors.Join(e, raw.Close())
			}
		}
	} else {
		raw, err = facts.OpenWindow(ctx, opts.Window)
	}
	if err != nil {
		return err
	}
	managed := opts.Window.Volume.MaxBytes > 0
	if managed {
		u, e := raw.Usage(ctx)
		if e != nil {
			return errors.Join(e, raw.Close())
		}
		if opts.OnUsage != nil {
			opts.OnUsage(u)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 3)
	go func() {
		if managed {
			if e := raw.WaitWritable(runCtx); e != nil {
				results <- e
				return
			}
		}
		results <- receive(runCtx, raw, facts)
	}()
	go func() { results <- process(runCtx, raw, facts) }()
	workers := 2
	if managed || opts.Objects != nil {
		workers++
		interval := opts.UsageInterval
		if interval == 0 {
			interval = 10 * time.Second
		}
		go func() {
			timer := time.NewTicker(interval)
			defer timer.Stop()
			for {
				select {
				case <-runCtx.Done():
					results <- runCtx.Err()
					return
				case <-timer.C:
					if managed {
						u, e := raw.Usage(runCtx)
						if e != nil {
							results <- e
							return
						}
						if opts.OnUsage != nil {
							opts.OnUsage(u)
						}
					}
					if opts.Objects != nil {
						u, e := opts.Objects.Usage(runCtx)
						if e != nil {
							results <- e
							return
						}
						if opts.OnObjectUsage != nil {
							opts.OnObjectUsage(u)
						}
					}
				}
			}
		}()
	}
	first := <-results
	cancel()
	// Cancellation of the peer is a consequence, not another failure. Keep
	// independent worker errors and the caller's cancellation/deadline.
	if first == context.Canceled && ctx.Err() == nil {
		first = nil
	}
	errs := []error{first}
	for i := 1; i < workers; i++ {
		e := <-results
		if e != context.Canceled {
			errs = append(errs, e)
		}
	}
	errs = append(errs, ctx.Err(), raw.Close())
	return errors.Join(errs...)
}

func sessionStore(db port.CaptureDatabase, chain model.FeedChain, fresh source.CanonicalReader, objects *window.Objects) (*Store, error) {
	if objects != nil {
		return NewWithObjects(db, chain, fresh, objects)
	}
	return New(db, chain, fresh)
}
