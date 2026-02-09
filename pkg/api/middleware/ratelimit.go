package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// RateLimiter provides IP-based rate limiting with automatic cleanup
type RateLimiter struct {
	limiters   map[string]*limiterEntry
	mu         sync.RWMutex
	rate       rate.Limit
	burst      int
	logger     *zap.Logger
	cleanupTTL time.Duration
}

// limiterEntry wraps a rate.Limiter with last-access tracking
type limiterEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// NewRateLimiter creates a new rate limiter with automatic cleanup
func NewRateLimiter(ratePerSecond float64, burst int, logger *zap.Logger) *RateLimiter {
	rl := &RateLimiter{
		limiters:   make(map[string]*limiterEntry, 256),
		rate:       rate.Limit(ratePerSecond),
		burst:      burst,
		logger:     logger,
		cleanupTTL: 10 * time.Minute,
	}
	go rl.autoCleanup()
	return rl
}

// autoCleanup periodically removes stale limiter entries
func (rl *RateLimiter) autoCleanup() {
	ticker := time.NewTicker(rl.cleanupTTL)
	defer ticker.Stop()
	for range ticker.C {
		rl.cleanupStaleLimiters()
	}
}

// cleanupStaleLimiters removes limiters that haven't been accessed within the TTL
func (rl *RateLimiter) cleanupStaleLimiters() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.cleanupTTL)
	for ip, entry := range rl.limiters {
		if entry.lastAccess.Before(cutoff) {
			delete(rl.limiters, ip)
		}
	}
}

// getLimiter returns the rate limiter for a given IP
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.RLock()
	entry, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	entry, exists = rl.limiters[ip]
	if exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	limiter := rate.NewLimiter(rl.rate, rl.burst)
	rl.limiters[ip] = &limiterEntry{
		limiter:    limiter,
		lastAccess: time.Now(),
	}

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
			ip := extractClientIP(r)

			if !limiter.Allow(ip) {
				logger.Warn("rate limit exceeded",
					zap.String("ip", ip),
					zap.String("path", r.URL.Path),
				)

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded","message":"too many requests, please retry later"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CleanupLimiters removes old limiters to prevent memory leaks
func (rl *RateLimiter) CleanupLimiters() {
	rl.cleanupStaleLimiters()
}

// LimiterCount returns the number of active limiters
func (rl *RateLimiter) LimiterCount() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return len(rl.limiters)
}

// extractClientIP extracts the real client IP from the request.
// It validates X-Forwarded-For and X-Real-IP headers to prevent spoofing.
func extractClientIP(r *http.Request) string {
	// Try X-Forwarded-For first (take the first/leftmost IP)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if parsedIP := net.ParseIP(ip); parsedIP != nil {
			return ip
		}
	}

	// Try X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		ip := strings.TrimSpace(xri)
		if parsedIP := net.ParseIP(ip); parsedIP != nil {
			return ip
		}
	}

	// Fall back to RemoteAddr (strip port)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
