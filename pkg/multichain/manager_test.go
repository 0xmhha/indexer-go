package multichain

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestNewManager(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Test with nil config (should use defaults)
	manager, err := NewManager(nil, nil, nil, logger)
	if err != nil {
		t.Fatalf("expected no error with nil config, got %v", err)
	}

	if manager == nil {
		t.Fatal("expected manager to not be nil")
	}

	if manager.config == nil {
		t.Error("expected default config to be set")
	}
}

func TestNewManagerWithConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &ManagerConfig{
		Enabled: true,
		Chains: []ChainConfig{
			{
				ID:          "test-chain",
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
			},
		},
	}

	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if manager == nil {
		t.Fatal("expected manager to not be nil")
	}

	if !manager.IsEnabled() {
		t.Error("expected manager to be enabled")
	}
}

func TestNewManagerInvalidConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Config with enabled=true but no chains
	config := &ManagerConfig{
		Enabled: true,
		Chains:  []ChainConfig{}, // Empty chains
	}

	_, err := NewManager(config, nil, nil, logger)
	if err == nil {
		t.Error("expected error for invalid config (enabled with no chains)")
	}
}

func TestManagerChainCount(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := DefaultManagerConfig()
	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Initially no chains
	if manager.ChainCount() != 0 {
		t.Errorf("expected chain count 0, got %d", manager.ChainCount())
	}

	if manager.ActiveChainCount() != 0 {
		t.Errorf("expected active chain count 0, got %d", manager.ActiveChainCount())
	}
}

func TestManagerIsEnabled(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Disabled config
	disabledConfig := DefaultManagerConfig()
	disabledConfig.Enabled = false
	disabledManager, _ := NewManager(disabledConfig, nil, nil, logger)

	if disabledManager.IsEnabled() {
		t.Error("expected disabled manager to report IsEnabled() = false")
	}

	// Enabled config with valid chain
	enabledConfig := &ManagerConfig{
		Enabled: true,
		Chains: []ChainConfig{
			{
				ID:          "test-chain",
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
			},
		},
	}
	enabledManager, _ := NewManager(enabledConfig, nil, nil, logger)

	if !enabledManager.IsEnabled() {
		t.Error("expected enabled manager to report IsEnabled() = true")
	}
}

func TestManagerGetChainNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	_, err := manager.GetChain("nonexistent")
	if err != ErrChainNotFound {
		t.Errorf("expected ErrChainNotFound, got %v", err)
	}
}

func TestManagerListChainsEmpty(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	chains := manager.ListChains()
	if len(chains) != 0 {
		t.Errorf("expected empty chain list, got %d chains", len(chains))
	}
}

func TestManagerHealthCheckEmpty(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	statuses := manager.HealthCheck(ctx)
	if len(statuses) != 0 {
		t.Errorf("expected empty health statuses, got %d", len(statuses))
	}
}

func TestManagerGetMetricsEmpty(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	metrics := manager.GetMetrics()
	if len(metrics) != 0 {
		t.Errorf("expected empty metrics, got %d", len(metrics))
	}
}

func TestManagerStartStopEmpty(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	// Start with no chains should succeed
	if err := manager.Start(ctx); err != nil {
		t.Errorf("start with no chains failed: %v", err)
	}

	// Stop should succeed
	if err := manager.Stop(ctx); err != nil {
		t.Errorf("stop failed: %v", err)
	}
}

func TestManagerDoubleStart(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	// First start
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Second start should be idempotent
	if err := manager.Start(ctx); err != nil {
		t.Errorf("second start should be idempotent, got error: %v", err)
	}

	// Cleanup
	_ = manager.Stop(ctx)
}

func TestManagerDoubleStop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	// Start first
	_ = manager.Start(ctx)

	// First stop
	if err := manager.Stop(ctx); err != nil {
		t.Fatalf("first stop failed: %v", err)
	}

	// Second stop should be idempotent
	if err := manager.Stop(ctx); err != nil {
		t.Errorf("second stop should be idempotent, got error: %v", err)
	}
}

func TestManagerRegisterChain(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	chainConfig := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	chainID, err := manager.RegisterChain(ctx, chainConfig)
	if err != nil {
		t.Fatalf("failed to register chain: %v", err)
	}

	if chainID != "test-chain" {
		t.Errorf("expected chain ID 'test-chain', got '%s'", chainID)
	}

	if manager.ChainCount() != 1 {
		t.Errorf("expected chain count 1, got %d", manager.ChainCount())
	}

	// Verify we can get the chain
	instance, err := manager.GetChain("test-chain")
	if err != nil {
		t.Errorf("failed to get registered chain: %v", err)
	}
	if instance == nil {
		t.Error("expected chain instance to not be nil")
	}
}

func TestManagerRegisterDuplicateChain(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	chainConfig := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	// First registration
	_, err := manager.RegisterChain(ctx, chainConfig)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Duplicate registration
	_, err = manager.RegisterChain(ctx, chainConfig)
	if err != ErrChainAlreadyExists {
		t.Errorf("expected ErrChainAlreadyExists, got %v", err)
	}
}

func TestManagerRegisterInvalidChain(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	// Missing required fields
	invalidConfig := &ChainConfig{
		ID: "", // Missing ID
	}

	_, err := manager.RegisterChain(ctx, invalidConfig)
	if err == nil {
		t.Error("expected error for invalid chain config")
	}
}

func TestManagerUnregisterChain(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	chainConfig := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	// Register first
	_, _ = manager.RegisterChain(ctx, chainConfig)
	if manager.ChainCount() != 1 {
		t.Fatal("chain should be registered")
	}

	// Unregister
	err := manager.UnregisterChain(ctx, "test-chain")
	if err != nil {
		t.Errorf("failed to unregister chain: %v", err)
	}

	if manager.ChainCount() != 0 {
		t.Error("chain count should be 0 after unregister")
	}
}

func TestManagerUnregisterNonexistent(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	err := manager.UnregisterChain(ctx, "nonexistent")
	if err != ErrChainNotFound {
		t.Errorf("expected ErrChainNotFound, got %v", err)
	}
}

func TestManagerStartChainNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	err := manager.StartChain(ctx, "nonexistent")
	if err != ErrChainNotFound {
		t.Errorf("expected ErrChainNotFound, got %v", err)
	}
}

func TestManagerStopChainNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	err := manager.StopChain(ctx, "nonexistent")
	if err != ErrChainNotFound {
		t.Errorf("expected ErrChainNotFound, got %v", err)
	}
}

func TestManagerStopChainRegistered(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	chainConfig := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	// Register chain (without starting it)
	_, err := manager.RegisterChain(ctx, chainConfig)
	if err != nil {
		t.Fatalf("failed to register chain: %v", err)
	}

	// StopChain on registered (not started) chain
	err = manager.StopChain(ctx, "test-chain")
	if err != nil {
		t.Errorf("stop chain should succeed on registered chain: %v", err)
	}
}

func TestManagerStartChainRequiresStorage(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger) // nil storage

	chainConfig := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	// Register chain
	_, err := manager.RegisterChain(ctx, chainConfig)
	if err != nil {
		t.Fatalf("failed to register chain: %v", err)
	}

	// StartChain should fail due to nil storage
	err = manager.StartChain(ctx, "test-chain")
	if err != ErrStorageRequired {
		t.Errorf("expected ErrStorageRequired, got %v", err)
	}
}
