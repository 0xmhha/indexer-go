package exporter

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"sync/atomic"
	"testing"
)

func TestClientOwnedProposalUsesSameSessionAfterCaptureJoinAndPreservesACK(t *testing.T) {
	c, b := runtimeFixture(t)
	b.b.head = 0
	proposals := make(chan execwire.Ref, 1)
	c.Proposals = proposals
	entered, joined := make(chan struct{}), make(chan struct{})
	c.BeforeCapture = func(ctx context.Context) error { close(entered); <-ctx.Done(); close(joined); return ctx.Err() }
	proposal := execwire.Ref{Generation: 1, Height: 3, Hash: execwire.Hash{42}}
	var sessions atomic.Int32
	seen := make(chan struct{})
	ctx, finish, done, serverErr := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, _ int) error {
		sessions.Add(1)
		if err := fakeIndexerHello(ctx, conn, c.Chain, execwire.Ref{Generation: 1, Hash: c.Chain.Genesis}); err != nil {
			return err
		}
		f, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if f.Kind != execwire.Rewind || f.Header.Ref != proposal {
			return errors.New("source proposal did not use existing Rewind/session")
		}
		select {
		case <-joined:
		default:
			return errors.New("Rewind overtook capture cancellation/join")
		}
		close(seen)
		return nil
	})
	defer finish()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	proposals <- proposal
	select {
	case <-seen:
	case err := <-serverErr:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("owned proposal was not serviced on active session")
	}
	select {
	case err := <-done:
		var fork *CanonicalChange
		if !errors.As(err, &fork) || fork.Ref != proposal {
			t.Fatalf("proposal owner did not receive exact transition result: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("source proposal owner was not joined")
	}
	if sessions.Load() != 1 {
		t.Fatal("proposal opened a second IPC session")
	}
	status, err := c.Spool.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.Committed.Height != 0 || status.Committed.Generation != 1 || status.Pending != 0 {
		t.Fatalf("proposal invented source ACK: %+v", status)
	}
}
