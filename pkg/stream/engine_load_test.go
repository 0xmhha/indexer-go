package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// loadResult is what the subscribers of a load run measured.
type loadResult struct {
	p50, p99, max time.Duration
	received      []int // events received per fast subscriber
	slowErrs      []error
}

func (r loadResult) String() string {
	return fmt.Sprintf("p50 %v p99 %v max %v", r.p50, r.p99, r.max)
}

// percentiles returns the 50th and 99th percentiles and the maximum.
func percentiles(samples []time.Duration) (p50, p99, max time.Duration) {
	slices.Sort(samples)
	at := func(q float64) time.Duration { return samples[int(q*float64(len(samples)-1))] }
	return at(0.50), at(0.99), samples[len(samples)-1]
}

// blockPayload encodes a block event the way the GraphQL newBlock topic
// does (a JSON object of a few fields).
func blockPayload(ev events.Event) ([]byte, bool) {
	b, ok := ev.(*events.BlockEvent)
	if !ok {
		return nil, false
	}
	data, err := json.Marshal(map[string]any{"data": map[string]any{"newBlock": map[string]any{
		"number": b.Number, "hash": b.Hash.Hex(), "transactionCount": b.TxCount,
	}}})
	return data, err == nil
}

// runEngineLoad publishes n events at rate per second to fast + slow
// connections of an engine. Fast connections read whenever frames are
// queued; slow ones never read. Latency is measured from the publish to
// the reader holding the frame, for every 10th event.
func runEngineLoad(t *testing.T, fast, slow, n, rate, buffer int) loadResult {
	e := NewEngine(EngineConfig{Buffer: buffer})
	e.AddTopic("newBlock", events.EventTypeBlock, blockPayload)
	published := make([]atomic.Int64, n+1) // publish time per sequence

	var wg sync.WaitGroup
	res := loadResult{received: make([]int, fast), slowErrs: make([]error, slow)}
	samples := make([][]time.Duration, fast)
	for i := 0; i < fast; i++ {
		c := e.Connect()
		require.NoError(t, c.Subscribe("1", "newBlock", nil, 0))
		wg.Add(1)
		go func(i int, c *Conn) {
			defer wg.Done()
			var last uint64
			for last < uint64(n) {
				select {
				case <-c.Ready():
				case <-c.Done():
				}
				frames, err := c.Take()
				if err != nil {
					return
				}
				now := time.Now().UnixNano()
				for _, f := range frames {
					if f.Seq != last+1 {
						return // a gap: received stays short
					}
					last = f.Seq
					res.received[i]++
					if f.Seq%10 == 0 {
						samples[i] = append(samples[i], time.Duration(now-published[f.Seq].Load()))
					}
				}
			}
		}(i, c)
	}
	slowConns := make([]*Conn, slow)
	for i := range slowConns {
		slowConns[i] = e.Connect()
		require.NoError(t, slowConns[i].Subscribe("1", "newBlock", nil, 0))
	}
	require.Equal(t, fast+slow, e.Stats().Subscriptions)

	tick := time.Second / time.Duration(rate)
	start := time.Now()
	for seq := 1; seq <= n; seq++ {
		if wait := time.Until(start.Add(time.Duration(seq) * tick)); wait > 0 {
			time.Sleep(wait)
		}
		ev := &events.BlockEvent{Number: uint64(seq), TxCount: seq % 7}
		ev.SetSequence(uint64(seq))
		published[seq].Store(time.Now().UnixNano())
		e.Publish(ev)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("fast subscribers did not receive every event")
	}
	for i, c := range slowConns {
		_, res.slowErrs[i] = c.Take()
	}
	var all []time.Duration
	for _, s := range samples {
		all = append(all, s...)
	}
	require.NotEmpty(t, all)
	res.p50, res.p99, res.max = percentiles(all)
	return res
}

// TestEngineLoad is the R3-3 criterion: 10,000 subscribers, 100 of which
// never read. Every reading subscriber receives every event in order, the
// slow ones are disconnected with the sequence to resume from, and the
// readers' p99 latency stays within the bound with the slow subscribers as
// without them.
func TestEngineLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load test")
	}
	const (
		subscribers = 10000
		slow        = 100
		n           = 1000
		rate        = 500 // events per second
		buffer      = 256
		bound       = 100 * time.Millisecond
	)
	t.Logf("GOMAXPROCS %d", runtime.GOMAXPROCS(0))
	base := runEngineLoad(t, subscribers, 0, n, rate, buffer)
	t.Logf("%d subscribers, %d events at %d/s: %v", subscribers, n, rate, base)
	withSlow := runEngineLoad(t, subscribers-slow, slow, n, rate, buffer)
	t.Logf("%d reading and %d slow subscribers: %v", subscribers-slow, slow, withSlow)

	for _, r := range []loadResult{base, withSlow} {
		for i, got := range r.received {
			require.Equal(t, n, got, "subscriber %d receives every event", i)
		}
		require.Less(t, r.p99, bound, "p99 latency")
	}
	for i, err := range withSlow.slowErrs {
		var slowErr *SlowError
		require.True(t, errors.As(err, &slowErr), "slow subscriber %d is disconnected: %v", i, err)
		require.Equal(t, uint64(1), slowErr.ResumeFrom, "it resumes from the first event it did not receive")
	}
}

// TestEventBusLoadForComparison runs the same load through the event bus
// path the GraphQL subscriptions used before the engine (a bus
// subscription per subscriber, an encoding per subscriber and event,
// events dropped when a channel is full). It only reports; run it with
// INDEXER_LOAD_COMPARE=1.
func TestEventBusLoadForComparison(t *testing.T) {
	if os.Getenv("INDEXER_LOAD_COMPARE") == "" {
		t.Skip("set INDEXER_LOAD_COMPARE=1")
	}
	const (
		subscribers = 10000
		slow        = 100
		n           = 1000
		rate        = 500
		buffer      = 256
	)
	bus := events.NewEventBus(1<<16, buffer)
	go bus.Run()
	defer bus.Stop()
	published := make([]atomic.Int64, n+1)
	var wg sync.WaitGroup
	samples := make([][]time.Duration, subscribers-slow)
	received := make([]int, subscribers-slow)
	for i := 0; i < subscribers; i++ {
		sub := bus.Subscribe(events.SubscriptionID(fmt.Sprint(i)), []events.EventType{events.EventTypeBlock}, nil, buffer)
		if i >= subscribers-slow {
			continue // never read
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for ev := range sub.Channel {
				if _, ok := blockPayload(ev); !ok {
					continue
				}
				now := time.Now().UnixNano()
				seq := events.SequenceOf(ev)
				received[i]++
				if seq%10 == 0 {
					samples[i] = append(samples[i], time.Duration(now-published[seq].Load()))
				}
				if seq == n {
					return
				}
			}
		}(i)
	}
	tick := time.Second / rate
	start := time.Now()
	for seq := 1; seq <= n; seq++ {
		if wait := time.Until(start.Add(time.Duration(seq) * tick)); wait > 0 {
			time.Sleep(wait)
		}
		ev := &events.BlockEvent{Number: uint64(seq)}
		ev.SetSequence(uint64(seq))
		published[seq].Store(time.Now().UnixNano())
		for !bus.Publish(ev) {
			time.Sleep(time.Millisecond)
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Log("readers did not finish: the last event was dropped for some")
	}
	var all []time.Duration
	short := 0
	for i, s := range samples {
		all = append(all, s...)
		if received[i] < n {
			short++
		}
	}
	p50, p99, max := percentiles(all)
	_, _, dropped := bus.Stats()
	t.Logf("event bus: p50 %v p99 %v max %v; readers missing events %d; deliveries dropped %d (slow subscribers are never disconnected)",
		p50, p99, max, short, dropped)
}
