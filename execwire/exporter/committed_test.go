package exporter

import (
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

func TestCommittedObserverRequiresDurableExactACK(t *testing.T) {
	c, _ := transferFixture(t)
	observed := make(chan execwire.Header, 8)
	c.OnCommitted = func(h execwire.Header) { observed <- h }
	ctx, peer, finish, done := startTransfer(t, c)
	defer finish()
	first, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-observed:
		t.Fatal("sending/anchor is not a durable ACK")
	default:
	}
	ack := execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: first.Header.Chain, Ref: first.Header.Ref, Digest: first.Header.Digest}}
	if err = peer.Write(ctx, ack); err != nil {
		t.Fatal(err)
	}
	select {
	case h := <-observed:
		st, e := c.Spool.Status(ctx)
		if e != nil || h.Ref != st.Committed || h.Digest != st.Digest || h.Digest == (execwire.Hash{}) {
			t.Fatalf("observer preceded durability: %+v %v", h, e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bad := execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: second.Header.Chain, Ref: second.Header.Ref, Digest: second.Header.Digest}}
	bad.Header.Digest[0] ^= 1
	if err = peer.Write(ctx, bad); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("invalid ACK did not stop workers")
	}
	select {
	case <-observed:
		t.Fatal("invalid ACK was reported committed")
	default:
	}
}

func TestCommittedObserverRestoresActualDurableStatus(t *testing.T) {
	c, o := transferFixture(t)
	e, ok, err := c.Spool.After(t.Context(), o.Anchor)
	if err != nil || !ok {
		t.Fatalf("first durable result: %v", err)
	}
	h := execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: e.Ref, Digest: e.Digest}
	if err = c.Spool.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: h}); err != nil {
		t.Fatal(err)
	}
	observed := make(chan execwire.Header, 1)
	c.OnCommitted = func(h execwire.Header) { observed <- h }
	_, _, finish, _ := startTransfer(t, c)
	defer finish()
	select {
	case got := <-observed:
		if got != h {
			t.Fatalf("restored ACK mismatch: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("restored durable status not reported")
	}
}
