package events

import (
	"testing"
	"time"
)

// eventually polls cond until it holds or five seconds pass. The bus
// delivers and counts asynchronously (deliveries are counted after the
// channel send), so tests wait for the counts they expect instead of
// sleeping a fixed time.
func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// deliveries is the bus's delivery count.
func deliveries(bus *EventBus) uint64 {
	_, d, _ := bus.Stats()
	return d
}
