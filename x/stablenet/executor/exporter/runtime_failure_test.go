package exporter

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
)

func TestClientFatalReplayOrQuotaStopsOwnerAndKeepsRaw(t *testing.T) {
	for _, quota := range []bool{true, false} {
		t.Run(map[bool]string{true: "quota", false: "parent"}[quota], func(t *testing.T) {
			limit := 16
			if quota {
				limit = 1
			}
			s, b, o := collectorFixture(t, limit)
			failure := errors.New("required retained parent is unavailable")
			if !quota {
				b.failure = failure
			}
			c := &Client{Chain: o.Chain, Spool: s, Backend: &synchronizedBackend{b: b}, Poll: time.Millisecond, Retry: time.Millisecond, Timeout: time.Second}
			ctx, finish, done, _ := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, _ int) error {
				if err := fakeIndexerHello(ctx, conn, c.Chain, o.Anchor); err != nil {
					return err
				}
				for {
					if _, err := conn.Read(ctx); err != nil {
						return nil
					}
				}
			})
			defer finish()
			select {
			case err := <-done:
				want := failure
				if quota {
					want = spool.ErrFull
				}
				if !errors.Is(err, want) {
					t.Fatalf("fatal error hidden: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			st, err := s.Status(t.Context())
			if err != nil || st.Committed != o.Anchor || quota && st.Pending != 1 || !quota && st.Pending != 0 {
				t.Fatalf("fatal source result changed ACK/raw: %+v %v", st, err)
			}
		})
	}
}
func TestClientRefusesConcurrentRunBeforeStartingAnotherNode(t *testing.T) {
	c, b := runtimeFixture(t)
	b.b.head = 0
	ready := make(chan struct{})
	var calls atomic.Int32
	c.OnReady = func(context.Context) error { calls.Add(1); close(ready); return nil }
	ctx, finish, _, _ := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, _ int) error {
		if err := fakeIndexerHello(ctx, conn, c.Chain, execwire.Ref{Generation: 1, Hash: c.Chain.Genesis}); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	})
	defer finish()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := c.Run(t.Context()); !errors.Is(err, spool.ErrBusy) {
		t.Fatalf("second lifecycle admitted: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("second node started")
	}
}

func TestClientCapacityFailureStopsBeforeReplayAndPreservesACK(t *testing.T) {
	s, b, o := collectorFixture(t, 16)
	failure := errors.New("owned node storage over budget")
	c := &Client{Chain: o.Chain, Spool: s, Backend: &synchronizedBackend{b: b}, Poll: time.Millisecond, Retry: time.Millisecond, Timeout: time.Second, BeforeCapture: func(context.Context) error { return failure }}
	ctx, finish, done, _ := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, _ int) error {
		if err := fakeIndexerHello(ctx, conn, c.Chain, o.Anchor); err != nil {
			return err
		}
		_, err := conn.Read(ctx)
		return err
	})
	defer finish()
	select {
	case err := <-done:
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	status, err := s.Status(t.Context())
	if err != nil || status.Committed != o.Anchor || status.Pending != 0 {
		t.Fatal("capacity halt discarded/promoted raw", status, err)
	}
}
