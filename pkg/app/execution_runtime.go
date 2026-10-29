package app

import (
	"context"
	"io"
	"sync"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
)

type executionRuntime struct {
	recover          func(context.Context) error
	relay            func()
	serve            func(context.Context) error
	ingress          ingress.Program
	ready            <-chan struct{}
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
	if r.recover == nil || r.relay == nil || r.serve == nil || r.ingress != nil && r.ready == nil {
		r.mu.Unlock()
		if r.ingress != nil {
			r.ingress.Stop()
		}
		return port.ErrInvalidData
	}
	ctx, cancel := context.WithCancel(ctx)
	r.started, r.cancel, r.done = true, cancel, make(chan struct{})
	done := r.done
	r.mu.Unlock()
	defer func() {
		cancel()
		if r.ingress != nil {
			r.ingress.Stop()
		}
		close(done)
	}()
	if err := r.recover(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.ingress != nil {
		if err := r.ingress.Check(ctx); err != nil {
			return err
		}
	}
	r.relay()
	if r.ingress != nil {
		return r.serveOwned(ctx, cancel)
	}
	return r.serve(ctx)
}

func executionEnded(err error) error {
	if err == nil {
		return io.ErrUnexpectedEOF
	}
	return err
}
func (r *executionRuntime) serveOwned(ctx context.Context, cancel context.CancelFunc) error {
	server := make(chan error, 1)
	go func() { server <- r.serve(ctx) }()
	select {
	case err := <-server:
		return executionEnded(err)
	case <-ctx.Done():
		cancel()
		<-server
		return ctx.Err()
	case <-r.ready:
	}
	if err := ctx.Err(); err != nil {
		cancel()
		<-server
		return err
	}
	owner := make(chan error, 1)
	go func() { owner <- r.ingress.Run(ctx) }()
	var first error
	serverDone, ownerDone := false, false
	select {
	case first = <-server:
		serverDone = true
	case first = <-owner:
		ownerDone = true
	case <-ctx.Done():
		first = ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		first = err
	}
	cancel()
	r.ingress.Stop()
	if !serverDone {
		<-server
	}
	if !ownerDone {
		<-owner
	}
	return executionEnded(first)
}
func (r *executionRuntime) Stop() {
	r.mu.Lock()
	r.stopped = true
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if r.ingress != nil {
		r.ingress.Stop()
	}
	if done != nil {
		<-done
	}
}
