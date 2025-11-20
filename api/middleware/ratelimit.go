package middleware

import (
	"net/http"
	"sync"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// RateLimiter provides IP-based rate limiting
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	logger   *zap.Logger
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(ratePerSecond float64, burst int, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(ratePerSecond),
		burst:    burst,
		logger:   logger,
	}
}

// getLimiter returns the rate limiter for a given IP
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	limiter, exists = rl.limiters[ip]
	if exists {
		return limiter
	}

	limiter = rate.NewLimiter(rl.rate, rl.burst)
	rl.limiters[ip] = limiter

	return limiter
}

// Allow checks if a request from the given IP is allowed
func (rl *RateLimiter) Allow(ip string) bool {
	return rl.getLimiter(ip).Allow()
}

// RateLimit returns a rate limiting middleware
func RateLimit(ratePerSecond float64, burst int, logger *zap.Logger) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(ratePerSecond, burst, logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr

			// Use X-Forwarded-For or X-Real-IP if available
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ip = xff
			} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
				ip = xri
			}

			if !limiter.Allow(ip) {
				logger.Warn("rate limit exceeded",
					zap.String("ip", ip),
					zap.String("path", r.URL.Path),
				)

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded","message":"too many requests, please retry later"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CleanupLimiters removes old limiters to prevent memory leaks
// This should be called periodically in production
func (rl *RateLimiter) CleanupLimiters() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Simple cleanup: remove all limiters
	// In production, you might want to track last access time
	rl.limiters = make(map[string]*rate.Limiter)
}

// LimiterCount returns the number of active limiters
func (rl *RateLimiter) LimiterCount() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return len(rl.limiters)
}
