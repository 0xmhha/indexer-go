package nativealerts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueueBackpressureRetainsCriticalWithoutResolving(t *testing.T) {
	e := engine(t)
	seq := uint64(0)
	for range MaxQueued / 2 {
		seq++
		apply(t, e, seq, 99)
		seq++
		apply(t, e, seq, 0)
		seq++
		apply(t, e, seq, 0)
	}
	if len(e.Snapshot().Deliveries) != MaxQueued {
		t.Fatal("queue not full")
	}
	seq++
	in := signal(seq, 99)
	if !errors.Is(e.Apply(rules().Key, in, in.ObservedAt), ErrBackpressure) {
		t.Fatal("overflow not reported")
	}
	first := e.Snapshot()
	if first.Records[0].Deferred == nil {
		t.Fatal("critical transition lost")
	}
	id := first.Records[0].Deferred.ID
	seq++
	in = signal(seq, 0)
	if !errors.Is(e.Apply(rules().Key, in, in.ObservedAt), ErrBackpressure) {
		t.Fatal("unreported critical cleared")
	}
	st, _ := e.State(rules().Key)
	if st.Active != Critical || e.Snapshot().Records[0].Deferred.ID != id {
		t.Fatal("critical discarded")
	}
	if err := validateSnapshot(e.Snapshot()); err != nil {
		t.Fatal(err)
	}
	next := engine(t)
	if err := next.Restore(e.Snapshot()); err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{}
	sink, _ := NewFakeSink(4)
	if err := next.Dispatch(context.Background(), in.ObservedAt, sink, store); err != nil {
		t.Fatal(err)
	}
	if err := next.Dispatch(context.Background(), in.ObservedAt, sink, store); err != nil {
		t.Fatal(err)
	}
	last := next.Snapshot()
	if last.Records[0].Deferred != nil {
		t.Fatal("deferred was not moved into available queue")
	}
	found := false
	for _, d := range last.Deliveries {
		found = found || d.Event.ID == id
	}
	if !found {
		t.Fatal("deferred critical identity changed")
	}
}

func TestPostReceiptPersistenceFailureReplaysSameDelivery(t *testing.T) {
	e := engine(t)
	apply(t, e, 1, 99)
	store := &memoryStore{failAt: 2}
	sink, _ := NewFakeSink(4)
	at := signal(1, 0).ObservedAt
	if !errors.Is(e.Dispatch(context.Background(), at, sink, store), ErrCheckpoint) {
		t.Fatal("post-receipt save failure hidden")
	}
	if len(e.Snapshot().Deliveries) != 1 || len(sink.Events()) != 1 || !e.PersistenceFailed() {
		t.Fatal("unconfirmed local removal")
	}
	next := engine(t)
	if err := next.Restore(store.snapshot); err != nil {
		t.Fatal(err)
	}
	if err := next.Dispatch(context.Background(), at.Add(time.Second), sink, store); err != nil {
		t.Fatal(err)
	}
	events := sink.Events()
	if len(events) != 2 || events[0].ID != events[1].ID {
		t.Fatal("restart changed retry ID")
	}
}

func TestDeadLetterDoesNotSendLaterResolution(t *testing.T) {
	e := engine(t)
	apply(t, e, 1, 99)
	apply(t, e, 2, 0)
	apply(t, e, 3, 0)
	store := &memoryStore{}
	sink, _ := NewFakeSink(8)
	sink.Fail = true
	for _, seconds := range []int{3, 4, 6} {
		if !errors.Is(e.Dispatch(context.Background(), clock().Add(time.Duration(seconds)*time.Second), sink, store), ErrDelivery) {
			t.Fatal("failed delivery confirmed")
		}
	}
	if !e.Snapshot().Deliveries[0].Dead {
		t.Fatal("retry budget not bounded")
	}
	sink.Fail = false
	if err := e.Dispatch(context.Background(), clock().Add(time.Hour), sink, store); err != nil {
		t.Fatal(err)
	}
	if len(sink.Events()) != MaxAttempts || len(e.Snapshot().Deliveries) != 2 {
		t.Fatal("resolution overtook unconfirmed critical")
	}
}

func TestDisabledCancelledAndImmutableSnapshots(t *testing.T) {
	e := engine(t)
	apply(t, e, 1, 99)
	store := &memoryStore{}
	if !errors.Is(e.Dispatch(context.Background(), clock(), DisabledSink{}, store), ErrDisabled) || store.writes != 0 {
		t.Fatal("disabled sink ran IO")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink, _ := NewFakeSink(4)
	if e.Dispatch(ctx, clock(), sink, store) == nil || len(sink.Events()) != 0 {
		t.Fatal("cancelled dispatch sent")
	}
	s := e.Snapshot()
	s.Deliveries[0].Event.Level = Normal
	s.Records[0].Active = Normal
	st, _ := e.State(rules().Key)
	if st.Active != Critical || e.Snapshot().Deliveries[0].Event.Level != Critical {
		t.Fatal("borrowed snapshot mutated engine")
	}
	if err := e.Load(context.Background(), &memoryStore{}); err == nil {
		t.Fatal("corrupt snapshot loaded")
	}
	st, _ = e.State(rules().Key)
	if st.Active != Critical || st.Quality != StateLost || !e.PersistenceFailed() {
		t.Fatal("state loss erased critical")
	}
	if e.Persist(context.Background(), store) == nil || e.Dispatch(context.Background(), clock(), sink, store) == nil {
		t.Fatal("failed load silently overwrote/recovered state")
	}
}
