package execution

import (
	"context"
	"sync"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type Limits struct {
	MaxBlocks int
	MaxBytes  int64
}
type ControllerStore interface {
	port.ExecutionStore
	port.ExecutionRewindStore
}

// Controller owns queue generations. A durable rewind marker precedes joining
// old work; recovery validates storage rather than declaring abandoned work
// successful. Every old Future retains its own failure.
type Controller struct {
	ctx               context.Context
	cancel            context.CancelFunc
	limits            Limits
	store             ControllerStore
	backend           Backend
	recover           func(context.Context) error
	life              chan struct{}
	mu                sync.Mutex
	scheduler         *Scheduler
	rewinding, closed bool
	closeOnce         sync.Once
}

func NewController(ctx context.Context, limits Limits, store ControllerStore, backend Backend, recover func(context.Context) error) (*Controller, error) {
	if ctx == nil || store == nil || backend == nil || recover == nil {
		return nil, port.ErrInvalidData
	}
	owned, cancel := context.WithCancel(ctx)
	c := &Controller{ctx: owned, cancel: cancel, limits: limits, store: store, backend: backend, recover: recover, life: make(chan struct{}, 1)}
	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	status, err := store.ExecutionStatus(owned)
	if err != nil {
		return nil, err
	}
	if status.Rewinding {
		if err := recover(owned); err != nil {
			return nil, err
		}
	}
	if err := c.start(owned); err != nil {
		return nil, err
	}
	success = true
	return c, nil
}
func (c *Controller) start(ctx context.Context) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	status, err := c.store.ExecutionStatus(ctx)
	if err != nil {
		return err
	}
	if status.Rewinding {
		return port.ErrExecutionRewinding
	}
	if !status.BaselineReady {
		return port.ErrExecutionCoverage
	}
	s, err := New(c.ctx, Config{Anchor: status.Visible, MaxBlocks: c.limits.MaxBlocks, MaxBytes: c.limits.MaxBytes}, c.backend)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = s.Close()
		return port.ErrClosed
	}
	c.scheduler = s
	c.rewinding = false
	c.mu.Unlock()
	return nil
}
func (c *Controller) Submit(ctx context.Context, p *Prepared) (*Future, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, port.ErrClosed
	}
	if c.rewinding || c.scheduler == nil {
		c.mu.Unlock()
		return nil, port.ErrExecutionRewinding
	}
	s := c.scheduler
	c.mu.Unlock()
	// Do not hold the lifecycle gate while waiting for queue capacity: rewind
	// must be able to cancel this very admission and all accepted old work.
	return s.Submit(ctx, p)
}
func (c *Controller) ownedCall(ctx context.Context) (context.Context, func()) {
	work, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	stop := context.AfterFunc(c.ctx, func() { cancel(); close(done) })
	return work, func() {
		cancel()
		if !stop() {
			<-done
		}
	}
}
func (c *Controller) Rewind(ctx context.Context, target execwire.Ref, root execwire.Hash) error {
	work, finish := c.ownedCall(ctx)
	select {
	case c.life <- struct{}{}:
	case <-work.Done():
		finish()
		return work.Err()
	}
	defer func() { finish(); <-c.life }()
	if err := work.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return port.ErrClosed
	}
	c.rewinding = true
	old := c.scheduler
	c.mu.Unlock()
	_, err := c.store.BeginExecutionRewind(work, target, root)
	if old != nil {
		_ = old.Close()
	}
	c.mu.Lock()
	c.scheduler = nil
	c.mu.Unlock()
	// Even an unsuccessful rewind request stops local admission. It must be
	// retried with an explicitly validated target or the owner must shut down.
	if err != nil {
		return err
	}
	if err := c.recover(work); err != nil {
		return err
	}
	return c.start(work)
}
func (c *Controller) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.cancel()
		c.life <- struct{}{}
		c.mu.Lock()
		s := c.scheduler
		c.scheduler = nil
		c.mu.Unlock()
		if s != nil {
			_ = s.Close()
		}
		<-c.life
	})
	return nil
}
