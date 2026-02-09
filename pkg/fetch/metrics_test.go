package fetch

import (
	"testing"
	"time"
)

func TestNewRPCMetrics(t *testing.T) {
	m := NewRPCMetrics(100, 30*time.Second)
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}
	if m.windowSize != 100 {
		t.Errorf("expected windowSize 100, got %d", m.windowSize)
	}
	if m.optimalWorkerCount != 100 {
		t.Errorf("expected default optimalWorkerCount 100, got %d", m.optimalWorkerCount)
	}
	if m.optimalBatchSize != 10 {
		t.Errorf("expected default optimalBatchSize 10, got %d", m.optimalBatchSize)
	}
}

func TestRecordRequest_Success(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(100*time.Millisecond, false, false)

	if m.totalRequests != 1 {
		t.Errorf("expected 1 total request, got %d", m.totalRequests)
	}
	if m.successRequests != 1 {
		t.Errorf("expected 1 success request, got %d", m.successRequests)
	}
	if m.errorRequests != 0 {
		t.Errorf("expected 0 error requests, got %d", m.errorRequests)
	}
	if m.consecutiveErrors != 0 {
		t.Errorf("expected 0 consecutive errors, got %d", m.consecutiveErrors)
	}
}

func TestRecordRequest_Error(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(50*time.Millisecond, true, false)

	if m.errorRequests != 1 {
		t.Errorf("expected 1 error request, got %d", m.errorRequests)
	}
	if m.consecutiveErrors != 1 {
		t.Errorf("expected 1 consecutive error, got %d", m.consecutiveErrors)
	}
	if m.maxConsecutiveErrs != 1 {
		t.Errorf("expected max consecutive errors 1, got %d", m.maxConsecutiveErrs)
	}
}

func TestRecordRequest_ConsecutiveErrors(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	// 3 consecutive errors
	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, true, false)

	if m.consecutiveErrors != 3 {
		t.Errorf("expected 3 consecutive errors, got %d", m.consecutiveErrors)
	}
	if m.maxConsecutiveErrs != 3 {
		t.Errorf("expected max 3, got %d", m.maxConsecutiveErrs)
	}

	// Success resets consecutive counter but not max
	m.RecordRequest(10*time.Millisecond, false, false)
	if m.consecutiveErrors != 0 {
		t.Errorf("expected 0 after success, got %d", m.consecutiveErrors)
	}
	if m.maxConsecutiveErrs != 3 {
		t.Errorf("expected max still 3, got %d", m.maxConsecutiveErrs)
	}
}

func TestRecordRequest_RateLimit(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(10*time.Millisecond, true, true)

	if m.rateLimitErrors != 1 {
		t.Errorf("expected 1 rate limit error, got %d", m.rateLimitErrors)
	}
	if !m.rateLimitDetected {
		t.Error("expected rate limit detected")
	}
}

func TestRecordRequest_RateLimitClears(t *testing.T) {
	m := NewRPCMetrics(10, 1*time.Millisecond) // Very short window
	m.RecordRequest(10*time.Millisecond, true, true)

	if !m.rateLimitDetected {
		t.Error("expected rate limit detected initially")
	}

	time.Sleep(5 * time.Millisecond)
	// Next request should clear the rate limit flag
	m.RecordRequest(10*time.Millisecond, false, false)

	if m.rateLimitDetected {
		t.Error("expected rate limit cleared after window passed")
	}
}

func TestRecordRequest_ResponseTimeTracking(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(100*time.Millisecond, false, false)
	m.RecordRequest(200*time.Millisecond, false, false)
	m.RecordRequest(50*time.Millisecond, false, false)

	if m.minResponseTime != 50 {
		t.Errorf("expected min 50, got %d", m.minResponseTime)
	}
	if m.maxResponseTime != 200 {
		t.Errorf("expected max 200, got %d", m.maxResponseTime)
	}
	if m.totalResponseTime != 350 {
		t.Errorf("expected total 350, got %d", m.totalResponseTime)
	}
}

func TestRecordRequest_SlidingWindow(t *testing.T) {
	m := NewRPCMetrics(3, 30*time.Second) // Window of 3
	m.RecordRequest(100*time.Millisecond, false, false)
	m.RecordRequest(200*time.Millisecond, false, false)
	m.RecordRequest(300*time.Millisecond, false, false)
	m.RecordRequest(400*time.Millisecond, false, false) // Should push out 100

	if len(m.recentResponseTimes) != 3 {
		t.Errorf("expected window size 3, got %d", len(m.recentResponseTimes))
	}
	// Window should be [200, 300, 400]
	if m.recentResponseTimes[0] != 200 {
		t.Errorf("expected first element 200, got %d", m.recentResponseTimes[0])
	}
}

func TestRecordRequest_ErrorSlidingWindow(t *testing.T) {
	m := NewRPCMetrics(3, 30*time.Second)
	m.RecordRequest(10*time.Millisecond, false, false)
	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, false, false)
	m.RecordRequest(10*time.Millisecond, true, false) // Pushes out first false

	if len(m.recentErrors) != 3 {
		t.Errorf("expected 3 error entries, got %d", len(m.recentErrors))
	}
}

func TestRecordBlockProcessed(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordBlockProcessed(5)
	m.RecordBlockProcessed(10)

	if m.blocksProcessed != 2 {
		t.Errorf("expected 2 blocks, got %d", m.blocksProcessed)
	}
	if m.receiptsProcessed != 15 {
		t.Errorf("expected 15 receipts, got %d", m.receiptsProcessed)
	}
}

func TestGetAverageResponseTime(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	// Empty should return 0
	if m.GetAverageResponseTime() != 0 {
		t.Error("expected 0 for empty metrics")
	}

	m.RecordRequest(100*time.Millisecond, false, false)
	m.RecordRequest(200*time.Millisecond, false, false)

	avg := m.GetAverageResponseTime()
	if avg != 150 {
		t.Errorf("expected avg 150, got %d", avg)
	}
}

func TestGetRecentAverageResponseTime(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	// Empty should return 0
	if m.GetRecentAverageResponseTime() != 0 {
		t.Error("expected 0 for empty")
	}

	m.RecordRequest(100*time.Millisecond, false, false)
	m.RecordRequest(300*time.Millisecond, false, false)

	avg := m.GetRecentAverageResponseTime()
	if avg != 200 {
		t.Errorf("expected recent avg 200, got %d", avg)
	}
}

func TestGetErrorRate(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	// Empty should return 0
	if m.GetErrorRate() != 0.0 {
		t.Error("expected 0 for empty")
	}

	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, false, false)

	rate := m.GetErrorRate()
	if rate != 0.5 {
		t.Errorf("expected error rate 0.5, got %f", rate)
	}
}

func TestGetRecentErrorRate(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	// Empty should return 0
	if m.GetRecentErrorRate() != 0.0 {
		t.Error("expected 0 for empty")
	}

	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, false, false)
	m.RecordRequest(10*time.Millisecond, false, false)

	rate := m.GetRecentErrorRate()
	if rate != 0.5 {
		t.Errorf("expected recent error rate 0.5, got %f", rate)
	}
}

func TestGetSuccessRate(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, false, false)

	rate := m.GetSuccessRate()
	if rate != 0.5 {
		t.Errorf("expected success rate 0.5, got %f", rate)
	}
}

func TestGetThroughput(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	// Initial throughput is 0
	if m.GetThroughput() != 0 {
		t.Error("expected 0 throughput initially")
	}
}

func TestIsRateLimited(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	if m.IsRateLimited() {
		t.Error("expected not rate limited initially")
	}

	m.RecordRequest(10*time.Millisecond, true, true)
	if !m.IsRateLimited() {
		t.Error("expected rate limited after rate limit error")
	}
}

func TestGetConsecutiveErrors(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	if m.GetConsecutiveErrors() != 0 {
		t.Error("expected 0 initially")
	}

	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, true, false)

	if m.GetConsecutiveErrors() != 2 {
		t.Errorf("expected 2, got %d", m.GetConsecutiveErrors())
	}
}

func TestGetMaxConsecutiveErrors(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)

	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, true, false)
	m.RecordRequest(10*time.Millisecond, false, false) // Reset
	m.RecordRequest(10*time.Millisecond, true, false)

	if m.GetMaxConsecutiveErrors() != 3 {
		t.Errorf("expected max 3, got %d", m.GetMaxConsecutiveErrors())
	}
}

func TestSetGetOptimalWorkerCount(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.SetOptimalWorkerCount(8)

	if m.GetOptimalWorkerCount() != 8 {
		t.Errorf("expected 8, got %d", m.GetOptimalWorkerCount())
	}
}

func TestSetGetOptimalBatchSize(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.SetOptimalBatchSize(25)

	if m.GetOptimalBatchSize() != 25 {
		t.Errorf("expected 25, got %d", m.GetOptimalBatchSize())
	}
}

func TestGetStats(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(100*time.Millisecond, false, false)
	m.RecordRequest(50*time.Millisecond, true, true)
	m.RecordBlockProcessed(5)

	stats := m.GetStats()

	if stats.TotalRequests != 2 {
		t.Errorf("expected 2 total, got %d", stats.TotalRequests)
	}
	if stats.SuccessRequests != 1 {
		t.Errorf("expected 1 success, got %d", stats.SuccessRequests)
	}
	if stats.ErrorRequests != 1 {
		t.Errorf("expected 1 error, got %d", stats.ErrorRequests)
	}
	if stats.RateLimitErrors != 1 {
		t.Errorf("expected 1 rate limit, got %d", stats.RateLimitErrors)
	}
	if !stats.RateLimitDetected {
		t.Error("expected rate limit detected")
	}
	if stats.BlocksProcessed != 1 {
		t.Errorf("expected 1 block, got %d", stats.BlocksProcessed)
	}
	if stats.ReceiptsProcessed != 5 {
		t.Errorf("expected 5 receipts, got %d", stats.ReceiptsProcessed)
	}
	if stats.OptimalWorkerCount != 100 {
		t.Errorf("expected default 100 workers, got %d", stats.OptimalWorkerCount)
	}
	if stats.OptimalBatchSize != 10 {
		t.Errorf("expected default 10 batch, got %d", stats.OptimalBatchSize)
	}
	if stats.Uptime <= 0 {
		t.Error("expected positive uptime")
	}
}

func TestReset(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(100*time.Millisecond, false, false)
	m.RecordRequest(50*time.Millisecond, true, true)
	m.RecordBlockProcessed(5)

	m.Reset()

	if m.totalRequests != 0 {
		t.Errorf("expected 0 total after reset, got %d", m.totalRequests)
	}
	if m.successRequests != 0 {
		t.Error("expected 0 success after reset")
	}
	if m.errorRequests != 0 {
		t.Error("expected 0 errors after reset")
	}
	if m.rateLimitErrors != 0 {
		t.Error("expected 0 rate limit errors after reset")
	}
	if m.rateLimitDetected {
		t.Error("expected rate limit cleared after reset")
	}
	if m.blocksProcessed != 0 {
		t.Error("expected 0 blocks after reset")
	}
	if m.currentThroughput != 0 {
		t.Error("expected 0 throughput after reset")
	}
	if len(m.recentResponseTimes) != 0 {
		t.Error("expected empty sliding window after reset")
	}
	if len(m.recentErrors) != 0 {
		t.Error("expected empty error window after reset")
	}
}

func TestRecordRequest_ErrorDoesNotTrackResponseTime(t *testing.T) {
	m := NewRPCMetrics(10, 30*time.Second)
	m.RecordRequest(500*time.Millisecond, true, false) // Error

	if len(m.recentResponseTimes) != 0 {
		t.Error("errors should not be added to response time window")
	}
	if m.totalResponseTime != 0 {
		t.Error("errors should not contribute to total response time")
	}
}
