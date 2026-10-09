package fetch

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

// ============================================================================
// Config.Validate Tests
// ============================================================================

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := &Config{
		BatchSize:  10,
		MaxRetries: 3,
		RetryDelay: time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestConfig_Validate_InvalidBatchSize(t *testing.T) {
	cfg := &Config{
		BatchSize:  0,
		MaxRetries: 3,
		RetryDelay: time.Second,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero batch size")
	}
}

func TestConfig_Validate_InvalidMaxRetries(t *testing.T) {
	cfg := &Config{
		BatchSize:  10,
		MaxRetries: 0,
		RetryDelay: time.Second,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero max retries")
	}
}

func TestConfig_Validate_InvalidRetryDelay(t *testing.T) {
	cfg := &Config{
		BatchSize:  10,
		MaxRetries: 3,
		RetryDelay: 0,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero retry delay")
	}
}

// ============================================================================
// Fetcher Metrics Methods Tests
// ============================================================================

func TestFetcher_GetMetrics(t *testing.T) {
	f := newTestFetcherForHelpers(t)

	stats := f.GetMetrics()
	if stats.OptimalWorkerCount != 100 {
		t.Errorf("expected default 100 workers, got %d", stats.OptimalWorkerCount)
	}
}

func TestFetcher_LogPerformanceMetrics(t *testing.T) {
	f := newTestFetcherForHelpers(t)
	// Just verify it doesn't panic
	f.LogPerformanceMetrics()
}

func TestFetcher_OptimizeParameters_NilOptimizer(t *testing.T) {
	f := newTestFetcherForHelpers(t)
	// No optimizer set, should not panic
	f.OptimizeParameters()
}

func TestFetcher_GetOptimalWorkerCount_NoOptimizer(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	config := &Config{
		StartHeight: 0,
		BatchSize:   10,
		MaxRetries:  3,
		RetryDelay:  time.Second,
		NumWorkers:  8,
	}
	f := NewFetcher(client, storage, config, zap.NewNop(), nil)

	// Without optimizer, returns config.NumWorkers
	if f.GetOptimalWorkerCount() != 8 {
		t.Errorf("expected 8 from config, got %d", f.GetOptimalWorkerCount())
	}
}

func TestFetcher_GetOptimalBatchSize_NoOptimizer(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	config := &Config{
		StartHeight: 0,
		BatchSize:   15,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}
	f := NewFetcher(client, storage, config, zap.NewNop(), nil)

	// Without optimizer, returns config.BatchSize
	if f.GetOptimalBatchSize() != 15 {
		t.Errorf("expected 15 from config, got %d", f.GetOptimalBatchSize())
	}
}

func TestFetcher_GetOptimalWorkerCount_WithOptimizer(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	config := &Config{
		StartHeight:                0,
		BatchSize:                  10,
		MaxRetries:                 3,
		RetryDelay:                 time.Second,
		NumWorkers:                 8,
		EnableAdaptiveOptimization: true,
	}
	f := NewFetcher(client, storage, config, zap.NewNop(), nil)

	// With optimizer enabled, should return optimizer recommendation
	result := f.GetOptimalWorkerCount()
	if result <= 0 {
		t.Errorf("expected positive worker count, got %d", result)
	}
}

func TestFetcher_GetOptimalBatchSize_WithOptimizer(t *testing.T) {
	client := newMockClient()
	storage := newMockStorage()
	config := &Config{
		StartHeight:                0,
		BatchSize:                  10,
		MaxRetries:                 3,
		RetryDelay:                 time.Second,
		EnableAdaptiveOptimization: true,
	}
	f := NewFetcher(client, storage, config, zap.NewNop(), nil)

	result := f.GetOptimalBatchSize()
	if result <= 0 {
		t.Errorf("expected positive batch size, got %d", result)
	}
}

// ============================================================================
// min helper Tests
// ============================================================================

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("expected min(3,5) = 3")
	}
	if min(10, 2) != 2 {
		t.Error("expected min(10,2) = 2")
	}
	if min(4, 4) != 4 {
		t.Error("expected min(4,4) = 4")
	}
}
