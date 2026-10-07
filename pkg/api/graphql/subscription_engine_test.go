package graphql

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

const logsQuery = `subscription { logs { address logIndex data } }`

// sequencedLog returns a log event with sequence seq and size bytes of data.
func sequencedLog(seq uint64, size int) events.Event {
	ev := events.NewLogEvent(&types.Log{Address: common.Address{0xaa}, Index: uint(seq), Data: make([]byte, size)})
	ev.SetSequence(seq)
	return ev
}

// wsMsg is a message as a client reads it.
type wsMsg struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// nextSequence returns the change stream sequence of a "next" message.
func nextSequence(t *testing.T, m wsMsg) uint64 {
	t.Helper()
	var p struct {
		Extensions struct {
			Sequence uint64 `json:"sequence"`
		} `json:"extensions"`
	}
	require.NoError(t, json.Unmarshal(m.Payload, &p))
	return p.Extensions.Sequence
}

// newEngineServer serves subscriptions from a bus whose subscriptions
// queue at most buffer frames.
func newEngineServer(t *testing.T, buffer int) (*events.EventBus, *SubscriptionServer, *httptest.Server) {
	bus := events.NewEventBus(1<<16, buffer)
	go bus.Run()
	t.Cleanup(bus.Stop)
	sub := NewSubscriptionServer(bus, zap.NewNop(), true)
	srv := httptest.NewServer(sub)
	t.Cleanup(srv.Close)
	return bus, sub, srv
}

// waitSubscriptions waits until the engine indexes n subscriptions.
func waitSubscriptions(t *testing.T, s *SubscriptionServer, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		e := s.subscriptionEngine()
		return e != nil && e.Stats().Subscriptions == n
	}, 5*time.Second, 5*time.Millisecond)
}

// TestEnginePayload pins the "next" message of the engine: the GraphQL
// result with the event's sequence in extensions.
func TestEnginePayload(t *testing.T) {
	bus, sub, srv := newEngineServer(t, 16)
	conn := subscribeWS(t, srv.URL, "b", `subscription { newBlock { number hash } }`, nil)
	defer func() { _ = conn.Close() }()
	waitSubscriptions(t, sub, 1)
	ev := &events.BlockEvent{Number: 9, Hash: common.Hash{9}, TxCount: 2, CreatedAt: time.Unix(1700000000, 0)}
	ev.SetSequence(42)
	require.True(t, bus.Publish(ev))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"b","type":"next","payload":{"data":{"newBlock":{
		"number":9,"hash":"`+common.Hash{9}.Hex()+`","timestamp":1700000000,"transactionCount":2}},
		"extensions":{"sequence":42}}}`, string(data))
}

// TestSlowSubscriberIsToldWhereToResume: a client that stops reading is
// disconnected when its queue overflows; when it reads again it receives
// the events up to the overflow, then an error per subscription with the
// sequence to resubscribe from, the one after the last event it received,
// and the connection closes. A client that reads receives every event.
func TestSlowSubscriberIsToldWhereToResume(t *testing.T) {
	bus, sub, srv := newEngineServer(t, 8)
	slow := subscribeWS(t, srv.URL, "s", logsQuery, nil)
	defer func() { _ = slow.Close() }()
	fast := subscribeWS(t, srv.URL, "f", logsQuery, nil)
	defer func() { _ = fast.Close() }()
	waitSubscriptions(t, sub, 2)

	// The reading client reads in a goroutine; the test publishes the next
	// event once it received the last, so only the other client falls
	// behind. Large events fill that client's socket buffers, then its
	// queue.
	var fastSeqs []uint64
	var fastAt atomic.Uint64
	fastDone := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		for {
			var m wsMsg
			if err := fast.ReadJSON(&m); err != nil {
				select {
				case <-stop:
					fastDone <- nil
				default:
					fastDone <- err
				}
				return
			}
			if m.Type != "next" {
				fastDone <- fmt.Errorf("reading client got %s: %s", m.Type, m.Payload)
				return
			}
			var p struct {
				Extensions struct{ Sequence uint64 } `json:"extensions"`
			}
			if err := json.Unmarshal(m.Payload, &p); err != nil {
				fastDone <- err
				return
			}
			fastSeqs = append(fastSeqs, p.Extensions.Sequence)
			fastAt.Store(p.Extensions.Sequence)
		}
	}()

	e := sub.subscriptionEngine()
	var published uint64
	for e.Stats().SlowDisconnects == 0 {
		require.Less(t, published, uint64(5000), "the client that does not read is disconnected")
		published++
		require.True(t, bus.Publish(sequencedLog(published, 32<<10)))
		require.Eventually(t, func() bool { return fastAt.Load() == published }, 10*time.Second, 100*time.Microsecond)
	}
	require.Equal(t, uint64(1), e.Stats().SlowDisconnects)

	var got []uint64
	var resumeFrom uint64
	require.NoError(t, slow.SetReadDeadline(time.Now().Add(10*time.Second)))
	for {
		var m wsMsg
		err := slow.ReadJSON(&m)
		if err != nil {
			var closeErr *websocket.CloseError
			require.True(t, errors.As(err, &closeErr), "the server closes the connection: %v", err)
			require.Equal(t, websocket.ClosePolicyViolation, closeErr.Code)
			break
		}
		if m.Type == "next" {
			got = append(got, nextSequence(t, m))
			continue
		}
		require.Equal(t, "error", m.Type)
		require.Equal(t, "s", m.ID)
		var errs []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code       string `json:"code"`
				ResumeFrom uint64 `json:"resumeFrom"`
			} `json:"extensions"`
		}
		require.NoError(t, json.Unmarshal(m.Payload, &errs))
		require.Equal(t, "SLOW_SUBSCRIBER", errs[0].Extensions.Code)
		resumeFrom = errs[0].Extensions.ResumeFrom
	}
	require.NotEmpty(t, got, "the slow client received events before it fell behind")
	for i, seq := range got {
		require.Equal(t, uint64(i+1), seq, "in order without gaps")
	}
	require.Equal(t, uint64(len(got)+1), resumeFrom, "resume right after the last event received")
	require.LessOrEqual(t, resumeFrom, published)

	close(stop)
	_ = fast.Close()
	require.NoError(t, <-fastDone)
	require.Len(t, fastSeqs, int(published))
	for i, seq := range fastSeqs {
		require.Equal(t, uint64(i+1), seq, "the reading client receives every event")
	}
}

// TestDirectSubscriptions: SetDirect(true) delivers through a bus
// subscription per client subscription, with the same payload.
func TestDirectSubscriptions(t *testing.T) {
	bus := events.NewEventBus(1000, 100)
	go bus.Run()
	defer bus.Stop()
	sub := NewSubscriptionServer(bus, zap.NewNop(), true)
	sub.SetDirect(true)
	srv := httptest.NewServer(sub)
	defer srv.Close()
	conn := subscribeWS(t, srv.URL, "b", `subscription { newBlock { number } }`, nil)
	defer func() { _ = conn.Close() }()
	require.Eventually(t, func() bool { return bus.SubscriberCount() == 1 }, 5*time.Second, 5*time.Millisecond)
	require.Nil(t, sub.engine, "no engine in direct mode")
	ev := &events.BlockEvent{Number: 3}
	ev.SetSequence(5)
	require.True(t, bus.Publish(ev))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var m wsMsg
	require.NoError(t, conn.ReadJSON(&m))
	require.Equal(t, "next", m.Type)
	require.Equal(t, uint64(5), nextSequence(t, m))
}

// TestWebSocketLoad runs 10,000 WebSocket subscribers, 100 of which never
// read, against one server (the R3-3 criterion end to end, over loopback).
// Run it with INDEXER_LOAD_WS=1.
func TestWebSocketLoad(t *testing.T) {
	if os.Getenv("INDEXER_LOAD_WS") == "" {
		t.Skip("set INDEXER_LOAD_WS=1")
	}
	const (
		subscribers = 10000
		slow        = 100
		n           = 100
		rate        = 5 // events per second: 50,000 WebSocket messages per second
	)
	bus, sub, srv := newEngineServer(t, 16384) // the default queue size
	direct := os.Getenv("INDEXER_LOAD_WS_DIRECT") != ""
	sub.SetDirect(direct) // the former delivery, for comparison
	published := make([]atomic.Int64, n+1)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var samples []time.Duration
	var short atomic.Int64
	conns := make([]*websocket.Conn, subscribers)
	for i := range conns {
		conns[i] = subscribeWS(t, srv.URL, "1", `subscription { newBlock { number } }`, nil)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	if direct {
		require.Eventually(t, func() bool { return bus.SubscriberCount() == subscribers }, time.Minute, 10*time.Millisecond)
	} else {
		waitSubscriptions(t, sub, subscribers)
	}
	for i := slow; i < subscribers; i++ {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()
			var last uint64
			var mine []time.Duration
			defer func() {
				mu.Lock()
				samples = append(samples, mine...)
				mu.Unlock()
			}()
			_ = c.SetReadDeadline(time.Now().Add(time.Minute))
			for last < n {
				var m wsMsg
				if err := c.ReadJSON(&m); err != nil {
					if short.Add(1) == 1 {
						t.Logf("first failed reader at %d: %v", last, err)
					}
					return
				}
				now := time.Now().UnixNano()
				var p struct {
					Extensions struct{ Sequence uint64 } `json:"extensions"`
				}
				if json.Unmarshal(m.Payload, &p) != nil || p.Extensions.Sequence != last+1 {
					if short.Add(1) == 1 {
						t.Logf("first failed reader at %d: %s %s", last, m.Type, m.Payload)
					}
					return
				}
				last = p.Extensions.Sequence
				if last%10 == 0 {
					mine = append(mine, time.Duration(now-published[last].Load()))
				}
			}
		}(conns[i])
	}
	tick := time.Second / rate
	start := time.Now()
	for seq := 1; seq <= n; seq++ {
		if wait := time.Until(start.Add(time.Duration(seq) * tick)); wait > 0 {
			time.Sleep(wait)
		}
		ev := &events.BlockEvent{Number: uint64(seq), Hash: common.Hash{byte(seq)}}
		ev.SetSequence(uint64(seq))
		published[seq].Store(time.Now().UnixNano())
		require.True(t, bus.Publish(ev))
	}
	wg.Wait()
	require.Zero(t, short.Load(), "every reading subscriber receives every event")
	slices.Sort(samples)
	p := func(q float64) time.Duration { return samples[int(q*float64(len(samples)-1))] }
	t.Logf("direct %v: %d WebSocket subscribers (%d not reading), %d events at %d/s: p50 %v p99 %v max %v",
		direct, subscribers, slow, n, rate, p(0.5), p(0.99), samples[len(samples)-1])
}

// TestReplayCoversEventsBeforeFirstConnection: the engine starts with the
// server, so replayLast replays events published before anyone connected.
func TestReplayCoversEventsBeforeFirstConnection(t *testing.T) {
	bus, sub, srv := newEngineServer(t, 16)
	for n := uint64(1); n <= 3; n++ {
		ev := &events.BlockEvent{Number: n}
		ev.SetSequence(n)
		require.True(t, bus.Publish(ev))
	}
	require.Eventually(t, func() bool { return sub.subscriptionEngine().Stats().Published == 3 }, 5*time.Second, 5*time.Millisecond)
	conn := subscribeWS(t, srv.URL, "b", `subscription { newBlock { number } }`, map[string]any{"replayLast": 2})
	defer func() { _ = conn.Close() }()
	var seqs []uint64
	for len(seqs) < 2 {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		var m wsMsg
		require.NoError(t, conn.ReadJSON(&m))
		seqs = append(seqs, nextSequence(t, m))
	}
	require.Equal(t, []uint64{2, 3}, seqs)
}
