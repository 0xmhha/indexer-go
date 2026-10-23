package events

import (
	"sync"
	"testing"
)

func TestEventBusStopDrainsAccepted(t *testing.T) {
	const count = 128
	bus := NewEventBus(count, count)
	sub := bus.Subscribe("drain", []EventType{EventTypeBlock}, nil, count)
	for i := 1; i <= count; i++ {
		ev := &BlockEvent{Number: uint64(i)}
		ev.SetSequence(uint64(i))
		if !bus.Publish(ev) {
			t.Fatal("initial enqueue refused")
		}
	}
	stopped := make(chan struct{})
	go func() { bus.Stop(); close(stopped) }()
	<-bus.ctx.Done() // All accepted events are pending when shutdown begins.
	bus.Run()
	<-stopped
	var got int
	for ev := range sub.Channel {
		got++
		if SequenceOf(ev) != uint64(got) {
			t.Fatalf("out of order: %d", SequenceOf(ev))
		}
	}
	if got != count || bus.LastSequence() != count {
		t.Fatalf("shutdown discarded accepted events: delivered=%d last=%d want=%d", got, bus.LastSequence(), count)
	}
	if bus.Publish(&BlockEvent{}) {
		t.Fatal("publish accepted after stop")
	}
	bus.Stop() // Repeated shutdown must join the same completion.
}

func TestEventBusConcurrentStopPublishPreservesAccepted(t *testing.T) {
	const producers = 32
	bus := NewEventBus(producers+1, producers+1)
	sub := bus.Subscribe("concurrent-stop", []EventType{EventTypeBlock}, nil, producers+1)
	if !bus.Publish(&BlockEvent{Number: producers}) {
		t.Fatal("initial enqueue refused")
	}
	start := make(chan struct{})
	accepted := make(chan uint64, producers+1)
	accepted <- producers
	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		wg.Go(func() {
			<-start
			if bus.Publish(&BlockEvent{Number: uint64(i)}) {
				accepted <- uint64(i)
			}
		})
	}
	stopped := make(chan struct{})
	go func() { <-start; bus.Stop(); close(stopped) }()
	go bus.Run()
	close(start)
	wg.Wait()
	<-stopped
	close(accepted)
	want := map[uint64]bool{}
	for n := range accepted {
		want[n] = true
	}
	for ev := range sub.Channel {
		n := ev.(*BlockEvent).Number
		if !want[n] {
			t.Fatalf("unknown or duplicate event %d", n)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		t.Fatalf("shutdown lost %d accepted events", len(want))
	}
}
