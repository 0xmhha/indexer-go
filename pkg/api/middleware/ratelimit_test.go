package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRateLimiter_Allow(t *testing.T) {
	logger := zap.NewNop()
	limiter := NewRateLimiter(10, 10, logger)

	// First 10 requests should be allowed (burst)
	for i := 0; i < 10; i++ {
		if !limiter.Allow("192.168.1.1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 11th request should be denied (burst exceeded)
	if limiter.Allow("192.168.1.1") {
		t.Error("11th request should be denied")
	}

	// Different IP should have its own limiter
	if !limiter.Allow("192.168.1.2") {
		t.Error("different IP should be allowed")
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	logger := zap.NewNop()
	limiter := NewRateLimiter(1, 1, logger)

	// Each IP should have its own limiter
	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}

	for _, ip := range ips {
		if !limiter.Allow(ip) {
			t.Errorf("first request from %s should be allowed", ip)
		}
	}

	// Verify limiter count
	if count := limiter.LimiterCount(); count != len(ips) {
		t.Errorf("expected %d limiters, got %d", len(ips), count)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	logger := zap.NewNop()
	limiter := NewRateLimiter(10, 10, logger)

	// Add some limiters
	limiter.Allow("10.0.0.1")
	limiter.Allow("10.0.0.2")
	limiter.Allow("10.0.0.3")

	if limiter.LimiterCount() != 3 {
		t.Errorf("expected 3 limiters, got %d", limiter.LimiterCount())
	}

	// Cleanup should NOT remove fresh entries (TTL-based)
	limiter.CleanupLimiters()

	if limiter.LimiterCount() != 3 {
		t.Errorf("expected 3 limiters after cleanup (entries are fresh), got %d", limiter.LimiterCount())
	}

	// Manually age the entries past the TTL
	limiter.mu.Lock()
	staleTime := time.Now().Add(-limiter.cleanupTTL - time.Minute)
	for _, entry := range limiter.limiters {
		entry.lastAccess.Store(staleTime.UnixNano())
	}
	limiter.mu.Unlock()

	// Cleanup should now remove stale entries
	limiter.CleanupLimiters()

	if limiter.LimiterCount() != 0 {
		t.Errorf("expected 0 limiters after cleanup of stale entries, got %d", limiter.LimiterCount())
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	logger := zap.NewNop()

	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with rate limiter (rate: 5/s, burst: 5)
	rateLimited := RateLimit(5, 5, logger)(handler)

	// First 5 requests should succeed
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()

		rateLimited.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// 6th request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	rateLimited.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}

	// Check Retry-After header
	if rec.Header().Get("Retry-After") != "1" {
		t.Error("expected Retry-After header")
	}
}

// TestRateLimitMiddleware_IgnoresForwardingHeaders: a client cannot get a
// fresh allowance by writing another X-Forwarded-For or X-Real-IP; the
// limit keys on the connection's peer (ClientIP replaces it for trusted
// proxies).
func TestRateLimitMiddleware_IgnoresForwardingHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rateLimited := RateLimit(1, 1, zap.NewNop())(handler)

	headers := []struct{ name, value string }{
		{"X-Forwarded-For", "203.0.113.1"},
		{"X-Forwarded-For", "203.0.113.2"},
		{"X-Real-IP", "198.51.100.178"},
	}
	for i, h := range headers {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.0.2.7:4000"
		req.Header.Set(h.name, h.value)
		rec := httptest.NewRecorder()
		rateLimited.ServeHTTP(rec, req)
		want := http.StatusTooManyRequests
		if i == 0 {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("request %d (%s: %s): expected %d, got %d", i, h.name, h.value, want, rec.Code)
		}
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	logger := zap.NewNop()
	limiter := NewRateLimiter(100, 100, logger)

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	// Concurrent requests from same IP
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- limiter.Allow("10.0.0.1")
		}()
	}

	wg.Wait()
	close(allowed)

	// Count allowed requests
	allowedCount := 0
	for a := range allowed {
		if a {
			allowedCount++
		}
	}

	// Should allow exactly burst amount (100)
	if allowedCount != 100 {
		t.Errorf("expected 100 allowed requests, got %d", allowedCount)
	}
}

func TestRateLimitMiddleware_GenerousDefaults(t *testing.T) {
	logger := zap.NewNop()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test with generous defaults (1000/s, burst 2000)
	rateLimited := RateLimit(1000, 2000, logger)(handler)

	// Should handle many requests without issues
	for i := 0; i < 1000; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		rateLimited.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200 with generous limits, got %d", i+1, rec.Code)
		}
	}
}
