package fetch

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestDefaultOptimizerConfig(t *testing.T) {
	cfg := DefaultOptimizerConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.MinWorkers <= 0 {
		t.Error("expected MinWorkers > 0")
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		t.Error("expected MaxWorkers >= MinWorkers")
	}
	if cfg.MinBatchSize <= 0 {
		t.Error("expected MinBatchSize > 0")
	}
	if cfg.MaxBatchSize < cfg.MinBatchSize {
		t.Error("expected MaxBatchSize >= MinBatchSize")
	}
	if cfg.AdjustmentInterval <= 0 {
		t.Error("expected positive AdjustmentInterval")
	}
	if cfg.TargetErrorRate <= 0 {
		t.Error("expected positive TargetErrorRate")
	}
	if cfg.MaxErrorRate <= cfg.TargetErrorRate {
		t.Error("expected MaxErrorRate > TargetErrorRate")
	}
}

func TestNewAdaptiveOptimizer(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	opt := NewAdaptiveOptimizer(metrics, nil, zap.NewNop())
	if opt == nil {
		t.Fatal("expected non-nil optimizer")
	}
}

func TestNewAdaptiveOptimizer_WithConfig(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := &OptimizerConfig{
		MinWorkers:           2,
		MaxWorkers:           10,
		MinBatchSize:         3,
		MaxBatchSize:         20,
		AdjustmentInterval:   5 * time.Second,
		TargetErrorRate:      0.02,
		MaxErrorRate:         0.10,
		TargetResponseTime:   200,
		RateLimitThreshold:   5,
		WorkerIncreaseFactor: 1.5,
		WorkerDecreaseFactor: 0.5,
		BatchIncreaseFactor:  2.0,
		BatchDecreaseFactor:  0.5,
	}
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())
	if opt.minWorkers != 2 {
		t.Errorf("expected minWorkers 2, got %d", opt.minWorkers)
	}
	if opt.maxWorkers != 10 {
		t.Errorf("expected maxWorkers 10, got %d", opt.maxWorkers)
	}
}

func TestShouldAdjust(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := &OptimizerConfig{
		MinWorkers:           1,
		MaxWorkers:           10,
		MinBatchSize:         1,
		MaxBatchSize:         50,
		AdjustmentInterval:   1 * time.Millisecond,
		TargetErrorRate:      0.01,
		MaxErrorRate:         0.05,
		TargetResponseTime:   500,
		RateLimitThreshold:   10,
		WorkerIncreaseFactor: 1.2,
		WorkerDecreaseFactor: 0.8,
		BatchIncreaseFactor:  1.5,
		BatchDecreaseFactor:  0.75,
	}
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	// Immediately after creation, lastAdjustment is now
	// Wait just past the interval
	time.Sleep(2 * time.Millisecond)
	if !opt.ShouldAdjust() {
		t.Error("expected ShouldAdjust=true after interval")
	}
}

func TestClampWorkers(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := &OptimizerConfig{
		MinWorkers:           2,
		MaxWorkers:           10,
		MinBatchSize:         1,
		MaxBatchSize:         50,
		AdjustmentInterval:   30 * time.Second,
		TargetErrorRate:      0.01,
		MaxErrorRate:         0.05,
		TargetResponseTime:   500,
		RateLimitThreshold:   10,
		WorkerIncreaseFactor: 1.2,
		WorkerDecreaseFactor: 0.8,
		BatchIncreaseFactor:  1.5,
		BatchDecreaseFactor:  0.75,
	}
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	if opt.clampWorkers(1) != 2 {
		t.Error("expected clamp to min=2")
	}
	if opt.clampWorkers(5) != 5 {
		t.Error("expected 5 within range")
	}
	if opt.clampWorkers(20) != 10 {
		t.Error("expected clamp to max=10")
	}
}

func TestClampBatchSize(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := &OptimizerConfig{
		MinWorkers:           1,
		MaxWorkers:           10,
		MinBatchSize:         5,
		MaxBatchSize:         25,
		AdjustmentInterval:   30 * time.Second,
		TargetErrorRate:      0.01,
		MaxErrorRate:         0.05,
		TargetResponseTime:   500,
		RateLimitThreshold:   10,
		WorkerIncreaseFactor: 1.2,
		WorkerDecreaseFactor: 0.8,
		BatchIncreaseFactor:  1.5,
		BatchDecreaseFactor:  0.75,
	}
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	if opt.clampBatchSize(2) != 5 {
		t.Error("expected clamp to min=5")
	}
	if opt.clampBatchSize(15) != 15 {
		t.Error("expected 15 within range")
	}
	if opt.clampBatchSize(50) != 25 {
		t.Error("expected clamp to max=25")
	}
}

func TestCalculateOptimalWorkers_RateLimited(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalWorkerCount: 10,
		RateLimitDetected:  true,
	}

	result := opt.calculateOptimalWorkers(stats)
	if result >= 10 {
		t.Errorf("expected workers reduced from 10 when rate limited, got %d", result)
	}
}

func TestCalculateOptimalWorkers_HighConsecutiveErrors(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalWorkerCount: 10,
		ConsecutiveErrors:  20, // Above threshold
	}

	result := opt.calculateOptimalWorkers(stats)
	if result >= 10 {
		t.Errorf("expected workers reduced for high consecutive errors, got %d", result)
	}
}

func TestCalculateOptimalWorkers_HighErrorRate(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalWorkerCount: 10,
		RecentErrorRate:    0.10, // Above max error rate
	}

	result := opt.calculateOptimalWorkers(stats)
	if result >= 10 {
		t.Errorf("expected workers reduced for high error rate, got %d", result)
	}
}

func TestCalculateOptimalWorkers_GoodPerformance(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalWorkerCount:    10,
		RecentErrorRate:       0.001, // Very low
		RecentAvgResponseTime: 100,   // Well below target
	}

	result := opt.calculateOptimalWorkers(stats)
	if result <= 10 {
		t.Errorf("expected workers increased for good performance, got %d", result)
	}
}

func TestCalculateOptimalWorkers_HighResponseTime(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalWorkerCount:    10,
		RecentErrorRate:       0.02, // Between target and max
		RecentAvgResponseTime: 2000, // Way above target*2
	}

	result := opt.calculateOptimalWorkers(stats)
	if result >= 10 {
		t.Errorf("expected workers reduced for high response time, got %d", result)
	}
}

func TestCalculateOptimalWorkers_NoChange(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalWorkerCount:    10,
		RecentErrorRate:       0.02,  // Between target and max
		RecentAvgResponseTime: 600,   // Above target but below target*2
	}

	result := opt.calculateOptimalWorkers(stats)
	if result != 10 {
		t.Errorf("expected no change, got %d", result)
	}
}

func TestCalculateOptimalBatchSize_RateLimited(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalBatchSize:  20,
		RateLimitDetected: true,
	}

	result := opt.calculateOptimalBatchSize(stats)
	if result >= 20 {
		t.Errorf("expected batch size reduced when rate limited, got %d", result)
	}
}

func TestCalculateOptimalBatchSize_GoodPerformance(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalBatchSize:      10,
		RecentAvgResponseTime: 100,   // Fast
		RecentErrorRate:       0.001, // Low
	}

	result := opt.calculateOptimalBatchSize(stats)
	if result <= 10 {
		t.Errorf("expected batch size increased for good performance, got %d", result)
	}
}

func TestCalculateOptimalBatchSize_SlowResponse(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	stats := MetricsSnapshot{
		OptimalBatchSize:      20,
		RecentAvgResponseTime: 2000, // Slow
		RecentErrorRate:       0.02,
	}

	result := opt.calculateOptimalBatchSize(stats)
	if result >= 20 {
		t.Errorf("expected batch size reduced for slow response, got %d", result)
	}
}

func TestOptimize_NotReady(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	cfg.AdjustmentInterval = 1 * time.Hour // Very long interval
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	initialWorkers := metrics.GetOptimalWorkerCount()
	opt.Optimize()
	// Should not have changed since interval hasn't passed
	if metrics.GetOptimalWorkerCount() != initialWorkers {
		t.Error("expected no change when not ready")
	}
}

func TestForceAdjustment(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	cfg := DefaultOptimizerConfig()
	cfg.AdjustmentInterval = 1 * time.Hour // Very long interval
	opt := NewAdaptiveOptimizer(metrics, cfg, zap.NewNop())

	// ForceAdjustment should work even with long interval
	opt.ForceAdjustment()
	// Just verify it doesn't panic
}

func TestGetRecommendedWorkers(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	metrics.SetOptimalWorkerCount(5)
	opt := NewAdaptiveOptimizer(metrics, nil, zap.NewNop())

	if opt.GetRecommendedWorkers() != 5 {
		t.Errorf("expected 5 workers, got %d", opt.GetRecommendedWorkers())
	}
}

func TestGetRecommendedBatchSize(t *testing.T) {
	metrics := NewRPCMetrics(100, 30*time.Second)
	metrics.SetOptimalBatchSize(15)
	opt := NewAdaptiveOptimizer(metrics, nil, zap.NewNop())

	if opt.GetRecommendedBatchSize() != 15 {
		t.Errorf("expected 15 batch size, got %d", opt.GetRecommendedBatchSize())
	}
}
