package arcingress

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"math"
	"time"
)

var errRawFork = errors.New("arc ingress: verified raw parent branch changed")
var errRawMismatch = errors.New("arc ingress: canonical raw input is unavailable")

// This is an admission/proposal read, never E1's final fresh or an ACK.
func (p *program) frozenCanonical(ctx context.Context, h nativeRef) (execwire.Ref, bool, error) {
	if err := p.stable(ctx); err != nil {
		return execwire.Ref{}, false, err
	}
	call, finish := context.WithTimeout(ctx, p.cfg.Timeout)
	defer finish()
	hash, err := p.deps.Canonical.HashAt(call, h.Height)
	height := h.Height
	if errors.Is(err, source.ErrNotFound) {
		reader, ok := p.deps.Canonical.(interface {
			Head(context.Context) (uint64, error)
		})
		if !ok {
			return execwire.Ref{}, false, port.ErrExecutionCanonicalLag
		}
		height, err = reader.Head(call)
		if err != nil {
			return execwire.Ref{}, false, err
		}
		if height >= h.Height {
			return execwire.Ref{}, false, port.ErrExecutionCanonicalLag
		}
		hash, err = p.deps.Canonical.HashAt(call, height)
	}
	if fence := p.stable(ctx); fence != nil {
		return execwire.Ref{}, false, fence
	}
	if err != nil {
		return execwire.Ref{}, false, err
	}
	if hash == ([32]byte{}) {
		return execwire.Ref{}, false, ErrNative
	}
	ref := execwire.Ref{Generation: p.deps.Control.Generation, Height: height, Hash: execwire.Hash(hash)}
	return ref, height == h.Height && ref.Hash == h.Hash, nil
}
func (p *program) watchNative(ctx context.Context) error {
	poll := min(time.Second, p.cfg.Timeout/2)
	if poll < time.Millisecond {
		poll = time.Millisecond
	}
	deadline := time.Now().Add(p.cfg.Timeout)
	for {
		p.mu.Lock()
		head := p.head.Tip
		p.mu.Unlock()
		call, stop := context.WithDeadline(ctx, deadline)
		_, same, err := p.frozenCanonical(call, head)
		stop()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, port.ErrExecutionRewinding) || errors.Is(err, port.ErrExecutionMaintenance) {
			return err
		}
		if err == nil {
			if !same {
				return errRawFork
			}
			deadline = time.Now().Add(p.cfg.Timeout)
		} else if !time.Now().Before(deadline) {
			return errors.Join(port.ErrExecutionCanonicalLag, err)
		}
		timer := time.NewTimer(min(poll, max(time.Until(deadline), time.Millisecond)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (p *program) rotateRaw(ctx context.Context, head nativeHead) error {
	_, same, err := p.frozenCanonical(ctx, head.Tip)
	if err != nil {
		return err
	}
	if !same {
		return errRestartRound
	}
	if err = checkNativeControls(ctx, p.cfg.Native.DataDir, nativeReady{Version: 1, Identity: p.identity, Head: head}); err != nil {
		return err
	}
	if err = p.stable(ctx); err != nil {
		return err
	}
	p.mu.Lock()
	if p.recovering || p.stopped {
		p.mu.Unlock()
		return port.ErrExecutionRewinding
	}
	if p.head != head || p.control.Version != 2 || p.control.RawEpoch >= math.MaxInt64 {
		p.mu.Unlock()
		return ErrNative
	}
	next := p.control
	next.RawEpoch++
	next.Received = head.Tip
	before, _ := json.Marshal(p.control)
	after, _ := json.Marshal(next)
	err = p.raw.CompareOwner(ctx, before, after)
	if err == nil {
		p.control = next
	}
	p.mu.Unlock()
	if err != nil {
		return err
	}
	if err = p.stable(ctx); err != nil {
		return err
	}
	return errRestartRound
}
func (p *program) rawMismatchIsFork(ctx context.Context) error {
	p.mu.Lock()
	received := p.control.Received
	p.mu.Unlock()
	call, stop := context.WithTimeout(ctx, p.cfg.Timeout)
	defer stop()
	hash, err := p.deps.Canonical.HashAt(call, received.Height)
	if fence := p.stable(ctx); fence != nil {
		return fence
	}
	if err != nil {
		return errors.Join(port.ErrExecutionCanonicalLag, err)
	}
	if execwire.Hash(hash) == received.Hash {
		return ErrNative
	}
	return nil
}
