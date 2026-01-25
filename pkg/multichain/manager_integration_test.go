package multichain

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestIntegration_ManagerLifecycle tests the full manager lifecycle
func TestIntegration_ManagerLifecycle(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create manager with valid config
	config := &ManagerConfig{
		Enabled: true,
		Chains: []ChainConfig{
			{
				ID:          "test-chain-1",
				Name:        "Test Chain 1",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
				Enabled:     true,
			},
		},
		HealthCheckInterval:  5 * time.Second,
		MaxUnhealthyDuration: 30 * time.Second,
		AutoRestart:          false,
	}

	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Start manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Verify manager is enabled
	if !manager.IsEnabled() {
		t.Error("Expected manager to be enabled")
	}

	// Register additional chain dynamically
	newChainConfig := &ChainConfig{
		ID:          "test-chain-2",
		Name:        "Test Chain 2",
		RPCEndpoint: "http://localhost:8546",
		ChainID:     2,
		Enabled:     true,
	}

	chainID, err := manager.RegisterChain(ctx, newChainConfig)
	if err != nil {
		t.Fatalf("Failed to register chain: %v", err)
	}

	if chainID != "test-chain-2" {
		t.Errorf("Expected chain ID 'test-chain-2', got '%s'", chainID)
	}

	// Verify chain count
	if manager.ChainCount() != 2 {
		t.Errorf("Expected 2 chains, got %d", manager.ChainCount())
	}

	// List chains
	chains := manager.ListChains()
	if len(chains) != 2 {
		t.Errorf("Expected 2 chains in list, got %d", len(chains))
	}

	// Unregister chain
	if err := manager.UnregisterChain(ctx, "test-chain-2"); err != nil {
		t.Errorf("Failed to unregister chain: %v", err)
	}

	if manager.ChainCount() != 1 {
		t.Errorf("Expected 1 chain after unregister, got %d", manager.ChainCount())
	}

	// Stop manager
	if err := manager.Stop(ctx); err != nil {
		t.Errorf("Failed to stop manager: %v", err)
	}

	t.Log("Manager lifecycle test passed")
}

// TestIntegration_ConcurrentChainOperations tests concurrent chain registration/unregistration
func TestIntegration_ConcurrentChainOperations(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := DefaultManagerConfig()
	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	var wg sync.WaitGroup
	chainCount := 10

	// Register chains concurrently
	wg.Add(chainCount)
	for i := 0; i < chainCount; i++ {
		go func(idx int) {
			defer wg.Done()
			chainConfig := &ChainConfig{
				ID:          fmt.Sprintf("concurrent-chain-%d", idx),
				Name:        fmt.Sprintf("Concurrent Chain %d", idx),
				RPCEndpoint: fmt.Sprintf("http://localhost:%d", 8545+idx),
				ChainID:     uint64(idx + 1),
				Enabled:     true,
			}
			_, err := manager.RegisterChain(ctx, chainConfig)
			if err != nil {
				t.Errorf("Failed to register chain %d: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all chains registered
	if manager.ChainCount() != chainCount {
		t.Errorf("Expected %d chains, got %d", chainCount, manager.ChainCount())
	}

	// Unregister chains concurrently
	wg.Add(chainCount)
	for i := 0; i < chainCount; i++ {
		go func(idx int) {
			defer wg.Done()
			err := manager.UnregisterChain(ctx, fmt.Sprintf("concurrent-chain-%d", idx))
			if err != nil {
				t.Errorf("Failed to unregister chain %d: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all chains unregistered
	if manager.ChainCount() != 0 {
		t.Errorf("Expected 0 chains after unregister, got %d", manager.ChainCount())
	}

	t.Log("Concurrent chain operations test passed")
}

// TestIntegration_HealthCheckMonitoring tests health check functionality
func TestIntegration_HealthCheckMonitoring(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := &ManagerConfig{
		Enabled: true,
		Chains: []ChainConfig{
			{
				ID:          "health-test-chain",
				Name:        "Health Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
				Enabled:     true,
			},
		},
		HealthCheckInterval:  1 * time.Second,
		MaxUnhealthyDuration: 5 * time.Second,
		AutoRestart:          false,
	}

	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop(ctx)

	// Run health check
	statuses := manager.HealthCheck(ctx)

	// With mock chain (no real connection), we expect status for the registered chain
	// The actual health status depends on whether a real RPC endpoint is available
	t.Logf("Health check returned %d statuses", len(statuses))

	for chainID, status := range statuses {
		t.Logf("Chain %s: IsHealthy=%v, CheckedAt=%v",
			chainID, status.IsHealthy, status.CheckedAt)
	}

	t.Log("Health check monitoring test passed")
}

// TestIntegration_MetricsCollection tests metrics collection
func TestIntegration_MetricsCollection(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := DefaultManagerConfig()
	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Register a chain
	chainConfig := &ChainConfig{
		ID:          "metrics-test-chain",
		Name:        "Metrics Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
		Enabled:     true,
	}

	_, err = manager.RegisterChain(ctx, chainConfig)
	if err != nil {
		t.Fatalf("Failed to register chain: %v", err)
	}

	// Get metrics
	metrics := manager.GetMetrics()

	// Verify metrics structure
	if len(metrics) != 1 {
		t.Errorf("Expected 1 chain in metrics, got %d", len(metrics))
	}

	for chainID, chainMetrics := range metrics {
		t.Logf("Chain %s metrics: %+v", chainID, chainMetrics)
	}

	t.Log("Metrics collection test passed")
}

// TestIntegration_DisabledManager tests that disabled manager doesn't process
func TestIntegration_DisabledManager(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := &ManagerConfig{
		Enabled:             false,
		HealthCheckInterval: 30 * time.Second, // Set valid interval even for disabled
	}

	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disabled manager: %v", err)
	}

	if manager.IsEnabled() {
		t.Error("Expected manager to be disabled")
	}

	// Operations on disabled manager should still work but be no-ops
	if err := manager.Start(ctx); err != nil {
		t.Errorf("Start on disabled manager should succeed: %v", err)
	}

	chains := manager.ListChains()
	if len(chains) != 0 {
		t.Errorf("Expected 0 chains for disabled manager, got %d", len(chains))
	}

	if err := manager.Stop(ctx); err != nil {
		t.Errorf("Stop on disabled manager should succeed: %v", err)
	}

	t.Log("Disabled manager test passed")
}
