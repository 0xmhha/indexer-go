package events

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetAllSubscriberInfoUnderWriters covers C3: GetAllSubscriberInfo took
// the read lock and then called GetSubscriberInfo, which took it again. With
// a writer waiting in between, sync.RWMutex blocks the second read lock and
// the call deadlocks.
func TestGetAllSubscriberInfoUnderWriters(t *testing.T) {
	bus := NewEventBus(100, 10)
	go bus.Run()
	defer bus.Stop()
	for i := 0; i < 200; i++ {
		bus.Subscribe(SubscriptionID(fmt.Sprintf("s%d", i)), []EventType{EventTypeBlock}, nil, 1)
	}

	var stop atomic.Bool
	go func() { // writer: keeps taking the write lock
		for i := 0; !stop.Load(); i++ {
			id := SubscriptionID(fmt.Sprintf("w%d", i%10))
			bus.Subscribe(id, []EventType{EventTypeBlock}, nil, 1)
			bus.Unsubscribe(id)
		}
	}()
	defer stop.Store(true)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			_ = bus.GetAllSubscriberInfo()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("GetAllSubscriberInfo deadlocked")
	}
}
