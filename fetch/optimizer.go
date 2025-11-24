package fetch

import (
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
)

// AdaptiveOptimizer dynamically adjusts fetcher parameters based on performance metrics
type AdaptiveOptimizer struct {
	metrics *RPCMetrics
	logger  *zap.Logger

	// Configuration
	minWorkers         int
	maxWorkers         int
	minBatchSize       int
	maxBatchSize       int
	adjustmentInterval time.Duration

	// Target metrics
	targetErrorRate      float64 // Target error rate (lower is better)
	maxErrorRate         float64 // Maximum acceptable error rate
	targetResponseTime   uint64  // Target response time in ms
	rateLimitThreshold   uint64  // Error count threshold for rate limit detection

	// Adjustment parameters
	workerIncreaseFactor float64 // Factor to increase workers (e.g., 1.2 = 20% increase)
	workerDecreaseFactor float64 // Factor to decrease workers (e.g., 0.8 = 20% decrease)
	batchIncreaseFactor  float64 // Factor to increase batch size
	batchDecreaseFactor  float64 // Factor to decrease batch size

	// State
	lastAdjustment time.Time
}

// OptimizerConfig holds configuration for the adaptive optimizer
type OptimizerConfig struct {
	MinWorkers           int
	MaxWorkers           int
	MinBatchSize         int
	MaxBatchSize         int
	AdjustmentInterval   time.Duration
	TargetErrorRate      float64
	MaxErrorRate         float64
	TargetResponseTime   uint64
	RateLimitThreshold   uint64
	WorkerIncreaseFactor float64
	WorkerDecreaseFactor float64
	BatchIncreaseFactor  float64
	BatchDecreaseFactor  float64
}

// DefaultOptimizerConfig returns default optimizer configuration
func DefaultOptimizerConfig() *OptimizerConfig {
	return &OptimizerConfig{
		MinWorkers:           constants.MinWorkers,
		MaxWorkers:           constants.MaxWorkers,
		MinBatchSize:         5,
		MaxBatchSize:         50,
		AdjustmentInterval:   30 * time.Second,
		TargetErrorRate:      0.01,  // 1% error rate
		MaxErrorRate:         0.05,  // 5% error rate
		TargetResponseTime:   500,   // 500ms
		RateLimitThreshold:   10,    // 10 errors in window
		WorkerIncreaseFactor: 1.2,   // 20% increase
		WorkerDecreaseFactor: 0.8,   // 20% decrease
		BatchIncreaseFactor:  1.5,   // 50% increase
		BatchDecreaseFactor:  0.75,  // 25% decrease
	}
}

// NewAdaptiveOptimizer creates a new adaptive optimizer
func NewAdaptiveOptimizer(metrics *RPCMetrics, config *OptimizerConfig, logger *zap.Logger) *AdaptiveOptimizer {
	if config == nil {
		config = DefaultOptimizerConfig()
	}

	return &AdaptiveOptimizer{
		metrics:              metrics,
		logger:               logger,
		minWorkers:           config.MinWorkers,
		maxWorkers:           config.MaxWorkers,
		minBatchSize:         config.MinBatchSize,
		maxBatchSize:         config.MaxBatchSize,
		adjustmentInterval:   config.AdjustmentInterval,
		targetErrorRate:      config.TargetErrorRate,
		maxErrorRate:         config.MaxErrorRate,
		targetResponseTime:   config.TargetResponseTime,
		rateLimitThreshold:   config.RateLimitThreshold,
		workerIncreaseFactor: config.WorkerIncreaseFactor,
		workerDecreaseFactor: config.WorkerDecreaseFactor,
		batchIncreaseFactor:  config.BatchIncreaseFactor,
		batchDecreaseFactor:  config.BatchDecreaseFactor,
		lastAdjustment:       time.Now(),
	}
}

// ShouldAdjust returns true if it's time to adjust parameters
func (o *AdaptiveOptimizer) ShouldAdjust() bool {
	return time.Since(o.lastAdjustment) >= o.adjustmentInterval
}

// Optimize analyzes current metrics and adjusts parameters
func (o *AdaptiveOptimizer) Optimize() {
	if !o.ShouldAdjust() {
		return
	}

	stats := o.metrics.GetStats()

	// Calculate new optimal worker count
	newWorkerCount := o.calculateOptimalWorkers(stats)

	// Calculate new optimal batch size
	newBatchSize := o.calculateOptimalBatchSize(stats)

	// Apply changes
	o.metrics.SetOptimalWorkerCount(newWorkerCount)
	o.metrics.SetOptimalBatchSize(newBatchSize)

	o.logger.Info("Adaptive optimization adjusted parameters",
		zap.Int("worker_count", newWorkerCount),
		zap.Int("batch_size", newBatchSize),
		zap.Float64("error_rate", stats.RecentErrorRate),
		zap.Uint64("avg_response_time_ms", stats.RecentAvgResponseTime),
		zap.Float64("throughput_bps", stats.Throughput),
		zap.Bool("rate_limited", stats.RateLimitDetected),
	)

	o.lastAdjustment = time.Now()
}

// calculateOptimalWorkers determines the optimal number of workers based on current metrics
func (o *AdaptiveOptimizer) calculateOptimalWorkers(stats MetricsSnapshot) int {
	currentWorkers := stats.OptimalWorkerCount

	// If rate limited, significantly decrease workers
	if stats.RateLimitDetected {
		newCount := int(math.Ceil(float64(currentWorkers) * 0.5)) // 50% reduction
		o.logger.Warn("Rate limit detected, reducing workers",
			zap.Int("from", currentWorkers),
			zap.Int("to", newCount),
		)
		return o.clampWorkers(newCount)
	}

	// If consecutive errors exceed threshold, reduce workers
	if stats.ConsecutiveErrors >= o.rateLimitThreshold {
		newCount := int(math.Ceil(float64(currentWorkers) * o.workerDecreaseFactor))
		o.logger.Warn("High consecutive errors, reducing workers",
			zap.Uint64("consecutive_errors", stats.ConsecutiveErrors),
			zap.Int("from", currentWorkers),
			zap.Int("to", newCount),
		)
		return o.clampWorkers(newCount)
	}

	// If error rate is too high, reduce workers
	if stats.RecentErrorRate > o.maxErrorRate {
		newCount := int(math.Ceil(float64(currentWorkers) * o.workerDecreaseFactor))
		o.logger.Info("High error rate, reducing workers",
			zap.Float64("error_rate", stats.RecentErrorRate),
			zap.Float64("max_error_rate", o.maxErrorRate),
			zap.Int("from", currentWorkers),
			zap.Int("to", newCount),
		)
		return o.clampWorkers(newCount)
	}

	// If error rate is low and response time is good, increase workers
	if stats.RecentErrorRate < o.targetErrorRate && stats.RecentAvgResponseTime < o.targetResponseTime {
		newCount := int(math.Ceil(float64(currentWorkers) * o.workerIncreaseFactor))
		o.logger.Info("Performance good, increasing workers",
			zap.Float64("error_rate", stats.RecentErrorRate),
			zap.Uint64("avg_response_time_ms", stats.RecentAvgResponseTime),
			zap.Int("from", currentWorkers),
			zap.Int("to", newCount),
		)
		return o.clampWorkers(newCount)
	}

	// If response time is too high, reduce workers to avoid overload
	if stats.RecentAvgResponseTime > o.targetResponseTime*2 {
		newCount := int(math.Ceil(float64(currentWorkers) * o.workerDecreaseFactor))
		o.logger.Info("High response time, reducing workers",
			zap.Uint64("avg_response_time_ms", stats.RecentAvgResponseTime),
			zap.Uint64("target_ms", o.targetResponseTime),
			zap.Int("from", currentWorkers),
			zap.Int("to", newCount),
		)
		return o.clampWorkers(newCount)
	}

	// No change needed
	return currentWorkers
}

// calculateOptimalBatchSize determines the optimal batch size based on current metrics
func (o *AdaptiveOptimizer) calculateOptimalBatchSize(stats MetricsSnapshot) int {
	currentBatchSize := stats.OptimalBatchSize

	// If rate limited or high error rate, reduce batch size
	if stats.RateLimitDetected || stats.RecentErrorRate > o.maxErrorRate {
		newSize := int(math.Ceil(float64(currentBatchSize) * o.batchDecreaseFactor))
		o.logger.Info("Reducing batch size due to errors/rate limit",
			zap.Int("from", currentBatchSize),
			zap.Int("to", newSize),
			zap.Bool("rate_limited", stats.RateLimitDetected),
			zap.Float64("error_rate", stats.RecentErrorRate),
		)
		return o.clampBatchSize(newSize)
	}

	// If response time is fast and error rate is low, increase batch size
	if stats.RecentAvgResponseTime < o.targetResponseTime/2 && stats.RecentErrorRate < o.targetErrorRate {
		newSize := int(math.Ceil(float64(currentBatchSize) * o.batchIncreaseFactor))
		o.logger.Info("Increasing batch size due to good performance",
			zap.Int("from", currentBatchSize),
			zap.Int("to", newSize),
			zap.Uint64("avg_response_time_ms", stats.RecentAvgResponseTime),
			zap.Float64("error_rate", stats.RecentErrorRate),
		)
		return o.clampBatchSize(newSize)
	}

	// If response time is slow, reduce batch size
	if stats.RecentAvgResponseTime > o.targetResponseTime*2 {
		newSize := int(math.Ceil(float64(currentBatchSize) * o.batchDecreaseFactor))
		o.logger.Info("Reducing batch size due to slow response time",
			zap.Int("from", currentBatchSize),
			zap.Int("to", newSize),
			zap.Uint64("avg_response_time_ms", stats.RecentAvgResponseTime),
		)
		return o.clampBatchSize(newSize)
	}

	// No change needed
	return currentBatchSize
}

// clampWorkers ensures worker count is within configured bounds
func (o *AdaptiveOptimizer) clampWorkers(count int) int {
	if count < o.minWorkers {
		return o.minWorkers
	}
	if count > o.maxWorkers {
		return o.maxWorkers
	}
	return count
}

// clampBatchSize ensures batch size is within configured bounds
func (o *AdaptiveOptimizer) clampBatchSize(size int) int {
	if size < o.minBatchSize {
		return o.minBatchSize
	}
	if size > o.maxBatchSize {
		return o.maxBatchSize
	}
	return size
}

// GetRecommendedWorkers returns the current recommended worker count
func (o *AdaptiveOptimizer) GetRecommendedWorkers() int {
	return o.metrics.GetOptimalWorkerCount()
}

// GetRecommendedBatchSize returns the current recommended batch size
func (o *AdaptiveOptimizer) GetRecommendedBatchSize() int {
	return o.metrics.GetOptimalBatchSize()
}

// ForceAdjustment forces an immediate optimization adjustment
func (o *AdaptiveOptimizer) ForceAdjustment() {
	o.lastAdjustment = time.Time{} // Reset to allow immediate adjustment
	o.Optimize()
}
