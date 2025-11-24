package fetch

import (
	"sync"
	"time"
)

// RPCMetrics tracks RPC performance metrics for dynamic optimization
type RPCMetrics struct {
	mu sync.RWMutex

	// Request counters
	totalRequests   uint64
	successRequests uint64
	errorRequests   uint64

	// Response time tracking (in milliseconds)
	totalResponseTime   uint64 // Sum of all response times
	minResponseTime     uint64
	maxResponseTime     uint64
	recentResponseTimes []uint64 // Sliding window for moving average

	// Error rate tracking
	recentErrors []bool // Sliding window for error rate calculation

	// Rate limit detection
	rateLimitErrors    uint64
	lastRateLimitTime  time.Time
	rateLimitDetected  bool
	consecutiveErrors  uint64
	maxConsecutiveErrs uint64

	// Throughput tracking
	blocksProcessed       uint64
	receiptsProcessed     uint64
	startTime             time.Time
	lastThroughputUpdate  time.Time
	currentThroughput     float64 // Blocks per second

	// Adaptive parameters
	optimalWorkerCount int
	optimalBatchSize   int

	// Configuration
	windowSize      int // Size of sliding window for averages
	rateLimitWindow time.Duration
}

// NewRPCMetrics creates a new metrics tracker
func NewRPCMetrics(windowSize int, rateLimitWindow time.Duration) *RPCMetrics {
	return &RPCMetrics{
		recentResponseTimes: make([]uint64, 0, windowSize),
		recentErrors:        make([]bool, 0, windowSize),
		windowSize:          windowSize,
		rateLimitWindow:     rateLimitWindow,
		startTime:           time.Now(),
		lastThroughputUpdate: time.Now(),
		minResponseTime:     ^uint64(0), // Max uint64
		optimalWorkerCount:  100,        // Default
		optimalBatchSize:    10,         // Default
	}
}

// RecordRequest records the result of an RPC request
func (m *RPCMetrics) RecordRequest(responseTime time.Duration, isError bool, isRateLimitError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	responseTimeMs := uint64(responseTime.Milliseconds())

	// Update counters
	m.totalRequests++
	if isError {
		m.errorRequests++
		m.consecutiveErrors++
		if m.consecutiveErrors > m.maxConsecutiveErrs {
			m.maxConsecutiveErrs = m.consecutiveErrors
		}
	} else {
		m.successRequests++
		m.consecutiveErrors = 0
	}

	// Handle rate limit errors
	if isRateLimitError {
		m.rateLimitErrors++
		m.lastRateLimitTime = time.Now()
		m.rateLimitDetected = true
	}

	// Update response time tracking
	if !isError {
		m.totalResponseTime += responseTimeMs
		if responseTimeMs < m.minResponseTime {
			m.minResponseTime = responseTimeMs
		}
		if responseTimeMs > m.maxResponseTime {
			m.maxResponseTime = responseTimeMs
		}

		// Add to sliding window
		m.recentResponseTimes = append(m.recentResponseTimes, responseTimeMs)
		if len(m.recentResponseTimes) > m.windowSize {
			m.recentResponseTimes = m.recentResponseTimes[1:]
		}
	}

	// Add to error sliding window
	m.recentErrors = append(m.recentErrors, isError)
	if len(m.recentErrors) > m.windowSize {
		m.recentErrors = m.recentErrors[1:]
	}

	// Update rate limit detection status
	if m.rateLimitDetected && time.Since(m.lastRateLimitTime) > m.rateLimitWindow {
		m.rateLimitDetected = false
	}
}

// RecordBlockProcessed records a successfully processed block
func (m *RPCMetrics) RecordBlockProcessed(receiptCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.blocksProcessed++
	m.receiptsProcessed += uint64(receiptCount)

	// Update throughput every second
	now := time.Now()
	if now.Sub(m.lastThroughputUpdate) >= time.Second {
		elapsed := now.Sub(m.startTime).Seconds()
		if elapsed > 0 {
			m.currentThroughput = float64(m.blocksProcessed) / elapsed
		}
		m.lastThroughputUpdate = now
	}
}

// GetAverageResponseTime returns the average response time in milliseconds
func (m *RPCMetrics) GetAverageResponseTime() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.successRequests == 0 {
		return 0
	}

	return m.totalResponseTime / m.successRequests
}

// GetRecentAverageResponseTime returns the moving average of recent response times
func (m *RPCMetrics) GetRecentAverageResponseTime() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.recentResponseTimes) == 0 {
		return 0
	}

	var sum uint64
	for _, rt := range m.recentResponseTimes {
		sum += rt
	}

	return sum / uint64(len(m.recentResponseTimes))
}

// GetErrorRate returns the overall error rate (0.0 to 1.0)
func (m *RPCMetrics) GetErrorRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.totalRequests == 0 {
		return 0.0
	}

	return float64(m.errorRequests) / float64(m.totalRequests)
}

// GetRecentErrorRate returns the error rate from the sliding window
func (m *RPCMetrics) GetRecentErrorRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.recentErrors) == 0 {
		return 0.0
	}

	errorCount := 0
	for _, isErr := range m.recentErrors {
		if isErr {
			errorCount++
		}
	}

	return float64(errorCount) / float64(len(m.recentErrors))
}

// GetSuccessRate returns the overall success rate (0.0 to 1.0)
func (m *RPCMetrics) GetSuccessRate() float64 {
	return 1.0 - m.GetErrorRate()
}

// GetThroughput returns current throughput in blocks per second
func (m *RPCMetrics) GetThroughput() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.currentThroughput
}

// IsRateLimited returns true if rate limiting is currently detected
func (m *RPCMetrics) IsRateLimited() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.rateLimitDetected
}

// GetConsecutiveErrors returns the current streak of consecutive errors
func (m *RPCMetrics) GetConsecutiveErrors() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.consecutiveErrors
}

// GetMaxConsecutiveErrors returns the maximum consecutive errors observed
func (m *RPCMetrics) GetMaxConsecutiveErrors() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.maxConsecutiveErrs
}

// SetOptimalWorkerCount sets the calculated optimal worker count
func (m *RPCMetrics) SetOptimalWorkerCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.optimalWorkerCount = count
}

// GetOptimalWorkerCount returns the calculated optimal worker count
func (m *RPCMetrics) GetOptimalWorkerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.optimalWorkerCount
}

// SetOptimalBatchSize sets the calculated optimal batch size
func (m *RPCMetrics) SetOptimalBatchSize(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.optimalBatchSize = size
}

// GetOptimalBatchSize returns the calculated optimal batch size
func (m *RPCMetrics) GetOptimalBatchSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.optimalBatchSize
}

// GetStats returns a snapshot of current metrics
func (m *RPCMetrics) GetStats() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MetricsSnapshot{
		TotalRequests:        m.totalRequests,
		SuccessRequests:      m.successRequests,
		ErrorRequests:        m.errorRequests,
		ErrorRate:            m.GetErrorRate(),
		RecentErrorRate:      m.GetRecentErrorRate(),
		AverageResponseTime:  m.GetAverageResponseTime(),
		RecentAvgResponseTime: m.GetRecentAverageResponseTime(),
		MinResponseTime:      m.minResponseTime,
		MaxResponseTime:      m.maxResponseTime,
		RateLimitErrors:      m.rateLimitErrors,
		RateLimitDetected:    m.rateLimitDetected,
		ConsecutiveErrors:    m.consecutiveErrors,
		MaxConsecutiveErrors: m.maxConsecutiveErrs,
		BlocksProcessed:      m.blocksProcessed,
		ReceiptsProcessed:    m.receiptsProcessed,
		Throughput:           m.currentThroughput,
		OptimalWorkerCount:   m.optimalWorkerCount,
		OptimalBatchSize:     m.optimalBatchSize,
		Uptime:               time.Since(m.startTime),
	}
}

// MetricsSnapshot represents a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	TotalRequests         uint64
	SuccessRequests       uint64
	ErrorRequests         uint64
	ErrorRate             float64
	RecentErrorRate       float64
	AverageResponseTime   uint64
	RecentAvgResponseTime uint64
	MinResponseTime       uint64
	MaxResponseTime       uint64
	RateLimitErrors       uint64
	RateLimitDetected     bool
	ConsecutiveErrors     uint64
	MaxConsecutiveErrors  uint64
	BlocksProcessed       uint64
	ReceiptsProcessed     uint64
	Throughput            float64
	OptimalWorkerCount    int
	OptimalBatchSize      int
	Uptime                time.Duration
}

// Reset resets all metrics to initial state
func (m *RPCMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests = 0
	m.successRequests = 0
	m.errorRequests = 0
	m.totalResponseTime = 0
	m.minResponseTime = ^uint64(0)
	m.maxResponseTime = 0
	m.recentResponseTimes = make([]uint64, 0, m.windowSize)
	m.recentErrors = make([]bool, 0, m.windowSize)
	m.rateLimitErrors = 0
	m.rateLimitDetected = false
	m.consecutiveErrors = 0
	m.maxConsecutiveErrs = 0
	m.blocksProcessed = 0
	m.receiptsProcessed = 0
	m.startTime = time.Now()
	m.lastThroughputUpdate = time.Now()
	m.currentThroughput = 0
}
