package fetch

import (
	"go.uber.org/zap"
)

// ============================================================================
// Fetcher Metrics Methods
// ============================================================================

// GetMetrics returns current performance metrics
func (f *Fetcher) GetMetrics() MetricsSnapshot {
	return f.metrics.GetStats()
}

// LogPerformanceMetrics logs current performance metrics
func (f *Fetcher) LogPerformanceMetrics() {
	stats := f.metrics.GetStats()

	f.logger.Info("Performance Metrics",
		zap.Uint64("total_requests", stats.TotalRequests),
		zap.Uint64("success_requests", stats.SuccessRequests),
		zap.Uint64("error_requests", stats.ErrorRequests),
		zap.Float64("error_rate", stats.ErrorRate),
		zap.Float64("recent_error_rate", stats.RecentErrorRate),
		zap.Uint64("avg_response_ms", stats.AverageResponseTime),
		zap.Uint64("recent_avg_response_ms", stats.RecentAvgResponseTime),
		zap.Uint64("min_response_ms", stats.MinResponseTime),
		zap.Uint64("max_response_ms", stats.MaxResponseTime),
		zap.Uint64("rate_limit_errors", stats.RateLimitErrors),
		zap.Bool("rate_limited", stats.RateLimitDetected),
		zap.Uint64("consecutive_errors", stats.ConsecutiveErrors),
		zap.Uint64("blocks_processed", stats.BlocksProcessed),
		zap.Uint64("receipts_processed", stats.ReceiptsProcessed),
		zap.Float64("throughput_bps", stats.Throughput),
		zap.Int("optimal_workers", stats.OptimalWorkerCount),
		zap.Int("optimal_batch_size", stats.OptimalBatchSize),
		zap.Duration("uptime", stats.Uptime),
	)
}

// OptimizeParameters runs the adaptive optimizer if enabled
func (f *Fetcher) OptimizeParameters() {
	if f.optimizer != nil {
		f.optimizer.Optimize()
	}
}

// GetOptimalWorkerCount returns the recommended worker count
func (f *Fetcher) GetOptimalWorkerCount() int {
	if f.optimizer != nil {
		return f.optimizer.GetRecommendedWorkers()
	}
	return f.config.NumWorkers
}

// GetOptimalBatchSize returns the recommended batch size
func (f *Fetcher) GetOptimalBatchSize() int {
	if f.optimizer != nil {
		return f.optimizer.GetRecommendedBatchSize()
	}
	return f.config.BatchSize
}
