package resilience

import (
	"time"
)

// SessionState represents the state of a WebSocket session
type SessionState string

const (
	SessionStateActive       SessionState = "active"
	SessionStateDisconnected SessionState = "disconnected"
	SessionStateExpired      SessionState = "expired"
)

// Session represents a WebSocket session with subscription state
type Session struct {
	ID            string                  `json:"id"`
	ClientID      string                  `json:"clientId"`
	State         SessionState            `json:"state"`
	Subscriptions map[string]*SubState    `json:"subscriptions"`
	LastEventID   string                  `json:"lastEventId"`
	LastSeen      time.Time               `json:"lastSeen"`
	CreatedAt     time.Time               `json:"createdAt"`
	TTL           time.Duration           `json:"ttl"`
	Metadata      map[string]string       `json:"metadata,omitempty"`
}

// SubState represents the state of a subscription
type SubState struct {
	Topic       string    `json:"topic"`
	LastEventID string    `json:"lastEventId"`
	CreatedAt   time.Time `json:"createdAt"`
	Active      bool      `json:"active"`
}

// CachedEvent represents an event cached for replay
type CachedEvent struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"sessionId"`
	EventType   string    `json:"eventType"`
	Payload     []byte    `json:"payload"`
	Timestamp   time.Time `json:"timestamp"`
	Delivered   bool      `json:"delivered"`
	Topic       string    `json:"topic,omitempty"`
}

// ResumeRequest represents a client request to resume a session
type ResumeRequest struct {
	SessionID   string `json:"sessionId"`
	LastEventID string `json:"lastEventId,omitempty"`
}

// ResumeResponse represents a server response to a resume request
type ResumeResponse struct {
	SessionID    string `json:"sessionId"`
	MissedEvents int    `json:"missedEvents"`
	Status       string `json:"status"`
}

// EventMeta contains metadata for events during replay
type EventMeta struct {
	EventID string `json:"eventId"`
	Replay  bool   `json:"replay,omitempty"`
}

// Config holds configuration for the resilience system
type Config struct {
	// Session configuration
	SessionTTL           time.Duration `yaml:"session_ttl"`
	SessionCleanupPeriod time.Duration `yaml:"cleanup_period"`

	// Event cache configuration
	CacheWindow    time.Duration `yaml:"cache_window"`
	MaxEventsPerSession int       `yaml:"max_events_per_session"`

	// Storage backend
	Backend string `yaml:"backend"` // "pebble" or "redis"
}

// DefaultConfig returns the default resilience configuration
func DefaultConfig() *Config {
	return &Config{
		SessionTTL:           24 * time.Hour,
		SessionCleanupPeriod: time.Hour,
		CacheWindow:          time.Hour,
		MaxEventsPerSession:  1000,
		Backend:              "pebble",
	}
}

// NewSession creates a new session with the given client ID
func NewSession(clientID string, ttl time.Duration) *Session {
	now := time.Now()
	return &Session{
		ID:            generateSessionID(),
		ClientID:      clientID,
		State:         SessionStateActive,
		Subscriptions: make(map[string]*SubState),
		CreatedAt:     now,
		LastSeen:      now,
		TTL:           ttl,
		Metadata:      make(map[string]string),
	}
}

// IsExpired checks if the session has expired
func (s *Session) IsExpired() bool {
	return time.Since(s.LastSeen) > s.TTL
}

// Touch updates the last seen time
func (s *Session) Touch() {
	s.LastSeen = time.Now()
}

// AddSubscription adds a subscription to the session
func (s *Session) AddSubscription(topic string) {
	s.Subscriptions[topic] = &SubState{
		Topic:     topic,
		CreatedAt: time.Now(),
		Active:    true,
	}
}

// RemoveSubscription removes a subscription from the session
func (s *Session) RemoveSubscription(topic string) {
	delete(s.Subscriptions, topic)
}

// UpdateLastEvent updates the last event ID for the session and topic
func (s *Session) UpdateLastEvent(eventID string, topic string) {
	s.LastEventID = eventID
	if sub, ok := s.Subscriptions[topic]; ok {
		sub.LastEventID = eventID
	}
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return "sess_" + generateID()
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return "evt_" + generateID()
}

// generateID generates a random ID using timestamp and random suffix
func generateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	now := time.Now().UnixNano()

	// Use timestamp as base
	result := make([]byte, 12)
	for i := 0; i < 6; i++ {
		result[i] = charset[now%36]
		now /= 36
	}

	// Add random component based on nanoseconds
	nano := time.Now().UnixNano()
	for i := 6; i < 12; i++ {
		result[i] = charset[nano%36]
		nano /= 17 // Different divisor for more randomness
	}

	return string(result)
}
