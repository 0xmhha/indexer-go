package exporter

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
)

type Client struct {
	Chain                execwire.Chain
	Spool                *spool.Store
	Backend              Backend
	Socket               string
	Window               int
	Poll, Retry, Timeout time.Duration
	Wake                 <-chan struct{}
	OnReady              func(context.Context) error
	OnError              func(error)
	dial                 func(context.Context) (net.Conn, error)
	wake                 chan struct{}
	running              atomic.Bool
}

func (c *Client) transfer(parent context.Context, conn *execwire.Conn) error {
	window := c.Window
	if window == 0 {
		window = 8
	}
	if window < 1 || window > 64 {
		return execwire.ErrLimit
	}
	poll := c.Poll
	if poll == 0 {
		poll = 100 * time.Millisecond
	}
	if poll <= 0 {
		return execwire.ErrLimit
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	if timeout <= 0 {
		return execwire.ErrLimit
	}
	if c.Spool == nil || conn == nil {
		return spool.ErrConflict
	}
	status, err := c.Spool.Status(parent)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.Close(); close(closed) })
	defer func() {
		cancel()
		if !stop() {
			<-closed
		}
	}()
	expected := make(chan spool.Entry, window)
	active := make(chan struct{}, window)
	errs := make(chan error, 2)
	go func() {
		errs <- func() error {
			cursor := status.Committed
			for {
				select {
				case active <- struct{}{}:
				case <-ctx.Done():
					return ctx.Err()
				}
				e, ok, err := c.Spool.After(ctx, cursor)
				if err != nil {
					<-active
					return err
				}
				if !ok {
					<-active
					timer := time.NewTimer(poll)
					select {
					case <-timer.C:
					case <-c.wake:
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					}
					timer.Stop()
					continue
				}
				if c.Backend != nil {
					hash, err := c.Backend.Canonical(ctx, e.Ref.Height)
					if err != nil {
						return err
					}
					if hash != e.Ref.Hash {
						return &CanonicalChange{Ref: execwire.Ref{Generation: e.Ref.Generation, Height: e.Ref.Height, Hash: hash}}
					}
				}
				r, err := c.Spool.Read(ctx, e.Ref)
				if err != nil {
					return err
				}
				payload, err := execwire.MarshalResult(r)
				if err != nil {
					return err
				}
				frames, err := execwire.Split(c.Chain, e.Ref, payload, execwire.MaxFrameBytes)
				if err != nil {
					return err
				}
				if frames[0].Header.Digest != e.Digest {
					return spool.ErrConflict
				}
				select {
				case expected <- e:
				case <-ctx.Done():
					return ctx.Err()
				}
				for _, frame := range frames {
					call, finish := context.WithTimeout(ctx, timeout)
					err := conn.Write(call, frame)
					finish()
					if err != nil {
						return err
					}
				}
				cursor = e.Ref
			}
		}()
	}()
	go func() {
		errs <- func() error {
			lastACK := status.Committed
			for {
				call, finish := context.WithTimeout(ctx, timeout)
				frame, err := conn.Read(call)
				finish()
				if err != nil {
					return err
				}
				if frame.Kind != execwire.Committed || frame.Header.Chain != c.Chain {
					return spool.ErrConflict
				}
				if frame.Header.Ref.Height <= lastACK.Height {
					if err = c.Spool.Acknowledge(ctx, frame); err != nil {
						return err
					}
					continue
				}
				var e spool.Entry
				select {
				case e = <-expected:
				default:
					return spool.ErrConflict
				}
				if frame.Kind != execwire.Committed || frame.Header.Chain != c.Chain || frame.Header.Ref != e.Ref || frame.Header.Digest != e.Digest {
					return spool.ErrConflict
				}
				if err = c.Spool.Acknowledge(ctx, frame); err != nil {
					return err
				}
				lastACK = e.Ref
				<-active
			}
		}()
	}()
	first := <-errs
	cancel()
	_ = conn.Close()
	second := <-errs
	for _, err := range []error{first, second} {
		if err != nil && !transportError(err) {
			return err
		}
	}
	return first
}

func transportError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var network net.Error
	return errors.As(err, &network)
}
