package arcingress

import (
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	arcfeed "github.com/0xmhha/indexer-go/x/arc/feed"
	"github.com/ethereum/go-ethereum/common"
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
	p.mu.Lock()
	if p.started || p.stopped {
		p.mu.Unlock()
		return port.ErrClosed
	}
	ctx, cancel := context.WithCancel(parent)
	p.started = true
	p.cancel = cancel
	p.done = make(chan struct{})
	done := p.done
	p.mu.Unlock()
	defer func() { cancel(); p.closeRaw(); close(done) }()
	w, err := startNative(ctx, p.cfg.Native.Executable, p.cfg.Native.DataDir, p.cfg.Timeout)
	if err != nil {
		return err
	}
	defer w.Close()
	if err = validateReady(w.ready, p.deps.Control, p.bootstrap); err != nil {
		return err
	}
	p.mu.Lock()
	p.identity, p.head = w.ready.Identity, w.ready.Head
	p.mu.Unlock()
	if p.raw == nil {
		if !p.cfg.CreateWindow || w.head.Tip != w.ready.Identity.Anchor {
			return ErrNative
		}
		if p.cfg.InitializeVolume {
			if err = window.InitializeVolume(p.opts.Volume); err != nil {
				return err
			}
		}
		control := rawControl{Version: 1, Instance: p.identity.Instance, Chain: model.FeedChain{ID: p.deps.Control.Chain.ID, GenesisHash: common.Hash(p.deps.Control.Chain.Genesis)}, Anchor: p.identity.Anchor, RawEpoch: 1, Received: p.identity.Anchor}
		state, _ := json.Marshal(control)
		p.raw, err = window.CreateOwned(p.opts, ownerDomain, state)
		if err != nil {
			return err
		}
		p.control = control
	}
	if err = p.reconcile(ctx); err != nil {
		return err
	}
	after := model.FeedBlock{Chain: p.control.Chain, Number: p.control.Received.Height, Hash: common.Hash(p.control.Received.Hash)}
	anchor := model.FeedBlock{Chain: p.control.Chain, Number: p.head.Tip.Height, Hash: common.Hash(p.head.Tip.Hash)}
	peer, err := arcfeed.OpenPeer(ctx, p.cfg.Peer, anchor, p.deps.Canonical)
	if err != nil {
		return err
	}
	client, err := ethpeer.New(peer)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	stream, err := client.Subscribe(ctx, after, chainfeed.ReadLimits{MaxPayloadBytes: p.opts.MaxPayloadBytes})
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	batch, ok := stream.(chainfeed.BatchStream)
	if !ok {
		return ErrNative
	}
	wake := make(chan struct{}, 1)
	results := make(chan error, 2)
	go func() { results <- p.capture(ctx, batch, wake) }()
	go func() {
		results <- p.consume(ctx, w, wake, func(refs []window.Reference) error { return client.ObserveCaptured(p.raw, refs) })
	}()
	err = <-results
	cancel()
	_ = stream.Close()
	_ = client.Close()
	<-results
	return err
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
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	} else {
		p.closeRaw()
	}
}
