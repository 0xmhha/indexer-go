package middleware

import (
	"net"
	"net/http"
	"sync"
	"sync/atomic"
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

	stop     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

// limiterEntry wraps a rate.Limiter with last-access tracking
type limiterEntry struct {
	limiter *rate.Limiter
	// lastAccess is UnixNano. It is updated on every request without the
	// map lock, so it must be atomic.
	lastAccess atomic.Int64
}

// NewRateLimiter creates a new rate limiter with automatic cleanup
func NewRateLimiter(ratePerSecond float64, burst int, logger *zap.Logger) *RateLimiter {
	rl := &RateLimiter{
		limiters:   make(map[string]*limiterEntry, 256),
		rate:       rate.Limit(ratePerSecond),
		burst:      burst,
		logger:     logger,
		cleanupTTL: 10 * time.Minute,
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	go rl.autoCleanup()
	return rl
}

// autoCleanup periodically removes stale limiter entries until Stop.
func (rl *RateLimiter) autoCleanup() {
	defer close(rl.stopped)
	ticker := time.NewTicker(rl.cleanupTTL)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			rl.cleanupStaleLimiters()
		}
	}
}

// Stop ends the cleanup goroutine; the limiter keeps limiting. Calling it
// twice is harmless.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stop) })
	<-rl.stopped
}

// Middleware returns the rate limiting middleware of the limiter.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return rateLimitWith(rl, rl.logger)
}

// cleanupStaleLimiters removes limiters that haven't been accessed within the TTL
func (rl *RateLimiter) cleanupStaleLimiters() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.cleanupTTL)
	for ip, entry := range rl.limiters {
		if time.Unix(0, entry.lastAccess.Load()).Before(cutoff) {
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
		entry.lastAccess.Store(time.Now().UnixNano())
		return entry.limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	entry, exists = rl.limiters[ip]
	if exists {
		entry.lastAccess.Store(time.Now().UnixNano())
		return entry.limiter
	}

	limiter := rate.NewLimiter(rl.rate, rl.burst)
	entry = &limiterEntry{limiter: limiter}
	entry.lastAccess.Store(time.Now().UnixNano())
	rl.limiters[ip] = entry

	return limiter
}

// Allow checks if a request from the given IP is allowed
func (rl *RateLimiter) Allow(ip string) bool {
	return rl.getLimiter(ip).Allow()
}

// RateLimit returns a rate limiting middleware over a new limiter, whose
// cleanup goroutine runs for the life of the process; a server that stops
// creates the limiter itself and stops it (RateLimiter.Middleware, Stop).
func RateLimit(ratePerSecond float64, burst int, logger *zap.Logger) func(http.Handler) http.Handler {
	return rateLimitWith(NewRateLimiter(ratePerSecond, burst, logger), logger)
}

func rateLimitWith(limiter *RateLimiter, logger *zap.Logger) func(http.Handler) http.Handler {
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

// extractClientIP returns the client address of the request: the peer of
// the connection, which ClientIP replaces with the forwarded client when
// the peer is a trusted proxy. Forwarding headers are not read here: the
// client can write them.
func extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
