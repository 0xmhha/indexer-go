package rpcproxy

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRequest(id string, priority Priority) *Request {
	return &Request{
		ID:        id,
		Priority:  priority,
		CreatedAt: time.Now(),
		Timeout:   time.Second,
	}
}

// ========== PriorityQueue ==========

func TestPriorityQueue_EnqueueDequeue(t *testing.T) {
	pq := NewPriorityQueue(10)

	req := newTestRequest("r1", PriorityNormal)
	ok := pq.Enqueue(req)
	assert.True(t, ok)
	assert.Equal(t, 1, pq.Size())

	got, ok := pq.TryDequeue()
	require.True(t, ok)
	assert.Equal(t, "r1", got.ID)
	assert.Equal(t, 0, pq.Size())
}

func TestPriorityQueue_PriorityOrder(t *testing.T) {
	pq := NewPriorityQueue(10)

	// Enqueue in reverse priority order
	pq.Enqueue(newTestRequest("normal", PriorityNormal))
	pq.Enqueue(newTestRequest("critical", PriorityCritical))
	pq.Enqueue(newTestRequest("high", PriorityHigh))

	// Should dequeue in priority order (lower value = higher priority)
	r1, ok := pq.TryDequeue()
	require.True(t, ok)
	assert.Equal(t, "critical", r1.ID)

	r2, ok := pq.TryDequeue()
	require.True(t, ok)
	assert.Equal(t, "high", r2.ID)

	r3, ok := pq.TryDequeue()
	require.True(t, ok)
	assert.Equal(t, "normal", r3.ID)
}

func TestPriorityQueue_FIFOWithinSamePriority(t *testing.T) {
	pq := NewPriorityQueue(10)

	now := time.Now()
	r1 := &Request{ID: "first", Priority: PriorityNormal, CreatedAt: now, Timeout: time.Second}
	r2 := &Request{ID: "second", Priority: PriorityNormal, CreatedAt: now.Add(time.Millisecond), Timeout: time.Second}

	pq.Enqueue(r1)
	pq.Enqueue(r2)

	got, _ := pq.TryDequeue()
	assert.Equal(t, "first", got.ID)

	got, _ = pq.TryDequeue()
	assert.Equal(t, "second", got.ID)
}

func TestPriorityQueue_Full(t *testing.T) {
	pq := NewPriorityQueue(2)

	assert.True(t, pq.Enqueue(newTestRequest("r1", PriorityNormal)))
	assert.True(t, pq.Enqueue(newTestRequest("r2", PriorityNormal)))
	assert.False(t, pq.Enqueue(newTestRequest("r3", PriorityNormal)), "should reject when full")

	_, _, dropped, _ := pq.Stats()
	assert.Equal(t, int64(1), dropped)
}

func TestPriorityQueue_TryDequeue_Empty(t *testing.T) {
	pq := NewPriorityQueue(10)

	req, ok := pq.TryDequeue()
	assert.False(t, ok)
	assert.Nil(t, req)
}

func TestPriorityQueue_DequeueBlocking(t *testing.T) {
	pq := NewPriorityQueue(10)

	// Dequeue in goroutine (blocks until item available)
	resultCh := make(chan *Request, 1)
	go func() {
		req, ok := pq.Dequeue()
		if ok {
			resultCh <- req
		}
	}()

	// Small delay then enqueue
	time.Sleep(20 * time.Millisecond)
	pq.Enqueue(newTestRequest("delayed", PriorityNormal))

	select {
	case req := <-resultCh:
		assert.Equal(t, "delayed", req.ID)
	case <-time.After(time.Second):
		t.Fatal("Dequeue did not unblock")
	}
}

func TestPriorityQueue_DequeueWithTimeout_Success(t *testing.T) {
	pq := NewPriorityQueue(10)

	pq.Enqueue(newTestRequest("r1", PriorityNormal))

	req, ok := pq.DequeueWithTimeout(time.Second)
	require.True(t, ok)
	assert.Equal(t, "r1", req.ID)
}

func TestPriorityQueue_DequeueWithTimeout_Timeout(t *testing.T) {
	pq := NewPriorityQueue(10)

	start := time.Now()
	req, ok := pq.DequeueWithTimeout(50 * time.Millisecond)
	elapsed := time.Since(start)

	assert.False(t, ok)
	assert.Nil(t, req)
	assert.True(t, elapsed >= 50*time.Millisecond, "should wait at least the timeout duration")
}

func TestPriorityQueue_Close(t *testing.T) {
	pq := NewPriorityQueue(10)

	pq.Enqueue(newTestRequest("r1", PriorityNormal))
	pq.Close()

	assert.True(t, pq.IsClosed())

	// Should not accept new items
	ok := pq.Enqueue(newTestRequest("r2", PriorityNormal))
	assert.False(t, ok)

	// Should still be able to drain existing items
	req, ok := pq.TryDequeue()
	assert.False(t, ok, "TryDequeue returns false when closed")
	assert.Nil(t, req)
}

func TestPriorityQueue_Close_UnblocksDequeue(t *testing.T) {
	pq := NewPriorityQueue(10)

	done := make(chan struct{})
	go func() {
		_, ok := pq.Dequeue()
		assert.False(t, ok)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	pq.Close()

	select {
	case <-done:
		// OK — Dequeue unblocked
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock Dequeue")
	}
}

func TestPriorityQueue_Drain(t *testing.T) {
	pq := NewPriorityQueue(10)

	pq.Enqueue(newTestRequest("r1", PriorityCritical))
	pq.Enqueue(newTestRequest("r2", PriorityHigh))
	pq.Enqueue(newTestRequest("r3", PriorityNormal))

	drained := pq.Drain()
	assert.Len(t, drained, 3)
	assert.Equal(t, 0, pq.Size())

	// Drained items come in priority order (heap pops highest first)
	assert.Equal(t, "r1", drained[0].ID)
	assert.Equal(t, "r2", drained[1].ID)
	assert.Equal(t, "r3", drained[2].ID)
}

func TestPriorityQueue_Drain_Empty(t *testing.T) {
	pq := NewPriorityQueue(10)

	drained := pq.Drain()
	assert.Empty(t, drained)
}

func TestPriorityQueue_Stats(t *testing.T) {
	pq := NewPriorityQueue(2)

	pq.Enqueue(newTestRequest("r1", PriorityNormal))
	pq.Enqueue(newTestRequest("r2", PriorityNormal))
	pq.Enqueue(newTestRequest("r3", PriorityNormal)) // dropped

	pq.TryDequeue() // dequeued

	enqueued, dequeued, dropped, size := pq.Stats()
	assert.Equal(t, int64(2), enqueued)
	assert.Equal(t, int64(1), dequeued)
	assert.Equal(t, int64(1), dropped)
	assert.Equal(t, 1, size)
}

func TestPriorityQueue_ConcurrentAccess(t *testing.T) {
	pq := NewPriorityQueue(100)

	var wg sync.WaitGroup

	// Producers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pq.Enqueue(newTestRequest("r", Priority(i%3)))
		}(i)
	}

	// Consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			pq.DequeueWithTimeout(50 * time.Millisecond)
		}
	}()

	wg.Wait()
	// No panic or deadlock
}

// ========== MultiPriorityQueue ==========

func TestMultiPriorityQueue_PriorityOrder(t *testing.T) {
	mpq := NewMultiPriorityQueue(10)

	// Enqueue to different priority queues
	mpq.Enqueue(newTestRequest("normal", PriorityNormal))
	mpq.Enqueue(newTestRequest("high", PriorityHigh))
	mpq.Enqueue(newTestRequest("critical", PriorityCritical))

	// Dequeue should try critical first
	req, ok := mpq.Dequeue()
	require.True(t, ok)
	assert.Equal(t, "critical", req.ID)

	req, ok = mpq.Dequeue()
	require.True(t, ok)
	assert.Equal(t, "high", req.ID)

	req, ok = mpq.Dequeue()
	require.True(t, ok)
	assert.Equal(t, "normal", req.ID)
}

func TestMultiPriorityQueue_Empty(t *testing.T) {
	mpq := NewMultiPriorityQueue(10)

	req, ok := mpq.Dequeue()
	assert.False(t, ok)
	assert.Nil(t, req)
}

func TestMultiPriorityQueue_InvalidPriority(t *testing.T) {
	mpq := NewMultiPriorityQueue(10)

	ok := mpq.Enqueue(newTestRequest("r1", Priority(99)))
	assert.False(t, ok, "should reject unknown priority")
}

func TestMultiPriorityQueue_TotalSize(t *testing.T) {
	mpq := NewMultiPriorityQueue(10)

	mpq.Enqueue(newTestRequest("r1", PriorityCritical))
	mpq.Enqueue(newTestRequest("r2", PriorityHigh))
	mpq.Enqueue(newTestRequest("r3", PriorityNormal))

	assert.Equal(t, 3, mpq.TotalSize())
}

func TestMultiPriorityQueue_Close(t *testing.T) {
	mpq := NewMultiPriorityQueue(10)

	mpq.Enqueue(newTestRequest("r1", PriorityCritical))
	mpq.Close()

	// All sub-queues should be closed
	ok := mpq.Enqueue(newTestRequest("r2", PriorityCritical))
	assert.False(t, ok)
}
