package arcingress

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"io"
	"time"
)

var errAwaitCoordinator = errors.New("arc ingress: awaiting existing coordinator recovery")
var errRestartRound = errors.New("arc ingress: restart from verified raw/native boundary")

func (p *program) waitResume(ctx context.Context) error {
	timeout := p.cfg.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	timer := time.NewTimer(3 * timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.Join(port.ErrExecutionRewinding, context.DeadlineExceeded)
	case <-p.resume:
		return nil
	}
}
func (p *program) runOwnedLoop(parent context.Context, run func(context.Context, *roundRun) error) error {
	p.mu.Lock()
	if p.started || p.stopped {
		p.mu.Unlock()
		return port.ErrClosed
	}
	ctx, cancel := context.WithCancel(parent)
	p.started = true
	p.cancel = cancel
	p.done = make(chan struct{})
	p.resume = make(chan struct{}, 1)
	done := p.done
	p.mu.Unlock()
	defer func() { cancel(); p.recoveryMu.Lock(); p.closeRaw(); p.recoveryMu.Unlock(); close(done) }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		r := &roundRun{stop: make(chan struct{}), done: make(chan struct{})}
		p.mu.Lock()
		p.round = r
		p.mu.Unlock()
		err := run(ctx, r)
		close(r.done)
		p.mu.Lock()
		p.round = nil
		recovering := p.recovering
		p.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errRestartRound) && !recovering {
			continue
		}
		if recovering || errors.Is(err, errAwaitCoordinator) || errors.Is(err, port.ErrExecutionRewinding) {
			if resumed := p.waitResume(ctx); resumed != nil {
				return errors.Join(err, resumed)
			}
			continue
		}
		if err == nil {
			return io.ErrUnexpectedEOF
		}
		return err
	}
}
