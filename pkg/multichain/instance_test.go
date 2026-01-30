package multichain

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewChainInstance(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "test-instance",
		Name:        "Test Instance",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	if instance == nil {
		t.Fatal("expected non-nil instance")
	}
	if instance.Config != cfg {
		t.Error("expected config to be set")
	}
	if instance.Status() != StatusRegistered {
		t.Errorf("expected status registered, got %v", instance.Status())
	}
}

func TestChainInstance_Status(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "status-test",
		Name:        "Status Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Initial status
	if instance.Status() != StatusRegistered {
		t.Errorf("expected initial status registered, got %v", instance.Status())
	}

	// Set status
	instance.setStatus(StatusSyncing)
	if instance.Status() != StatusSyncing {
		t.Errorf("expected status syncing, got %v", instance.Status())
	}

	instance.setStatus(StatusActive)
	if instance.Status() != StatusActive {
		t.Errorf("expected status active, got %v", instance.Status())
	}
}

func TestChainInstance_Info(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "info-test",
		Name:        "Info Test",
		RPCEndpoint: "http://localhost:8545",
		WSEndpoint:  "ws://localhost:8546",
		ChainID:     42,
		AdapterType: "evm",
		StartHeight: 100,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)
	info := instance.Info()

	if info.ID != cfg.ID {
		t.Errorf("expected ID %s, got %s", cfg.ID, info.ID)
	}
	if info.Name != cfg.Name {
		t.Errorf("expected Name %s, got %s", cfg.Name, info.Name)
	}
	if info.ChainID != cfg.ChainID {
		t.Errorf("expected ChainID %d, got %d", cfg.ChainID, info.ChainID)
	}
	if info.RPCEndpoint != cfg.RPCEndpoint {
		t.Errorf("expected RPCEndpoint %s, got %s", cfg.RPCEndpoint, info.RPCEndpoint)
	}
	if info.WSEndpoint != cfg.WSEndpoint {
		t.Errorf("expected WSEndpoint %s, got %s", cfg.WSEndpoint, info.WSEndpoint)
	}
	if info.AdapterType != cfg.AdapterType {
		t.Errorf("expected AdapterType %s, got %s", cfg.AdapterType, info.AdapterType)
	}
	if info.StartHeight != cfg.StartHeight {
		t.Errorf("expected StartHeight %d, got %d", cfg.StartHeight, info.StartHeight)
	}
	if info.Status != StatusRegistered {
		t.Errorf("expected Status registered, got %v", info.Status)
	}
	if info.StartedAt != nil {
		t.Error("expected StartedAt to be nil")
	}
}

func TestChainInstance_GetMetrics(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "metrics-test",
		Name:        "Metrics Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Initial metrics should be zero
	metrics := instance.GetMetrics()
	if metrics.ChainID != cfg.ID {
		t.Errorf("expected chainId %s, got %s", cfg.ID, metrics.ChainID)
	}
	if metrics.BlocksIndexed != 0 {
		t.Errorf("expected BlocksIndexed 0, got %d", metrics.BlocksIndexed)
	}
	if metrics.TransactionsIndexed != 0 {
		t.Errorf("expected TransactionsIndexed 0, got %d", metrics.TransactionsIndexed)
	}
	if metrics.LogsIndexed != 0 {
		t.Errorf("expected LogsIndexed 0, got %d", metrics.LogsIndexed)
	}

	// Increment metrics
	instance.blocksIndexed.Add(10)
	instance.transactionsIndexed.Add(100)
	instance.logsIndexed.Add(500)
	instance.rpcCalls.Add(1000)
	instance.rpcErrors.Add(5)

	metrics = instance.GetMetrics()
	if metrics.BlocksIndexed != 10 {
		t.Errorf("expected BlocksIndexed 10, got %d", metrics.BlocksIndexed)
	}
	if metrics.TransactionsIndexed != 100 {
		t.Errorf("expected TransactionsIndexed 100, got %d", metrics.TransactionsIndexed)
	}
	if metrics.LogsIndexed != 500 {
		t.Errorf("expected LogsIndexed 500, got %d", metrics.LogsIndexed)
	}
	if metrics.RPCCalls != 1000 {
		t.Errorf("expected RPCCalls 1000, got %d", metrics.RPCCalls)
	}
	if metrics.RPCErrors != 5 {
		t.Errorf("expected RPCErrors 5, got %d", metrics.RPCErrors)
	}
}

func TestChainInstance_StartWithoutStorage(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "no-storage-test",
		Name:        "No Storage Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger) // nil storage

	err := instance.Start(ctx)
	if err != ErrStorageRequired {
		t.Errorf("expected ErrStorageRequired, got %v", err)
	}
	if instance.Status() != StatusError {
		t.Errorf("expected status error after failed start, got %v", instance.Status())
	}
}

func TestChainInstance_StartAlreadyRunning(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "already-running-test",
		Name:        "Already Running Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Manually set status to syncing
	instance.setStatus(StatusSyncing)

	err := instance.Start(ctx)
	if err != ErrChainAlreadyRunning {
		t.Errorf("expected ErrChainAlreadyRunning, got %v", err)
	}
}

func TestChainInstance_StartFromError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "error-restart-test",
		Name:        "Error Restart Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Set error status
	instance.setStatus(StatusError)

	// Should be able to start from error state, but will fail due to no storage
	err := instance.Start(ctx)
	if err != ErrStorageRequired {
		t.Errorf("expected ErrStorageRequired, got %v", err)
	}
}

func TestChainInstance_StartFromStopped(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "stopped-restart-test",
		Name:        "Stopped Restart Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Set stopped status
	instance.setStatus(StatusStopped)

	// Should be able to start from stopped state, but will fail due to no storage
	err := instance.Start(ctx)
	if err != ErrStorageRequired {
		t.Errorf("expected ErrStorageRequired, got %v", err)
	}
}

func TestChainInstance_StopRegistered(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "stop-registered-test",
		Name:        "Stop Registered Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Stop a registered (not started) chain
	err := instance.Stop(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if instance.Status() != StatusStopped {
		t.Errorf("expected status stopped, got %v", instance.Status())
	}
}

func TestChainInstance_StopAlreadyStopped(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "stop-already-stopped",
		Name:        "Stop Already Stopped",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)
	instance.setStatus(StatusStopped)

	// Stop should be idempotent
	err := instance.Stop(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestChainInstance_StopStopping(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "stop-stopping",
		Name:        "Stop Stopping",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)
	instance.setStatus(StatusStopping)

	// Stop should be idempotent when already stopping
	err := instance.Stop(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestChainInstance_StopWithTimeout(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "stop-timeout-test",
		Name:        "Stop Timeout Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)
	instance.setStatus(StatusSyncing)

	// Set up a cancel function
	ctx, cancel := context.WithCancel(context.Background())
	instance.ctx = ctx
	instance.cancelFunc = cancel

	// Simulate a running goroutine
	instance.runningWg.Add(1)
	go func() {
		time.Sleep(500 * time.Millisecond) // Delay before completing
		instance.runningWg.Done()
	}()

	// Stop with short timeout
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stopCancel()

	err := instance.Stop(stopCtx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Status should be stopped even if timed out
	if instance.Status() != StatusStopped {
		t.Errorf("expected status stopped, got %v", instance.Status())
	}
}

func TestChainInstance_HealthCheck(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "health-check-test",
		Name:        "Health Check Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Health check without client
	status := instance.HealthCheck(ctx)
	if status.ChainID != cfg.ID {
		t.Errorf("expected chainId %s, got %s", cfg.ID, status.ChainID)
	}
	if status.Status != StatusRegistered {
		t.Errorf("expected status registered, got %v", status.Status)
	}
}

func TestChainInstance_HealthCheckWithUptime(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "uptime-test",
		Name:        "Uptime Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Set started time
	now := time.Now()
	instance.startedAt = &now

	time.Sleep(50 * time.Millisecond)

	status := instance.HealthCheck(ctx)
	if status.Uptime < 50*time.Millisecond {
		t.Errorf("expected uptime >= 50ms, got %v", status.Uptime)
	}
}

func TestChainInstance_HealthCheckWithError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	cfg := &ChainConfig{
		ID:          "error-check-test",
		Name:        "Error Check Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Set error
	instance.setError(ErrClientInitFailed)

	status := instance.HealthCheck(ctx)
	if status.LastError == "" {
		t.Error("expected LastError to be set")
	}
	if status.LastErrorTime == nil {
		t.Error("expected LastErrorTime to be set")
	}
}

func TestChainInstance_setError(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "set-error-test",
		Name:        "Set Error Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	testErr := ErrClientInitFailed
	instance.setError(testErr)

	if instance.Status() != StatusError {
		t.Errorf("expected status error, got %v", instance.Status())
	}
	if instance.lastError != testErr {
		t.Errorf("expected lastError to be set")
	}
	if instance.lastErrorAt == nil {
		t.Error("expected lastErrorAt to be set")
	}
}

func TestChainInstance_setStatusLocked(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "status-locked-test",
		Name:        "Status Locked Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Test status change
	instance.statusMu.Lock()
	instance.setStatusLocked(StatusSyncing)
	instance.statusMu.Unlock()

	if instance.Status() != StatusSyncing {
		t.Errorf("expected status syncing, got %v", instance.Status())
	}

	// Test same status (should not log)
	instance.statusMu.Lock()
	instance.setStatusLocked(StatusSyncing)
	instance.statusMu.Unlock()

	if instance.Status() != StatusSyncing {
		t.Errorf("expected status still syncing, got %v", instance.Status())
	}
}

func TestChainInstance_ConcurrentStatusAccess(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "concurrent-status-test",
		Name:        "Concurrent Status Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Concurrent reads and writes
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			instance.setStatus(StatusSyncing)
			instance.setStatus(StatusActive)
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		_ = instance.Status()
		_ = instance.Info()
	}

	<-done
}

func TestChainInstance_ConcurrentMetricsAccess(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &ChainConfig{
		ID:          "concurrent-metrics-test",
		Name:        "Concurrent Metrics Test",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := NewChainInstance(cfg, nil, nil, logger)

	// Concurrent metric updates and reads
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			instance.blocksIndexed.Add(1)
			instance.transactionsIndexed.Add(10)
			instance.logsIndexed.Add(5)
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		_ = instance.GetMetrics()
	}

	<-done

	metrics := instance.GetMetrics()
	if metrics.BlocksIndexed != 100 {
		t.Errorf("expected 100 blocks, got %d", metrics.BlocksIndexed)
	}
}
