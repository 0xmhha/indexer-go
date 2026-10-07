package stream

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// liveChain is an outbox written like the indexer writes it, relayed to an
// engine like the API serves it: a block is committed, then its events
// reach the engine through the relay.
type liveChain struct {
	t      *testing.T
	store  *storage.PebbleStorage
	bus    *OutboxBus
	engine *Engine
	stop   func() error
}

func newLiveChain(t *testing.T, buffer int) *liveChain {
	s := newStore(t)
	b := NewOutboxBus(s, OutboxBusConfig{Poll: time.Hour}, nil)
	e := NewEngine(EngineConfig{Buffer: buffer})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	e.SetOutbox(s)
	_, err := b.Join(context.Background(), DefaultGroup, StartEarliest)
	require.NoError(t, err)
	r := NewRelay(b, "", func(ev events.Event) bool { e.Publish(ev); return true }, nil)
	stop := runRelay(r)
	t.Cleanup(func() { _ = stop() })
	return &liveChain{t: t, store: s, bus: b, engine: e, stop: stop}
}

// commit commits n events, at most 50 per block, and wakes the relay.
func (lc *liveChain) commit(n int) {
	for n > 0 {
		k := min(n, 50)
		appendBlocks(lc.t, lc.store, 1, k)
		n -= k
	}
	lc.bus.Notify()
}

// waitPublished waits until the engine published every committed event.
func (lc *liveChain) waitPublished() {
	lc.t.Helper()
	last, err := lc.store.LastOutboxSeq(context.Background())
	require.NoError(lc.t, err)
	require.Eventually(lc.t, func() bool { return lc.engine.Stats().Published == last }, 10*time.Second, time.Millisecond)
}

// reader takes frames from c until it holds up to seq or c fails.
func readUpTo(t *testing.T, c *Conn, seq uint64) ([]uint64, error) {
	t.Helper()
	var got []uint64
	deadline := time.After(20 * time.Second)
	for len(got) == 0 || got[len(got)-1] < seq {
		frames, err := c.Take()
		if err != nil {
			return got, err
		}
		for _, f := range frames {
			n, _ := strconv.ParseUint(string(f.Payload), 10, 64)
			require.Equal(t, f.Seq, n, "the frame carries its event")
			got = append(got, f.Seq)
		}
		if len(got) > 0 && got[len(got)-1] >= seq {
			break
		}
		select {
		case <-c.Ready():
		case <-c.Done():
		case <-deadline:
			t.Fatalf("got up to %v, want %d", got[max(0, len(got)-3):], seq)
		}
	}
	return got, nil
}

func requireRange(t *testing.T, got []uint64, from, to uint64) {
	t.Helper()
	require.Len(t, got, int(to-from+1), "sequences %d..%d", from, to)
	for i, seq := range got {
		require.Equal(t, from+uint64(i), seq, "in order, without gaps or repeats")
	}
}

// TestResumeWhileEventsArrive is the R3-4 criterion at the engine: a
// subscriber that resumes from an earlier sequence while blocks keep being
// committed and published receives every sequence from there once, in
// order, the stored ones first and the live ones after.
func TestResumeWhileEventsArrive(t *testing.T) {
	for _, from := range []uint64{1, 700, 1500} {
		t.Run(fmt.Sprint(from), func(t *testing.T) {
			lc := newLiveChain(t, 64)
			lc.commit(1500)
			lc.waitPublished()

			var committing atomic.Bool
			committing.Store(true)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for i := 0; i < 100; i++ {
					lc.commit(10)
					time.Sleep(time.Millisecond)
				}
				committing.Store(false)
			}()
			c := lc.engine.Connect()
			require.NoError(t, c.SubscribeFrom(context.Background(), "1", "blocks", nil, from))
			got, err := readUpTo(t, c, 2500)
			require.NoError(t, err)
			<-done
			requireRange(t, got, from, 2500)
			require.False(t, committing.Load())
		})
	}
}

// TestResumeAfterDisconnect: a connection that stops reading is
// disconnected with the sequence to resume from; a new connection that
// subscribes from it receives what the first one missed and then the live
// events, so the two together hold every sequence once.
func TestResumeAfterDisconnect(t *testing.T) {
	lc := newLiveChain(t, 16)
	first := lc.engine.Connect()
	require.NoError(t, first.Subscribe("1", "blocks", nil, 0))
	lc.commit(5)
	got, err := readUpTo(t, first, 5)
	require.NoError(t, err)
	lc.commit(100) // first does not read: its queue overflows
	lc.waitPublished()
	_, err = first.Take()
	var slow *SlowError
	require.ErrorAs(t, err, &slow)
	require.Equal(t, uint64(6), slow.ResumeFrom)

	second := lc.engine.Connect()
	require.NoError(t, second.SubscribeFrom(context.Background(), "1", "blocks", nil, slow.ResumeFrom))
	lc.commit(20)
	more, err := readUpTo(t, second, 125)
	require.NoError(t, err)
	requireRange(t, append(got, more...), 1, 125)
}

// TestResumeFiltersAndTopics: a resumed subscription receives the stored
// events of its type that match its filter, like a live one.
func TestResumeFiltersAndTopics(t *testing.T) {
	lc := newLiveChain(t, 64)
	lc.engine.AddTopic("logs", events.EventTypeLog, func(events.Event) ([]byte, bool) { return []byte("log"), true })
	lc.commit(20)
	lc.waitPublished()
	c := lc.engine.Connect()
	even := func(ev events.Event) bool { return ev.(*events.BlockEvent).Number%2 == 0 }
	require.NoError(t, c.SubscribeFrom(context.Background(), "even", "blocks", even, 5))
	require.NoError(t, c.SubscribeFrom(context.Background(), "logs", "logs", nil, 1))
	lc.commit(4)
	got, err := readUpTo(t, c, 24)
	require.NoError(t, err)
	require.Equal(t, []uint64{6, 8, 10, 12, 14, 16, 18, 20, 22, 24}, got)
}

func TestResumeTooOld(t *testing.T) {
	lc := newLiveChain(t, 64)
	lc.commit(50)
	lc.waitPublished()
	require.NoError(t, lc.store.PruneOutbox(context.Background(), 31))

	c := lc.engine.Connect()
	err := c.SubscribeFrom(context.Background(), "1", "blocks", nil, 10)
	var tooOld *TooOldError
	require.ErrorAs(t, err, &tooOld)
	require.Equal(t, uint64(31), tooOld.Oldest)
	require.NoError(t, c.SubscribeFrom(context.Background(), "1", "blocks", nil, 31), "the id is free again")
	got, err := readUpTo(t, c, 50)
	require.NoError(t, err)
	requireRange(t, got, 31, 50)

	require.NoError(t, lc.store.PruneOutbox(context.Background(), 51))
	err = lc.engine.Connect().SubscribeFrom(context.Background(), "1", "blocks", nil, 40)
	require.ErrorAs(t, err, &tooOld, "nothing from 40 is kept although it was committed")
	require.Zero(t, tooOld.Oldest)
}

// TestResumeFromFuture: a sequence beyond the last event starts there.
func TestResumeFromFuture(t *testing.T) {
	lc := newLiveChain(t, 64)
	lc.commit(3)
	lc.waitPublished()
	c := lc.engine.Connect()
	require.NoError(t, c.SubscribeFrom(context.Background(), "1", "blocks", nil, 6))
	lc.commit(5)
	got, err := readUpTo(t, c, 8)
	require.NoError(t, err)
	requireRange(t, got, 6, 8)
}

// TestResumeLongBackfillDoesNotOverflow: reading more stored events than a
// connection's queue holds waits for the writer instead of disconnecting.
func TestResumeLongBackfillDoesNotOverflow(t *testing.T) {
	lc := newLiveChain(t, 8)
	lc.commit(600)
	lc.waitPublished()
	c := lc.engine.Connect()
	require.NoError(t, c.SubscribeFrom(context.Background(), "1", "blocks", nil, 1))
	got, err := readUpTo(t, c, 600)
	require.NoError(t, err)
	requireRange(t, got, 1, 600)
	require.Zero(t, lc.engine.Stats().SlowDisconnects)
}

func TestResumeWithoutOutbox(t *testing.T) {
	e := NewEngine(EngineConfig{})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	err := e.Connect().SubscribeFrom(context.Background(), "1", "blocks", nil, 1)
	require.True(t, errors.Is(err, ErrNoOutbox))
}
