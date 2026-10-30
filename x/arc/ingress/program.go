package arcingress

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"golang.org/x/sync/semaphore"
	"sync"
)

type program struct {
	cfg              ingress.Config
	deps             ingress.Dependencies
	opts             window.Options
	mu               sync.Mutex
	raw              *window.Store
	control          rawControl
	head             nativeHead
	identity         nativeIdentity
	bootstrap        execwire.Hash
	slots            chan struct{}
	bytes            *semaphore.Weighted
	started, stopped bool
	cancel           context.CancelFunc
	done             chan struct{}
	recoveryMu       sync.Mutex
	recoveryCancel   context.CancelFunc
	recovering       bool
	recoveryMarker   port.ExecutionRewind
	round            *roundRun
	resume           chan struct{}
	transitionNative func(context.Context, nativeinput.TransitionRequest) (nativeinput.Transition, error)
	nativeStart      func(context.Context) (nativeProcess, error)
	peerStart        func(context.Context, model.FeedBlock) (peerProcess, error)
	collectorRun     func(context.Context, collector.Config) error
}

func activeBytes(o window.Options) int64 { return 8*o.MaxPayloadBytes + (1 << 20) }
func (p *program) active(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.slots <- struct{}{}:
	}
	n := activeBytes(p.opts)
	if err := p.bytes.Acquire(ctx, n); err != nil {
		<-p.slots
		return nil, err
	}
	return func() { p.bytes.Release(n); <-p.slots }, nil
}
func (p *program) Run(parent context.Context) error {
	if err := p.Check(parent); err != nil {
		return err
	}
	return p.runOwnedLoop(parent, p.runRound)
}
func (p *program) closeRaw() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.raw != nil {
		_ = p.raw.Close()
		p.raw = nil
	}
}
func (p *program) Stop() {
	p.mu.Lock()
	p.stopped = true
	cancel, done := p.cancel, p.done
	recoveryCancel, round := p.recoveryCancel, p.round
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if recoveryCancel != nil {
		recoveryCancel()
	}
	if round != nil {
		round.pause()
	}
	if done != nil {
		<-done
	}
	p.recoveryMu.Lock()
	defer p.recoveryMu.Unlock()
	if done == nil {
		p.closeRaw()
	}
}
