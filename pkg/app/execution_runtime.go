package app

import (
	"context"
	"sync"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type executionRuntime struct {
	recover          func(context.Context) error
	relay            func()
	serve            func(context.Context) error
	mu               sync.Mutex
	started, stopped bool
	cancel           context.CancelFunc
	done             chan struct{}
}

func (r *executionRuntime) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return port.ErrClosed
	}
	if r.recover == nil || r.relay == nil || r.serve == nil {
		r.mu.Unlock()
		return port.ErrInvalidData
	}
	ctx, cancel := context.WithCancel(ctx)
	r.started, r.cancel, r.done = true, cancel, make(chan struct{})
	done := r.done
	r.mu.Unlock()
	defer close(done)
	defer cancel()
	if err := r.recover(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.relay()
	return r.serve(ctx)
}
func (r *executionRuntime) Stop() {
	r.mu.Lock()
	r.stopped = true
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
