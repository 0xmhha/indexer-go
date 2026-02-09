package fetch

import (
	"math/big"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/core/types"
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
		StartHeight:               0,
		BatchSize:                 10,
		MaxRetries:                3,
		RetryDelay:                time.Second,
		NumWorkers:                8,
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
		StartHeight:               0,
		BatchSize:                 10,
		MaxRetries:                3,
		RetryDelay:                time.Second,
		EnableAdaptiveOptimization: true,
	}
	f := NewFetcher(client, storage, config, zap.NewNop(), nil)

	result := f.GetOptimalBatchSize()
	if result <= 0 {
		t.Errorf("expected positive batch size, got %d", result)
	}
}

// ============================================================================
// ConsensusFetcher Tests
// ============================================================================

func TestNewConsensusFetcher(t *testing.T) {
	client := newMockClient()
	cf := NewConsensusFetcher(client, zap.NewNop())

	if cf == nil {
		t.Fatal("expected non-nil ConsensusFetcher")
	}
	if cf.eventBus != nil {
		t.Error("expected nil eventBus initially")
	}
}

func TestConsensusFetcher_SetEventBus(t *testing.T) {
	client := newMockClient()
	cf := NewConsensusFetcher(client, zap.NewNop())

	bus := &events.EventBus{}
	cf.SetEventBus(bus)

	if cf.eventBus == nil {
		t.Error("expected eventBus to be set")
	}
}

// ============================================================================
// LargeBlockProcessor Tests
// ============================================================================

func TestNewLargeBlockProcessor(t *testing.T) {
	storage := newMockStorage()
	p := NewLargeBlockProcessor(storage, zap.NewNop())

	if p == nil {
		t.Fatal("expected non-nil LargeBlockProcessor")
	}
	if p.largeBlockThreshold != 50000000 {
		t.Errorf("expected threshold 50M, got %d", p.largeBlockThreshold)
	}
	if p.receiptBatchSize != 100 {
		t.Errorf("expected batch size 100, got %d", p.receiptBatchSize)
	}
	if p.maxReceiptWorkers != 10 {
		t.Errorf("expected max workers 10, got %d", p.maxReceiptWorkers)
	}
}

func TestLargeBlockProcessor_IsLargeBlock(t *testing.T) {
	storage := newMockStorage()
	p := NewLargeBlockProcessor(storage, zap.NewNop())

	// Small block
	smallHeader := &types.Header{
		Number:  big.NewInt(1),
		GasUsed: 21000,
	}
	smallBlock := types.NewBlockWithHeader(smallHeader)
	if p.IsLargeBlock(smallBlock) {
		t.Error("expected small block not to be large")
	}

	// Large block
	largeHeader := &types.Header{
		Number:  big.NewInt(2),
		GasUsed: 60000000, // 60M > 50M threshold
	}
	largeBlock := types.NewBlockWithHeader(largeHeader)
	if !p.IsLargeBlock(largeBlock) {
		t.Error("expected large block to be detected")
	}
}

func TestLargeBlockProcessor_SetTokenIndexer(t *testing.T) {
	storage := newMockStorage()
	p := NewLargeBlockProcessor(storage, zap.NewNop())
	mock := &mockTokenIndexer{}
	p.SetTokenIndexer(mock)

	if p.tokenIndexer == nil {
		t.Error("expected tokenIndexer to be set")
	}
}

func TestLargeBlockProcessor_SetSetCodeProcessor(t *testing.T) {
	storage := newMockStorage()
	p := NewLargeBlockProcessor(storage, zap.NewNop())
	p.SetSetCodeProcessor(nil)
	// Just verify no panic
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
