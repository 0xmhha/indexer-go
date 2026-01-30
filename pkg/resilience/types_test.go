package resilience

import (
	"testing"
	"time"
)

func TestSession_AddSubscription(t *testing.T) {
	session := NewSession("client-1", time.Hour)

	// Initially no subscriptions
	if len(session.Subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(session.Subscriptions))
	}

	// Add subscription
	session.AddSubscription("topic-1")

	if len(session.Subscriptions) != 1 {
		t.Errorf("expected 1 subscription, got %d", len(session.Subscriptions))
	}

	sub, ok := session.Subscriptions["topic-1"]
	if !ok {
		t.Fatal("subscription 'topic-1' not found")
	}

	if sub.Topic != "topic-1" {
		t.Errorf("expected topic 'topic-1', got '%s'", sub.Topic)
	}
	if !sub.Active {
		t.Error("subscription should be active")
	}
	if sub.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	// Add another subscription
	session.AddSubscription("topic-2")

	if len(session.Subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(session.Subscriptions))
	}

	// Overwrite existing subscription
	session.AddSubscription("topic-1")
	if len(session.Subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions after overwrite, got %d", len(session.Subscriptions))
	}
}

func TestSession_RemoveSubscription(t *testing.T) {
	session := NewSession("client-1", time.Hour)

	// Add subscriptions
	session.AddSubscription("topic-1")
	session.AddSubscription("topic-2")

	if len(session.Subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(session.Subscriptions))
	}

	// Remove subscription
	session.RemoveSubscription("topic-1")

	if len(session.Subscriptions) != 1 {
		t.Errorf("expected 1 subscription after removal, got %d", len(session.Subscriptions))
	}

	if _, ok := session.Subscriptions["topic-1"]; ok {
		t.Error("subscription 'topic-1' should be removed")
	}
	if _, ok := session.Subscriptions["topic-2"]; !ok {
		t.Error("subscription 'topic-2' should still exist")
	}

	// Remove non-existent subscription (should not fail)
	session.RemoveSubscription("nonexistent")
	if len(session.Subscriptions) != 1 {
		t.Errorf("expected 1 subscription after removing nonexistent, got %d", len(session.Subscriptions))
	}

	// Remove last subscription
	session.RemoveSubscription("topic-2")
	if len(session.Subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions after removing all, got %d", len(session.Subscriptions))
	}
}

func TestSession_UpdateLastEvent(t *testing.T) {
	session := NewSession("client-1", time.Hour)

	// Add subscription
	session.AddSubscription("topic-1")

	// Update last event
	session.UpdateLastEvent("evt_123", "topic-1")

	// Check session-level last event
	if session.LastEventID != "evt_123" {
		t.Errorf("expected LastEventID 'evt_123', got '%s'", session.LastEventID)
	}

	// Check subscription-level last event
	sub := session.Subscriptions["topic-1"]
	if sub.LastEventID != "evt_123" {
		t.Errorf("expected subscription LastEventID 'evt_123', got '%s'", sub.LastEventID)
	}

	// Update with different event and topic
	session.AddSubscription("topic-2")
	session.UpdateLastEvent("evt_456", "topic-2")

	if session.LastEventID != "evt_456" {
		t.Errorf("expected LastEventID 'evt_456', got '%s'", session.LastEventID)
	}

	// topic-1 should still have its last event
	if session.Subscriptions["topic-1"].LastEventID != "evt_123" {
		t.Errorf("topic-1 LastEventID should still be 'evt_123', got '%s'", session.Subscriptions["topic-1"].LastEventID)
	}

	// topic-2 should have the new event
	if session.Subscriptions["topic-2"].LastEventID != "evt_456" {
		t.Errorf("topic-2 LastEventID should be 'evt_456', got '%s'", session.Subscriptions["topic-2"].LastEventID)
	}

	// Update for non-existent topic (should only update session-level)
	session.UpdateLastEvent("evt_789", "nonexistent")
	if session.LastEventID != "evt_789" {
		t.Errorf("expected LastEventID 'evt_789', got '%s'", session.LastEventID)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.SessionTTL != 24*time.Hour {
		t.Errorf("expected SessionTTL 24h, got %v", config.SessionTTL)
	}
	if config.SessionCleanupPeriod != time.Hour {
		t.Errorf("expected SessionCleanupPeriod 1h, got %v", config.SessionCleanupPeriod)
	}
	if config.CacheWindow != time.Hour {
		t.Errorf("expected CacheWindow 1h, got %v", config.CacheWindow)
	}
	if config.MaxEventsPerSession != 1000 {
		t.Errorf("expected MaxEventsPerSession 1000, got %d", config.MaxEventsPerSession)
	}
	if config.Backend != "pebble" {
		t.Errorf("expected Backend 'pebble', got '%s'", config.Backend)
	}
}

func TestNewSession(t *testing.T) {
	session := NewSession("client-123", 2*time.Hour)

	if session.ClientID != "client-123" {
		t.Errorf("expected ClientID 'client-123', got '%s'", session.ClientID)
	}
	if session.State != SessionStateActive {
		t.Errorf("expected state Active, got %s", session.State)
	}
	if session.TTL != 2*time.Hour {
		t.Errorf("expected TTL 2h, got %v", session.TTL)
	}
	if session.Subscriptions == nil {
		t.Error("Subscriptions should be initialized")
	}
	if session.Metadata == nil {
		t.Error("Metadata should be initialized")
	}
	if session.ID == "" {
		t.Error("ID should be generated")
	}
	if !hasPrefix(session.ID, "sess_") {
		t.Errorf("ID should start with 'sess_', got '%s'", session.ID)
	}
	if session.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if session.LastSeen.IsZero() {
		t.Error("LastSeen should be set")
	}
}

func TestSession_IsExpired(t *testing.T) {
	// Create session with very short TTL
	session := NewSession("client-1", 10*time.Millisecond)

	// Initially not expired
	if session.IsExpired() {
		t.Error("session should not be expired immediately")
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	if !session.IsExpired() {
		t.Error("session should be expired after TTL")
	}
}

func TestSession_Touch(t *testing.T) {
	session := NewSession("client-1", time.Hour)
	originalLastSeen := session.LastSeen

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Touch
	session.Touch()

	if !session.LastSeen.After(originalLastSeen) {
		t.Error("LastSeen should be updated after Touch()")
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()

	if id1 == "" {
		t.Error("generateSessionID should not return empty string")
	}
	if !hasPrefix(id1, "sess_") {
		t.Errorf("session ID should start with 'sess_', got '%s'", id1)
	}

	// Verify multiple IDs have correct format
	for range 10 {
		id := generateSessionID()
		if !hasPrefix(id, "sess_") {
			t.Errorf("session ID should start with 'sess_', got '%s'", id)
		}
	}
}

func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID()

	if id1 == "" {
		t.Error("generateEventID should not return empty string")
	}
	if !hasPrefix(id1, "evt_") {
		t.Errorf("event ID should start with 'evt_', got '%s'", id1)
	}

	// Verify multiple IDs have correct format
	for range 10 {
		id := generateEventID()
		if !hasPrefix(id, "evt_") {
			t.Errorf("event ID should start with 'evt_', got '%s'", id)
		}
	}
}

func TestGenerateID(t *testing.T) {
	id := generateID()

	if len(id) != 12 {
		t.Errorf("expected ID length 12, got %d", len(id))
	}

	// Should only contain lowercase letters and digits
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			t.Errorf("ID contains invalid character: %c", c)
		}
	}
}

func TestSessionState_Constants(t *testing.T) {
	if SessionStateActive != "active" {
		t.Errorf("expected 'active', got '%s'", SessionStateActive)
	}
	if SessionStateDisconnected != "disconnected" {
		t.Errorf("expected 'disconnected', got '%s'", SessionStateDisconnected)
	}
	if SessionStateExpired != "expired" {
		t.Errorf("expected 'expired', got '%s'", SessionStateExpired)
	}
}

// hasPrefix checks if a string has the given prefix
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
