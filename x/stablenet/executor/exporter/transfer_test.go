package exporter

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
)

func transferFixture(t *testing.T) (*Client, spool.Options) {
	t.Helper()
	s, b, o := collectorFixture(t, 16)
	for n := uint64(1); n <= 3; n++ {
		if _, err := s.Append(t.Context(), b.blocks[n]); err != nil {
			t.Fatal(err)
		}
	}
	return &Client{Chain: o.Chain, Spool: s, Window: 2, Poll: time.Millisecond, Timeout: time.Second}, o
}
func startTransfer(t *testing.T, c *Client) (context.Context, *execwire.Conn, func(), <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	client, server := net.Pipe()
	peer := execwire.NewConn(server)
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		defer func() { _ = client.Close() }()
		done <- c.transfer(ctx, execwire.NewConn(client))
	}()
	finish := func() {
		cancel()
		_ = peer.Close()
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			t.Error("transfer workers did not join")
		}
	}
	return ctx, peer, finish, done
}
func TestTransferPipelinesBoundedWindowAndOnlyExactACKDeletes(t *testing.T) {
	c, o := transferFixture(t)
	ctx, peer, finish, _ := startTransfer(t, c)
	defer finish()
	first, err := peer.Read(ctx)
	if err != nil {
		t.Fatal("first result:", err)
	}
	second, err := peer.Read(ctx)
	if err != nil {
		t.Fatal("second must precede first ACK:", err)
	}
	if first.Kind != execwire.ResultPart || first.Header.Ref.Height != 1 || second.Header.Ref.Height != 2 {
		t.Fatal("unordered pipeline")
	}
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = peer.Read(wait)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("window exceeded before ACK:", err)
	}
	ack := func(f execwire.Frame) {
		t.Helper()
		if err := peer.Write(ctx, execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: f.Header.Ref, Digest: f.Header.Digest}}); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := c.Spool.Status(ctx)
	if st.Committed != o.Anchor || st.Pending != 3 {
		t.Fatal("sending inferred deletion ACK")
	}
	ack(first)
	third, err := peer.Read(ctx)
	if err != nil || third.Header.Ref.Height != 3 {
		t.Fatalf("third after ACK: %+v %v", third.Header.Ref, err)
	}
	ack(second)
	ack(third)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		st, err := c.Spool.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Committed.Height == 3 && st.Pending == 0 {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("exact ACK did not become durable")
		}
	}
}
func TestTransferWrongACKAndCancellationPreserveRaw(t *testing.T) {
	for _, wrong := range []bool{true, false} {
		t.Run(map[bool]string{true: "wrong_ack", false: "cancel"}[wrong], func(t *testing.T) {
			c, o := transferFixture(t)
			ctx, peer, finish, done := startTransfer(t, c)
			defer finish()
			first, err := peer.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if wrong {
				f := execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: first.Header.Ref, Digest: first.Header.Digest}}
				f.Header.Digest[0]++
				if err := peer.Write(ctx, f); err != nil {
					t.Fatal(err)
				}
			} else {
				_ = peer.Close()
			}
			select {
			case err := <-done:
				if err == nil || wrong && !errors.Is(err, spool.ErrConflict) {
					t.Fatalf("wrong ACK accepted: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			st, err := c.Spool.Status(t.Context())
			if err != nil || st.Committed != o.Anchor || st.Pending != 3 {
				t.Fatalf("failed transfer lost raw: %+v %v", st, err)
			}
		})
	}
}
