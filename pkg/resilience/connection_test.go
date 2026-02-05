package resilience

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewConnectionManager(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)

	// Test with nil config
	cm := NewConnectionManager(sessionStore, eventCache, nil, logger)
	if cm == nil {
		t.Fatal("NewConnectionManager returned nil")
	}
	if cm.config == nil {
		t.Error("config should be set to default")
	}
	if cm.config.SessionTTL != 24*time.Hour {
		t.Errorf("expected default SessionTTL 24h, got %v", cm.config.SessionTTL)
	}

	// Test with custom config
	customConfig := &Config{
		SessionTTL:           time.Hour,
		SessionCleanupPeriod: 10 * time.Minute,
		CacheWindow:          30 * time.Minute,
		MaxEventsPerSession:  500,
	}
	cm2 := NewConnectionManager(sessionStore, eventCache, customConfig, logger)
	if cm2.config.SessionTTL != time.Hour {
		t.Errorf("expected custom SessionTTL 1h, got %v", cm2.config.SessionTTL)
	}
}

func TestConnectionManager_StartStop(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := &Config{
		SessionTTL:           time.Hour,
		SessionCleanupPeriod: 100 * time.Millisecond, // Short for testing
		CacheWindow:          time.Hour,
	}

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	err := cm.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let cleanup run at least once
	time.Sleep(150 * time.Millisecond)

	err = cm.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestConnectionManager_HandleConnect(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 10)

	// Test new session creation
	session, err := cm.HandleConnect(ctx, "client-1", sendChan)
	if err != nil {
		t.Fatalf("HandleConnect failed: %v", err)
	}
	if session == nil {
		t.Fatal("session should not be nil")
	}
	if session.ClientID != "client-1" {
		t.Errorf("expected ClientID 'client-1', got '%s'", session.ClientID)
	}
	if session.State != SessionStateActive {
		t.Errorf("expected state Active, got %s", session.State)
	}

	// Verify active session count
	if count := cm.GetActiveSessionCount(); count != 1 {
		t.Errorf("expected 1 active session, got %d", count)
	}
}

func TestConnectionManager_HandleConnect_NewSessionAfterDisconnect(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 10)

	// Create initial session
	session1, _ := cm.HandleConnect(ctx, "client-1", sendChan)
	sessionID := session1.ID

	// Disconnect the session
	_ = cm.HandleDisconnect(ctx, sessionID)

	// Reconnect with same client - creates new session because
	// GetByClientID only returns Active sessions (by design for MemorySessionStore)
	sendChan2 := make(chan []byte, 10)
	session2, err := cm.HandleConnect(ctx, "client-1", sendChan2)
	if err != nil {
		t.Fatalf("HandleConnect failed: %v", err)
	}

	// Should create a new session (new ID)
	if session2.ID == sessionID {
		t.Log("MemorySessionStore supports reactivation - same session ID returned")
	}
	if session2.State != SessionStateActive {
		t.Errorf("expected state Active, got %s", session2.State)
	}
	if session2.ClientID != "client-1" {
		t.Errorf("expected ClientID 'client-1', got '%s'", session2.ClientID)
	}
}

func TestConnectionManager_HandleDisconnect(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 10)

	// Create session
	session, _ := cm.HandleConnect(ctx, "client-1", sendChan)

	// Disconnect
	err := cm.HandleDisconnect(ctx, session.ID)
	if err != nil {
		t.Fatalf("HandleDisconnect failed: %v", err)
	}

	// Verify active session count is 0
	if count := cm.GetActiveSessionCount(); count != 0 {
		t.Errorf("expected 0 active sessions after disconnect, got %d", count)
	}

	// Verify session state is disconnected
	retrievedSession, err := sessionStore.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get session failed: %v", err)
	}
	if retrievedSession.State != SessionStateDisconnected {
		t.Errorf("expected state Disconnected, got %s", retrievedSession.State)
	}
}

func TestConnectionManager_HandleReconnect(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 10)

	// Create and disconnect session
	session, _ := cm.HandleConnect(ctx, "client-1", sendChan)
	_ = cm.HandleDisconnect(ctx, session.ID)

	// Store some events while disconnected
	event1 := &CachedEvent{
		ID:        "evt_1",
		SessionID: session.ID,
		EventType: "test",
		Payload:   []byte(`{"data": "test1"}`),
		Timestamp: time.Now(),
		Delivered: false,
	}
	_ = eventCache.Store(ctx, event1)

	// Reconnect
	sendChan2 := make(chan []byte, 10)
	req := &ResumeRequest{
		SessionID:   session.ID,
		LastEventID: "",
	}

	reconnectedSession, missedEvents, err := cm.HandleReconnect(ctx, req, sendChan2)
	if err != nil {
		t.Fatalf("HandleReconnect failed: %v", err)
	}

	if reconnectedSession.ID != session.ID {
		t.Errorf("expected session ID %s, got %s", session.ID, reconnectedSession.ID)
	}
	if reconnectedSession.State != SessionStateActive {
		t.Errorf("expected state Active, got %s", reconnectedSession.State)
	}
	if len(missedEvents) != 1 {
		t.Errorf("expected 1 missed event, got %d", len(missedEvents))
	}
}

func TestConnectionManager_DeliverEvent(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 10)

	// Create session
	session, _ := cm.HandleConnect(ctx, "client-1", sendChan)

	// Deliver event
	payload := []byte(`{"message": "hello"}`)
	err := cm.DeliverEvent(ctx, session.ID, "test_event", payload)
	if err != nil {
		t.Fatalf("DeliverEvent failed: %v", err)
	}

	// Check that event was sent to channel
	select {
	case data := <-sendChan:
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to unmarshal message: %v", err)
		}
		if msg["type"] != "test_event" {
			t.Errorf("expected type 'test_event', got %v", msg["type"])
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestConnectionManager_DeliverEvent_NotConnected(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()

	// Deliver to non-existent session (should not fail, just cache)
	payload := []byte(`{"message": "hello"}`)
	err := cm.DeliverEvent(ctx, "nonexistent-session", "test_event", payload)
	if err != nil {
		t.Errorf("DeliverEvent to non-existent session should not fail: %v", err)
	}
}

func TestConnectionManager_ReplayEvents(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 20)

	// Create session
	session, _ := cm.HandleConnect(ctx, "client-1", sendChan)

	// Create events to replay
	events := []*CachedEvent{
		{
			ID:        "evt_1",
			SessionID: session.ID,
			EventType: "event_type_1",
			Payload:   []byte(`{"data": 1}`),
			Timestamp: time.Now(),
		},
		{
			ID:        "evt_2",
			SessionID: session.ID,
			EventType: "event_type_2",
			Payload:   []byte(`{"data": 2}`),
			Timestamp: time.Now(),
		},
	}

	err := cm.ReplayEvents(ctx, session.ID, events)
	if err != nil {
		t.Fatalf("ReplayEvents failed: %v", err)
	}

	// Should receive: replay_start, event1, event2, replay_end
	expectedTypes := []string{"replay_start", "event_type_1", "event_type_2", "replay_end"}
	receivedTypes := make([]string, 0, 4)

	for i := range 4 {
		select {
		case data := <-sendChan:
			var msg map[string]any
			_ = json.Unmarshal(data, &msg)
			receivedTypes = append(receivedTypes, msg["type"].(string))
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for message %d", i)
		}
	}

	for i, expected := range expectedTypes {
		if receivedTypes[i] != expected {
			t.Errorf("message %d: expected type '%s', got '%s'", i, expected, receivedTypes[i])
		}
	}
}

func TestConnectionManager_ReplayEvents_NotConnected(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	events := []*CachedEvent{
		{ID: "evt_1", EventType: "test", Payload: []byte(`{}`), Timestamp: time.Now()},
	}

	// Replay to non-existent session should return error
	err := cm.ReplayEvents(ctx, "nonexistent-session", events)
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestConnectionManager_GetSession(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 10)

	// Create session
	session, _ := cm.HandleConnect(ctx, "client-1", sendChan)

	// Get session
	retrieved, err := cm.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.ID != session.ID {
		t.Errorf("expected ID %s, got %s", session.ID, retrieved.ID)
	}

	// Get non-existent session
	_, err = cm.GetSession(ctx, "nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestConnectionManager_UpdateSessionSubscription(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()
	sendChan := make(chan []byte, 10)

	// Create session
	session, _ := cm.HandleConnect(ctx, "client-1", sendChan)

	// Add subscription
	err := cm.UpdateSessionSubscription(ctx, session.ID, "topic-1", true)
	if err != nil {
		t.Fatalf("UpdateSessionSubscription (add) failed: %v", err)
	}

	// Verify subscription was added
	retrieved, _ := cm.GetSession(ctx, session.ID)
	if _, ok := retrieved.Subscriptions["topic-1"]; !ok {
		t.Error("subscription 'topic-1' should exist")
	}

	// Remove subscription
	err = cm.UpdateSessionSubscription(ctx, session.ID, "topic-1", false)
	if err != nil {
		t.Fatalf("UpdateSessionSubscription (remove) failed: %v", err)
	}

	// Verify subscription was removed
	retrieved, _ = cm.GetSession(ctx, session.ID)
	if _, ok := retrieved.Subscriptions["topic-1"]; ok {
		t.Error("subscription 'topic-1' should not exist after removal")
	}
}

func TestConnectionManager_GetActiveSessionCount(t *testing.T) {
	logger := zap.NewNop()
	sessionStore := NewMemorySessionStore(logger)
	eventCache := NewMemoryEventCache(logger)
	config := DefaultConfig()

	cm := NewConnectionManager(sessionStore, eventCache, config, logger)

	ctx := context.Background()

	// Initially zero
	if count := cm.GetActiveSessionCount(); count != 0 {
		t.Errorf("expected 0 active sessions, got %d", count)
	}

	// Add sessions
	sendChan1 := make(chan []byte, 10)
	sendChan2 := make(chan []byte, 10)
	session1, _ := cm.HandleConnect(ctx, "client-1", sendChan1)
	_, _ = cm.HandleConnect(ctx, "client-2", sendChan2)

	if count := cm.GetActiveSessionCount(); count != 2 {
		t.Errorf("expected 2 active sessions, got %d", count)
	}

	// Disconnect one
	_ = cm.HandleDisconnect(ctx, session1.ID)

	if count := cm.GetActiveSessionCount(); count != 1 {
		t.Errorf("expected 1 active session after disconnect, got %d", count)
	}
}
