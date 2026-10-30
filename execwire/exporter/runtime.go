package exporter

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/spool"
)

type captureRun struct {
	cancel context.CancelFunc
	exited chan struct{}
	err    error
}
type clientRun struct {
	c             *Client
	ctx           context.Context
	fail          context.CancelCauseFunc
	poll, timeout time.Duration
	capture       *captureRun
	ready         sync.Once
	readyErr      error
	readyStarted  atomic.Bool
	fork          *CanonicalChange
}

var ErrCheckpointRecovery = errors.New("exporter: retained node headers do not cover backlog; verified checkpoint recovery required")

func (o *clientRun) ensureReady() error {
	o.ready.Do(func() {
		if o.c.OnReady != nil {
			o.readyErr = o.c.OnReady(o.ctx)
		}
		if o.readyErr == nil {
			o.readyStarted.Store(true)
		}
	})
	return o.readyErr
}

// Run owns its collector across transport loss. Quota/replay failures stop the
// owner; the node assembly closes its own node when Run returns.
func (c *Client) Run(parent context.Context) error {
	if c == nil || c.Spool == nil || c.Backend == nil {
		return spool.ErrConflict
	}
	if !c.running.CompareAndSwap(false, true) {
		return spool.ErrBusy
	}
	defer c.running.Store(false)
	poll, retry, timeout := c.Poll, c.Retry, c.Timeout
	if poll == 0 {
		poll = 100 * time.Millisecond
	}
	if retry == 0 {
		retry = 500 * time.Millisecond
	}
	if timeout == 0 {
		timeout = time.Minute
	}
	if poll <= 0 || retry <= 0 || timeout <= 0 || timeout > time.Hour || c.Window < 0 || c.Window > 64 || cap(c.Proposals) > 1 || c.dial == nil && !filepath.IsAbs(c.Socket) {
		return execwire.ErrLimit
	}
	status, err := c.Spool.Status(parent)
	if err != nil {
		return err
	}
	if status.Chain != c.Chain {
		return spool.ErrConflict
	}
	c.committed(status)
	if err = execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: c.Chain, Ref: status.Committed}}); err != nil {
		return err
	}
	ctx, fail := context.WithCancelCause(parent)
	defer fail(context.Canceled)
	o := &clientRun{c: c, ctx: ctx, fail: fail, poll: poll, timeout: timeout}
	defer o.stopCapture()
	c.wake = make(chan struct{}, 1)
	for {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		call, finish := context.WithTimeout(ctx, timeout)
		var raw net.Conn
		if c.dial != nil {
			raw, err = c.dial(call)
		} else {
			raw, err = dialExecution(call, c.Socket)
		}
		finish()
		if err == nil {
			err = o.session(raw)
		}
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		var proposed *proposedChange
		if errors.As(err, &proposed) {
			return proposed.CanonicalChange
		}
		var fork *CanonicalChange
		if errors.As(err, &fork) {
			copy := *fork
			o.fork = &copy
		} else if !transportError(err) {
			return err
		}
		if c.OnError != nil {
			c.OnError(err)
		}
		if err = o.pause(ctx, retry); err != nil {
			return context.Cause(ctx)
		}
	}
}
func (o *clientRun) pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (o *clientRun) stopCapture() {
	if o.capture != nil {
		o.capture.cancel()
		<-o.capture.exited
		o.capture = nil
	}
}
func (o *clientRun) startCapture() {
	if o.capture != nil {
		return
	}
	ctx, cancel := context.WithCancel(o.ctx)
	run := &captureRun{cancel: cancel, exited: make(chan struct{})}
	o.capture = run
	go func() {
		defer close(run.exited)
		// Drain the retained startup tail before restarting import/replay.
		// Otherwise a full spool would fail again before it could receive ACKs.
		tail, err := o.c.Spool.Tail(ctx)
		if err == nil && !o.readyStarted.Load() {
			for {
				var ack execwire.Ref
				ack, err = o.c.Spool.Checkpoint(ctx)
				if err != nil {
					break
				}
				if ack.Generation != tail.Generation {
					err = spool.ErrConflict
					break
				}
				if ack.Height >= tail.Height {
					break
				}
				if err = o.pause(ctx, o.poll); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = o.ensureReady()
		}
		if err != nil {
			run.err = err
			if !errors.Is(err, context.Canceled) {
				o.fail(err)
			}
			return
		}
		collector := &Collector{Backend: o.c.Backend, Spool: o.c.Spool}
		for {
			if o.c.BeforeCapture != nil {
				if err := o.c.BeforeCapture(ctx); err != nil {
					run.err = err
					if !errors.Is(err, context.Canceled) {
						o.fail(err)
					}
					return
				}
			}
			n, err := collector.CatchUp(ctx)
			if err != nil {
				run.err = err
				if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrCanonicalChanged) {
					o.fail(err)
				}
				return
			}
			if n > 0 {
				select {
				case o.c.wake <- struct{}{}:
				default:
				}
			}
			timer := time.NewTimer(o.poll)
			select {
			case <-ctx.Done():
				timer.Stop()
				run.err = ctx.Err()
				return
			case <-timer.C:
			case <-o.c.Wake:
			}
			timer.Stop()
		}
	}()
}
func (o *clientRun) canonical(ctx context.Context, height uint64) (execwire.Hash, error) {
	for {
		head, err := o.c.Backend.Head(ctx)
		if err != nil {
			return execwire.Hash{}, err
		}
		if head.Height >= height {
			return o.c.Backend.Canonical(ctx, height)
		}
		if err = o.pause(ctx, o.poll); err != nil {
			return execwire.Hash{}, err
		}
	}
}
func (o *clientRun) request(ctx context.Context, conn *execwire.Conn, ref execwire.Ref) error {
	o.stopCapture()
	if err := conn.Write(ctx, execwire.Frame{Kind: execwire.Rewind, Header: execwire.Header{Version: execwire.Version, Chain: o.c.Chain, Ref: ref}}); err != nil {
		return err
	}
	return &CanonicalChange{Ref: ref}
}
func (o *clientRun) session(raw net.Conn) error {
	conn := execwire.NewConn(raw)
	defer func() { _ = conn.Close() }()
	ctx, finish := context.WithTimeout(o.ctx, o.timeout)
	defer finish()
	status, err := o.c.Spool.Status(ctx)
	if err != nil {
		finish()
		return err
	}
	err = conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: o.c.Chain, Ref: status.Committed}})
	if err != nil {
		finish()
		return err
	}
	hello, err := conn.Read(ctx)
	if err != nil {
		finish()
		return err
	}
	if hello.Kind != execwire.Hello || hello.Header.Chain != o.c.Chain || hello.Header.Ref.Generation < status.Committed.Generation || hello.Header.Ref.Height < status.Anchor.Height {
		finish()
		return spool.ErrConflict
	}
	if status.Pending == 0 || o.readyStarted.Load() {
		if err = o.ensureReady(); err != nil {
			finish()
			return err
		}
	} else {
		head, e := o.c.Backend.Head(ctx)
		if e != nil {
			finish()
			return e
		}
		tail, e := o.c.Spool.Tail(ctx)
		if e != nil {
			finish()
			return e
		}
		if hello.Header.Ref.Generation == status.Committed.Generation && head.Height < tail.Height {
			known, ok, e := o.c.Spool.Reference(ctx, head.Height)
			if e != nil {
				finish()
				return e
			}
			if !ok || known.Hash != head.Hash {
				head.Generation = hello.Header.Ref.Generation
				err = o.request(ctx, conn, head)
				finish()
				return err
			}
			finish()
			return ErrCheckpointRecovery
		}
		if head.Height < hello.Header.Ref.Height {
			finish()
			return ErrCheckpointRecovery
		}
	}
	visible := hello.Header.Ref
	hash, err := o.canonical(ctx, visible.Height)
	if err != nil {
		finish()
		return err
	}
	if hash != visible.Hash {
		ref := visible
		ref.Hash = hash
		err = o.request(ctx, conn, ref)
		finish()
		return err
	}
	if visible.Generation > status.Committed.Generation {
		o.stopCapture()
		base := visible
		if status.Committed.Height < base.Height {
			base.Height = status.Committed.Height
		}
		base.Hash, err = o.canonical(ctx, base.Height)
		if err == nil {
			err = o.c.Spool.Rebind(ctx, base)
		}
		if err != nil {
			finish()
			return err
		}
		o.fork = nil
	} else {
		if status.Committed.Height > visible.Height {
			err = o.request(ctx, conn, visible)
			finish()
			return err
		}
		hash, err = o.canonical(ctx, status.Committed.Height)
		if err != nil {
			finish()
			return err
		}
		if hash != status.Committed.Hash {
			ref := status.Committed
			ref.Hash = hash
			err = o.request(ctx, conn, ref)
			finish()
			return err
		}
		if o.capture != nil {
			select {
			case <-o.capture.exited:
				var fork *CanonicalChange
				if errors.As(o.capture.err, &fork) {
					copy := *fork
					o.fork = &copy
				}
			default:
			}
		}
		if o.fork != nil {
			ref := o.fork.Ref
			ref.Generation = visible.Generation
			ref.Hash, err = o.canonical(ctx, ref.Height)
			if err == nil {
				err = o.request(ctx, conn, ref)
			}
			finish()
			return err
		}
		tail, err := o.c.Spool.Tail(ctx)
		if err != nil {
			finish()
			return err
		}
		head, err := o.c.Backend.Head(ctx)
		if err != nil {
			finish()
			return err
		}
		if head.Height < tail.Height {
			known, ok, err := o.c.Spool.Reference(ctx, head.Height)
			if err != nil {
				finish()
				return err
			}
			if !ok || known.Hash != head.Hash {
				head.Generation = visible.Generation
				err = o.request(ctx, conn, head)
				finish()
				return err
			}
		}
		hash, err = o.canonical(ctx, tail.Height)
		if err != nil {
			finish()
			return err
		}
		if hash != tail.Hash {
			tail.Hash = hash
			err = o.request(ctx, conn, tail)
			finish()
			return err
		}
	}
	finish()
	o.startCapture()
	return o.stream(conn)
}
func (o *clientRun) stream(conn *execwire.Conn) error {
	ctx, cancel := context.WithCancel(o.ctx)
	defer cancel()
	errs := make(chan error, 2)
	capture := o.capture
	go func() { errs <- o.c.transfer(ctx, conn) }()
	go func() {
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
		case ref, ok := <-o.c.Proposals:
			if !ok {
				errs <- execwire.ErrLimit
				return
			}
			status, err := o.c.Spool.Status(ctx)
			if err != nil {
				errs <- err
				return
			}
			if ref.Generation != status.Committed.Generation {
				errs <- spool.ErrConflict
				return
			}
			o.stopCapture()
			call, finish := context.WithTimeout(ctx, o.timeout)
			err = conn.Write(call, execwire.Frame{Kind: execwire.Rewind, Header: execwire.Header{Version: execwire.Version, Chain: o.c.Chain, Ref: ref}})
			finish()
			if err != nil {
				errs <- err
				return
			}
			errs <- &proposedChange{CanonicalChange: &CanonicalChange{Ref: ref}}
		case <-capture.exited:
			var fork *CanonicalChange
			if errors.As(capture.err, &fork) {
				call, finish := context.WithTimeout(ctx, o.timeout)
				err := conn.Write(call, execwire.Frame{Kind: execwire.Rewind, Header: execwire.Header{Version: execwire.Version, Chain: o.c.Chain, Ref: fork.Ref}})
				finish()
				errs <- errors.Join(fork, err)
			} else {
				errs <- capture.err
			}
		}
	}()
	first := <-errs
	cancel()
	_ = conn.Close()
	second := <-errs
	for _, err := range []error{first, second} {
		if err != nil && !transportError(err) && !errors.Is(err, ErrCanonicalChanged) {
			return err
		}
	}
	for _, err := range []error{first, second} {
		if errors.Is(err, ErrCanonicalChanged) {
			return err
		}
	}
	return first
}
