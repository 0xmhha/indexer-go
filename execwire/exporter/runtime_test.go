package exporter

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

type synchronizedBackend struct {
	mu sync.Mutex
	b  *fakeBackend
}

func (b *synchronizedBackend) Head(ctx context.Context) (execwire.Ref, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Head(ctx)
}
func (b *synchronizedBackend) Canonical(ctx context.Context, n uint64) (execwire.Hash, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Canonical(ctx, n)
}
func (b *synchronizedBackend) Replay(ctx context.Context, n, g uint64) (execwire.Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Replay(ctx, n, g)
}
func runtimeFixture(t *testing.T) (*Client, *synchronizedBackend) {
	t.Helper()
	s, b, o := collectorFixture(t, 32)
	safe := &synchronizedBackend{b: b}
	return &Client{Chain: o.Chain, Spool: s, Backend: safe, Window: 2, Poll: time.Millisecond, Retry: time.Millisecond, Timeout: time.Second}, safe
}
func fakeIndexerHello(ctx context.Context, conn *execwire.Conn, chain execwire.Chain, ref execwire.Ref) error {
	hello, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	if hello.Kind != execwire.Hello || hello.Header.Chain != chain {
		return errors.New("bad source Hello")
	}
	return conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: ref}})
}
func fakeIndexerACK(ctx context.Context, conn *execwire.Conn, f execwire.Frame) error {
	return conn.Write(ctx, execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: f.Header.Chain, Ref: f.Header.Ref, Digest: f.Header.Digest}})
}
func runClientTest(t *testing.T, c *Client, serve func(context.Context, *execwire.Conn, int) error) (context.Context, func(), <-chan error, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	var servers sync.WaitGroup
	var connections atomic.Int32
	serverErr := make(chan error, 16)
	c.dial = func(context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		attempt := int(connections.Add(1))
		servers.Go(func() {
			conn := execwire.NewConn(server)
			defer func() { _ = conn.Close() }()
			if err := serve(ctx, conn, attempt); err != nil {
				select {
				case serverErr <- err:
				default:
				}
			}
		})
		return client, nil
	}
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() { defer close(exited); done <- c.Run(ctx) }()
	finish := func() {
		cancel()
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			t.Error("client did not join")
		}
		servers.Wait()
	}
	return ctx, finish, done, serverErr
}
func waitClientState(t *testing.T, ctx context.Context, c *Client, h, g uint64, done, serverErr <-chan error) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		st, err := c.Spool.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Committed.Height == h && st.Committed.Generation == g {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("client stopped before checkpoint: %v", err)
		case err := <-serverErr:
			t.Fatal(err)
		case <-tick.C:
		case <-ctx.Done():
			t.Fatalf("checkpoint %+v: %v", st, ctx.Err())
		}
	}
}
func TestClientReconnectResendsLostACKWithoutTrustingHello(t *testing.T) {
	c, _ := runtimeFixture(t)
	var ready, connections atomic.Int32
	c.OnReady = func(context.Context) error { ready.Add(1); return nil }
	ctx, finish, done, serverErr := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, attempt int) error {
		connections.Add(1)
		ref := execwire.Ref{Generation: 1, Hash: c.Chain.Genesis}
		if attempt > 1 {
			ref.Height = 3
			ref.Hash = execwire.Hash{4}
		}
		if err := fakeIndexerHello(ctx, conn, c.Chain, ref); err != nil {
			return err
		}
		start := uint64(1)
		if attempt > 1 {
			start = 3
		}
		for n := start; n <= 3; n++ {
			f, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if f.Kind != execwire.ResultPart || f.Header.Ref.Height != n {
				return errors.New("Hello skipped unacked result")
			}
			if n == 3 && attempt == 1 {
				return nil
			}
			if err = fakeIndexerACK(ctx, conn, f); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return nil
	})
	defer finish()
	waitClientState(t, ctx, c, 3, 1, done, serverErr)
	if ready.Load() != 1 || connections.Load() < 2 {
		t.Fatal("lifecycle repeated or reconnect missing")
	}
}
func TestClientCollectsDurablyWhileIndexerIsDisconnected(t *testing.T) {
	c, b := runtimeFixture(t)
	b.b.head = 1
	var calls atomic.Int32
	disconnected := make(chan struct{})
	ctx, finish, done, serverErr := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, attempt int) error {
		if attempt > 1 {
			calls.Add(1)
			select {
			case <-disconnected:
			default:
				close(disconnected)
			}
			<-ctx.Done()
			return nil
		}
		if err := fakeIndexerHello(ctx, conn, c.Chain, execwire.Ref{Generation: 1, Hash: c.Chain.Genesis}); err != nil {
			return err
		}
		f, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		return fakeIndexerACK(ctx, conn, f)
	})
	defer finish()
	waitClientState(t, ctx, c, 1, 1, done, serverErr)
	select {
	case <-disconnected:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	b.mu.Lock()
	b.b.head = 3
	b.mu.Unlock()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		st, err := c.Spool.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Pending == 2 && st.Committed.Height == 1 {
			return
		}
		select {
		case err := <-done:
			t.Fatal(err)
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("collector stopped with socket:", ctx.Err())
		}
	}
}
func TestClientCanonicalChangeWaitsForNewGenerationAndPreservesOldRaw(t *testing.T) {
	c, b := runtimeFixture(t)
	e, err := c.Spool.Append(t.Context(), b.b.blocks[1])
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Spool.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: c.Chain, Ref: e.Ref, Digest: e.Digest}}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Spool.Append(t.Context(), b.b.blocks[2]); err != nil {
		t.Fatal(err)
	}
	r2 := b.b.blocks[2]
	r2.Ref.Hash = execwire.Hash{99}
	r3 := b.b.blocks[3]
	r3.Parent = r2.Ref.Hash
	r3.Ref.Hash = execwire.Hash{100}
	b.b.blocks[2], b.b.blocks[3] = r2, r3
	b.b.canonical[2], b.b.canonical[3] = r2.Ref.Hash, r3.Ref.Hash
	var connections atomic.Int32
	ctx, finish, done, serverErr := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, attempt int) error {
		connections.Add(1)
		ref := b.b.blocks[1].Ref
		if attempt >= 3 {
			ref.Generation = 2
		}
		if err := fakeIndexerHello(ctx, conn, c.Chain, ref); err != nil {
			return err
		}
		if attempt < 3 {
			f, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if f.Kind != execwire.Rewind || f.Header.Ref.Height != 2 || f.Header.Ref.Hash != r2.Ref.Hash || f.Header.Ref.Generation != 1 {
				return errors.New("missing fixed canonical rewind request")
			}
			return nil
		}
		for n := uint64(2); n <= 3; n++ {
			f, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if f.Header.Ref.Generation != 2 || f.Header.Ref.Height != n || f.Header.Ref.Hash != b.b.canonical[n] {
				return errors.New("orphan or wrong generation resent")
			}
			if err = fakeIndexerACK(ctx, conn, f); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return nil
	})
	defer finish()
	waitClientState(t, ctx, c, 3, 2, done, serverErr)
	st, err := c.Spool.Status(ctx)
	if err != nil || st.Quarantined < 1 || connections.Load() < 3 {
		t.Fatal("raw deleted or canonical lag retry skipped")
	}
}
