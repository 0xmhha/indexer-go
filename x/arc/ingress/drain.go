package arcingress

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
)

var errParked = errors.New("arc ingress: workers drained for explicit maintenance")

type drainResult struct {
	point ingress.DrainPoint
	err   error
}
type drainRequest struct {
	ctx    context.Context
	result chan drainResult
	point  ingress.DrainPoint
	parked bool // protected by program.mu
}

// Drain keeps Run (and therefore the receiver and SQL owner) alive. Only a
// fully joined round may acknowledge the request. Cancellation is a hard stop;
// callers must recover retained pending evidence before starting again.
func (p *program) Drain(parent context.Context) (ingress.DrainPoint, error) {
	p.recoveryMu.Lock()
	p.mu.Lock()
	if !p.started || p.stopped || p.round == nil || p.draining != nil || p.recovering {
		p.mu.Unlock()
		p.recoveryMu.Unlock()
		return ingress.DrainPoint{}, port.ErrClosed
	}
	runCtx, round := p.runCtx, p.round
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, p.cfg.Timeout)
	defer cancel()
	c, err := p.currentControl(ctx)
	if err == nil && c.Visible.Height <= c.Anchor.Height {
		err = port.ErrExecutionCoverage
	}
	if err != nil {
		p.recoveryMu.Unlock()
		return ingress.DrainPoint{}, err
	}
	request := &drainRequest{ctx: ctx, result: make(chan drainResult, 1)}
	p.mu.Lock()
	if p.stopped || p.round != round || p.recovering || runCtx.Err() != nil {
		p.mu.Unlock()
		p.recoveryMu.Unlock()
		return ingress.DrainPoint{}, port.ErrClosed
	}
	p.draining = request
	round.pause() // synchronous admission fence; active Import keeps runCtx
	p.mu.Unlock()
	var result drainResult
	select {
	case result = <-request.result:
	case <-ctx.Done():
		result.err = ctx.Err()
	case <-runCtx.Done():
		result.err = runCtx.Err()
	}
	if result.err != nil {
		p.cancel()
	}
	p.recoveryMu.Unlock() // Run's cleanup also needs this mutex. Never join under it.
	if result.err != nil {
		p.Stop()
		return ingress.DrainPoint{}, result.err
	}
	return result.point, nil
}

func (p *program) Resume(ctx context.Context) error {
	p.maintenanceMu.Lock()
	defer p.maintenanceMu.Unlock()
	p.recoveryMu.Lock()
	defer p.recoveryMu.Unlock()
	if err := p.maintenanceGate(ctx); err != nil {
		return err
	}
	return p.resumeLocked(ctx)
}
func (p *program) resumeLocked(ctx context.Context) error {
	p.mu.Lock()
	request := p.draining
	valid := request != nil && request.parked && p.started && !p.stopped && !p.recovering
	p.mu.Unlock()
	if !valid {
		return port.ErrClosed
	}
	h := execwire.Header{Version: execwire.Version, Chain: request.point.Chain, Ref: request.point.Ref, Digest: request.point.Digest}
	ok, err := p.drainCoverage(ctx, nativeRef{Height: h.Ref.Height, Hash: h.Ref.Hash, Root: request.point.Root}, h)
	if err != nil {
		return err
	}
	if !ok {
		return port.ErrExecutionCoverage
	}
	if err = p.checkPrepared(ctx, true); err != nil {
		p.closeRaw()
		return err
	}
	p.mu.Lock()
	if p.stopped || p.runCtx.Err() != nil {
		p.mu.Unlock()
		p.closeRaw()
		return port.ErrClosed
	}
	p.originsClosing = false
	p.draining = nil
	p.mu.Unlock()
	select {
	case p.resume <- struct{}{}:
	default:
	}
	return nil
}

func (p *program) drainCoverage(ctx context.Context, n nativeRef, ack execwire.Header) (bool, error) {
	c, err := p.currentControl(ctx)
	if err != nil {
		return false, err
	}
	if c.Visible.Height > n.Height {
		return false, port.ErrExecutionConflict
	}
	if c.Visible.Height < n.Height {
		return false, nil
	}
	if c.Visible.Hash != n.Hash || c.Root != n.Root || c.Visible.Generation != c.Generation {
		return false, port.ErrExecutionConflict
	}
	if ack.Chain != c.Chain || ack.Ref != c.Visible || ack.Digest == (execwire.Hash{}) {
		return false, nil
	}
	store, ok := p.deps.Store.(interface {
		ExecutionCursor(context.Context, port.ExecutionWriter) (port.ExecutionPart, error)
		ExecutionParts(context.Context, uint64) ([]port.ExecutionPart, error)
	})
	if !ok {
		return false, port.ErrInvalidData
	}
	parts, err := store.ExecutionParts(ctx, n.Height)
	if err != nil {
		return false, err
	}
	if len(parts) != len(port.ExecutionWriters()) {
		return false, port.ErrExecutionCoverage
	}
	seen := make(map[port.ExecutionWriter]bool, len(parts))
	var parent execwire.Hash
	for i, part := range parts {
		if part.Ref != ack.Ref || part.Digest != ack.Digest || part.Parent == (execwire.Hash{}) || seen[part.Writer] {
			return false, port.ErrExecutionConflict
		}
		if i == 0 {
			parent = part.Parent
		}
		if part.Parent != parent {
			return false, port.ErrExecutionConflict
		}
		seen[part.Writer] = true
	}
	for _, w := range port.ExecutionWriters() {
		if !seen[w] {
			return false, port.ErrExecutionCoverage
		}
		cursor, e := store.ExecutionCursor(ctx, w)
		if e != nil {
			return false, e
		}
		if cursor.Writer != w || cursor.Ref != ack.Ref || cursor.Digest != ack.Digest || cursor.Parent != parent {
			return false, port.ErrExecutionConflict
		}
	}
	after, err := p.currentControl(ctx)
	if err != nil {
		return false, err
	}
	if after != c {
		return false, port.ErrExecutionStale
	}
	return true, nil
}

func (r *ownedRound) drain(request *drainRequest) error {
	// stopInputs has joined the active Import, so inspect the actual sealed head
	// even if its reply was lost and memory did not advance.
	var head nativeHead
	if readArtifact(filepath.Join(r.p.cfg.Native.DataDir, headName), &head) != nil {
		return ErrNative
	}
	if err := checkNativeControls(request.ctx, r.p.cfg.Native.DataDir, nativeReady{Version: 1, Identity: r.p.identity, Head: head}); err != nil {
		return err
	}
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		ack := r.ack.Load()
		var h execwire.Header
		if ack != nil {
			h = *ack
		}
		ok, err := r.p.drainCoverage(request.ctx, head.Tip, h)
		if err != nil {
			return err
		}
		if ok {
			r.stopCollector()
			if r.collectorErr != nil && !errors.Is(r.collectorErr, context.Canceled) {
				return r.collectorErr
			}
			r.p.mu.Lock()
			r.p.head = head
			request.point = ingress.DrainPoint{Chain: h.Chain, Ref: h.Ref, Root: head.Tip.Root, Digest: h.Digest}
			r.p.mu.Unlock()
			return errParked
		}
		select {
		case <-request.ctx.Done():
			return request.ctx.Err()
		case <-r.ctx.Done():
			return r.ctx.Err()
		case err := <-r.collectorDone:
			r.collectorJoined = true
			r.collectorErr = err
			return errors.Join(ErrNative, err)
		case <-poll.C:
		}
	}
}
