package arcingress

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/exporter"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	arcfeed "github.com/0xmhha/indexer-go/x/arc/feed"
	"github.com/ethereum/go-ethereum/common"
	"path/filepath"
	"sync/atomic"
	"time"
)

type nativeProcess interface {
	nativeWriter
	Initial() nativeReady
	Close()
}
type peerProcess interface {
	Subscribe(context.Context, model.FeedBlock, chainfeed.ReadLimits) (chainfeed.Stream, error)
	ObserveCaptured(*window.Store, []window.Reference) error
	Close() error
}
type ownedRound struct {
	p                                           *program
	ctx                                         context.Context
	control                                     *roundRun
	writer                                      nativeProcess
	peer                                        peerProcess
	stream                                      chainfeed.Stream
	nativeStop                                  <-chan struct{}
	ack                                         atomic.Pointer[execwire.Header]
	inputErr                                    error
	captureCancel, watchCancel, collectorCancel context.CancelFunc
	results                                     chan error
	workers, joined                             int
	inputsClosed                                bool
	proposals                                   chan execwire.Ref
	collectorDone                               chan error
	collectorJoined                             bool
	collectorErr                                error
}

func (r *ownedRound) stopInputs() {
	if r.inputsClosed {
		return
	}
	r.inputsClosed = true
	r.control.stopNative()
	if r.captureCancel != nil {
		r.captureCancel()
	}
	if r.watchCancel != nil {
		r.watchCancel()
	}
	if r.stream != nil {
		_ = r.stream.Close()
	}
	for r.joined < r.workers {
		err := <-r.results
		r.recordInputError(err)
		r.joined++
	}
	if r.peer != nil {
		_ = r.peer.Close()
	}
	if r.writer != nil {
		r.writer.Close()
	}
}
func (r *ownedRound) stopCollector() {
	if r.collectorCancel != nil {
		r.collectorCancel()
	}
	if !r.collectorJoined && r.collectorDone != nil {
		r.collectorErr = <-r.collectorDone
		r.collectorJoined = true
	}
}
func (r *ownedRound) close() { r.stopInputs(); r.stopCollector() }
func (r *ownedRound) startCollector() {
	p := r.p
	identity := p.identity
	c := collector.Config{OnCommitted: func(h execwire.Header) { r.ack.Store(&h) }, Native: collector.Options{Executable: p.cfg.Collector.Executable, DataDir: p.cfg.Native.DataDir, ScratchDir: filepath.Dir(p.cfg.Collector.SpoolDir), Chain: p.deps.Control.Chain, Timeout: p.cfg.Timeout, Managed: &identity, StateProofs: p.deps.StateProofs}, Anchor: p.deps.Control.Anchor, SpoolDir: p.cfg.Collector.SpoolDir, Socket: p.deps.Socket, MaxBytes: p.cfg.Collector.MaxBytes, MaxRecords: p.cfg.Collector.MaxRecords, Window: 8, Poll: 100 * time.Millisecond, Retry: 500 * time.Millisecond, Proposals: r.proposals}
	ctx, cancel := context.WithCancel(r.ctx)
	r.collectorCancel = cancel
	r.collectorDone = make(chan error, 1)
	run := p.collectorRun
	if run == nil {
		run = collector.Run
	}
	go func() { r.collectorDone <- run(ctx, c) }()
}
func (p *program) startRoundNative(ctx context.Context) (nativeProcess, error) {
	if p.nativeStart != nil {
		return p.nativeStart(ctx)
	}
	return startNative(ctx, p.cfg.Native.Executable, p.cfg.Native.DataDir, p.cfg.Timeout)
}
func (p *program) startRoundPeer(ctx context.Context, anchor model.FeedBlock) (peerProcess, error) {
	if p.peerStart != nil {
		return p.peerStart(ctx, anchor)
	}
	config, err := arcfeed.OpenPeer(ctx, p.cfg.Peer, anchor, p.deps.Canonical)
	if err != nil {
		return nil, err
	}
	return ethpeer.New(config)
}
func (p *program) initializeRaw(ctx context.Context, ready nativeReady) error {
	release, err := p.active(ctx)
	if err != nil {
		return err
	}
	defer release()
	p.mu.Lock()
	err = p.initializeRawLocked(ctx, ready)
	p.mu.Unlock()
	if err != nil {
		return err
	}
	return p.bindSourceOwner(ctx, ready)
}

// Startup migration and origin snapshots share the same state boundary.
func (p *program) initializeRawLocked(ctx context.Context, ready nativeReady) error {
	p.identity, p.head = ready.Identity, ready.Head
	if p.raw == nil {
		if !p.cfg.CreateWindow || ready.Head.Tip != ready.Identity.Anchor {
			return ErrNative
		}
		if p.cfg.InitializeVolume {
			if err := window.InitializeVolume(p.opts.Volume); err != nil {
				return err
			}
		}
		control := rawControl{Version: 2, Instance: p.identity.Instance, Chain: model.FeedChain{ID: p.deps.Control.Chain.ID, GenesisHash: common.Hash(p.deps.Control.Chain.Genesis)}, Anchor: p.identity.Anchor, RawEpoch: 1, Received: p.identity.Anchor, NativeEpoch: ready.Head.ImportEpoch, Generation: p.deps.Control.Generation}
		state, _ := json.Marshal(control)
		raw, err := window.CreateOwned(p.opts, ownerDomain, state)
		if err != nil {
			return err
		}
		p.raw = raw
		p.control = control
	}
	return p.migrateControl(ctx, ready)
}
func (r *ownedRound) captureAndConsume() error {
	p := r.p
	after := model.FeedBlock{Chain: p.control.Chain, Number: p.control.Received.Height, Hash: common.Hash(p.control.Received.Hash)}
	anchor := model.FeedBlock{Chain: p.control.Chain, Number: p.head.Tip.Height, Hash: common.Hash(p.head.Tip.Hash)}
	captureCtx, captureCancel := context.WithCancel(r.ctx)
	r.captureCancel = captureCancel
	r.control.bindPause(captureCancel)
	peer, err := p.startRoundPeer(captureCtx, anchor)
	if err != nil {
		return err
	}
	r.peer = peer
	stream, err := peer.Subscribe(captureCtx, after, chainfeed.ReadLimits{MaxPayloadBytes: p.opts.MaxPayloadBytes})
	if err != nil {
		return err
	}
	r.stream = stream
	batch, ok := stream.(chainfeed.BatchStream)
	if !ok {
		return ErrNative
	}
	watchCtx, watchCancel := context.WithCancel(r.ctx)
	r.watchCancel = watchCancel
	wake := make(chan struct{}, 1)
	r.workers = 3
	go func() { r.results <- p.capture(captureCtx, batch, wake) }()
	go func() {
		r.results <- p.consumeUntil(r.ctx, r.writer, wake, func(refs []window.Reference) error { return peer.ObserveCaptured(p.raw, refs) }, r.nativeStop)
	}()
	go func() { r.results <- p.watchNative(watchCtx) }()
	select {
	case err := <-r.results:
		r.recordInputError(err)
		r.joined++
		return err
	case err := <-r.collectorDone:
		r.collectorJoined = true
		r.collectorErr = err
		return err
	case <-r.control.stop:
		return port.ErrExecutionRewinding
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}
func (r *ownedRound) propose(ref execwire.Ref) error {
	call, stop := context.WithTimeout(r.ctx, r.p.cfg.Timeout)
	defer stop()
	select {
	case r.proposals <- ref:
	case err := <-r.collectorDone:
		r.collectorJoined = true
		r.collectorErr = err
		return errors.Join(errAwaitCoordinator, err)
	case <-call.Done():
		return errors.Join(errAwaitCoordinator, call.Err())
	}
	select {
	case err := <-r.collectorDone:
		r.collectorJoined = true
		r.collectorErr = err
		var change *exporter.CanonicalChange
		if errors.As(err, &change) && change.Ref != ref {
			return errors.Join(ErrNative, err)
		}
		return errors.Join(errAwaitCoordinator, err)
	case <-r.control.stop:
		r.stopCollector()
		return port.ErrExecutionRewinding
	case <-call.Done():
		return errors.Join(errAwaitCoordinator, call.Err())
	}
}
func (p *program) runRound(ctx context.Context, control *roundRun) error {
	if err := p.stable(ctx); err != nil {
		return err
	}
	round := &ownedRound{p: p, ctx: ctx, control: control, nativeStop: control.nativeAdmission(), results: make(chan error, 3), proposals: make(chan execwire.Ref, 1)}
	defer round.close()
	writer, err := p.startRoundNative(ctx)
	if err != nil {
		return err
	}
	round.writer = writer
	ready := writer.Initial()
	if err = validateReady(ready, p.deps.Control, p.bootstrap); err != nil {
		return err
	}
	if err = p.initializeRaw(ctx, ready); err != nil {
		return err
	}
	select {
	case <-control.stop:
		return port.ErrExecutionRewinding
	default:
	}
	round.startCollector()
	err = round.captureAndConsume()
	reason := err
	round.stopInputs()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.mu.Lock()
	recovering := p.recovering
	request := p.draining
	p.mu.Unlock()
	if request != nil {
		if round.inputErr != nil {
			return round.inputErr
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, port.ErrExecutionRewinding) {
			return err
		}
		return round.drain(request)
	}
	if recovering || errors.Is(err, port.ErrExecutionRewinding) {
		return port.ErrExecutionRewinding
	}
	status, statusErr := p.deps.Store.ExecutionStatus(ctx)
	if statusErr != nil {
		return statusErr
	}
	if status.Rewinding || status.ReorgPending || status.Generation != p.deps.Control.Generation {
		return port.ErrExecutionRewinding
	}
	fork := errors.Is(reason, errRawFork) || errors.Is(reason, errRawMismatch) || errors.Is(reason, capture.ErrCanonical) || errors.Is(reason, source.ErrNotFound)
	if !fork {
		if err == nil {
			return ErrNative
		}
		return err
	}
	// A post-receipt SQL fence can leave memory behind the completed native
	// head. Both DB children still join before any exclusive recovery action.
	var head nativeHead
	if readArtifact(filepath.Join(p.cfg.Native.DataDir, headName), &head) != nil {
		return ErrNative
	}
	if err = checkNativeControls(ctx, p.cfg.Native.DataDir, nativeReady{Version: 1, Identity: p.identity, Head: head}); err != nil {
		return err
	}
	p.mu.Lock()
	p.head = head
	p.mu.Unlock()
	proposal, same, err := p.frozenCanonical(ctx, head.Tip)
	if err != nil {
		return err
	}
	if !same {
		if p.deps.FenceReorg != nil {
			call, stop := context.WithTimeout(ctx, p.cfg.Timeout)
			err = p.deps.FenceReorg(call, proposal)
			stop()
			if err != nil {
				return err
			}
		}
		return round.propose(proposal)
	}
	if errors.Is(reason, errRawMismatch) {
		if err = p.rawMismatchIsFork(ctx); err != nil {
			return err
		}
	}
	round.stopCollector()
	return p.rotateRaw(ctx, head)
}

func (r *ownedRound) recordInputError(err error) {
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, port.ErrExecutionRewinding) {
		r.inputErr = errors.Join(r.inputErr, err)
	}
}
