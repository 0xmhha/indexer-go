package execwire

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

// Conn permits one reader and one writer concurrently. Each direction has a
// cancellable gate. Partial frames poison the connection; an idle cancellation
// does not. Deadline callbacks are joined before the next operation starts.
type Conn struct {
	conn          net.Conn
	reads, writes chan struct{}
	closed        chan struct{}
	once          sync.Once
	closeErr      error
}

func NewConn(conn net.Conn) *Conn {
	c := &Conn{conn: conn, reads: make(chan struct{}, 1), writes: make(chan struct{}, 1), closed: make(chan struct{})}
	c.reads <- struct{}{}
	c.writes <- struct{}{}
	return c
}

func (c *Conn) acquire(ctx context.Context, gate chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.conn == nil {
		return net.ErrClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return net.ErrClosed
	case <-gate:
	}
	if err := ctx.Err(); err != nil {
		gate <- struct{}{}
		return err
	}
	select {
	case <-c.closed:
		gate <- struct{}{}
		return net.ErrClosed
	default:
	}
	return nil
}

func arm(ctx context.Context, set func(time.Time) error) (func(), error) {
	deadline, _ := ctx.Deadline()
	if err := set(deadline); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = set(time.Now()); close(done) })
	return func() {
		if !stop() {
			<-done
		}
		_ = set(time.Time{})
	}, nil
}

func (c *Conn) Read(ctx context.Context) (Frame, error) {
	if err := c.acquire(ctx, c.reads); err != nil {
		return Frame{}, err
	}
	defer func() { c.reads <- struct{}{} }()
	cleanup, err := arm(ctx, c.conn.SetReadDeadline)
	if err != nil {
		return Frame{}, err
	}
	defer cleanup()
	r := &countReader{Reader: c.conn}
	f, err := Decode(r)
	if err != nil && r.n > 0 {
		_ = c.Close()
	}
	err = contextError(ctx, err)
	return f, err
}

func (c *Conn) Write(ctx context.Context, f Frame) error {
	if err := c.acquire(ctx, c.writes); err != nil {
		return err
	}
	defer func() { c.writes <- struct{}{} }()
	cleanup, err := arm(ctx, c.conn.SetWriteDeadline)
	if err != nil {
		return err
	}
	defer cleanup()
	w := &countWriter{Writer: c.conn}
	err = Encode(w, f)
	if err != nil && w.n > 0 {
		_ = c.Close()
	}
	err = contextError(ctx, err)
	return err
}

func contextError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
			return context.DeadlineExceeded
		}
	}
	return err
}

func (c *Conn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		if c.conn != nil {
			c.closeErr = c.conn.Close()
		}
	})
	return c.closeErr
}

type countReader struct {
	io.Reader
	n int
}

func (r *countReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += n
	return n, err
}

type countWriter struct {
	io.Writer
	n int
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.n += n
	return n, err
}
