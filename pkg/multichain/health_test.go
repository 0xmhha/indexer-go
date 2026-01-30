package multichain

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewHealthChecker(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, 5*time.Second, logger)

	if hc == nil {
		t.Fatal("expected non-nil health checker")
	}
	if hc.manager != manager {
		t.Error("expected manager to be set")
	}
	if hc.interval != 5*time.Second {
		t.Errorf("expected interval 5s, got %v", hc.interval)
	}
}

func TestHealthChecker_StartStop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, 100*time.Millisecond, logger)

	ctx := context.Background()
	hc.Start(ctx)

	// Let it run for a bit
	time.Sleep(150 * time.Millisecond)

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		hc.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("health checker stop timed out")
	}
}

func TestHealthChecker_StartStopMultiple(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, 100*time.Millisecond, logger)
	ctx := context.Background()

	// Start and stop multiple times
	for i := 0; i < 3; i++ {
		hc.Start(ctx)
		time.Sleep(50 * time.Millisecond)
		hc.Stop()
	}
}

func TestHealthChecker_CheckChain(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, time.Second, logger)
	ctx := context.Background()

	t.Run("chain not found", func(t *testing.T) {
		_, err := hc.CheckChain(ctx, "nonexistent")
		if err != ErrChainNotFound {
			t.Errorf("expected ErrChainNotFound, got %v", err)
		}
	})

	t.Run("chain exists", func(t *testing.T) {
		chainConfig := &ChainConfig{
			ID:          "test-chain",
			Name:        "Test Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     1,
		}
		_, _ = manager.RegisterChain(ctx, chainConfig)

		status, err := hc.CheckChain(ctx, "test-chain")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status == nil {
			t.Fatal("expected non-nil status")
		}
		if status.ChainID != "test-chain" {
			t.Errorf("expected chainId 'test-chain', got '%s'", status.ChainID)
		}
	})
}

func TestHealthChecker_checkAll(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, time.Second, logger)
	ctx, cancel := context.WithCancel(context.Background())
	hc.ctx = ctx
	defer cancel()

	// Register a chain
	chainConfig := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}
	_, _ = manager.RegisterChain(ctx, chainConfig)

	// checkAll should complete without error
	hc.checkAll()
}

func TestHealthChecker_checkAllEmpty(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, time.Second, logger)
	ctx, cancel := context.WithCancel(context.Background())
	hc.ctx = ctx
	defer cancel()

	// checkAll with no chains should complete without error
	hc.checkAll()
}

func TestHealthChecker_checkAllWithUnhealthyChain(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	// Register a chain (without client, it's unhealthy)
	chainConfig := &ChainConfig{
		ID:          "unhealthy-chain",
		Name:        "Unhealthy Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}
	ctx := context.Background()
	_, _ = manager.RegisterChain(ctx, chainConfig)

	hc := NewHealthChecker(manager, time.Second, logger)
	hc.ctx, hc.cancelFunc = context.WithCancel(ctx)
	defer hc.cancelFunc()

	// This should log the unhealthy chain warning
	hc.checkAll()
}

func TestHealthChecker_WaitForHealthy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, time.Second, logger)

	t.Run("context cancelled", func(t *testing.T) {
		// Register a chain first
		chainConfig := &ChainConfig{
			ID:          "wait-test-chain",
			Name:        "Wait Test Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     1,
		}
		ctx := context.Background()
		_, _ = manager.RegisterChain(ctx, chainConfig)

		// Create a context that cancels quickly
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := hc.WaitForHealthy(ctx, "wait-test-chain")
		if err != context.DeadlineExceeded {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	})

	t.Run("chain not found", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := hc.WaitForHealthy(ctx, "nonexistent-chain")
		// WaitForHealthy checks on ticker, so either returns ErrChainNotFound or context error
		if err != ErrChainNotFound && err != context.DeadlineExceeded {
			t.Errorf("expected ErrChainNotFound or context.DeadlineExceeded, got %v", err)
		}
	})
}

func TestHealthChecker_WaitForAllHealthy(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("no chains returns nil (all healthy)", func(t *testing.T) {
		config := DefaultManagerConfig()
		manager, _ := NewManager(config, nil, nil, logger)
		hc := NewHealthChecker(manager, time.Second, logger)

		// WaitForAllHealthy uses a hardcoded 1-second ticker
		// So we need timeout > 1 second
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		// With no chains, all chains are healthy (vacuously true)
		err := hc.WaitForAllHealthy(ctx)
		if err != nil {
			t.Errorf("expected nil error for no chains, got %v", err)
		}
	})

	t.Run("context cancelled with unhealthy chains", func(t *testing.T) {
		config := DefaultManagerConfig()
		manager, _ := NewManager(config, nil, nil, logger)
		hc := NewHealthChecker(manager, 50*time.Millisecond, logger)

		chainConfig := &ChainConfig{
			ID:          "wait-all-chain",
			Name:        "Wait All Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     2,
		}
		ctx := context.Background()
		_, _ = manager.RegisterChain(ctx, chainConfig)

		// Short timeout - chains without clients are never healthy
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := hc.WaitForAllHealthy(ctx)
		if err != context.DeadlineExceeded {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	})

	t.Run("context immediately cancelled", func(t *testing.T) {
		config := DefaultManagerConfig()
		manager, _ := NewManager(config, nil, nil, logger)
		hc := NewHealthChecker(manager, time.Second, logger)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := hc.WaitForAllHealthy(ctx)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestHealthChecker_run(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	// Use a very short interval for testing
	hc := NewHealthChecker(manager, 50*time.Millisecond, logger)

	ctx, cancel := context.WithCancel(context.Background())
	hc.ctx = ctx

	// Start run in background
	done := make(chan struct{})
	hc.wg.Add(1)
	go func() {
		hc.run()
		close(done)
	}()

	// Let it tick a couple times
	time.Sleep(120 * time.Millisecond)

	// Cancel to stop
	cancel()

	// Wait for run to complete
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("run did not stop after context cancel")
	}
}

func TestHealthChecker_StopWithoutStart(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultManagerConfig()
	manager, _ := NewManager(config, nil, nil, logger)

	hc := NewHealthChecker(manager, time.Second, logger)

	// Stop without start should not panic
	hc.Stop()
}
