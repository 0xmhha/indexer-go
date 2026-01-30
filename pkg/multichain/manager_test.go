package multichain

import (
	"context"
	"testing"
	"time"

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

func TestManagerWaitForSync(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	t.Run("times out with no healthy chains", func(t *testing.T) {
		// Register a chain (won't be healthy)
		chainConfig := &ChainConfig{
			ID:          "wait-sync-chain",
			Name:        "Wait Sync Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     1,
		}
		ctx := context.Background()
		_, _ = manager.RegisterChain(ctx, chainConfig)

		// WaitForSync should timeout
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := manager.WaitForSync(ctx)
		if err != context.DeadlineExceeded {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	})
}

func TestManagerCheckAndRestartFailedChains(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &ManagerConfig{
		Enabled:             false,
		AutoRestart:         true,
		AutoRestartDelay:    10 * time.Millisecond, // Short delay for testing
		HealthCheckInterval: 50 * time.Millisecond,
	}
	manager, _ := NewManager(config, nil, nil, logger)

	ctx := context.Background()

	// Register a chain
	chainConfig := &ChainConfig{
		ID:          "restart-test-chain",
		Name:        "Restart Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}
	_, _ = manager.RegisterChain(ctx, chainConfig)

	// Get the instance and set to error state
	instance, _ := manager.GetChain("restart-test-chain")
	instance.setError(ErrClientInitFailed)

	// Wait for auto-restart delay
	time.Sleep(20 * time.Millisecond)

	// Set context for manager
	manager.ctx, manager.cancelFunc = context.WithCancel(ctx)
	defer manager.cancelFunc()

	// Call checkAndRestartFailedChains
	manager.checkAndRestartFailedChains()

	// The chain should attempt restart (will fail due to no storage, but that's expected)
	// Just verify no panic and the function completes
}

func TestManagerCheckAndRestartFailedChains_DelayNotElapsed(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &ManagerConfig{
		Enabled:             false,
		AutoRestart:         true,
		AutoRestartDelay:    5 * time.Second, // Long delay - won't be reached
		HealthCheckInterval: 50 * time.Millisecond,
	}
	manager, _ := NewManager(config, nil, nil, logger)

	ctx := context.Background()

	// Register a chain
	chainConfig := &ChainConfig{
		ID:          "delay-test-chain",
		Name:        "Delay Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}
	_, _ = manager.RegisterChain(ctx, chainConfig)

	// Get the instance and set to error state
	instance, _ := manager.GetChain("delay-test-chain")
	instance.setError(ErrClientInitFailed)

	// Don't wait for auto-restart delay

	// Set context for manager
	manager.ctx, manager.cancelFunc = context.WithCancel(ctx)
	defer manager.cancelFunc()

	// Save the status before calling checkAndRestartFailedChains
	statusBefore := instance.Status()

	// Call checkAndRestartFailedChains - should skip due to delay not elapsed
	manager.checkAndRestartFailedChains()

	// Status should still be error (no restart attempted)
	if instance.Status() != statusBefore {
		t.Errorf("expected status to remain %v, got %v", statusBefore, instance.Status())
	}
}

func TestManagerCheckAndRestartFailedChains_NoErrorChains(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &ManagerConfig{
		Enabled:             false,
		AutoRestart:         true,
		AutoRestartDelay:    10 * time.Millisecond,
		HealthCheckInterval: 50 * time.Millisecond,
	}
	manager, _ := NewManager(config, nil, nil, logger)

	ctx := context.Background()

	// Register a chain but don't set to error
	chainConfig := &ChainConfig{
		ID:          "no-error-chain",
		Name:        "No Error Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}
	_, _ = manager.RegisterChain(ctx, chainConfig)

	// Set context for manager
	manager.ctx, manager.cancelFunc = context.WithCancel(ctx)
	defer manager.cancelFunc()

	// Call checkAndRestartFailedChains - should do nothing (no error chains)
	manager.checkAndRestartFailedChains()

	// Chain should still be registered status
	instance, _ := manager.GetChain("no-error-chain")
	if instance.Status() != StatusRegistered {
		t.Errorf("expected status registered, got %v", instance.Status())
	}
}

func TestManagerAutoRestartMonitor(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &ManagerConfig{
		Enabled:             false,
		AutoRestart:         true,
		AutoRestartDelay:    10 * time.Millisecond,
		HealthCheckInterval: 50 * time.Millisecond,
	}
	manager, _ := NewManager(config, nil, nil, logger)

	ctx, cancel := context.WithCancel(context.Background())
	manager.ctx = ctx

	// Start auto-restart monitor
	manager.runningWg.Add(1)
	go manager.autoRestartMonitor()

	// Let it run for a couple ticks
	time.Sleep(120 * time.Millisecond)

	// Cancel to stop
	cancel()

	// Wait for completion with timeout
	done := make(chan struct{})
	go func() {
		manager.runningWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("autoRestartMonitor did not stop")
	}
}

func TestManagerStartWithAutoRestart(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := &ManagerConfig{
		Enabled:             false,
		AutoRestart:         true,
		AutoRestartDelay:    time.Second,
		HealthCheckInterval: time.Second,
	}
	manager, _ := NewManager(config, nil, nil, logger)

	// Start should launch auto-restart monitor
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop
	err = manager.Stop(ctx)
	if err != nil {
		t.Errorf("stop failed: %v", err)
	}
}

func TestManagerStartWithChains(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := &ManagerConfig{
		Enabled: true,
		Chains: []ChainConfig{
			{
				ID:          "auto-start-chain",
				Name:        "Auto Start Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
				Enabled:     true,
			},
		},
		HealthCheckInterval: time.Second,
	}

	// Create manager with nil storage (chains will fail to start but shouldn't panic)
	manager, err := NewManager(config, nil, nil, logger)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Start should attempt to start all enabled chains
	err = manager.Start(ctx)
	if err != nil {
		t.Errorf("start failed: %v", err)
	}

	// Verify chain was registered
	if manager.ChainCount() != 1 {
		t.Errorf("expected 1 chain registered, got %d", manager.ChainCount())
	}

	// Cleanup
	manager.Stop(ctx)
}

func TestManagerUnregisterRunningChain(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	chainConfig := &ChainConfig{
		ID:          "unregister-running-chain",
		Name:        "Unregister Running Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	// Register chain
	_, err := manager.RegisterChain(ctx, chainConfig)
	if err != nil {
		t.Fatalf("failed to register chain: %v", err)
	}

	// Manually set to syncing status
	instance, _ := manager.GetChain("unregister-running-chain")
	instance.setStatus(StatusSyncing)

	// Unregister should stop the chain first
	err = manager.UnregisterChain(ctx, "unregister-running-chain")
	if err != nil {
		t.Errorf("unregister failed: %v", err)
	}

	// Verify unregistered
	if manager.ChainCount() != 0 {
		t.Error("chain should be unregistered")
	}
}

func TestManagerStopWithRunningChains(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	// Register multiple chains
	for i := 0; i < 3; i++ {
		chainConfig := &ChainConfig{
			ID:          "stop-chain-" + string(rune('A'+i)),
			Name:        "Stop Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     uint64(i + 1),
		}
		_, _ = manager.RegisterChain(ctx, chainConfig)
	}

	// Start manager
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Stop should stop all chains
	err = manager.Stop(ctx)
	if err != nil {
		t.Errorf("stop failed: %v", err)
	}

	// Verify all stopped
	for _, info := range manager.ListChains() {
		if info.Status != StatusStopped {
			t.Errorf("chain %s should be stopped, got %v", info.ID, info.Status)
		}
	}
}

func TestManagerStopWithTimeout(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	config := &ManagerConfig{
		Enabled:             false,
		AutoRestart:         true,
		HealthCheckInterval: time.Second,
	}
	manager, _ := NewManager(config, nil, nil, logger)

	// Start manager
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Give time for goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Stop with short timeout
	stopCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err = manager.Stop(stopCtx)
	if err != nil {
		t.Errorf("stop failed: %v", err)
	}
}
