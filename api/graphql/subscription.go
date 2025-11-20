package graphql

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/events"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// WebSocket configuration
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

// SubscriptionServer handles GraphQL subscriptions over WebSocket
type SubscriptionServer struct {
	eventBus *events.EventBus
	logger   *zap.Logger
	upgrader websocket.Upgrader
}

// NewSubscriptionServer creates a new subscription server
func NewSubscriptionServer(eventBus *events.EventBus, logger *zap.Logger) *SubscriptionServer {
	return &SubscriptionServer{
		eventBus: eventBus,
		logger:   logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
	}
}

// ServeHTTP handles WebSocket connections for GraphQL subscriptions
func (s *SubscriptionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade connection", zap.Error(err))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &subscriptionClient{
		server:        s,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*clientSubscription),
		logger:        s.logger,
		ctx:           ctx,
		cancel:        cancel,
	}

	go client.writePump()
	go client.readPump()
}

// subscriptionClient represents a WebSocket client for subscriptions
type subscriptionClient struct {
	server        *SubscriptionServer
	conn          *websocket.Conn
	send          chan []byte
	subscriptions map[string]*clientSubscription // id -> subscription
	mu            sync.RWMutex
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc
}

// clientSubscription holds subscription state
type clientSubscription struct {
	id         string
	subType    string
	eventSub   *events.Subscription
	cancelFunc context.CancelFunc
}

// GraphQL over WebSocket protocol messages
type wsMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type subscribePayload struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	OperationName string                 `json:"operationName,omitempty"`
}

// readPump reads messages from the WebSocket connection
func (c *subscriptionClient) readPump() {
	defer func() {
		c.cleanup()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("websocket read error", zap.Error(err))
			}
			break
		}

		c.handleMessage(message)
	}
}

// writePump writes messages to the WebSocket connection
func (c *subscriptionClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (c *subscriptionClient) handleMessage(data []byte) {
	var msg wsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.logger.Error("failed to unmarshal message", zap.Error(err))
		return
	}

	switch msg.Type {
	case "connection_init":
		c.sendMessage(wsMessage{Type: "connection_ack"})

	case "subscribe":
		c.handleSubscribe(msg.ID, msg.Payload)

	case "complete":
		c.handleComplete(msg.ID)

	case "ping":
		c.sendMessage(wsMessage{Type: "pong"})

	default:
		c.logger.Warn("unknown message type", zap.String("type", msg.Type))
	}
}

// handleSubscribe handles subscription requests
func (c *subscriptionClient) handleSubscribe(id string, payload json.RawMessage) {
	var sub subscribePayload
	if err := json.Unmarshal(payload, &sub); err != nil {
		c.sendError(id, "invalid payload")
		return
	}

	// Parse the subscription query to determine type
	subType := c.parseSubscriptionType(sub.Query)
	if subType == "" {
		c.sendError(id, "invalid subscription query")
		return
	}

	// Subscribe to EventBus
	if c.server.eventBus == nil {
		c.sendError(id, "event bus not available")
		return
	}

	var eventType events.EventType
	switch subType {
	case "newBlock":
		eventType = events.EventTypeBlock
	case "newTransaction":
		eventType = events.EventTypeTransaction
	default:
		c.sendError(id, "unknown subscription type")
		return
	}

	// Create subscription ID
	subID := events.SubscriptionID(id)
	eventSub := c.server.eventBus.Subscribe(subID, []events.EventType{eventType}, nil, 100)
	if eventSub == nil {
		c.sendError(id, "failed to create subscription")
		return
	}

	// Create context for this subscription
	subCtx, subCancel := context.WithCancel(c.ctx)

	// Store subscription
	clientSub := &clientSubscription{
		id:         id,
		subType:    subType,
		eventSub:   eventSub,
		cancelFunc: subCancel,
	}

	c.mu.Lock()
	c.subscriptions[id] = clientSub
	c.mu.Unlock()

	// Start goroutine to handle events
	go c.eventLoop(subCtx, clientSub)

	c.logger.Info("subscription started",
		zap.String("id", id),
		zap.String("type", subType),
	)
}

// eventLoop handles events for a subscription
func (c *subscriptionClient) eventLoop(ctx context.Context, sub *clientSubscription) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.eventSub.Channel:
			if !ok {
				return
			}
			c.handleEvent(sub.id, sub.subType, event)
		}
	}
}

// handleComplete handles subscription completion
func (c *subscriptionClient) handleComplete(id string) {
	c.mu.Lock()
	if sub, ok := c.subscriptions[id]; ok {
		// Cancel the event loop
		sub.cancelFunc()
		// Unsubscribe from EventBus
		if c.server.eventBus != nil {
			c.server.eventBus.Unsubscribe(events.SubscriptionID(id))
		}
		delete(c.subscriptions, id)
	}
	c.mu.Unlock()

	c.logger.Info("subscription completed", zap.String("id", id))
}

// handleEvent handles events from EventBus
func (c *subscriptionClient) handleEvent(id string, subType string, event interface{}) {
	var payload interface{}

	switch subType {
	case "newBlock":
		if blockEvent, ok := event.(*events.BlockEvent); ok {
			blockData := map[string]interface{}{
				"number":    blockEvent.Number,
				"hash":      blockEvent.Hash.Hex(),
				"timestamp": blockEvent.CreatedAt.Unix(),
				"txCount":   blockEvent.TxCount,
			}
			// Add parentHash if block is available
			if blockEvent.Block != nil {
				blockData["parentHash"] = blockEvent.Block.ParentHash().Hex()
			}
			payload = map[string]interface{}{
				"newBlock": blockData,
			}
		}

	case "newTransaction":
		if txEvent, ok := event.(*events.TransactionEvent); ok {
			txData := map[string]interface{}{
				"hash":        txEvent.Hash.Hex(),
				"from":        txEvent.From.Hex(),
				"value":       txEvent.Value,
				"blockNumber": txEvent.BlockNumber,
			}
			// Add to address if available
			if txEvent.To != nil {
				txData["to"] = txEvent.To.Hex()
			}
			payload = map[string]interface{}{
				"newTransaction": txData,
			}
		}
	}

	if payload != nil {
		c.sendNext(id, payload)
	}
}

// parseSubscriptionType extracts subscription type from query
func (c *subscriptionClient) parseSubscriptionType(query string) string {
	// Simple parsing - check for subscription keywords
	if contains(query, "newBlock") {
		return "newBlock"
	}
	if contains(query, "newTransaction") {
		return "newTransaction"
	}
	return ""
}

// contains checks if s contains substr (simple implementation)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// sendMessage sends a message to the client
func (c *subscriptionClient) sendMessage(msg wsMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error("failed to marshal message", zap.Error(err))
		return
	}

	select {
	case c.send <- data:
	default:
		c.logger.Warn("send buffer full, dropping message")
	}
}

// sendNext sends subscription data
func (c *subscriptionClient) sendNext(id string, payload interface{}) {
	data, _ := json.Marshal(payload)
	c.sendMessage(wsMessage{
		ID:      id,
		Type:    "next",
		Payload: data,
	})
}

// sendError sends an error message
func (c *subscriptionClient) sendError(id string, errMsg string) {
	payload, _ := json.Marshal([]map[string]string{
		{"message": errMsg},
	})
	c.sendMessage(wsMessage{
		ID:      id,
		Type:    "error",
		Payload: payload,
	})
}

// cleanup unsubscribes from all EventBus subscriptions
func (c *subscriptionClient) cleanup() {
	// Cancel main context to stop all event loops
	c.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Unsubscribe all subscriptions from EventBus
	if c.server.eventBus != nil {
		for id, sub := range c.subscriptions {
			sub.cancelFunc()
			c.server.eventBus.Unsubscribe(events.SubscriptionID(id))
		}
	}

	c.subscriptions = make(map[string]*clientSubscription)
	close(c.send)
}

// SetEventBus sets the EventBus (for dependency injection)
func (s *SubscriptionServer) SetEventBus(bus *events.EventBus) {
	s.eventBus = bus
}

// SubscriptionHandler returns a handler that checks for EventBus availability
func (s *SubscriptionServer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.eventBus == nil {
			http.Error(w, "subscriptions not available", http.StatusServiceUnavailable)
			return
		}
		s.ServeHTTP(w, r)
	}
}

// SubscriptionContext holds context for subscription operations
type SubscriptionContext struct {
	context.Context
	EventBus *events.EventBus
}
