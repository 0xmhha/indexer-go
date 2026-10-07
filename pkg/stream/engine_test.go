package stream

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// blockEvent returns a block event with sequence seq (0: none).
func blockEvent(n, seq uint64) events.Event {
	ev := &events.BlockEvent{Number: n}
	ev.SetSequence(seq)
	return ev
}

// numberEncoder encodes a block event as its number and counts calls.
func numberEncoder(calls *atomic.Int64) Encoder {
	return func(ev events.Event) ([]byte, bool) {
		if calls != nil {
			calls.Add(1)
		}
		b, ok := ev.(*events.BlockEvent)
		if !ok {
			return nil, false
		}
		return []byte(fmt.Sprint(b.Number)), true
	}
}

// takeAll reads frames until want frames arrived or the connection failed.
func takeAll(t *testing.T, c *Conn, want int) []Frame {
	t.Helper()
	var out []Frame
	deadline := time.After(5 * time.Second)
	for len(out) < want {
		frames, err := c.Take()
		require.NoError(t, err)
		out = append(out, frames...)
		if len(out) >= want {
			break
		}
		select {
		case <-c.Ready():
		case <-deadline:
			t.Fatalf("got %d frames, want %d", len(out), want)
		}
	}
	return out
}

func payloads(frames []Frame) []string {
	var out []string
	for _, f := range frames {
		out = append(out, f.Sub+":"+string(f.Payload))
	}
	return out
}

func TestEngineDeliversByTopicAndFilter(t *testing.T) {
	var calls atomic.Int64
	e := NewEngine(EngineConfig{})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(&calls))
	e.AddTopic("logs", events.EventTypeLog, func(events.Event) ([]byte, bool) { return []byte("log"), true })

	a, b := e.Connect(), e.Connect()
	require.NoError(t, a.Subscribe("all", "blocks", nil, 0))
	require.NoError(t, a.Subscribe("even", "blocks", func(ev events.Event) bool { return ev.(*events.BlockEvent).Number%2 == 0 }, 0))
	require.NoError(t, b.Subscribe("1", "blocks", nil, 0))
	require.NoError(t, b.Subscribe("l", "logs", nil, 0))
	require.ErrorIs(t, b.Subscribe("1", "blocks", nil, 0), ErrDuplicateSub)
	require.ErrorIs(t, b.Subscribe("x", "nope", nil, 0), ErrUnknownTopic)

	for n := uint64(1); n <= 4; n++ {
		e.Publish(blockEvent(n, n))
	}
	require.Equal(t, []string{"all:1", "all:2", "even:2", "all:3", "all:4", "even:4"}, payloads(takeAll(t, a, 6)))
	require.Equal(t, []string{"1:1", "1:2", "1:3", "1:4"}, payloads(takeAll(t, b, 4)))
	require.Equal(t, int64(4), calls.Load(), "each event is encoded once for the topic, whatever the number of subscriptions")

	fa, _ := a.Take()
	require.Empty(t, fa)
	st := e.Stats()
	require.Equal(t, uint64(4), st.Published)
	require.Equal(t, 2, st.Connections)
	require.Equal(t, 4, st.Subscriptions)
}

// TestEngineSharesPayloads: the subscriptions of a topic share the encoded
// bytes of an event.
func TestEngineSharesPayloads(t *testing.T) {
	e := NewEngine(EngineConfig{})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	a, b := e.Connect(), e.Connect()
	require.NoError(t, a.Subscribe("1", "blocks", nil, 0))
	require.NoError(t, b.Subscribe("1", "blocks", nil, 0))
	e.Publish(blockEvent(7, 1))
	fa, fb := takeAll(t, a, 1), takeAll(t, b, 1)
	require.Same(t, &fa[0].Payload[0], &fb[0].Payload[0])
}

// TestEngineDisconnectsSlowConnection: a connection that does not read is
// disconnected when its queue is full, with the sequence to resume from;
// the other connections receive everything.
func TestEngineDisconnectsSlowConnection(t *testing.T) {
	e := NewEngine(EngineConfig{Buffer: 4})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	slow, fast := e.Connect(), e.Connect()
	require.NoError(t, slow.Subscribe("1", "blocks", nil, 0))
	require.NoError(t, fast.Subscribe("1", "blocks", nil, 0))

	for n := uint64(1); n <= 3; n++ {
		e.Publish(blockEvent(n, n))
	}
	_, err := slow.Take() // the slow connection read 1..3
	require.NoError(t, err)
	got := takeAll(t, fast, 3)
	for n := uint64(4); n <= 10; n++ {
		e.Publish(blockEvent(n, n))
		got = append(got, takeAll(t, fast, 1)...)
	}

	select {
	case <-slow.Done():
	default:
		t.Fatal("the slow connection is disconnected")
	}
	_, err = slow.Take()
	var slowErr *SlowError
	require.ErrorAs(t, err, &slowErr)
	require.Equal(t, uint64(4), slowErr.ResumeFrom, "the first event it did not receive")
	require.Len(t, got, 10, "the fast connection receives every event")
	st := e.Stats()
	require.Equal(t, uint64(1), st.SlowDisconnects)
	require.Equal(t, 1, st.Connections)
	require.Equal(t, 1, st.Subscriptions, "the slow connection's subscriptions left the index")
}

func TestEngineUnsubscribeDropsQueuedFrames(t *testing.T) {
	e := NewEngine(EngineConfig{})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	c := e.Connect()
	require.NoError(t, c.Subscribe("a", "blocks", nil, 0))
	require.NoError(t, c.Subscribe("b", "blocks", nil, 0))
	e.Publish(blockEvent(1, 1))
	c.Unsubscribe("a")
	e.Publish(blockEvent(2, 2))
	require.Equal(t, []string{"b:1", "b:2"}, payloads(takeAll(t, c, 2)))
	require.NoError(t, c.Subscribe("a", "blocks", nil, 0), "the id can be used again")
}

func TestEngineReplaysHistory(t *testing.T) {
	e := NewEngine(EngineConfig{History: 5})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	e.AddTopic("logs", events.EventTypeLog, func(events.Event) ([]byte, bool) { return []byte("log"), true })
	for n := uint64(1); n <= 8; n++ {
		e.Publish(blockEvent(n, n))
	}
	e.Publish(&events.LogEvent{})
	c := e.Connect()
	require.NoError(t, c.Subscribe("r", "blocks", func(ev events.Event) bool { return ev.(*events.BlockEvent).Number != 7 }, 2))
	e.Publish(blockEvent(9, 9))
	require.Equal(t, []string{"r:6", "r:8", "r:9"}, payloads(takeAll(t, c, 3)),
		"the last matching events of the history, then new ones")
}

// TestEngineGapDisconnectsAll: a sequence gap means events were lost before
// the engine; every connection resubscribes from the first missing one.
func TestEngineGapDisconnectsAll(t *testing.T) {
	e := NewEngine(EngineConfig{})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	a, b := e.Connect(), e.Connect()
	require.NoError(t, a.Subscribe("1", "blocks", nil, 0))
	e.Publish(blockEvent(1, 1))
	e.Publish(blockEvent(2, 2))
	e.Publish(blockEvent(5, 5))
	for _, c := range []*Conn{a, b} {
		_, err := c.Take()
		var slowErr *SlowError
		require.ErrorAs(t, err, &slowErr)
		require.Equal(t, uint64(3), slowErr.ResumeFrom)
	}
	e.Publish(blockEvent(0, 0)) // unsequenced events never count as gaps
	c := e.Connect()
	require.NoError(t, c.Subscribe("1", "blocks", nil, 0))
	e.Publish(blockEvent(6, 6))
	require.Len(t, takeAll(t, c, 1), 1)
}

func TestEngineClose(t *testing.T) {
	e := NewEngine(EngineConfig{})
	e.AddTopic("blocks", events.EventTypeBlock, numberEncoder(nil))
	c := e.Connect()
	require.NoError(t, c.Subscribe("1", "blocks", nil, 0))
	e.Close()
	<-c.Done()
	_, err := c.Take()
	require.True(t, errors.Is(err, ErrConnClosed))
	e.Publish(blockEvent(1, 1))
	late := e.Connect()
	require.ErrorIs(t, late.Subscribe("1", "blocks", nil, 0), ErrConnClosed)
}
