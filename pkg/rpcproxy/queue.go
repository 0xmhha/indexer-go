package rpcproxy

import (
	"container/heap"
	"sync"
	"time"
)

// PriorityQueue implements a thread-safe priority queue for requests
type PriorityQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	items    requestHeap
	maxSize  int
	closed   bool
	enqueued int64
	dequeued int64
	dropped  int64
}

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue(maxSize int) *PriorityQueue {
	pq := &PriorityQueue{
		items:   make(requestHeap, 0),
		maxSize: maxSize,
	}
	pq.cond = sync.NewCond(&pq.mu)
	heap.Init(&pq.items)
	return pq
}

// Enqueue adds a request to the queue
// Returns false if the queue is full or closed
func (pq *PriorityQueue) Enqueue(req *Request) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.closed {
		return false
	}

	// Check if queue is full
	if pq.items.Len() >= pq.maxSize {
		pq.dropped++
		return false
	}

	heap.Push(&pq.items, req)
	pq.enqueued++
	pq.cond.Signal()
	return true
}

// Dequeue removes and returns the highest priority request
// Blocks if the queue is empty
func (pq *PriorityQueue) Dequeue() (*Request, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	// Wait for items or close
	for pq.items.Len() == 0 && !pq.closed {
		pq.cond.Wait()
	}

	if pq.closed && pq.items.Len() == 0 {
		return nil, false
	}

	item := heap.Pop(&pq.items)
	req, ok := item.(*Request)
	if !ok {
		return nil, false
	}
	pq.dequeued++
	return req, true
}

// TryDequeue attempts to dequeue without blocking
// Returns nil, false if queue is empty
func (pq *PriorityQueue) TryDequeue() (*Request, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.items.Len() == 0 || pq.closed {
		return nil, false
	}

	item := heap.Pop(&pq.items)
	req, ok := item.(*Request)
	if !ok {
		return nil, false
	}
	pq.dequeued++
	return req, true
}

// DequeueWithTimeout attempts to dequeue with a timeout
func (pq *PriorityQueue) DequeueWithTimeout(timeout time.Duration) (*Request, bool) {
	deadline := time.Now().Add(timeout)

	pq.mu.Lock()
	defer pq.mu.Unlock()

	for pq.items.Len() == 0 && !pq.closed {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, false
		}

		// Use a timer to wake up the condition
		done := make(chan struct{})
		go func() {
			timer := time.NewTimer(remaining)
			defer timer.Stop()
			select {
			case <-timer.C:
				pq.cond.Broadcast()
			case <-done:
			}
		}()

		pq.cond.Wait()
		close(done)

		if time.Now().After(deadline) {
			return nil, false
		}
	}

	if pq.closed && pq.items.Len() == 0 {
		return nil, false
	}

	item := heap.Pop(&pq.items)
	req, ok := item.(*Request)
	if !ok {
		return nil, false
	}
	pq.dequeued++
	return req, true
}

// Size returns the current number of items in the queue
func (pq *PriorityQueue) Size() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.items.Len()
}

// Stats returns queue statistics
func (pq *PriorityQueue) Stats() (enqueued, dequeued, dropped int64, size int) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.enqueued, pq.dequeued, pq.dropped, pq.items.Len()
}

// Close closes the queue and wakes up all waiting goroutines
func (pq *PriorityQueue) Close() {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.closed = true
	pq.cond.Broadcast()
}

// IsClosed returns true if the queue is closed
func (pq *PriorityQueue) IsClosed() bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.closed
}

// Drain removes and returns all remaining items from the queue
func (pq *PriorityQueue) Drain() []*Request {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	result := make([]*Request, 0, pq.items.Len())
	for pq.items.Len() > 0 {
		item := heap.Pop(&pq.items)
		req, ok := item.(*Request)
		if !ok {
			continue
		}
		result = append(result, req)
	}
	return result
}

// requestHeap implements heap.Interface for priority queue
type requestHeap []*Request

func (h requestHeap) Len() int { return len(h) }

func (h requestHeap) Less(i, j int) bool {
	// Lower priority value = higher priority
	if h[i].Priority != h[j].Priority {
		return h[i].Priority < h[j].Priority
	}
	// Same priority: earlier request first (FIFO within same priority)
	return h[i].CreatedAt.Before(h[j].CreatedAt)
}

func (h requestHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *requestHeap) Push(x interface{}) {
	if req, ok := x.(*Request); ok {
		*h = append(*h, req)
	}
}

func (h *requestHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// MultiPriorityQueue manages multiple priority queues for different request types
type MultiPriorityQueue struct {
	queues map[Priority]*PriorityQueue
	mu     sync.RWMutex
}

// NewMultiPriorityQueue creates a new multi-priority queue
func NewMultiPriorityQueue(maxSizePerPriority int) *MultiPriorityQueue {
	return &MultiPriorityQueue{
		queues: map[Priority]*PriorityQueue{
			PriorityCritical: NewPriorityQueue(maxSizePerPriority),
			PriorityHigh:     NewPriorityQueue(maxSizePerPriority),
			PriorityNormal:   NewPriorityQueue(maxSizePerPriority),
		},
	}
}

// Enqueue adds a request to the appropriate priority queue
func (mpq *MultiPriorityQueue) Enqueue(req *Request) bool {
	mpq.mu.RLock()
	pq, exists := mpq.queues[req.Priority]
	mpq.mu.RUnlock()

	if !exists {
		return false
	}

	return pq.Enqueue(req)
}

// Dequeue removes the highest priority request across all queues
func (mpq *MultiPriorityQueue) Dequeue() (*Request, bool) {
	// Try queues in priority order
	priorities := []Priority{PriorityCritical, PriorityHigh, PriorityNormal}

	for _, priority := range priorities {
		mpq.mu.RLock()
		pq := mpq.queues[priority]
		mpq.mu.RUnlock()

		if req, ok := pq.TryDequeue(); ok {
			return req, true
		}
	}

	return nil, false
}

// TotalSize returns the total size of all queues
func (mpq *MultiPriorityQueue) TotalSize() int {
	mpq.mu.RLock()
	defer mpq.mu.RUnlock()

	total := 0
	for _, pq := range mpq.queues {
		total += pq.Size()
	}
	return total
}

// Close closes all queues
func (mpq *MultiPriorityQueue) Close() {
	mpq.mu.Lock()
	defer mpq.mu.Unlock()

	for _, pq := range mpq.queues {
		pq.Close()
	}
}
