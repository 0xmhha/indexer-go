package rpcproxy

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// RequestHandler processes RPC requests
type RequestHandler func(ctx context.Context, req *Request) *Response

// WorkerPool manages a pool of worker goroutines
type WorkerPool struct {
	config  *WorkerConfig
	logger  *zap.Logger
	queue   *PriorityQueue
	handler RequestHandler
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	mu      sync.Mutex
	active  int32
	total   int64
	success int64
	failed  int64
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(config *WorkerConfig, handler RequestHandler, logger *zap.Logger) *WorkerPool {
	if config == nil {
		config = DefaultWorkerConfig()
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		config:  config,
		logger:  logger,
		queue:   NewPriorityQueue(config.QueueSize),
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start starts the worker pool
func (wp *WorkerPool) Start() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.started {
		return
	}

	wp.started = true

	for i := 0; i < wp.config.NumWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}

	wp.logger.Info("Worker pool started",
		zap.Int("workers", wp.config.NumWorkers),
		zap.Int("queue_size", wp.config.QueueSize))
}

// Stop gracefully stops the worker pool
func (wp *WorkerPool) Stop() {
	wp.mu.Lock()
	if !wp.started {
		wp.mu.Unlock()
		return
	}
	wp.mu.Unlock()

	wp.logger.Info("Stopping worker pool")

	// Signal workers to stop
	wp.cancel()
	wp.queue.Close()

	// Wait for workers to finish
	wp.wg.Wait()

	wp.mu.Lock()
	wp.started = false
	wp.mu.Unlock()

	wp.logger.Info("Worker pool stopped")
}

// Submit submits a request to the worker pool
func (wp *WorkerPool) Submit(req *Request) bool {
	return wp.queue.Enqueue(req)
}

// SubmitAndWait submits a request and waits for the response
func (wp *WorkerPool) SubmitAndWait(ctx context.Context, req *Request) (*Response, error) {
	resultCh := make(chan *Response, 1)
	req.ResultCh = resultCh

	if !wp.Submit(req) {
		return nil, ErrQueueFull
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-resultCh:
		return resp, nil
	}
}

// worker is the main worker loop
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	wp.logger.Debug("Worker started", zap.Int("worker_id", id))

	for {
		select {
		case <-wp.ctx.Done():
			wp.logger.Debug("Worker stopping", zap.Int("worker_id", id))
			return
		default:
		}

		// Get request from queue with timeout
		req, ok := wp.queue.DequeueWithTimeout(100 * time.Millisecond)
		if !ok {
			continue
		}

		wp.processRequest(id, req)
	}
}

// processRequest processes a single request
func (wp *WorkerPool) processRequest(workerID int, req *Request) {
	atomic.AddInt32(&wp.active, 1)
	defer atomic.AddInt32(&wp.active, -1)

	start := time.Now()
	atomic.AddInt64(&wp.total, 1)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(wp.ctx, req.Timeout)
	defer cancel()

	// Execute request with retries
	var resp *Response
	for attempt := 0; attempt <= wp.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := wp.config.RetryDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				resp = &Response{
					ID:      req.ID,
					Success: false,
					Error:   ctx.Err(),
					Latency: time.Since(start),
				}
				break
			case <-time.After(delay):
			}
		}

		resp = wp.handler(ctx, req)
		if resp.Success {
			break
		}

		// Don't retry on context errors
		if ctx.Err() != nil {
			break
		}
	}

	resp.Latency = time.Since(start)

	if resp.Success {
		atomic.AddInt64(&wp.success, 1)
	} else {
		atomic.AddInt64(&wp.failed, 1)
	}

	// Send response
	if req.ResultCh != nil {
		select {
		case req.ResultCh <- resp:
		default:
			wp.logger.Warn("Failed to send response - channel full or closed",
				zap.String("request_id", req.ID))
		}
	}

	wp.logger.Debug("Request processed",
		zap.Int("worker_id", workerID),
		zap.String("request_id", req.ID),
		zap.Bool("success", resp.Success),
		zap.Duration("latency", resp.Latency))
}

// Stats returns worker pool statistics
func (wp *WorkerPool) Stats() (total, success, failed int64, active, queueDepth int) {
	return atomic.LoadInt64(&wp.total),
		atomic.LoadInt64(&wp.success),
		atomic.LoadInt64(&wp.failed),
		int(atomic.LoadInt32(&wp.active)),
		wp.queue.Size()
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config          *CircuitBreakerConfig
	mu              sync.RWMutex
	state           CircuitState
	failures        int
	successes       int
	lastFailure     time.Time
	lastStateChange time.Time
}

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreaker{
		config:          config,
		state:           CircuitClosed,
		lastStateChange: time.Now(),
	}
}

// Allow checks if a request should be allowed
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if reset timeout has passed
		if time.Since(cb.lastStateChange) > cb.config.ResetTimeout {
			cb.state = CircuitHalfOpen
			cb.successes = 0
			cb.lastStateChange = time.Now()
			return true
		}
		return false

	case CircuitHalfOpen:
		// Allow limited requests in half-open state
		return cb.successes < cb.config.HalfOpenRequests

	default:
		return false
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	if cb.state == CircuitHalfOpen {
		cb.successes++
		if cb.successes >= cb.config.HalfOpenRequests {
			cb.state = CircuitClosed
			cb.lastStateChange = time.Now()
		}
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == CircuitClosed && cb.failures >= cb.config.MaxFailures {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
	} else if cb.state == CircuitHalfOpen {
		// Any failure in half-open state opens the circuit
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
	}
}

// State returns the current circuit state
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastStateChange = time.Now()
}

// Errors
var (
	ErrQueueFull           = &ProxyError{Code: "QUEUE_FULL", Message: "Request queue is full"}
	ErrCircuitOpen         = &ProxyError{Code: "CIRCUIT_OPEN", Message: "Circuit breaker is open"}
	ErrRateLimited         = &ProxyError{Code: "RATE_LIMITED", Message: "Request rate limited"}
	ErrTimeout             = &ProxyError{Code: "TIMEOUT", Message: "Request timeout"}
	ErrInvalidRequest      = &ProxyError{Code: "INVALID_REQUEST", Message: "Invalid request"}
	ErrContractNotVerified = &ProxyError{Code: "CONTRACT_NOT_VERIFIED", Message: "Contract is not verified"}
	ErrABINotFound         = &ProxyError{Code: "ABI_NOT_FOUND", Message: "Contract ABI not found"}
	ErrMethodNotFound      = &ProxyError{Code: "METHOD_NOT_FOUND", Message: "Method not found in ABI"}
)

// ProxyError represents an RPC proxy error
type ProxyError struct {
	Code    string
	Message string
}

func (e *ProxyError) Error() string {
	return e.Code + ": " + e.Message
}
