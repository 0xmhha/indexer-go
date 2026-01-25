package resilience

import (
	"context"
	"testing"
	"time"
)

func TestNewPebbleEventCache(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)

	if cache == nil {
		t.Fatal("expected cache to not be nil")
	}
}

func TestEventCacheStoreAndGet(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	event := &CachedEvent{
		ID:        "event-1",
		SessionID: "session-1",
		EventType: "test_event",
		Payload:   []byte(`{"message": "hello"}`),
		Timestamp: time.Now(),
	}

	// Store
	if err := cache.Store(ctx, event); err != nil {
		t.Fatalf("failed to store event: %v", err)
	}

	// Get by session
	events, err := cache.GetBySession(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].ID != event.ID {
		t.Errorf("expected event ID %s, got %s", event.ID, events[0].ID)
	}
	if events[0].EventType != event.EventType {
		t.Errorf("expected event type %s, got %s", event.EventType, events[0].EventType)
	}
}

func TestEventCacheStoreGeneratesID(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	event := &CachedEvent{
		SessionID: "session-1",
		EventType: "test_event",
		Payload:   []byte(`{}`),
	}

	// Store without ID
	if err := cache.Store(ctx, event); err != nil {
		t.Fatalf("failed to store event: %v", err)
	}

	// ID should be generated
	if event.ID == "" {
		t.Error("expected event ID to be generated")
	}

	// Timestamp should be set
	if event.Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
}

func TestEventCacheGetBySession(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	// Store events for different sessions
	for i := 0; i < 3; i++ {
		event := &CachedEvent{
			ID:        "event-s1-" + string(rune('a'+i)),
			SessionID: "session-1",
			EventType: "event",
			Payload:   []byte(`{}`),
			Timestamp: time.Now().Add(time.Duration(i) * time.Millisecond),
		}
		if err := cache.Store(ctx, event); err != nil {
			t.Fatalf("failed to store event: %v", err)
		}
	}

	event2 := &CachedEvent{
		ID:        "event-s2-a",
		SessionID: "session-2",
		EventType: "event",
		Payload:   []byte(`{}`),
		Timestamp: time.Now(),
	}
	if err := cache.Store(ctx, event2); err != nil {
		t.Fatalf("failed to store event: %v", err)
	}

	// Get session-1 events
	events, err := cache.GetBySession(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events for session-1, got %d", len(events))
	}

	// Get session-2 events
	events2, err := cache.GetBySession(ctx, "session-2", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(events2) != 1 {
		t.Errorf("expected 1 event for session-2, got %d", len(events2))
	}
}

func TestEventCacheGetBySessionWithLimit(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	// Store 5 events
	for i := 0; i < 5; i++ {
		event := &CachedEvent{
			ID:        "event-" + string(rune('a'+i)),
			SessionID: "session-1",
			EventType: "event",
			Payload:   []byte(`{}`),
			Timestamp: time.Now().Add(time.Duration(i) * time.Millisecond),
		}
		if err := cache.Store(ctx, event); err != nil {
			t.Fatalf("failed to store event: %v", err)
		}
	}

	// Get with limit 3
	events, err := cache.GetBySession(ctx, "session-1", 3)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events with limit, got %d", len(events))
	}
}

func TestEventCacheGetAfter(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	now := time.Now()

	// Store events with sequential timestamps
	events := []CachedEvent{
		{ID: "event-1", SessionID: "session-1", EventType: "event", Timestamp: now},
		{ID: "event-2", SessionID: "session-1", EventType: "event", Timestamp: now.Add(10 * time.Millisecond)},
		{ID: "event-3", SessionID: "session-1", EventType: "event", Timestamp: now.Add(20 * time.Millisecond)},
		{ID: "event-4", SessionID: "session-1", EventType: "event", Timestamp: now.Add(30 * time.Millisecond)},
	}

	for i := range events {
		events[i].Payload = []byte(`{}`)
		if err := cache.Store(ctx, &events[i]); err != nil {
			t.Fatalf("failed to store event: %v", err)
		}
	}

	// Get events after event-2
	afterEvents, err := cache.GetAfter(ctx, "session-1", "event-2", 10)
	if err != nil {
		t.Fatalf("failed to get events after: %v", err)
	}

	if len(afterEvents) != 2 {
		t.Errorf("expected 2 events after event-2, got %d", len(afterEvents))
	}

	// Verify we got event-3 and event-4
	expectedIDs := map[string]bool{"event-3": true, "event-4": true}
	for _, e := range afterEvents {
		if !expectedIDs[e.ID] {
			t.Errorf("unexpected event ID: %s", e.ID)
		}
	}
}

func TestEventCacheGetAfterEmpty(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	// Get from empty cache
	events, err := cache.GetAfter(ctx, "session-1", "", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestEventCacheDeleteBySession(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	// Store events for session-1
	for i := 0; i < 3; i++ {
		event := &CachedEvent{
			ID:        "event-s1-" + string(rune('a'+i)),
			SessionID: "session-1",
			EventType: "event",
			Payload:   []byte(`{}`),
			Timestamp: time.Now(),
		}
		if err := cache.Store(ctx, event); err != nil {
			t.Fatalf("failed to store event: %v", err)
		}
	}

	// Store event for session-2
	event2 := &CachedEvent{
		ID:        "event-s2",
		SessionID: "session-2",
		EventType: "event",
		Payload:   []byte(`{}`),
		Timestamp: time.Now(),
	}
	if err := cache.Store(ctx, event2); err != nil {
		t.Fatalf("failed to store event: %v", err)
	}

	// Delete session-1 events
	if err := cache.DeleteBySession(ctx, "session-1"); err != nil {
		t.Fatalf("failed to delete session events: %v", err)
	}

	// session-1 should have no events
	events, err := cache.GetBySession(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for session-1, got %d", len(events))
	}

	// session-2 should still have events
	events2, err := cache.GetBySession(ctx, "session-2", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}
	if len(events2) != 1 {
		t.Errorf("expected 1 event for session-2, got %d", len(events2))
	}
}

func TestEventCacheCleanup(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	now := time.Now()

	// Store an old event
	oldEvent := &CachedEvent{
		ID:        "old-event",
		SessionID: "session-1",
		EventType: "event",
		Payload:   []byte(`{}`),
		Timestamp: now.Add(-2 * time.Hour),
	}
	if err := cache.Store(ctx, oldEvent); err != nil {
		t.Fatalf("failed to store old event: %v", err)
	}

	// Store a new event
	newEvent := &CachedEvent{
		ID:        "new-event",
		SessionID: "session-1",
		EventType: "event",
		Payload:   []byte(`{}`),
		Timestamp: now,
	}
	if err := cache.Store(ctx, newEvent); err != nil {
		t.Fatalf("failed to store new event: %v", err)
	}

	// Cleanup events older than 1 hour
	count, err := cache.Cleanup(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 event cleaned up, got %d", count)
	}

	// New event should still exist
	events, err := cache.GetBySession(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 event remaining, got %d", len(events))
	}
	if events[0].ID != "new-event" {
		t.Errorf("expected new-event to remain, got %s", events[0].ID)
	}
}

func TestEventCacheEventsSortedByTimestamp(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	now := time.Now()

	// Store events out of order
	events := []CachedEvent{
		{ID: "event-3", SessionID: "session-1", EventType: "event", Timestamp: now.Add(20 * time.Millisecond)},
		{ID: "event-1", SessionID: "session-1", EventType: "event", Timestamp: now},
		{ID: "event-2", SessionID: "session-1", EventType: "event", Timestamp: now.Add(10 * time.Millisecond)},
	}

	for i := range events {
		events[i].Payload = []byte(`{}`)
		if err := cache.Store(ctx, &events[i]); err != nil {
			t.Fatalf("failed to store event: %v", err)
		}
	}

	// Get events - should be sorted by timestamp
	retrieved, err := cache.GetBySession(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(retrieved) != 3 {
		t.Fatalf("expected 3 events, got %d", len(retrieved))
	}

	// Verify order
	expectedOrder := []string{"event-1", "event-2", "event-3"}
	for i, expected := range expectedOrder {
		if retrieved[i].ID != expected {
			t.Errorf("event at position %d: expected %s, got %s", i, expected, retrieved[i].ID)
		}
	}
}

func TestEventCacheMarkDelivered(t *testing.T) {
	kv := newMockKVStore()
	cache := NewPebbleEventCache(kv, nil)
	ctx := context.Background()

	// This is a simplified implementation that doesn't do much
	// Just verify it doesn't error
	err := cache.MarkDelivered(ctx, "any-event-id")
	if err != nil {
		t.Errorf("MarkDelivered should not error: %v", err)
	}
}
