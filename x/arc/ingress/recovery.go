package arcingress

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type roundRun struct {
	stop, done  chan struct{}
	once        sync.Once
	pauseMu     sync.Mutex
	pauseCancel context.CancelFunc
}

func (r *roundRun) pause() {
	r.once.Do(func() { close(r.stop) })
	r.pauseMu.Lock()
	cancel := r.pauseCancel
	r.pauseMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (r *roundRun) bindPause(cancel context.CancelFunc) {
	r.pauseMu.Lock()
	r.pauseCancel = cancel
	r.pauseMu.Unlock()
	select {
	case <-r.stop:
		cancel()
	default:
	}
}
func (p *program) quiesce(ctx context.Context) error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return port.ErrClosed
	}
	p.recovering = true
	r := p.round
	p.mu.Unlock()
	p.joinOrigins(false)
	if r != nil {
		r.pause()
		select {
		case <-r.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}
func (p *program) recoveryFence(ctx context.Context, m port.ExecutionRewind) (port.ExecutionControl, error) {
	c, err := p.deps.Store.ExecutionStatus(ctx)
	if err != nil {
		return c, err
	}
	if c.Maintenance {
		return c, port.ErrExecutionMaintenance
	}
	if !c.Rewinding || c.ReorgPending || c.Generation != m.Target.Generation || c.Visible.Generation != c.Generation || c.Visible.Height != m.From.Height || c.Visible.Hash != m.From.Hash || c.Root != m.FromRoot || c.ExecutionConfig != p.deps.Control.ExecutionConfig || !c.BaselineReady {
		return c, port.ErrExecutionStale
	}
	store, ok := p.deps.Store.(interface {
		ExecutionRewindStatus(context.Context) (port.ExecutionRewind, error)
	})
	if !ok {
		return c, port.ErrInvalidData
	}
	marker, err := store.ExecutionRewindStatus(ctx)
	if err != nil {
		return c, err
	}
	if marker != m {
		return c, port.ErrExecutionStale
	}
	return c, nil
}
func (p *program) bootstrapFor(c port.ExecutionControl) (execwire.Hash, error) {
	bytes, err := privateBytes(filepath.Join(p.cfg.Native.DataDir, nativeinput.BootstrapName))
	if err != nil {
		return execwire.Hash{}, err
	}
	var b bootstrapReceipt
	if strictJSON(bytes, &b) != nil || b.Version != 1 || b.ArcRevision != ArcRevision || b.RethRevision != RethRevision || b.Anchor.Version != 1 || b.Anchor.Coverage != "full" || b.Anchor.Chain != c.Chain || b.Anchor.Ref != c.Anchor || b.Anchor.Root != c.AnchorRoot || b.Floor != c.Anchor.Height || b.Next != c.Anchor.Height+1 || b.StateDigest == (execwire.Hash{}) {
		return execwire.Hash{}, ErrNative
	}
	return execwire.Hash(sha256.Sum256(bytes)), nil
}
func nativeIdentityMatches(i nativeIdentity, c port.ExecutionControl, digest execwire.Hash) bool {
	return nativeinput.ValidIdentity(i) == nil && c.Chain.ID == "5042002" && i.Genesis == c.Chain.Genesis && i.BootstrapDigest == digest && i.Anchor == (nativeRef{Height: c.Anchor.Height, Hash: c.Anchor.Hash, Root: c.AnchorRoot})
}
func (p *program) recoveryRequest(c port.ExecutionControl, m port.ExecutionRewind) (nativeinput.TransitionRequest, error) {
	var request nativeinput.TransitionRequest
	digest, err := p.bootstrapFor(c)
	if err != nil {
		return request, err
	}
	var identity nativeIdentity
	var head nativeHead
	if readArtifact(filepath.Join(p.cfg.Native.DataDir, identityName), &identity) != nil || readArtifact(filepath.Join(p.cfg.Native.DataDir, headName), &head) != nil || !nativeIdentityMatches(identity, c, digest) {
		return request, ErrNative
	}
	if p.identity.Instance != (execwire.Hash{}) && p.identity != identity {
		return request, ErrNative
	}
	request = nativeinput.TransitionRequest{Version: 1, Identity: identity, Previous: head, Marker: m}
	pending := filepath.Join(p.cfg.Native.DataDir, ".arc-native-import.pending")
	_, err = os.Lstat(pending)
	if err == nil {
		var intent struct {
			Version    uint8
			Transition nativeinput.Transition
		}
		if readArtifact(pending, &intent) != nil || intent.Version != 3 || intent.Transition.Identity != identity || intent.Transition.Marker != m || nativeinput.ValidateTransition(intent.Transition, identity) != nil || head != intent.Transition.Previous && head != intent.Transition.Head {
			return request, ErrNative
		}
		request.Previous = intent.Transition.Previous
	} else if !errors.Is(err, os.ErrNotExist) {
		return request, err
	} else if head.ImportEpoch > 1 {
		var t nativeinput.Transition
		if readArtifact(filepath.Join(p.cfg.Native.DataDir, ".arc-native-import-epochs", strconv.FormatUint(head.ImportEpoch, 10)+".json"), &t) != nil || nativeinput.ValidateTransition(t, identity) != nil {
			return request, ErrNative
		}
		if t.Marker == m {
			if t.Head != head {
				return request, ErrNative
			}
			request.Previous = t.Previous
		} else if m.RequestGeneration < t.Marker.Target.Generation {
			return request, ErrNative
		}
	}
	if _, err = nativeinput.ExpectedTransition(request); err != nil {
		return request, err
	}
	p.bootstrap = digest
	return request, nil
}
func (p *program) rawRecoveryProof(ctx context.Context, request nativeinput.TransitionRequest, result nativeinput.Transition) (rawControl, bool, error) {
	if p.raw == nil {
		var err error
		p.raw, err = window.OpenOwnedChecked(p.opts, ownerDomain, func(window.DeletionRecord) error { return window.ErrUnsafeGC })
		if err != nil {
			return rawControl{}, false, err
		}
	}
	owner, err := p.raw.ReadOwner(ctx)
	if err != nil {
		return rawControl{}, false, err
	}
	var c rawControl
	if strictJSON(owner.State, &c) != nil {
		return c, false, ErrNative
	}
	head, generation := request.Previous, request.Marker.RequestGeneration
	completed := c.Version == 2 && c.Generation == request.Marker.Target.Generation && c.NativeEpoch == result.Head.ImportEpoch && c.Received == result.Head.Tip
	if completed {
		head = result.Head
		generation = request.Marker.Target.Generation
	}
	deps := p.deps
	deps.Control.Generation = generation
	deps.Control.Visible.Generation = generation
	deps.Control.Rewinding = false
	deps.Control.ReorgPending = false
	proof := &program{cfg: p.cfg, deps: deps, opts: p.opts, raw: p.raw, identity: request.Identity, head: head, control: c}
	if err = proof.reconcile(ctx); err != nil {
		return c, false, err
	}
	if !completed && proof.control.RawEpoch >= math.MaxInt64 {
		return c, false, ErrNative
	}
	return proof.control, completed, nil
}
func (p *program) Recover(parent context.Context, m port.ExecutionRewind) error {
	p.recoveryMu.Lock()
	defer p.recoveryMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return port.ErrClosed
	}
	p.recoveryCancel = cancel
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.recoveryCancel = nil; p.mu.Unlock() }()
	if err := p.quiesce(ctx); err != nil {
		return err
	}
	release, err := p.active(ctx)
	if err != nil {
		return err
	}
	defer release()
	c, err := p.recoveryFence(ctx, m)
	if err != nil {
		return err
	}
	if err = checkPaths(p.cfg, p.opts, p.deps.Socket); err != nil {
		return err
	}
	request, err := p.recoveryRequest(c, m)
	if err != nil {
		return err
	}
	expected, err := nativeinput.ExpectedTransition(request)
	if err != nil {
		return err
	}
	old, completed, err := p.rawRecoveryProof(ctx, request, expected)
	if err != nil {
		return err
	}
	// Admission is borrowed from the same budget. SQL still performs a NEW
	// uncached canonical read immediately before its own final commit.
	call, finish := context.WithTimeout(ctx, p.cfg.Timeout)
	hash, err := p.deps.Canonical.HashAt(call, m.Target.Height)
	finish()
	if err != nil {
		return err
	}
	if execwire.Hash(hash) != m.Target.Hash {
		return port.ErrExecutionCanonicalLag
	}
	if _, err = p.recoveryFence(ctx, m); err != nil {
		return err
	}
	var result nativeinput.Transition
	if p.transitionNative != nil {
		result, err = p.transitionNative(ctx, request)
	} else {
		result, err = runNativeTransition(ctx, p.cfg.Native.Executable, p.cfg.Native.DataDir, p.cfg.Timeout, request)
	}
	if err != nil {
		return err
	}
	if result != expected {
		return ErrNative
	}
	if err = checkNativeControls(ctx, p.cfg.Native.DataDir, nativeReady{Version: 1, Identity: request.Identity, Head: result.Head}); err != nil {
		return err
	}
	if _, err = p.recoveryFence(ctx, m); err != nil {
		return err
	}
	next := old
	if !completed {
		next.Version = 2
		next.RawEpoch++
		next.NativeEpoch = result.Head.ImportEpoch
		next.Generation = m.Target.Generation
		next.Received = result.Head.Tip
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(next)
		if err = p.raw.CompareOwner(ctx, before, after); err != nil {
			return err
		}
	}
	p.mu.Lock()
	p.identity = request.Identity
	p.head = result.Head
	p.control = next
	p.recoveryMarker = m
	p.mu.Unlock()
	return nil
}
func (p *program) Recovered(parent context.Context, c port.ExecutionControl) error {
	p.recoveryMu.Lock()
	defer p.recoveryMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return port.ErrClosed
	}
	p.recoveryCancel = cancel
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.recoveryCancel = nil; p.mu.Unlock() }()
	current, err := p.deps.Store.ExecutionStatus(ctx)
	if err != nil {
		return err
	}
	if current != c {
		return port.ErrExecutionStale
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return port.ErrClosed
	}
	if c.Rewinding || c.ReorgPending || c.Maintenance || !c.BaselineReady || c.ExecutionConfig != p.deps.Control.ExecutionConfig || c.Generation != p.recoveryMarker.Target.Generation || c.Visible != p.recoveryMarker.Target || c.Root != p.recoveryMarker.Root || p.control.Version != 2 || p.control.Generation != c.Generation || p.control.NativeEpoch != p.head.ImportEpoch || p.control.Received != p.head.Tip || p.head.Tip != (nativeRef{Height: c.Visible.Height, Hash: c.Visible.Hash, Root: c.Root}) {
		return port.ErrExecutionConflict
	}
	p.deps.Control = c
	p.recovering = false
	if p.started && p.resume != nil {
		select {
		case p.resume <- struct{}{}:
		default:
		}
	}
	return nil
}
