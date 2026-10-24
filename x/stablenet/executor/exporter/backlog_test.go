package exporter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

func TestClientRestartDrainsFullBacklogBeforeStartingNodeAndCapture(t *testing.T) {
	s, b, o := collectorFixture(t, 3)
	for n := uint64(1); n <= 3; n++ {
		if _, err := s.Append(t.Context(), b.blocks[n]); err != nil {
			t.Fatal(err)
		}
	}
	r4 := b.blocks[3]
	r4.Parent = r4.Ref.Hash
	r4.Ref.Height = 4
	r4.Ref.Hash = execwire.Hash{5}
	r4.Time = 4
	b.blocks[4] = r4
	b.canonical[4] = r4.Ref.Hash
	b.head = 4
	c := &Client{Chain: o.Chain, Spool: s, Backend: &synchronizedBackend{b: b}, Window: 2, Poll: time.Millisecond, Retry: time.Millisecond, Timeout: time.Second}
	c.OnReady = func(ctx context.Context) error {
		st, err := s.Status(ctx)
		if err != nil {
			return err
		}
		if st.Committed.Height < 3 {
			return errors.New("node started before durable backlog drained")
		}
		return nil
	}
	ctx, finish, done, serverErr := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, _ int) error {
		if err := fakeIndexerHello(ctx, conn, c.Chain, o.Anchor); err != nil {
			return err
		}
		for n := uint64(1); n <= 4; n++ {
			f, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if f.Kind != execwire.ResultPart || f.Header.Ref.Height != n {
				return errors.New("backlog skipped or new capture overtook drain")
			}
			if err = fakeIndexerACK(ctx, conn, f); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return nil
	})
	defer finish()
	waitClientState(t, ctx, c, 4, 1, done, serverErr)
}
