package multichain

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewRegistry(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	if registry == nil {
		t.Fatal("expected registry to not be nil")
	}

	if registry.Count() != 0 {
		t.Errorf("expected count 0, got %d", registry.Count())
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	config := &ChainConfig{
		ID:          "test-chain-1",
		Name:        "Test Chain 1",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
		Enabled:     true,
	}

	instance := &ChainInstance{
		Config: config,
		status: StatusRegistered,
	}

	// Register
	err := registry.Register(instance)
	if err != nil {
		t.Fatalf("failed to register chain: %v", err)
	}

	// Get
	got, err := registry.Get("test-chain-1")
	if err != nil {
		t.Fatalf("failed to get chain: %v", err)
	}

	if got.Config.ID != config.ID {
		t.Errorf("expected chain ID %s, got %s", config.ID, got.Config.ID)
	}
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	config := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := &ChainInstance{
		Config: config,
		status: StatusRegistered,
	}

	// First register should succeed
	if err := registry.Register(instance); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	// Second register should fail
	if err := registry.Register(instance); err != ErrChainAlreadyExists {
		t.Errorf("expected ErrChainAlreadyExists, got %v", err)
	}
}

func TestRegistryUnregister(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	config := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	instance := &ChainInstance{
		Config: config,
		status: StatusRegistered,
	}

	// Register
	if err := registry.Register(instance); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Verify exists
	if !registry.Exists("test-chain") {
		t.Fatal("chain should exist")
	}

	// Unregister
	if err := registry.Unregister("test-chain"); err != nil {
		t.Fatalf("unregister failed: %v", err)
	}

	// Verify removed
	if registry.Exists("test-chain") {
		t.Fatal("chain should not exist after unregister")
	}
}

func TestRegistryUnregisterNotFound(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	err := registry.Unregister("nonexistent")
	if err != ErrChainNotFound {
		t.Errorf("expected ErrChainNotFound, got %v", err)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	_, err := registry.Get("nonexistent")
	if err != ErrChainNotFound {
		t.Errorf("expected ErrChainNotFound, got %v", err)
	}
}

func TestRegistryList(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	// Register multiple chains
	for i := 1; i <= 3; i++ {
		config := &ChainConfig{
			ID:          "test-chain-" + string(rune('0'+i)),
			Name:        "Test Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     uint64(i),
		}
		instance := &ChainInstance{
			Config: config,
			status: StatusRegistered,
		}
		if err := registry.Register(instance); err != nil {
			t.Fatalf("register failed: %v", err)
		}
	}

	// List all
	instances := registry.List()
	if len(instances) != 3 {
		t.Errorf("expected 3 instances, got %d", len(instances))
	}
}

func TestRegistryListByStatus(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	// Create chains with different statuses
	statuses := []ChainStatus{StatusRegistered, StatusActive, StatusActive, StatusStopped}
	for i, status := range statuses {
		config := &ChainConfig{
			ID:          "chain-" + string(rune('a'+i)),
			Name:        "Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     uint64(i + 1),
		}
		instance := &ChainInstance{
			Config: config,
			status: status,
		}
		if err := registry.Register(instance); err != nil {
			t.Fatalf("register failed: %v", err)
		}
	}

	// Count by status
	activeCount := registry.CountByStatus(StatusActive)
	if activeCount != 2 {
		t.Errorf("expected 2 active chains, got %d", activeCount)
	}

	stoppedCount := registry.CountByStatus(StatusStopped)
	if stoppedCount != 1 {
		t.Errorf("expected 1 stopped chain, got %d", stoppedCount)
	}

	// List by status
	activeChains := registry.ListByStatus(StatusActive)
	if len(activeChains) != 2 {
		t.Errorf("expected 2 active chains, got %d", len(activeChains))
	}
}

func TestRegistryCount(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	if registry.Count() != 0 {
		t.Errorf("expected count 0, got %d", registry.Count())
	}

	config := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}
	instance := &ChainInstance{
		Config: config,
		status: StatusRegistered,
	}

	if err := registry.Register(instance); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if registry.Count() != 1 {
		t.Errorf("expected count 1, got %d", registry.Count())
	}
}

func TestRegistryExists(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	if registry.Exists("test-chain") {
		t.Error("chain should not exist initially")
	}

	config := &ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}
	instance := &ChainInstance{
		Config: config,
		status: StatusRegistered,
	}

	if err := registry.Register(instance); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if !registry.Exists("test-chain") {
		t.Error("chain should exist after register")
	}
}

func TestRegistryGetAll(t *testing.T) {
	logger := zap.NewNop()
	registry := NewRegistry(logger)

	// Register chains
	for i := 1; i <= 2; i++ {
		config := &ChainConfig{
			ID:          "chain-" + string(rune('0'+i)),
			Name:        "Chain",
			RPCEndpoint: "http://localhost:8545",
			ChainID:     uint64(i),
		}
		instance := &ChainInstance{
			Config: config,
			status: StatusRegistered,
		}
		if err := registry.Register(instance); err != nil {
			t.Fatalf("register failed: %v", err)
		}
	}

	all := registry.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 chains, got %d", len(all))
	}

	// Verify it's a copy
	all["chain-1"] = nil
	original, _ := registry.Get("chain-1")
	if original == nil {
		t.Error("modifying GetAll result should not affect registry")
	}
}
