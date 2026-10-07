package stream

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

func newStore(t *testing.T) *storage.PebbleStorage {
	t.Helper()
	s, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// appendBlocks commits n block transactions with perBlock block events each
// (the event's Number is its position, 1-based), as the indexer does.
func appendBlocks(t *testing.T, s *storage.PebbleStorage, n, perBlock int) {
	t.Helper()
	last, err := s.LastOutboxSeq(context.Background())
	require.NoError(t, err)
	for b := 0; b < n; b++ {
		txCtx, tx, err := s.BeginBlock(context.Background())
		require.NoError(t, err)
		var entries []port.OutboxEntry
		for i := 0; i < perBlock; i++ {
			last++
			data, err := events.MarshalEvent(&events.BlockEvent{Number: last, CreatedAt: time.Unix(1, 0)})
			require.NoError(t, err)
			entries = append(entries, port.OutboxEntry{Type: string(events.EventTypeBlock), Data: data})
		}
		require.NoError(t, s.AppendOutbox(txCtx, entries))
		require.NoError(t, tx.Commit())
	}
}

// consumer subscribes to a bus and checks what it receives: events in
// sequence 1, 2, ... without gaps or repeats, each the outbox entry with
// that number.
type consumer struct {
	t   *testing.T
	sub *events.Subscription
	got uint64
}

func newConsumer(t *testing.T, bus *events.EventBus) *consumer {
	sub := bus.Subscribe("consumer", []events.EventType{events.EventTypeBlock}, nil, 1<<16)
	require.NotNil(t, sub)
	return &consumer{t: t, sub: sub}
}

// await reads until it has received the events up to seq.
func (c *consumer) await(seq uint64) {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	for c.got < seq {
		select {
		case ev := <-c.sub.Channel:
			s := events.SequenceOf(ev)
			require.Equal(c.t, c.got+1, s, "sequence after %d", c.got)
			require.Equal(c.t, s, ev.(*events.BlockEvent).Number, "the entry with that number")
			c.got = s
		case <-deadline:
			c.t.Fatalf("received up to %d, want %d", c.got, seq)
		}
	}
}

// quiet checks that nothing more arrives.
func (c *consumer) quiet() {
	c.t.Helper()
	select {
	case ev := <-c.sub.Channel:
		c.t.Fatalf("unexpected event %d after %d", events.SequenceOf(ev), c.got)
	case <-time.After(100 * time.Millisecond):
	}
}

func runRelay(r *Relay) (stop func() error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	return func() error { cancel(); return <-done }
}

func newBus(t *testing.T) *events.EventBus {
	bus := events.NewEventBus(1024, 1024)
	go bus.Run()
	t.Cleanup(bus.Stop)
	return bus
}

func TestRelayDeliversInSequence(t *testing.T) {
	s := newStore(t)
	bus := newBus(t)
	c := newConsumer(t, bus)
	appendBlocks(t, s, 3, 4) // committed before the relay starts

	r := NewRelay(s, bus.Publish, RelayConfig{Poll: time.Hour}, nil)
	stop := runRelay(r)
	c.await(12)
	appendBlocks(t, s, 2, 3)
	r.Notify()
	c.await(18)
	require.ErrorIs(t, stop(), context.Canceled)
	cursor, err := s.OutboxCursor(context.Background(), DefaultRelayName)
	require.NoError(t, err)
	require.Equal(t, uint64(18), cursor)
	c.quiet()
}

// TestRelayKilledMidwayLosesAndRepeatsNothing is the R3-1 criterion: the
// relay is killed at every point of a batch (after publishing some events,
// before recording how far it got) and restarted; the consumer receives
// every sequence once, in order.
func TestRelayKilledMidwayLosesAndRepeatsNothing(t *testing.T) {
	s := newStore(t)
	bus := newBus(t)
	c := newConsumer(t, bus)
	appendBlocks(t, s, 10, 7) // 70 events
	const total = 70

	var published atomic.Uint64
	for killAt := uint64(3); ; killAt += 11 {
		require.Less(t, killAt, uint64(10*total), "the relay makes progress")
		// A relay whose publish kills it after killAt more events: the
		// events it published are delivered, its cursor for the batch is
		// not recorded.
		ctx, kill := context.WithCancel(context.Background())
		var n atomic.Uint64
		publish := func(ev events.Event) bool {
			if n.Add(1) > killAt {
				kill()
				return false
			}
			published.Add(1)
			return bus.Publish(ev)
		}
		r := NewRelay(s, publish, RelayConfig{Batch: 16, Poll: 10 * time.Millisecond}, nil)
		done := make(chan error, 1)
		go func() { done <- r.Run(ctx) }()
		finished := false
	wait:
		for {
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
				break wait
			case <-time.After(5 * time.Millisecond):
				cursor, err := s.OutboxCursor(context.Background(), DefaultRelayName)
				require.NoError(t, err)
				if cursor == total { // delivered the rest before its kill point
					kill()
					<-done
					finished = true
					break wait
				}
			}
		}
		kill()
		if finished {
			break
		}
	}
	c.await(total)
	c.quiet()
	require.Greater(t, published.Load(), uint64(total), "killed relays published events again")
	require.Equal(t, published.Load()-total, bus.Duplicates(), "the bus dropped exactly the repeats")
}

// TestRelayWaitsForFullBus: a bus that refuses events (full buffer) makes
// the relay wait, not skip.
func TestRelayWaitsForFullBus(t *testing.T) {
	s := newStore(t)
	bus := newBus(t)
	c := newConsumer(t, bus)
	appendBlocks(t, s, 1, 20)

	var refusals atomic.Int64
	publish := func(ev events.Event) bool {
		if refusals.Add(1)%3 != 0 { // two of three offers fail
			return false
		}
		return bus.Publish(ev)
	}
	stop := runRelay(NewRelay(s, publish, RelayConfig{Poll: time.Hour}, nil))
	c.await(20)
	require.ErrorIs(t, stop(), context.Canceled)
	c.quiet()
}

// TestRelayRestartsFromCursor: a relay started on a database whose cursor
// records delivered events continues after it.
func TestRelayRestartsFromCursor(t *testing.T) {
	s := newStore(t)
	appendBlocks(t, s, 4, 5)
	require.NoError(t, s.SetOutboxCursor(context.Background(), DefaultRelayName, 12))

	bus := newBus(t)
	sub := bus.Subscribe("c", []events.EventType{events.EventTypeBlock}, nil, 64)
	stop := runRelay(NewRelay(s, bus.Publish, RelayConfig{Poll: time.Hour}, nil))
	defer func() { _ = stop() }()
	for want := uint64(13); want <= 20; want++ {
		select {
		case ev := <-sub.Channel:
			require.Equal(t, want, events.SequenceOf(ev))
		case <-time.After(5 * time.Second):
			t.Fatalf("event %d not delivered", want)
		}
	}
}

// TestRelayPrunesDeliveredEntries keeps Retain delivered entries.
func TestRelayPrunesDeliveredEntries(t *testing.T) {
	s := newStore(t)
	bus := newBus(t)
	c := newConsumer(t, bus)
	appendBlocks(t, s, 30, 100) // 3000 events
	stop := runRelay(NewRelay(s, bus.Publish, RelayConfig{Retain: 500, Poll: time.Hour}, nil))
	c.await(3000)
	require.ErrorIs(t, stop(), context.Canceled)

	left, err := s.ReadOutbox(context.Background(), 0, 0)
	require.NoError(t, err)
	require.NotEmpty(t, left)
	require.Equal(t, uint64(3000), left[len(left)-1].Seq, "undelivered and recent entries stay")
	require.GreaterOrEqual(t, len(left), 500, "at least Retain entries stay")
	require.Less(t, len(left), 500+pruneStep+DefaultRelayBatch, "older entries are pruned")
}

// TestRelaySkipsUndecodableEntry: an entry of a type this build cannot
// decode is logged and skipped; the rest is delivered.
func TestRelaySkipsUndecodableEntry(t *testing.T) {
	s := newStore(t)
	appendBlocks(t, s, 1, 1)
	txCtx, tx, err := s.BeginBlock(context.Background())
	require.NoError(t, err)
	require.NoError(t, s.AppendOutbox(txCtx, []port.OutboxEntry{{Type: "no.such.type", Data: []byte("{}")}}))
	require.NoError(t, tx.Commit())
	appendBlocks(t, s, 1, 1) // seq 3, Number 2 (appendBlocks numbers from the last seq)

	bus := newBus(t)
	sub := bus.Subscribe("c", []events.EventType{events.EventTypeBlock}, nil, 8)
	stop := runRelay(NewRelay(s, bus.Publish, RelayConfig{Poll: time.Hour}, nil))
	defer func() { _ = stop() }()
	var seqs []uint64
	for len(seqs) < 2 {
		select {
		case ev := <-sub.Channel:
			seqs = append(seqs, events.SequenceOf(ev))
		case <-time.After(5 * time.Second):
			t.Fatalf("received %v", seqs)
		}
	}
	require.Equal(t, []uint64{1, 3}, seqs)
}
