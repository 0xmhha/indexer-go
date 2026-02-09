package rpcproxy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func successHandler(_ context.Context, req *Request) *Response {
	return &Response{ID: req.ID, Success: true, Data: "ok"}
}

func failHandler(_ context.Context, req *Request) *Response {
	return &Response{ID: req.ID, Success: false, Error: ErrTimeout}
}

func testWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		NumWorkers:     2,
		QueueSize:      10,
		RequestTimeout: time.Second,
		MaxRetries:     0,
		RetryDelay:     10 * time.Millisecond,
	}
}

// ========== WorkerPool ==========

func TestWorkerPool_StartStop(t *testing.T) {
	wp := NewWorkerPool(testWorkerConfig(), successHandler, zap.NewNop())

	wp.Start()
	// Double start should be safe
	wp.Start()

	wp.Stop()
	// Double stop should be safe
	wp.Stop()
}

func TestWorkerPool_NilConfig(t *testing.T) {
	wp := NewWorkerPool(nil, successHandler, nil)
	require.NotNil(t, wp)
	assert.Equal(t, 10, wp.config.NumWorkers) // DefaultWorkerConfig
}

func TestWorkerPool_SubmitAndWait(t *testing.T) {
	wp := NewWorkerPool(testWorkerConfig(), successHandler, zap.NewNop())
	wp.Start()
	defer wp.Stop()

	req := &Request{
		ID:        "test-1",
		Type:      RequestTypeBalance,
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
		Timeout:   time.Second,
	}

	resp, err := wp.SubmitAndWait(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "test-1", resp.ID)
	assert.Equal(t, "ok", resp.Data)
}

func TestWorkerPool_SubmitAndWait_ContextCancelled(t *testing.T) {
	// Use a handler that respects context cancellation
	slowHandler := func(ctx context.Context, req *Request) *Response {
		select {
		case <-ctx.Done():
			return &Response{ID: req.ID, Success: false, Error: ctx.Err()}
		case <-time.After(5 * time.Second):
			return &Response{ID: req.ID, Success: true}
		}
	}

	wp := NewWorkerPool(testWorkerConfig(), slowHandler, zap.NewNop())
	wp.Start()
	defer wp.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &Request{
		ID:        "slow-1",
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
		Timeout:   5 * time.Second,
	}

	_, err := wp.SubmitAndWait(ctx, req)
	assert.Error(t, err)
}

func TestWorkerPool_Submit_QueueFull(t *testing.T) {
	cfg := testWorkerConfig()
	cfg.QueueSize = 1
	cfg.NumWorkers = 0 // No workers consuming

	// Use manual pool creation to avoid starting workers
	wp := NewWorkerPool(cfg, successHandler, zap.NewNop())
	// Don't start — items stay in queue

	req1 := newTestRequest("r1", PriorityNormal)
	assert.True(t, wp.Submit(req1))

	req2 := newTestRequest("r2", PriorityNormal)
	assert.False(t, wp.Submit(req2), "queue should be full")
}

func TestWorkerPool_SubmitAndWait_QueueFull(t *testing.T) {
	cfg := testWorkerConfig()
	cfg.QueueSize = 1
	cfg.NumWorkers = 0

	wp := NewWorkerPool(cfg, successHandler, zap.NewNop())

	// Fill queue
	wp.Submit(newTestRequest("r1", PriorityNormal))

	req := newTestRequest("r2", PriorityNormal)
	_, err := wp.SubmitAndWait(context.Background(), req)
	assert.ErrorIs(t, err, ErrQueueFull)
}

func TestWorkerPool_Stats(t *testing.T) {
	wp := NewWorkerPool(testWorkerConfig(), successHandler, zap.NewNop())
	wp.Start()
	defer wp.Stop()

	req := &Request{
		ID:        "stat-1",
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
		Timeout:   time.Second,
	}

	resp, err := wp.SubmitAndWait(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success)

	total, success, failed, _, _ := wp.Stats()
	assert.Equal(t, int64(1), total)
	assert.Equal(t, int64(1), success)
	assert.Equal(t, int64(0), failed)
}

func TestWorkerPool_FailedRequest(t *testing.T) {
	cfg := testWorkerConfig()
	cfg.MaxRetries = 0

	wp := NewWorkerPool(cfg, failHandler, zap.NewNop())
	wp.Start()
	defer wp.Stop()

	req := &Request{
		ID:        "fail-1",
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
		Timeout:   time.Second,
	}

	resp, err := wp.SubmitAndWait(context.Background(), req)
	require.NoError(t, err) // SubmitAndWait itself doesn't error
	assert.False(t, resp.Success)

	_, _, failed, _, _ := wp.Stats()
	assert.Equal(t, int64(1), failed)
}

func TestWorkerPool_Retries(t *testing.T) {
	var attempts int32

	retryHandler := func(_ context.Context, req *Request) *Response {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return &Response{ID: req.ID, Success: false, Error: ErrTimeout}
		}
		return &Response{ID: req.ID, Success: true, Data: "ok"}
	}

	cfg := testWorkerConfig()
	cfg.MaxRetries = 3
	cfg.RetryDelay = 10 * time.Millisecond

	wp := NewWorkerPool(cfg, retryHandler, zap.NewNop())
	wp.Start()
	defer wp.Stop()

	req := &Request{
		ID:        "retry-1",
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
		Timeout:   5 * time.Second,
	}

	resp, err := wp.SubmitAndWait(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

// ========== CircuitBreaker ==========

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(nil)

	assert.Equal(t, CircuitClosed, cb.State())
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:      3,
		ResetTimeout:     time.Second,
		HalfOpenRequests: 2,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitClosed, cb.State())
	assert.True(t, cb.Allow())

	cb.RecordFailure() // 3rd failure — opens
	assert.Equal(t, CircuitOpen, cb.State())
	assert.False(t, cb.Allow())
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     50 * time.Millisecond,
		HalfOpenRequests: 2,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure() // Opens
	assert.Equal(t, CircuitOpen, cb.State())

	time.Sleep(80 * time.Millisecond)

	// Allow() should transition to half-open
	assert.True(t, cb.Allow())
	assert.Equal(t, CircuitHalfOpen, cb.State())
}

func TestCircuitBreaker_ClosesAfterHalfOpenSuccess(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     50 * time.Millisecond,
		HalfOpenRequests: 2,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure() // Open
	time.Sleep(80 * time.Millisecond)
	cb.Allow() // Transition to half-open

	cb.RecordSuccess()
	assert.Equal(t, CircuitHalfOpen, cb.State()) // Still half-open (need 2)

	cb.RecordSuccess()
	assert.Equal(t, CircuitClosed, cb.State()) // Closed after 2 successes
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     50 * time.Millisecond,
		HalfOpenRequests: 3,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure() // Open
	time.Sleep(80 * time.Millisecond)
	cb.Allow() // Half-open

	cb.RecordFailure() // Any failure in half-open reopens
	assert.Equal(t, CircuitOpen, cb.State())
}

func TestCircuitBreaker_HalfOpenLimitsRequests(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     50 * time.Millisecond,
		HalfOpenRequests: 2,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure() // Open
	time.Sleep(80 * time.Millisecond)
	cb.Allow() // Half-open, successes=0

	assert.True(t, cb.Allow())  // successes=0 < 2
	cb.RecordSuccess()          // successes=1
	assert.True(t, cb.Allow())  // successes=1 < 2
	cb.RecordSuccess()          // successes=2 → closed
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     time.Hour,
		HalfOpenRequests: 1,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure() // Open
	assert.Equal(t, CircuitOpen, cb.State())

	cb.Reset()
	assert.Equal(t, CircuitClosed, cb.State())
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cfg := &CircuitBreakerConfig{
		MaxFailures:      3,
		ResetTimeout:     time.Second,
		HalfOpenRequests: 1,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // Resets failure count to 0
	cb.RecordFailure()
	cb.RecordFailure()

	// Still closed — success reset the counter
	assert.Equal(t, CircuitClosed, cb.State())
}

// ========== CircuitState ==========

func TestCircuitState_String(t *testing.T) {
	assert.Equal(t, "closed", CircuitClosed.String())
	assert.Equal(t, "open", CircuitOpen.String())
	assert.Equal(t, "half-open", CircuitHalfOpen.String())
	assert.Equal(t, "unknown", CircuitState(99).String())
}

// ========== ProxyError ==========

func TestProxyError(t *testing.T) {
	err := &ProxyError{Code: "TEST", Message: "test error"}
	assert.Equal(t, "TEST: test error", err.Error())
}

func TestProxyErrors_Predefined(t *testing.T) {
	assert.Contains(t, ErrQueueFull.Error(), "QUEUE_FULL")
	assert.Contains(t, ErrCircuitOpen.Error(), "CIRCUIT_OPEN")
	assert.Contains(t, ErrRateLimited.Error(), "RATE_LIMITED")
	assert.Contains(t, ErrTimeout.Error(), "TIMEOUT")
	assert.Contains(t, ErrInvalidRequest.Error(), "INVALID_REQUEST")
	assert.Contains(t, ErrContractNotVerified.Error(), "CONTRACT_NOT_VERIFIED")
	assert.Contains(t, ErrABINotFound.Error(), "ABI_NOT_FOUND")
	assert.Contains(t, ErrMethodNotFound.Error(), "METHOD_NOT_FOUND")
}

// ========== Config Defaults ==========

func TestDefaultConfigs(t *testing.T) {
	cc := DefaultCacheConfig()
	assert.Equal(t, 10000, cc.MaxSize)
	assert.Equal(t, 30*time.Second, cc.DefaultTTL)

	wc := DefaultWorkerConfig()
	assert.Equal(t, 10, wc.NumWorkers)
	assert.Equal(t, 1000, wc.QueueSize)

	rc := DefaultRateLimitConfig()
	assert.Equal(t, float64(100), rc.RequestsPerSecond)
	assert.True(t, rc.PerIPLimit)

	cbc := DefaultCircuitBreakerConfig()
	assert.Equal(t, 5, cbc.MaxFailures)
	assert.Equal(t, 30*time.Second, cbc.ResetTimeout)

	cfg := DefaultConfig()
	require.NotNil(t, cfg.Cache)
	require.NotNil(t, cfg.Worker)
	require.NotNil(t, cfg.RateLimit)
	require.NotNil(t, cfg.CircuitBreaker)
}

// ========== Proxy (Metrics, RateLimiter) ==========

func TestProxy_GetIPRateLimiter(t *testing.T) {
	cfg := DefaultConfig()
	p := &Proxy{
		config: cfg,
	}

	limiter1 := p.GetIPRateLimiter("192.168.1.1")
	require.NotNil(t, limiter1)

	// Same IP should return the same limiter
	limiter2 := p.GetIPRateLimiter("192.168.1.1")
	assert.Equal(t, limiter1, limiter2)

	// Different IP should return a different limiter instance
	limiter3 := p.GetIPRateLimiter("10.0.0.1")
	assert.False(t, limiter1 == limiter3, "different IPs should have different limiter instances")
}

func TestProxy_AllowIP_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimit.PerIPLimit = false
	p := &Proxy{
		config: cfg,
	}

	// Should always allow when disabled
	assert.True(t, p.AllowIP("192.168.1.1"))
}

func TestProxy_AllowIP_RateLimited(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimit.PerIPLimit = true
	cfg.RateLimit.PerIPRequestsPerSecond = 1
	cfg.RateLimit.BurstSize = 10 // burst/10 = 1
	p := &Proxy{
		config: cfg,
	}

	// First request should be allowed
	assert.True(t, p.AllowIP("192.168.1.1"))

	// Immediate second request should be rate limited (rate=1/s, burst=1)
	assert.False(t, p.AllowIP("192.168.1.1"))
}
