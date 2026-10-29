package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

type runtimeIngress struct {
	mu               sync.Mutex
	started, exited  chan struct{}
	stopped, running bool
	fail             <-chan error
	cancel           context.CancelFunc
}

func newRuntimeIngress() *runtimeIngress {
	return &runtimeIngress{started: make(chan struct{}), exited: make(chan struct{})}
}
func (*runtimeIngress) Check(ctx context.Context) error { return ctx.Err() }
func (p *runtimeIngress) Run(parent context.Context) error {
	p.mu.Lock()
	if p.stopped || p.running {
		p.mu.Unlock()
		return port.ErrClosed
	}
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.running = true
	close(p.started)
	p.mu.Unlock()
	defer close(p.exited)
	defer cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-p.fail:
		return err
	}
}
func (p *runtimeIngress) Stop() {
	p.mu.Lock()
	p.stopped = true
	cancel, running := p.cancel, p.running
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if running {
		<-p.exited
	}
}
func (p *runtimeIngress) isStopped() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.stopped }

func TestExecutionRuntimeIngressWaitsForListenerReadyAndConcurrentStopsJoin(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	p := newRuntimeIngress()
	ready := make(chan struct{})
	serving := make(chan struct{})
	serverExited := make(chan struct{})
	r := &executionRuntime{recover: func(context.Context) error { return nil }, relay: func() {}, ready: ready, ingress: p, serve: func(ctx context.Context) error {
		close(serving)
		defer close(serverExited)
		<-ctx.Done()
		return ctx.Err()
	}}
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	defer r.Stop()
	select {
	case <-serving:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-p.started:
		t.Fatal("ingress started before own listener was published")
	default:
	}
	close(ready)
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("ingress did not start after listener readiness")
	}
	var stops sync.WaitGroup
	stops.Go(r.Stop)
	stops.Go(r.Stop)
	stops.Wait()
	require.ErrorIs(t, <-done, context.Canceled)
	select {
	case <-serverExited:
	default:
		t.Fatal("Stop returned before server joined")
	}
	select {
	case <-p.exited:
	default:
		t.Fatal("Stop returned before ingress joined")
	}
	require.True(t, p.isStopped())
	require.ErrorIs(t, r.Run(ctx), port.ErrClosed)
}

func TestExecutionRuntimeIngressFailureCancelsAndJoinsListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	p := newRuntimeIngress()
	fail := make(chan error, 1)
	p.fail = fail
	ready := make(chan struct{})
	close(ready)
	exited := make(chan struct{})
	want := errors.New("owned ingress failed")
	r := &executionRuntime{recover: func(context.Context) error { return nil }, relay: func() {}, ready: ready, ingress: p, serve: func(ctx context.Context) error { defer close(exited); <-ctx.Done(); return ctx.Err() }}
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	defer r.Stop()
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	fail <- want
	require.ErrorIs(t, <-done, want)
	<-exited
	require.True(t, p.isStopped())
}

func TestExecutionRuntimeIngressPreparationIsReleasedWithoutRun(t *testing.T) {
	want := errors.New("recovery failed")
	p := newRuntimeIngress()
	ready := make(chan struct{})
	r := &executionRuntime{recover: func(context.Context) error { return want }, relay: func() {}, ready: ready, ingress: p, serve: func(context.Context) error { return errors.New("served before successful recovery") }}
	require.ErrorIs(t, r.Run(t.Context()), want)
	require.True(t, p.isStopped())
	p = newRuntimeIngress()
	r = &executionRuntime{ingress: p}
	r.Stop()
	require.True(t, p.isStopped())
	require.ErrorIs(t, r.Run(t.Context()), port.ErrClosed)
}

func TestExecutionRuntimeIngressListenerFailureOrCancellationBeforeReadyStartsNothing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		p := newRuntimeIngress()
		ready := make(chan struct{})
		serving := make(chan struct{})
		want := errors.New("listener failed")
		r := &executionRuntime{recover: func(context.Context) error { return nil }, relay: func() {}, ready: ready, ingress: p, serve: func(ctx context.Context) error {
			close(serving)
			if fail {
				return want
			}
			<-ctx.Done()
			return ctx.Err()
		}}
		done := make(chan error, 1)
		go func() { done <- r.Run(ctx) }()
		<-serving
		if !fail {
			cancel()
		}
		err := <-done
		if fail {
			require.ErrorIs(t, err, want)
		} else {
			require.ErrorIs(t, err, context.Canceled)
		}
		cancel()
		r.Stop()
		require.True(t, p.isStopped())
		select {
		case <-p.started:
			t.Fatal("ingress started before readiness")
		default:
		}
	}
}
