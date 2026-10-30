package collector

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"os"
)

// operation must include frame I/O AND complete payload/epoch/proof validation.
func (b *Backend) operation(ctx context.Context, f func(context.Context) error) error {
	err, fatal := b.guardedOperation(ctx, f)
	// The guarded call has deregistered before Close can wait for operations.
	if err != nil && fatal {
		return b.reject(err)
	}
	return err
}
func (b *Backend) guardedOperation(parent context.Context, f func(context.Context) error) (err error, fatal bool) {
	if err = parent.Err(); err != nil {
		return err, false
	}
	b.lifeMu.Lock()
	if b.closed {
		b.lifeMu.Unlock()
		return os.ErrClosed, false
	}
	b.operations.Add(1)
	b.lifeMu.Unlock()
	defer b.operations.Done()
	ctx, cancel := context.WithTimeout(parent, b.timeout)
	defer cancel()
	joined := make(chan struct{})
	stop := context.AfterFunc(b.lifetime, func() { cancel(); close(joined) })
	defer func() {
		if !stop() {
			<-joined
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err(), false
	case <-b.done:
		return ErrNative, true
	case <-b.operationGate:
	}
	defer func() { b.operationGate <- struct{}{} }()
	fatal = true
	ioJoined := make(chan struct{})
	stopIO := context.AfterFunc(ctx, func() { b.interrupt(); close(ioJoined) })
	defer func() {
		if !stopIO() {
			<-ioJoined
		}
	}()
	var lease *nativestore.ViewLease
	defer func() {
		if err != nil {
			b.lifeMu.Lock()
			b.closed = true
			b.lifeMu.Unlock()
			b.interrupt()
			<-b.done
		}
		if lease != nil {
			err = errors.Join(err, lease.Close())
		}
		if err != nil {
			b.lifeMu.Lock()
			b.closed = true
			b.lifeMu.Unlock()
		}
	}()
	if b.readView != nil {
		lease, err = b.readView.Acquire(ctx)
		if err != nil {
			return err, true
		}
	}
	if err = ctx.Err(); err != nil {
		return err, true
	}
	err = f(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && lease != nil {
		err = lease.Validate()
	}
	b.lifeMu.Lock()
	closed := b.closed
	b.lifeMu.Unlock()
	if closed && err == nil {
		err = os.ErrClosed
	}
	return err, true
}
