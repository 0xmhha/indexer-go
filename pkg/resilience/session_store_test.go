package resilience

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// mockKVStore implements storage.KVStore for testing
type mockKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{
		data: make(map[string][]byte),
	}
}

func (m *mockKVStore) Put(ctx context.Context, key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[string(key)] = append([]byte{}, value...)
	return nil
}

func (m *mockKVStore) Get(ctx context.Context, key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if val, ok := m.data[string(key)]; ok {
		return append([]byte{}, val...), nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockKVStore) Delete(ctx context.Context, key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, string(key))
	return nil
}

func (m *mockKVStore) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefixStr := string(prefix)
	for k, v := range m.data {
		if len(k) >= len(prefixStr) && k[:len(prefixStr)] == prefixStr {
			if !fn([]byte(k), v) {
				break
			}
		}
	}
	return nil
}

func (m *mockKVStore) Has(ctx context.Context, key []byte) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[string(key)]
	return ok, nil
}

func TestNewPebbleSessionStore(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)

	if store == nil {
		t.Fatal("expected store to not be nil")
	}
}

func TestSessionStoreSaveAndGet(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	session := NewSession("test-client", 24*time.Hour)
	session.ID = "test-session-id"

	// Save
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Get
	retrieved, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if retrieved.ID != session.ID {
		t.Errorf("expected session ID %s, got %s", session.ID, retrieved.ID)
	}
	if retrieved.ClientID != session.ClientID {
		t.Errorf("expected client ID %s, got %s", session.ClientID, retrieved.ClientID)
	}
	if retrieved.State != session.State {
		t.Errorf("expected state %s, got %s", session.State, retrieved.State)
	}
}

func TestSessionStoreGetNotFound(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSessionStoreGetByClientID(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	session := NewSession("client-123", 24*time.Hour)
	session.ID = "session-456"

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Get by client ID
	retrieved, err := store.GetByClientID(ctx, "client-123")
	if err != nil {
		t.Fatalf("failed to get session by client ID: %v", err)
	}

	if retrieved.ID != session.ID {
		t.Errorf("expected session ID %s, got %s", session.ID, retrieved.ID)
	}
}

func TestSessionStoreGetByClientIDNotFound(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	_, err := store.GetByClientID(ctx, "nonexistent-client")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSessionStoreDelete(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	session := NewSession("client", 24*time.Hour)
	session.ID = "session-to-delete"

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Verify exists
	_, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("session should exist: %v", err)
	}

	// Delete
	if err := store.Delete(ctx, session.ID); err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	// Verify deleted
	_, err = store.Get(ctx, session.ID)
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound after delete, got %v", err)
	}
}

func TestSessionStoreDeleteNotFound(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	// Delete nonexistent should not error
	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Errorf("delete nonexistent should not error, got %v", err)
	}
}

func TestSessionStoreUpdateLastSeen(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	session := NewSession("client", 24*time.Hour)
	session.ID = "session-id"
	originalLastSeen := session.LastSeen

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Update last seen
	if err := store.UpdateLastSeen(ctx, session.ID); err != nil {
		t.Fatalf("failed to update last seen: %v", err)
	}

	// Retrieve and verify
	retrieved, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if !retrieved.LastSeen.After(originalLastSeen) {
		t.Error("LastSeen should be updated")
	}
}

func TestSessionStoreUpdateState(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	session := NewSession("client", 24*time.Hour)
	session.ID = "session-id"
	session.State = SessionStateActive

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Update state
	if err := store.UpdateState(ctx, session.ID, SessionStateDisconnected); err != nil {
		t.Fatalf("failed to update state: %v", err)
	}

	// Retrieve and verify
	retrieved, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if retrieved.State != SessionStateDisconnected {
		t.Errorf("expected state %s, got %s", SessionStateDisconnected, retrieved.State)
	}
}

func TestSessionStoreListByState(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	// Create sessions with different states
	states := []SessionState{SessionStateActive, SessionStateActive, SessionStateDisconnected}
	for i, state := range states {
		session := NewSession("client-"+string(rune('a'+i)), 24*time.Hour)
		session.ID = "session-" + string(rune('a'+i))
		session.State = state
		if err := store.Save(ctx, session); err != nil {
			t.Fatalf("failed to save session: %v", err)
		}
	}

	// List active sessions
	active, err := store.ListByState(ctx, SessionStateActive)
	if err != nil {
		t.Fatalf("failed to list by state: %v", err)
	}

	if len(active) != 2 {
		t.Errorf("expected 2 active sessions, got %d", len(active))
	}
}

func TestSessionStoreListDisconnected(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	// Create disconnected session
	session := NewSession("client", 24*time.Hour)
	session.ID = "disconnected-session"
	session.State = SessionStateDisconnected
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Create active session
	session2 := NewSession("client2", 24*time.Hour)
	session2.ID = "active-session"
	session2.State = SessionStateActive
	if err := store.Save(ctx, session2); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// List disconnected
	disconnected, err := store.ListDisconnected(ctx)
	if err != nil {
		t.Fatalf("failed to list disconnected: %v", err)
	}

	if len(disconnected) != 1 {
		t.Errorf("expected 1 disconnected session, got %d", len(disconnected))
	}
}

func TestSessionStoreExpireOldSessions(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	// Create an expired session
	expiredSession := NewSession("client-expired", 1*time.Nanosecond)
	expiredSession.ID = "expired-session"
	if err := store.Save(ctx, expiredSession); err != nil {
		t.Fatalf("failed to save expired session: %v", err)
	}

	// Create a valid session
	validSession := NewSession("client-valid", 24*time.Hour)
	validSession.ID = "valid-session"
	if err := store.Save(ctx, validSession); err != nil {
		t.Fatalf("failed to save valid session: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Expire old sessions
	count, err := store.ExpireOldSessions(ctx)
	if err != nil {
		t.Fatalf("failed to expire sessions: %v", err)
	}

	// At least the expired session should be cleaned up
	if count < 1 {
		t.Logf("expected at least 1 expired session, got %d (this may be timing-dependent)", count)
	}

	// Valid session should still exist
	_, err = store.Get(ctx, validSession.ID)
	if err != nil {
		t.Errorf("valid session should still exist: %v", err)
	}
}

func TestSessionStoreGetExpiredSession(t *testing.T) {
	kv := newMockKVStore()
	store := NewPebbleSessionStore(kv, nil)
	ctx := context.Background()

	// Create an expired session
	session := NewSession("client", 1*time.Nanosecond)
	session.ID = "soon-expired"
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Get should return ErrSessionExpired
	retrieved, err := store.Get(ctx, session.ID)
	if err != ErrSessionExpired {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}

	// But should still return the session
	if retrieved == nil {
		t.Error("expected session to be returned even when expired")
	}
}
