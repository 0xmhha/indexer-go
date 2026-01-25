package resilience

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MemorySessionStore is an in-memory implementation of SessionStore for testing
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logger   *zap.Logger
}

// NewMemorySessionStore creates a new in-memory session store
func NewMemorySessionStore(logger *zap.Logger) *MemorySessionStore {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MemorySessionStore{
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// Save stores a session
func (m *MemorySessionStore) Save(ctx context.Context, session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep copy to avoid external modification
	copy := *session
	if session.Subscriptions != nil {
		copy.Subscriptions = make(map[string]*SubState)
		for k, v := range session.Subscriptions {
			subCopy := *v
			copy.Subscriptions[k] = &subCopy
		}
	}
	m.sessions[session.ID] = &copy
	return nil
}

// Get retrieves a session by ID
func (m *MemorySessionStore) Get(ctx context.Context, sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	// Return a copy
	copy := *session
	return &copy, nil
}

// GetByClientID retrieves a session by client ID
func (m *MemorySessionStore) GetByClientID(ctx context.Context, clientID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		if session.ClientID == clientID && session.State == SessionStateActive {
			copy := *session
			return &copy, nil
		}
	}
	return nil, ErrSessionNotFound
}

// Delete removes a session
func (m *MemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, sessionID)
	return nil
}

// UpdateLastSeen updates the last seen timestamp
func (m *MemorySessionStore) UpdateLastSeen(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	session.LastSeen = time.Now()
	return nil
}

// UpdateState updates the session state
func (m *MemorySessionStore) UpdateState(ctx context.Context, sessionID string, state SessionState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	session.State = state
	return nil
}

// ListByState returns sessions with the given state
func (m *MemorySessionStore) ListByState(ctx context.Context, state SessionState) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Session
	for _, session := range m.sessions {
		if session.State == state {
			copy := *session
			result = append(result, &copy)
		}
	}
	return result, nil
}

// ListDisconnected returns all disconnected sessions
func (m *MemorySessionStore) ListDisconnected(ctx context.Context) ([]*Session, error) {
	return m.ListByState(ctx, SessionStateDisconnected)
}

// ExpireOldSessions expires sessions that have exceeded their TTL
func (m *MemorySessionStore) ExpireOldSessions(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	expired := 0

	for _, session := range m.sessions {
		if session.State != SessionStateExpired &&
		   now.Sub(session.LastSeen) > session.TTL {
			session.State = SessionStateExpired
			expired++
		}
	}

	return expired, nil
}

// MemoryEventCache is an in-memory implementation of EventCache for testing
type MemoryEventCache struct {
	mu     sync.RWMutex
	events map[string]*CachedEvent // keyed by event ID
	logger *zap.Logger
}

// NewMemoryEventCache creates a new in-memory event cache
func NewMemoryEventCache(logger *zap.Logger) *MemoryEventCache {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MemoryEventCache{
		events: make(map[string]*CachedEvent),
		logger: logger,
	}
}

// Store stores an event in the cache
func (m *MemoryEventCache) Store(ctx context.Context, event *CachedEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	copy := *event
	m.events[event.ID] = &copy
	return nil
}

// GetAfter returns events after the specified event ID for a session
func (m *MemoryEventCache) GetAfter(ctx context.Context, sessionID string, afterEventID string, limit int) ([]*CachedEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find the target event to get its timestamp
	var afterTime time.Time
	if afterEventID != "" {
		if afterEvent, ok := m.events[afterEventID]; ok {
			afterTime = afterEvent.Timestamp
		}
	}

	var result []*CachedEvent
	for _, event := range m.events {
		if event.SessionID == sessionID && event.Timestamp.After(afterTime) {
			copy := *event
			result = append(result, &copy)
		}
	}

	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	// Apply limit
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// GetBySession returns events for a session
func (m *MemoryEventCache) GetBySession(ctx context.Context, sessionID string, limit int) ([]*CachedEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*CachedEvent
	for _, event := range m.events {
		if event.SessionID == sessionID {
			copy := *event
			result = append(result, &copy)
		}
	}

	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	// Apply limit
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// MarkDelivered marks an event as delivered
func (m *MemoryEventCache) MarkDelivered(ctx context.Context, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if event, ok := m.events[eventID]; ok {
		event.Delivered = true
	}
	return nil
}

// Cleanup removes old events
func (m *MemoryEventCache) Cleanup(ctx context.Context, olderThan time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	threshold := time.Now().Add(-olderThan)
	cleaned := 0

	for id, event := range m.events {
		if event.Timestamp.Before(threshold) {
			delete(m.events, id)
			cleaned++
		}
	}

	return cleaned, nil
}

// DeleteBySession removes all events for a session
func (m *MemoryEventCache) DeleteBySession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, event := range m.events {
		if event.SessionID == sessionID {
			delete(m.events, id)
		}
	}

	return nil
}
