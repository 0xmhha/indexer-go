package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// TestIdleSubscriberKeepsLargeBlockEvents indexes several 1000-transaction
// blocks while a subscriber with the default channel size reads nothing:
// with the configured bus sizes every block, transaction and log event
// must still reach it (decision G4-b: size the bus so slow subscribers do
// not lose events).
func TestIdleSubscriberKeepsLargeBlockEvents(t *testing.T) {
	sc := testchain.BuildLoad(4, 1000, 0)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()

	want := 0
	for n := uint64(0); n <= head; n++ {
		b := sc.Chain.Block(n)
		want += 1 + len(b.Block.Transactions())
		for _, r := range b.Receipts {
			want += len(r.Logs)
		}
	}
	require.Greater(t, want, 4000)

	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	defer app.Shutdown()
	sub := app.eventBus.Subscribe("idle", []events.EventType{events.EventTypeBlock, events.EventTypeTransaction, events.EventTypeLog}, nil, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	require.Eventually(t, func() bool { return len(sub.Channel) == want }, 30*time.Second, 20*time.Millisecond,
		"events delivered %d of %d", len(sub.Channel), want)
	info := app.eventBus.GetSubscriberInfo("idle")
	require.Zero(t, info.EventsDropped)
	_, _, dropped := app.eventBus.Stats()
	require.Zero(t, dropped)
}
