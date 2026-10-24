package exporter

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
)

func TestClientRestartDetectsShortReorgBeforeWaitingForOldTail(t *testing.T) {
	c, b := runtimeFixture(t)
	for n := uint64(1); n <= 3; n++ {
		if _, err := c.Spool.Append(t.Context(), b.b.blocks[n]); err != nil {
			t.Fatal(err)
		}
	}
	b.b.head = 2
	r2 := b.b.blocks[2]
	r2.Ref.Hash = execwire.Hash{99}
	b.b.blocks[2] = r2
	b.b.canonical[2] = r2.Ref.Hash
	ctx, finish, done, serverErr := runClientTest(t, c, func(ctx context.Context, conn *execwire.Conn, attempt int) error {
		ref := execwire.Ref{Generation: 1, Hash: c.Chain.Genesis}
		if attempt > 1 {
			ref.Generation = 2
		}
		if err := fakeIndexerHello(ctx, conn, c.Chain, ref); err != nil {
			return err
		}
		if attempt == 1 {
			f, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if f.Kind != execwire.Rewind || f.Header.Ref.Height != 2 || f.Header.Ref.Hash != r2.Ref.Hash {
				return errors.New("short fork waited for unavailable old tail")
			}
			return nil
		}
		for n := uint64(1); n <= 2; n++ {
			f, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if f.Header.Ref.Generation != 2 || f.Header.Ref.Height != n {
				return errors.New("wrong short-reorg replay")
			}
			if err = fakeIndexerACK(ctx, conn, f); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return nil
	})
	defer finish()
	waitClientState(t, ctx, c, 2, 2, done, serverErr)
}
