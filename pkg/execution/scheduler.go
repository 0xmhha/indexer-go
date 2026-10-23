package execution

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"golang.org/x/sync/semaphore"
)

// Backend owns no queue. Part must obey cancellation and return only after its
// transaction is ended. Publish is called in input order after all seven parts.
type Backend interface {
	Part(context.Context, port.ExecutionWriter, *Prepared) error
	Publish(context.Context, *Prepared) error
}
type Config struct {
	Anchor    execwire.Ref
	MaxBlocks int
	MaxBytes  int64
}

var errStopped = errors.New("execution: scheduler closed")

type Scheduler struct {
	ctx             context.Context
	cancel          context.CancelCauseFunc
	backend         Backend
	cfg             Config
	permits         *semaphore.Weighted
	gate            chan struct{}
	intake, ordered chan *job
	queues          []chan *job
	wg              sync.WaitGroup
	done            chan struct{}
	mu              sync.Mutex
	pending         map[*job]struct{}
	used            int64
	tail            execwire.Ref
	changed         chan struct{}
}
type job struct {
	work      *Prepared
	future    *Future
	remaining atomic.Int32
	parts     chan struct{}
}
type Future struct {
	done chan struct{}
	err  error
	once sync.Once
}

func (f *Future) finish(err error) { f.once.Do(func() { f.err = err; close(f.done) }) }
func (f *Future) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.done:
		return f.err
	}
}
func New(parent context.Context, cfg Config, b Backend) (*Scheduler, error) {
	if parent == nil || b == nil || cfg.Anchor.Generation == 0 || cfg.Anchor.Generation > math.MaxInt64 || cfg.Anchor.Height >= math.MaxInt64 || cfg.Anchor.Hash == (execwire.Hash{}) || cfg.MaxBlocks <= 0 || cfg.MaxBlocks > 65536 || cfg.MaxBytes <= 0 || cfg.MaxBytes > 2<<30 {
		return nil, port.ErrInvalidData
	}
	ctx, cancel := context.WithCancelCause(parent)
	s := &Scheduler{ctx: ctx, cancel: cancel, backend: b, cfg: cfg, permits: semaphore.NewWeighted(2), gate: make(chan struct{}, 1), intake: make(chan *job, cfg.MaxBlocks), ordered: make(chan *job, cfg.MaxBlocks), done: make(chan struct{}), pending: map[*job]struct{}{}, tail: cfg.Anchor, changed: make(chan struct{})}
	for _, w := range port.ExecutionWriters() {
		q := make(chan *job, cfg.MaxBlocks)
		s.queues = append(s.queues, q)
		s.wg.Go(func() { s.worker(w, q) })
	}
	s.wg.Go(s.dispatch)
	s.wg.Go(s.publish)
	go func() {
		s.wg.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		err := context.Cause(s.ctx)
		for j := range s.pending {
			j.future.finish(err)
		}
		clear(s.pending)
		s.used = 0
		close(s.changed)
		close(s.done)
	}()
	return s, nil
}

// Submit transfers whole-block ownership on success. Cancelling a Future wait
// does not cancel one queue or undo accepted work. Budget includes active jobs.
func (s *Scheduler) Submit(ctx context.Context, p *Prepared) (*Future, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || p.bytes <= 0 || p.bytes > s.cfg.MaxBytes {
		return nil, execwire.ErrLimit
	}
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Cause(s.ctx)
	}
	defer func() { <-s.gate }()
	for {
		s.mu.Lock()
		if err := context.Cause(s.ctx); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if p.result.Ref.Generation != s.tail.Generation {
			s.mu.Unlock()
			return nil, port.ErrExecutionStale
		}
		if p.result.Ref.Height <= s.tail.Height {
			for pending := range s.pending {
				if pending.work.result.Ref.Height != p.result.Ref.Height {
					continue
				}
				if pending.work.result.Ref != p.result.Ref || pending.work.result.Parent != p.result.Parent || pending.work.digest != p.digest {
					s.mu.Unlock()
					return nil, port.ErrExecutionConflict
				}
				future := pending.future
				s.mu.Unlock()
				return future, nil
			}
			s.mu.Unlock()
			return nil, port.ErrExecutionSequence
		}
		if p.result.Ref.Height != s.tail.Height+1 || p.result.Parent != s.tail.Hash {
			s.mu.Unlock()
			return nil, port.ErrExecutionSequence
		}
		if len(s.pending) < s.cfg.MaxBlocks && s.used <= s.cfg.MaxBytes-p.bytes {
			j := &job{work: p, future: &Future{done: make(chan struct{})}, parts: make(chan struct{})}
			j.remaining.Store(7)
			s.pending[j] = struct{}{}
			s.used += p.bytes
			s.tail = p.result.Ref
			// Intake capacity equals the entire active budget, so this send cannot
			// block while holding mu and the scheduler owns subsequent dispatch.
			s.intake <- j
			s.mu.Unlock()
			return j.future, nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.ctx.Done():
			return nil, context.Cause(s.ctx)
		}
	}
}
func (s *Scheduler) dispatch() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case j := <-s.intake:
			for _, q := range s.queues {
				select {
				case q <- j:
				case <-s.ctx.Done():
					return
				}
			}
			select {
			case s.ordered <- j:
			case <-s.ctx.Done():
				return
			}
		}
	}
}
func (s *Scheduler) worker(w port.ExecutionWriter, q <-chan *job) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case j := <-q:
			if err := s.permits.Acquire(s.ctx, 1); err != nil {
				return
			}
			if err := s.ctx.Err(); err != nil {
				s.permits.Release(1)
				return
			}
			err := s.backend.Part(s.ctx, w, j.work)
			s.permits.Release(1)
			if err != nil {
				s.cancel(err)
				return
			}
			if j.remaining.Add(-1) == 0 {
				close(j.parts)
			}
		}
	}
}
func (s *Scheduler) publish() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case j := <-s.ordered:
			select {
			case <-j.parts:
			case <-s.ctx.Done():
				return
			}
			if err := s.ctx.Err(); err != nil {
				return
			}
			if err := s.backend.Publish(s.ctx, j.work); err != nil {
				s.cancel(err)
				return
			}
			s.mu.Lock()
			delete(s.pending, j)
			s.used -= j.work.bytes
			j.future.finish(nil)
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
		}
	}
}
func (s *Scheduler) Close() error {
	s.cancel(errStopped)
	<-s.done
	if err := context.Cause(s.ctx); !errors.Is(err, errStopped) {
		return err
	}
	return nil
}
