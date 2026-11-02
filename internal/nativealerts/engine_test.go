package nativealerts

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	snapshot       Snapshot
	writes, failAt int
}

func (m *memoryStore) Read(context.Context) (Snapshot, error) { return m.snapshot, nil }
func (m *memoryStore) Write(_ context.Context, s Snapshot) error {
	m.writes++
	if m.writes == m.failAt {
		return ErrCheckpoint
	}
	m.snapshot = s
	return nil
}
func engine(t *testing.T) *Engine {
	t.Helper()
	e, err := New([]Rule{rules()}, Incarnation{1})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func apply(t *testing.T, e *Engine, seq, value uint64) {
	t.Helper()
	in := signal(seq, value)
	if err := e.Apply(rules().Key, in, in.ObservedAt); err != nil {
		t.Fatal(err)
	}
}

func TestRestartKeepsCriticalAndResetsClearBaseline(t *testing.T) {
	e := engine(t)
	apply(t, e, 1, 99)
	s := e.Snapshot()
	next := engine(t)
	if err := next.Restore(s); err != nil {
		t.Fatal(err)
	}
	apply(t, next, 1, 0)
	st, _ := next.State(rules().Key)
	if st.Active != Critical {
		t.Fatal("restart resolved a known critical")
	}
	apply(t, next, 2, 0)
	st, _ = next.State(rules().Key)
	if st.Active != Critical {
		t.Fatal("restart reused clear dwell")
	}
	apply(t, next, 3, 0)
	st, _ = next.State(rules().Key)
	if st.Active != Normal {
		t.Fatal("new valid baseline never cleared")
	}
}

func TestRestoreRejectsReorderedTransitions(t *testing.T) {
	e := engine(t)
	apply(t, e, 1, 99)
	apply(t, e, 2, 0)
	apply(t, e, 3, 0)
	s := e.Snapshot()
	s.Deliveries[0], s.Deliveries[1] = s.Deliveries[1], s.Deliveries[0]
	if err := engine(t).Restore(s); err == nil {
		t.Fatal("corrupt delivery order admitted")
	}
}

func TestLiveRestoreCannotEraseUnreportedCritical(t *testing.T) {
	e := engine(t)
	old := e.Snapshot()
	apply(t, e, 1, 99)
	if err := e.Restore(old); err == nil {
		t.Fatal("live restore discarded a confirmed incident")
	}
	st, _ := e.State(rules().Key)
	if st.Active != Critical {
		t.Fatal("active critical lost")
	}
}

func TestDeliveryRequiresDurabilityAndExactReceipt(t *testing.T) {
	e := engine(t)
	apply(t, e, 1, 99)
	m := &memoryStore{failAt: 1}
	sink, _ := NewFakeSink(8)
	at := signal(1, 0).ObservedAt
	if !errors.Is(e.Dispatch(context.Background(), at, sink, m), ErrCheckpoint) || len(sink.Events()) != 0 {
		t.Fatal("sent before persistence")
	}
	m.failAt = 0
	sink.WrongReceipt = true
	if !errors.Is(e.Dispatch(context.Background(), at, sink, m), ErrDelivery) {
		t.Fatal("wrong receipt treated as delivered")
	}
	id := e.Snapshot().Deliveries[0].Event.ID
	if err := e.Dispatch(context.Background(), at.Add(time.Second), sink, m); !errors.Is(err, ErrDelivery) {
		t.Fatal(err)
	}
	sink.WrongReceipt = false
	if err := e.Dispatch(context.Background(), at.Add(3*time.Second), sink, m); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot().Deliveries) != 0 {
		t.Fatal("exact receipt not removed")
	}
	for _, ev := range sink.Events() {
		if ev.ID != id {
			t.Fatal("retry changed stable delivery ID")
		}
	}
}
