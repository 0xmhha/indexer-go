package arcingress

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

type boundedError struct {
	mu    sync.Mutex
	bytes []byte
}

func (b *boundedError) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n := 4096 - len(b.bytes); n > 0 {
		b.bytes = append(b.bytes, p[:min(n, len(p))]...)
	}
	return len(p), nil
}

type nativeClient struct {
	input     io.WriteCloser
	output    io.ReadCloser
	cancel    context.CancelFunc
	done      chan struct{}
	gate      chan struct{}
	lease     *os.File
	closeOnce sync.Once
	timeout   time.Duration
	ready     nativeReady
	head      nativeHead
	stderr    boundedError
}

func childEnvironment() []string {
	var env []string
	for _, key := range []string{"LANG", "LC_ALL", "TZ"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}
func startNative(ctx context.Context, executable, dir string, timeout time.Duration) (*nativeClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lease, err := nativestore.AcquireRun(dir)
	if err != nil {
		return nil, err
	}
	child, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(child, executable, "--datadir", dir, "--serve")
	return startOwnedNative(child, cancel, cmd, lease, timeout)
}
func startOwnedNative(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, lease *os.File, timeout time.Duration) (*nativeClient, error) {
	var err error
	cmd.Env = childEnvironment()
	c := &nativeClient{cancel: cancel, done: make(chan struct{}), gate: make(chan struct{}, 1), lease: lease, timeout: timeout}
	c.gate <- struct{}{}
	if c.input, err = cmd.StdinPipe(); err == nil {
		c.output, err = cmd.StdoutPipe()
	}
	if err == nil {
		cmd.Stderr = &c.stderr
		err = cmd.Start()
	}
	if err != nil {
		cancel()
		if c.input != nil {
			_ = c.input.Close()
		}
		if c.output != nil {
			_ = c.output.Close()
		}
		_ = lease.Close()
		return nil, err
	}
	go func() { _ = cmd.Wait(); close(c.done) }()
	err = c.operation(ctx, func() error { return readReply(c.output, "AWH1", 0, &c.ready) })
	if err != nil {
		c.Close()
		return nil, err
	}
	c.head = c.ready.Head
	return c, nil
}
func (c *nativeClient) interrupt() { c.cancel(); _ = c.input.Close(); _ = c.output.Close() }

// Close first interrupts I/O, joins the child and current request, then releases
// maintenance exclusion. No request may wait for Close while holding gate.
func (c *nativeClient) Close() {
	c.interrupt()
	<-c.done
	c.closeOnce.Do(func() { <-c.gate; _ = c.lease.Close(); c.gate <- struct{}{} })
}
func (c *nativeClient) operation(parent context.Context, f func() error) error {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrNative
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { c.interrupt(); close(stopped) })
	err := f()
	if !stop() {
		<-stopped
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (c *nativeClient) Import(ctx context.Context, m importMeta, block, receipts []byte, root, receiptsRoot execwire.Hash) (importReceipt, error) {
	var out importReceipt
	err := c.operation(ctx, func() error {
		if m.Height != c.head.Tip.Height+1 || m.Parent != c.head.Tip.Hash || m.ParentRoot != c.head.Tip.Root || m.RequestID != c.head.RequestID+1 || m.ImportEpoch != c.head.ImportEpoch {
			return ErrNative
		}
		frame, digest, err := encodeInput(m, block, receipts)
		if err != nil {
			return err
		}
		if _, err = io.Copy(c.input, bytes.NewReader(frame)); err != nil {
			return err
		}
		if err = readReply(c.output, "AWR1", m.RequestID, &out); err != nil {
			return err
		}
		if err = validateCompletion(c.ready.Identity, c.head, m, digest, root, receiptsRoot, out); err != nil {
			return err
		}
		c.head = nativeHead{Version: 1, Instance: out.Identity.Instance, ImportEpoch: out.ImportEpoch, ReadEpoch: out.ReadEpoch, RequestID: out.RequestID, Tip: nativeRef{out.Height, out.Hash, out.Root}}
		return nil
	})
	if err != nil {
		c.Close()
		return out, errors.Join(ErrNative, err)
	}
	return out, nil
}
