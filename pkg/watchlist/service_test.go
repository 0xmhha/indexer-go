package watchlist

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("expected config to not be nil")
	}

	if !config.Enabled {
		t.Error("expected Enabled=true by default")
	}

	if config.BloomFilter == nil {
		t.Error("expected BloomFilter config to not be nil")
	}

	if config.HistoryRetention != 720*time.Hour {
		t.Errorf("expected HistoryRetention 720h, got %v", config.HistoryRetention)
	}

	if config.MaxAddressesTotal != 100000 {
		t.Errorf("expected MaxAddressesTotal 100000, got %d", config.MaxAddressesTotal)
	}
}

func TestNewService(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Test with nil config
	service := NewService(nil, nil, nil, logger)

	if service == nil {
		t.Fatal("expected service to not be nil")
	}

	if service.config == nil {
		t.Error("expected default config to be set")
	}

	if service.matcher == nil {
		t.Error("expected matcher to be initialized")
	}

	if service.subscribers == nil {
		t.Error("expected subscribers map to be initialized")
	}

	if service.addrSubs == nil {
		t.Error("expected addrSubs map to be initialized")
	}
}

func TestNewServiceWithConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &Config{
		Enabled:           false,
		BloomFilter:       DefaultBloomConfig(),
		HistoryRetention:  48 * time.Hour,
		MaxAddressesTotal: 1000,
	}

	service := NewService(config, nil, nil, logger)

	if service == nil {
		t.Fatal("expected service to not be nil")
	}

	if service.config.Enabled {
		t.Error("expected Enabled=false")
	}

	if service.config.HistoryRetention != 48*time.Hour {
		t.Errorf("expected HistoryRetention 48h, got %v", service.config.HistoryRetention)
	}
}

func TestServiceStartStop(t *testing.T) {
	// Note: Service Start requires storage for loadWatchedAddresses
	// This test verifies Stop without Start works correctly
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	service := NewService(nil, nil, nil, logger)

	// Stop without starting should succeed (idempotent)
	err := service.Stop(ctx)
	if err != nil {
		t.Errorf("stop failed: %v", err)
	}
}

func TestServiceDoubleStop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	service := NewService(nil, nil, nil, logger)

	// Stop without starting (should be idempotent)
	err := service.Stop(ctx)
	if err != nil {
		t.Errorf("first stop without start should succeed: %v", err)
	}

	err = service.Stop(ctx)
	if err != nil {
		t.Errorf("second stop should be idempotent: %v", err)
	}
}

func TestWatchRequestStruct(t *testing.T) {
	// Test WatchRequest struct creation (validation requires storage)
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Label:   "Test Label",
		Filter:  DefaultWatchFilter(),
	}

	if req.Address == (common.Address{}) {
		t.Error("address should not be zero")
	}

	if req.ChainID != "chain-1" {
		t.Errorf("expected ChainID 'chain-1', got '%s'", req.ChainID)
	}

	if req.Label != "Test Label" {
		t.Errorf("expected Label 'Test Label', got '%s'", req.Label)
	}

	if req.Filter == nil {
		t.Error("filter should not be nil")
	}
}

func TestServiceMatcherIntegration(t *testing.T) {
	// Test that service has a properly initialized matcher
	logger, _ := zap.NewDevelopment()

	service := NewService(nil, nil, nil, logger)

	// Service should have a matcher
	if service.matcher == nil {
		t.Error("expected matcher to be initialized")
	}

	// Matcher should have no watched addresses initially
	if service.matcher.HasWatchedAddresses("any-chain") {
		t.Error("expected no watched addresses initially")
	}
}

func TestListFilterStruct(t *testing.T) {
	// Test ListFilter struct creation
	filter := &ListFilter{
		ChainID: "chain-1",
		Limit:   10,
		Offset:  5,
	}

	if filter.ChainID != "chain-1" {
		t.Error("ChainID not set correctly")
	}

	if filter.Limit != 10 {
		t.Error("Limit not set correctly")
	}

	if filter.Offset != 5 {
		t.Error("Offset not set correctly")
	}
}

func TestServiceUnsubscribeNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	service := NewService(nil, nil, nil, logger)

	err := service.Unsubscribe(ctx, "nonexistent-sub")
	if err != ErrSubscriberNotFound {
		t.Errorf("expected ErrSubscriberNotFound, got %v", err)
	}
}

func TestWatchEventBusEvent(t *testing.T) {
	watchEvent := &WatchEvent{
		ID:          "event-1",
		AddressID:   "addr-1",
		ChainID:     "chain-1",
		EventType:   WatchEventTypeTxFrom,
		BlockNumber: 100,
		Timestamp:   time.Now(),
	}

	busEvent := &WatchEventBusEvent{
		WatchEvent: watchEvent,
	}

	if busEvent.Type() != "watch_event" {
		t.Errorf("expected type 'watch_event', got '%s'", busEvent.Type())
	}

	if busEvent.Timestamp().IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

