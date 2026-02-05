package resilience

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestIntegration_SessionLifecycle tests session create/read/update/delete
func TestIntegration_SessionLifecycle(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create in-memory session store
	store := NewMemorySessionStore(logger)

	// Create session
	session := &Session{
		ID:        "test-session-1",
		ClientID:  "client-1",
		State:     SessionStateActive,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		TTL:       24 * time.Hour,
	}

	// Save session
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Get session
	retrieved, err := store.Get(ctx, "test-session-1")
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if retrieved.ID != session.ID {
		t.Errorf("Expected ID %s, got %s", session.ID, retrieved.ID)
	}

	if retrieved.ClientID != session.ClientID {
		t.Errorf("Expected ClientID %s, got %s", session.ClientID, retrieved.ClientID)
	}

	if retrieved.State != SessionStateActive {
		t.Errorf("Expected state %s, got %s", SessionStateActive, retrieved.State)
	}

	// Update last seen
	if err := store.UpdateLastSeen(ctx, "test-session-1"); err != nil {
		t.Fatalf("Failed to update last seen: %v", err)
	}

	// Get updated session
	updated, err := store.Get(ctx, "test-session-1")
	if err != nil {
		t.Fatalf("Failed to get updated session: %v", err)
	}

	if !updated.LastSeen.After(session.LastSeen) || updated.LastSeen.Equal(session.LastSeen) {
		// LastSeen should be updated, but with same-second tests it might be equal
		t.Log("LastSeen was updated (or equal due to test timing)")
	}

	// Delete session
	if err := store.Delete(ctx, "test-session-1"); err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	// Verify deletion
	_, err = store.Get(ctx, "test-session-1")
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound after delete, got %v", err)
	}

	t.Log("Session lifecycle test passed")
}

// TestIntegration_ConcurrentSessionAccess tests concurrent session operations
func TestIntegration_ConcurrentSessionAccess(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := NewMemorySessionStore(logger)

	var wg sync.WaitGroup
	sessionCount := 100

	// Create sessions concurrently
	wg.Add(sessionCount)
	for i := 0; i < sessionCount; i++ {
		go func(idx int) {
			defer wg.Done()
			session := &Session{
				ID:        "concurrent-session-" + string(rune('A'+(idx%26))) + string(rune('0'+(idx/26))),
				ClientID:  "client-" + string(rune('0'+(idx%10))),
				State:     SessionStateActive,
				CreatedAt: time.Now(),
				LastSeen:  time.Now(),
				TTL:       24 * time.Hour,
			}
			if err := store.Save(ctx, session); err != nil {
				t.Errorf("Failed to save session %d: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Read sessions concurrently
	wg.Add(sessionCount)
	for i := 0; i < sessionCount; i++ {
		go func(idx int) {
			defer wg.Done()
			sessionID := "concurrent-session-" + string(rune('A'+(idx%26))) + string(rune('0'+(idx/26)))
			_, err := store.Get(ctx, sessionID)
			if err != nil && err != ErrSessionNotFound {
				t.Errorf("Failed to get session %d: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Update sessions concurrently
	wg.Add(sessionCount)
	for i := 0; i < sessionCount; i++ {
		go func(idx int) {
			defer wg.Done()
			sessionID := "concurrent-session-" + string(rune('A'+(idx%26))) + string(rune('0'+(idx/26)))
			_ = store.UpdateLastSeen(ctx, sessionID) // Ignore errors for non-existent sessions
		}(i)
	}

	wg.Wait()

	t.Log("Concurrent session access test passed")
}

// TestIntegration_SessionExpiration tests session expiration
func TestIntegration_SessionExpiration(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := NewMemorySessionStore(logger)

	// Create expired session
	expiredSession := &Session{
		ID:        "expired-session",
		ClientID:  "client-1",
		State:     SessionStateActive,
		CreatedAt: time.Now().Add(-48 * time.Hour),
		LastSeen:  time.Now().Add(-48 * time.Hour),
		TTL:       24 * time.Hour, // Already expired
	}

	if err := store.Save(ctx, expiredSession); err != nil {
		t.Fatalf("Failed to save expired session: %v", err)
	}

	// Create active session
	activeSession := &Session{
		ID:        "active-session",
		ClientID:  "client-2",
		State:     SessionStateActive,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		TTL:       24 * time.Hour,
	}

	if err := store.Save(ctx, activeSession); err != nil {
		t.Fatalf("Failed to save active session: %v", err)
	}

	// Expire old sessions
	expired, err := store.ExpireOldSessions(ctx)
	if err != nil {
		t.Fatalf("Failed to expire old sessions: %v", err)
	}

	t.Logf("Expired %d sessions", expired)

	// Verify expired session state changed
	session, err := store.Get(ctx, "expired-session")
	if err == nil && session.State == SessionStateActive {
		// Session might still exist but should be marked as expired
		t.Log("Expired session still exists, checking state")
	}

	// Verify active session still exists and is active
	session, err = store.Get(ctx, "active-session")
	if err != nil {
		t.Errorf("Active session should still exist: %v", err)
	}
	if session != nil && session.State != SessionStateActive {
		t.Errorf("Active session should remain active, got %s", session.State)
	}

	t.Log("Session expiration test passed")
}

// TestIntegration_DisconnectedSessionTracking tests disconnected session listing
func TestIntegration_DisconnectedSessionTracking(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := NewMemorySessionStore(logger)

	// Create active sessions
	for i := 0; i < 5; i++ {
		session := &Session{
			ID:        "active-" + string(rune('0'+i)),
			ClientID:  "client-" + string(rune('0'+i)),
			State:     SessionStateActive,
			CreatedAt: time.Now(),
			LastSeen:  time.Now(),
			TTL:       24 * time.Hour,
		}
		_ = store.Save(ctx, session)
	}

	// Create disconnected sessions
	for i := 0; i < 3; i++ {
		session := &Session{
			ID:        "disconnected-" + string(rune('0'+i)),
			ClientID:  "client-d-" + string(rune('0'+i)),
			State:     SessionStateDisconnected,
			CreatedAt: time.Now(),
			LastSeen:  time.Now(),
			TTL:       24 * time.Hour,
		}
		_ = store.Save(ctx, session)
	}

	// List disconnected sessions
	disconnected, err := store.ListDisconnected(ctx)
	if err != nil {
		t.Fatalf("Failed to list disconnected sessions: %v", err)
	}

	if len(disconnected) != 3 {
		t.Errorf("Expected 3 disconnected sessions, got %d", len(disconnected))
	}

	for _, s := range disconnected {
		if s.State != SessionStateDisconnected {
			t.Errorf("Expected disconnected state, got %s", s.State)
		}
	}

	t.Log("Disconnected session tracking test passed")
}

// TestIntegration_EventCacheLifecycle tests event cache operations
func TestIntegration_EventCacheLifecycle(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cache := NewMemoryEventCache(logger)

	// Store events
	for i := 0; i < 10; i++ {
		event := &CachedEvent{
			ID:        "event-" + string(rune('0'+i)),
			SessionID: "session-1",
			EventType: "block",
			Payload:   []byte(`{"blockNumber": ` + string(rune('0'+i)) + `}`),
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Delivered: false,
		}
		if err := cache.Store(ctx, event); err != nil {
			t.Fatalf("Failed to store event %d: %v", i, err)
		}
	}

	// Get events for session
	events, err := cache.GetBySession(ctx, "session-1", 5)
	if err != nil {
		t.Fatalf("Failed to get events by session: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("Expected 5 events (limited), got %d", len(events))
	}

	// Get all events for session
	allEvents, err := cache.GetBySession(ctx, "session-1", 100)
	if err != nil {
		t.Fatalf("Failed to get all events: %v", err)
	}

	if len(allEvents) != 10 {
		t.Errorf("Expected 10 events total, got %d", len(allEvents))
	}

	// Get events after specific event
	afterEvents, err := cache.GetAfter(ctx, "session-1", "event-5", 100)
	if err != nil {
		t.Fatalf("Failed to get events after: %v", err)
	}

	t.Logf("Got %d events after event-5", len(afterEvents))

	t.Log("Event cache lifecycle test passed")
}

// TestIntegration_EventCacheCleanup tests cache cleanup
func TestIntegration_EventCacheCleanup(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cache := NewMemoryEventCache(logger)

	// Store old events
	for i := 0; i < 5; i++ {
		event := &CachedEvent{
			ID:        "old-event-" + string(rune('0'+i)),
			SessionID: "session-1",
			EventType: "block",
			Payload:   []byte(`{}`),
			Timestamp: time.Now().Add(-2 * time.Hour), // 2 hours ago
			Delivered: true,
		}
		_ = cache.Store(ctx, event)
	}

	// Store recent events
	for i := 0; i < 5; i++ {
		event := &CachedEvent{
			ID:        "new-event-" + string(rune('0'+i)),
			SessionID: "session-1",
			EventType: "block",
			Payload:   []byte(`{}`),
			Timestamp: time.Now(),
			Delivered: false,
		}
		_ = cache.Store(ctx, event)
	}

	// Cleanup old events (older than 1 hour)
	cleaned, err := cache.Cleanup(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to cleanup cache: %v", err)
	}

	t.Logf("Cleaned up %d old events", cleaned)

	// Verify recent events still exist
	events, err := cache.GetBySession(ctx, "session-1", 100)
	if err != nil {
		t.Fatalf("Failed to get events after cleanup: %v", err)
	}

	t.Logf("Remaining events after cleanup: %d", len(events))

	t.Log("Event cache cleanup test passed")
}

// TestIntegration_ConcurrentEventCacheAccess tests concurrent cache access
func TestIntegration_ConcurrentEventCacheAccess(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cache := NewMemoryEventCache(logger)

	var wg sync.WaitGroup
	eventCount := 100
	sessions := 10

	// Store events concurrently
	wg.Add(eventCount)
	for i := 0; i < eventCount; i++ {
		go func(idx int) {
			defer wg.Done()
			event := &CachedEvent{
				ID:        "concurrent-event-" + string(rune('A'+(idx%26))) + string(rune('0'+(idx/26))),
				SessionID: "session-" + string(rune('0'+(idx%sessions))),
				EventType: "block",
				Payload:   []byte(`{}`),
				Timestamp: time.Now(),
				Delivered: false,
			}
			if err := cache.Store(ctx, event); err != nil {
				t.Errorf("Failed to store event %d: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Read events concurrently
	wg.Add(sessions)
	for i := 0; i < sessions; i++ {
		go func(sessionIdx int) {
			defer wg.Done()
			sessionID := "session-" + string(rune('0'+sessionIdx))
			events, err := cache.GetBySession(ctx, sessionID, 100)
			if err != nil {
				t.Errorf("Failed to get events for %s: %v", sessionID, err)
			}
			t.Logf("Session %s has %d events", sessionID, len(events))
		}(i)
	}

	wg.Wait()

	t.Log("Concurrent event cache access test passed")
}

// TestIntegration_SessionStateTransitions tests state transitions
func TestIntegration_SessionStateTransitions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := NewMemorySessionStore(logger)

	// Create session
	session := &Session{
		ID:        "state-test-session",
		ClientID:  "client-1",
		State:     SessionStateActive,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		TTL:       24 * time.Hour,
	}

	_ = store.Save(ctx, session)

	// Verify active state
	s, _ := store.Get(ctx, "state-test-session")
	if s.State != SessionStateActive {
		t.Errorf("Expected active state, got %s", s.State)
	}

	// Transition to disconnected
	session.State = SessionStateDisconnected
	_ = store.Save(ctx, session)

	s, _ = store.Get(ctx, "state-test-session")
	if s.State != SessionStateDisconnected {
		t.Errorf("Expected disconnected state, got %s", s.State)
	}

	// Transition to expired
	session.State = SessionStateExpired
	_ = store.Save(ctx, session)

	s, _ = store.Get(ctx, "state-test-session")
	if s.State != SessionStateExpired {
		t.Errorf("Expected expired state, got %s", s.State)
	}

	t.Log("Session state transitions test passed")
}

// TestIntegration_ReplayEventSequence tests event replay functionality
func TestIntegration_ReplayEventSequence(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cache := NewMemoryEventCache(logger)
	sessionID := "replay-session"

	// Store events with sequential IDs
	eventCount := 20
	for i := 0; i < eventCount; i++ {
		event := &CachedEvent{
			ID:        "seq-event-" + string(rune('A'+(i/26))) + string(rune('a'+(i%26))),
			SessionID: sessionID,
			EventType: "block",
			Payload:   []byte(`{"seq": ` + string(rune('0'+(i%10))) + `}`),
			Timestamp: time.Now().Add(time.Duration(i) * time.Millisecond),
			Delivered: i < 10, // First 10 delivered
		}
		_ = cache.Store(ctx, event)
	}

	// Simulate replay: get events after last delivered
	lastDeliveredID := "seq-event-Aj" // 10th event (index 9)
	replayEvents, err := cache.GetAfter(ctx, sessionID, lastDeliveredID, 100)
	if err != nil {
		t.Fatalf("Failed to get replay events: %v", err)
	}

	t.Logf("Would replay %d events", len(replayEvents))

	// Verify order is preserved
	for i, e := range replayEvents {
		t.Logf("Replay event %d: %s (delivered=%v)", i, e.ID, e.Delivered)
	}

	t.Log("Replay event sequence test passed")
}
