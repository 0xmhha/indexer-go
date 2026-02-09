package resilience

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConnectionManager manages WebSocket session lifecycle with resilience
type ConnectionManager struct {
	sessionStore SessionStore
	eventCache   EventCache
	config       *Config
	logger       *zap.Logger

	// Active sessions mapped by session ID
	activeSessions map[string]*activeSession
	mu             sync.RWMutex

	// Cleanup goroutine control
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// activeSession tracks an active WebSocket connection
type activeSession struct {
	Session   *Session
	Connected bool
	SendChan  chan []byte // Channel to send messages to the client
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(
	sessionStore SessionStore,
	eventCache EventCache,
	config *Config,
	logger *zap.Logger,
) *ConnectionManager {
	if config == nil {
		config = DefaultConfig()
	}

	return &ConnectionManager{
		sessionStore:   sessionStore,
		eventCache:     eventCache,
		config:         config,
		logger:         logger.Named("resilience"),
		activeSessions: make(map[string]*activeSession),
	}
}

// Start starts the connection manager background tasks
func (cm *ConnectionManager) Start(ctx context.Context) error {
	cm.ctx, cm.cancelFunc = context.WithCancel(ctx)

	// Start cleanup goroutine
	go cm.cleanupLoop()

	cm.logger.Info("Connection manager started",
		zap.Duration("session_ttl", cm.config.SessionTTL),
		zap.Duration("cleanup_period", cm.config.SessionCleanupPeriod),
	)

	return nil
}

// Stop stops the connection manager
func (cm *ConnectionManager) Stop(ctx context.Context) error {
	if cm.cancelFunc != nil {
		cm.cancelFunc()
	}

	cm.logger.Info("Connection manager stopped")
	return nil
}

// cleanupLoop periodically cleans up expired sessions and old events
func (cm *ConnectionManager) cleanupLoop() {
	ticker := time.NewTicker(cm.config.SessionCleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			cm.runCleanup()
		}
	}
}

// runCleanup removes expired sessions and old cached events
func (cm *ConnectionManager) runCleanup() {
	ctx, cancel := context.WithTimeout(cm.ctx, 30*time.Second)
	defer cancel()

	// Cleanup expired sessions
	expiredCount, err := cm.sessionStore.ExpireOldSessions(ctx)
	if err != nil {
		cm.logger.Warn("Failed to cleanup expired sessions", zap.Error(err))
	} else if expiredCount > 0 {
		cm.logger.Info("Cleaned up expired sessions", zap.Int("count", expiredCount))
	}

	// Cleanup old cached events
	deletedEvents, err := cm.eventCache.Cleanup(ctx, cm.config.CacheWindow)
	if err != nil {
		cm.logger.Warn("Failed to cleanup old events", zap.Error(err))
	} else if deletedEvents > 0 {
		cm.logger.Info("Cleaned up old events", zap.Int("count", deletedEvents))
	}
}

// HandleConnect handles a new WebSocket connection
// Returns a session for the connection
func (cm *ConnectionManager) HandleConnect(ctx context.Context, clientID string, sendChan chan []byte) (*Session, error) {
	// Check if client has an existing disconnected session
	existingSession, err := cm.sessionStore.GetByClientID(ctx, clientID)
	if err == nil && existingSession != nil && existingSession.State == SessionStateDisconnected {
		// Reactivate existing session
		existingSession.State = SessionStateActive
		existingSession.Touch()

		if err := cm.sessionStore.Save(ctx, existingSession); err != nil {
			cm.logger.Warn("Failed to reactivate session", zap.Error(err))
		}

		cm.mu.Lock()
		cm.activeSessions[existingSession.ID] = &activeSession{
			Session:   existingSession,
			Connected: true,
			SendChan:  sendChan,
		}
		cm.mu.Unlock()

		cm.logger.Info("Session reactivated",
			zap.String("session_id", existingSession.ID),
			zap.String("client_id", clientID),
		)

		return existingSession, nil
	}

	// Create new session
	session := NewSession(clientID, cm.config.SessionTTL)

	if err := cm.sessionStore.Save(ctx, session); err != nil {
		return nil, err
	}

	cm.mu.Lock()
	cm.activeSessions[session.ID] = &activeSession{
		Session:   session,
		Connected: true,
		SendChan:  sendChan,
	}
	cm.mu.Unlock()

	cm.logger.Info("New session created",
		zap.String("session_id", session.ID),
		zap.String("client_id", clientID),
	)

	return session, nil
}

// HandleDisconnect handles a WebSocket disconnection
func (cm *ConnectionManager) HandleDisconnect(ctx context.Context, sessionID string) error {
	cm.mu.Lock()
	active, exists := cm.activeSessions[sessionID]
	if exists {
		active.Connected = false
		delete(cm.activeSessions, sessionID)
	}
	cm.mu.Unlock()

	// Update session state to disconnected
	if err := cm.sessionStore.UpdateState(ctx, sessionID, SessionStateDisconnected); err != nil {
		cm.logger.Warn("Failed to update session state on disconnect",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
	}

	cm.logger.Info("Session disconnected",
		zap.String("session_id", sessionID),
	)

	return nil
}

// HandleReconnect handles a session resume request
// Returns the session and missed events
func (cm *ConnectionManager) HandleReconnect(
	ctx context.Context,
	req *ResumeRequest,
	sendChan chan []byte,
) (*Session, []*CachedEvent, error) {
	// Get session
	session, err := cm.sessionStore.Get(ctx, req.SessionID)
	if err != nil {
		return nil, nil, err
	}

	// Reactivate session
	session.State = SessionStateActive
	session.Touch()

	if err := cm.sessionStore.Save(ctx, session); err != nil {
		return nil, nil, err
	}

	cm.mu.Lock()
	cm.activeSessions[session.ID] = &activeSession{
		Session:   session,
		Connected: true,
		SendChan:  sendChan,
	}
	cm.mu.Unlock()

	// Get missed events
	missedEvents, err := cm.eventCache.GetAfter(ctx, session.ID, req.LastEventID, cm.config.MaxEventsPerSession)
	if err != nil {
		cm.logger.Warn("Failed to get missed events",
			zap.String("session_id", session.ID),
			zap.Error(err),
		)
		missedEvents = nil // Continue without events
	}

	cm.logger.Info("Session reconnected",
		zap.String("session_id", session.ID),
		zap.Int("missed_events", len(missedEvents)),
	)

	return session, missedEvents, nil
}

// DeliverEvent delivers an event to a session and caches it for replay
func (cm *ConnectionManager) DeliverEvent(ctx context.Context, sessionID string, eventType string, payload []byte) error {
	// Create cached event
	event := &CachedEvent{
		ID:        generateEventID(),
		SessionID: sessionID,
		EventType: eventType,
		Payload:   payload,
		Timestamp: time.Now(),
		Delivered: false,
	}

	// Cache the event first (for replay if disconnected)
	if err := cm.eventCache.Store(ctx, event); err != nil {
		cm.logger.Warn("Failed to cache event",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
	}

	// Try to deliver to active session
	cm.mu.RLock()
	active, exists := cm.activeSessions[sessionID]
	cm.mu.RUnlock()

	if !exists || !active.Connected {
		cm.logger.Debug("Session not connected, event cached for replay",
			zap.String("session_id", sessionID),
			zap.String("event_id", event.ID),
		)
		return nil
	}

	// Wrap payload with event metadata
	wrappedPayload := map[string]interface{}{
		"type":    eventType,
		"payload": json.RawMessage(payload),
		"meta": EventMeta{
			EventID: event.ID,
			Replay:  false,
		},
	}

	data, err := json.Marshal(wrappedPayload)
	if err != nil {
		return err
	}

	// Non-blocking send
	select {
	case active.SendChan <- data:
		event.Delivered = true
		// Update delivered status in cache (best effort)
		if err := cm.eventCache.Store(ctx, event); err != nil {
			cm.logger.Warn("failed to update event cache after delivery",
				zap.String("event_id", event.ID),
				zap.Error(err),
			)
		}
	default:
		cm.logger.Warn("Send channel full, event cached for retry",
			zap.String("session_id", sessionID),
			zap.String("event_id", event.ID),
		)
	}

	// Update session's last event
	cm.mu.Lock()
	if active, exists := cm.activeSessions[sessionID]; exists {
		active.Session.LastEventID = event.ID
	}
	cm.mu.Unlock()

	return nil
}

// ReplayEvents sends missed events to a session
func (cm *ConnectionManager) ReplayEvents(ctx context.Context, sessionID string, events []*CachedEvent) error {
	cm.mu.RLock()
	active, exists := cm.activeSessions[sessionID]
	cm.mu.RUnlock()

	if !exists || !active.Connected {
		return ErrSessionNotFound
	}

	// Send replay start message
	startMsg, _ := json.Marshal(map[string]interface{}{
		"type": "replay_start",
		"payload": map[string]int{
			"count": len(events),
		},
	})

	select {
	case active.SendChan <- startMsg:
	default:
		return nil // Channel full
	}

	// Send each event
	for _, event := range events {
		wrappedPayload := map[string]interface{}{
			"type":    event.EventType,
			"payload": json.RawMessage(event.Payload),
			"meta": EventMeta{
				EventID: event.ID,
				Replay:  true,
			},
		}

		data, err := json.Marshal(wrappedPayload)
		if err != nil {
			continue
		}

		select {
		case active.SendChan <- data:
		default:
			// Channel full, skip remaining
			break
		}
	}

	// Send replay end message
	endMsg, _ := json.Marshal(map[string]interface{}{
		"type": "replay_end",
	})

	select {
	case active.SendChan <- endMsg:
	default:
	}

	return nil
}

// GetSession returns a session by ID
func (cm *ConnectionManager) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	return cm.sessionStore.Get(ctx, sessionID)
}

// UpdateSessionSubscription updates a session's subscription
func (cm *ConnectionManager) UpdateSessionSubscription(ctx context.Context, sessionID string, topic string, add bool) error {
	session, err := cm.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	if add {
		session.AddSubscription(topic)
	} else {
		session.RemoveSubscription(topic)
	}

	return cm.sessionStore.Save(ctx, session)
}

// GetActiveSessionCount returns the number of active sessions
func (cm *ConnectionManager) GetActiveSessionCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.activeSessions)
}
