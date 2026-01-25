package resilience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Storage key prefixes for resilience data
const (
	prefixSession      = "/rs/session/"
	prefixSessionClient = "/rs/session/idx/client/"
	prefixSessionState = "/rs/session/idx/state/"
)

// Errors
var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session has expired")
)

// SessionStore defines the interface for session persistence
type SessionStore interface {
	// Save persists a session
	Save(ctx context.Context, session *Session) error

	// Get retrieves a session by ID
	Get(ctx context.Context, sessionID string) (*Session, error)

	// GetByClientID retrieves a session by client ID
	GetByClientID(ctx context.Context, clientID string) (*Session, error)

	// Delete removes a session
	Delete(ctx context.Context, sessionID string) error

	// UpdateLastSeen updates the last seen timestamp
	UpdateLastSeen(ctx context.Context, sessionID string) error

	// UpdateState updates the session state
	UpdateState(ctx context.Context, sessionID string, state SessionState) error

	// ListByState returns sessions with a specific state
	ListByState(ctx context.Context, state SessionState) ([]*Session, error)

	// ListDisconnected returns all disconnected sessions
	ListDisconnected(ctx context.Context) ([]*Session, error)

	// ExpireOldSessions removes sessions older than their TTL
	ExpireOldSessions(ctx context.Context) (int, error)
}

// PebbleSessionStore implements SessionStore using PebbleDB via storage.KVStore
type PebbleSessionStore struct {
	kv     storage.KVStore
	config *Config
}

// NewPebbleSessionStore creates a new PebbleDB-backed session store
func NewPebbleSessionStore(kv storage.KVStore, config *Config) *PebbleSessionStore {
	if config == nil {
		config = DefaultConfig()
	}
	return &PebbleSessionStore{
		kv:     kv,
		config: config,
	}
}

// sessionKey returns the key for storing a session
func sessionKey(sessionID string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixSession, sessionID))
}

// sessionClientIndexKey returns the index key for client -> session lookup
func sessionClientIndexKey(clientID string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixSessionClient, clientID))
}

// sessionStateIndexKey returns the index key for state -> session lookup
func sessionStateIndexKey(state SessionState, sessionID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixSessionState, state, sessionID))
}

// Save persists a session
func (s *PebbleSessionStore) Save(ctx context.Context, session *Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Store main session record
	if err := s.kv.Put(ctx, sessionKey(session.ID), data); err != nil {
		return fmt.Errorf("failed to store session: %w", err)
	}

	// Store client index
	if err := s.kv.Put(ctx, sessionClientIndexKey(session.ClientID), []byte(session.ID)); err != nil {
		return fmt.Errorf("failed to store client index: %w", err)
	}

	// Store state index
	if err := s.kv.Put(ctx, sessionStateIndexKey(session.State, session.ID), []byte(session.ID)); err != nil {
		return fmt.Errorf("failed to store state index: %w", err)
	}

	return nil
}

// Get retrieves a session by ID
func (s *PebbleSessionStore) Get(ctx context.Context, sessionID string) (*Session, error) {
	data, err := s.kv.Get(ctx, sessionKey(sessionID))
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	// Check if expired
	if session.IsExpired() {
		return &session, ErrSessionExpired
	}

	return &session, nil
}

// GetByClientID retrieves a session by client ID
func (s *PebbleSessionStore) GetByClientID(ctx context.Context, clientID string) (*Session, error) {
	sessionIDBytes, err := s.kv.Get(ctx, sessionClientIndexKey(clientID))
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("failed to lookup client session: %w", err)
	}

	return s.Get(ctx, string(sessionIDBytes))
}

// Delete removes a session
func (s *PebbleSessionStore) Delete(ctx context.Context, sessionID string) error {
	// Get session first to clean up indexes
	session, err := s.Get(ctx, sessionID)
	if err != nil && err != ErrSessionExpired {
		if err == ErrSessionNotFound {
			return nil // Already deleted
		}
		return err
	}

	// Delete main record
	if err := s.kv.Delete(ctx, sessionKey(sessionID)); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	if session != nil {
		// Delete client index
		if err := s.kv.Delete(ctx, sessionClientIndexKey(session.ClientID)); err != nil {
			// Log but don't fail
		}

		// Delete state index
		if err := s.kv.Delete(ctx, sessionStateIndexKey(session.State, sessionID)); err != nil {
			// Log but don't fail
		}
	}

	return nil
}

// UpdateLastSeen updates the last seen timestamp
func (s *PebbleSessionStore) UpdateLastSeen(ctx context.Context, sessionID string) error {
	session, err := s.Get(ctx, sessionID)
	if err != nil && err != ErrSessionExpired {
		return err
	}
	if session == nil {
		return ErrSessionNotFound
	}

	session.Touch()
	return s.Save(ctx, session)
}

// UpdateState updates the session state
func (s *PebbleSessionStore) UpdateState(ctx context.Context, sessionID string, state SessionState) error {
	session, err := s.Get(ctx, sessionID)
	if err != nil && err != ErrSessionExpired {
		return err
	}
	if session == nil {
		return ErrSessionNotFound
	}

	oldState := session.State

	// Delete old state index
	if err := s.kv.Delete(ctx, sessionStateIndexKey(oldState, sessionID)); err != nil {
		// Log but continue
	}

	session.State = state
	return s.Save(ctx, session)
}

// ListByState returns sessions with a specific state
func (s *PebbleSessionStore) ListByState(ctx context.Context, state SessionState) ([]*Session, error) {
	prefix := []byte(fmt.Sprintf("%s%s/", prefixSessionState, state))
	var sessions []*Session

	err := s.kv.Iterate(ctx, prefix, func(key, value []byte) bool {
		sessionID := string(value)
		session, err := s.Get(ctx, sessionID)
		if err == nil && session != nil {
			sessions = append(sessions, session)
		}
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	return sessions, nil
}

// ListDisconnected returns all disconnected sessions
func (s *PebbleSessionStore) ListDisconnected(ctx context.Context) ([]*Session, error) {
	return s.ListByState(ctx, SessionStateDisconnected)
}

// ExpireOldSessions removes sessions older than their TTL
func (s *PebbleSessionStore) ExpireOldSessions(ctx context.Context) (int, error) {
	prefix := []byte(prefixSession)
	var expiredCount int
	var toDelete []string

	// First pass: identify expired sessions
	err := s.kv.Iterate(ctx, prefix, func(key, value []byte) bool {
		// Skip index entries
		keyStr := string(key)
		if len(keyStr) > len(prefixSession) && keyStr[len(prefixSession):len(prefixSession)+3] == "idx" {
			return true
		}

		var session Session
		if err := json.Unmarshal(value, &session); err != nil {
			return true
		}

		if session.IsExpired() {
			toDelete = append(toDelete, session.ID)
		}
		return true
	})

	if err != nil {
		return 0, fmt.Errorf("failed to scan sessions: %w", err)
	}

	// Second pass: delete expired sessions
	for _, sessionID := range toDelete {
		if err := s.Delete(ctx, sessionID); err == nil {
			expiredCount++
		}
	}

	return expiredCount, nil
}
