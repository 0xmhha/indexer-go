package websocket

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

// Client represents a WebSocket client connection
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	// Subscriptions tracks which event types this client is subscribed to
	subscriptions map[SubscriptionType]bool
	mu            sync.RWMutex

	logger *zap.Logger
}

// NewClient creates a new WebSocket client
func NewClient(hub *Hub, conn *websocket.Conn, logger *zap.Logger) *Client {
	return &Client{
		hub:           hub,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[SubscriptionType]bool),
		logger:        logger,
	}
}

// IsSubscribed checks if the client is subscribed to an event type
func (c *Client) IsSubscribed(eventType SubscriptionType) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subscriptions[eventType]
}

// Subscribe subscribes the client to an event type
func (c *Client) Subscribe(eventType SubscriptionType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscriptions[eventType] = true
}

// Unsubscribe unsubscribes the client from an event type
func (c *Client) Unsubscribe(eventType SubscriptionType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subscriptions, eventType)
}

// ReadPump pumps messages from the WebSocket connection to the hub
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
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

// WritePump pumps messages from the hub to the WebSocket connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming messages from the client
func (c *Client) handleMessage(message []byte) {
	var msg Message
	if err := json.Unmarshal(message, &msg); err != nil {
		c.logger.Error("failed to unmarshal message", zap.Error(err))
		c.sendError("invalid message format")
		return
	}

	switch msg.Type {
	case "subscribe":
		c.handleSubscribe(msg.Payload)
	case "unsubscribe":
		c.handleUnsubscribe(msg.Payload)
	case "ping":
		c.sendMessage(Message{Type: "pong"})
	default:
		c.sendError("unknown message type: " + msg.Type)
	}
}

// handleSubscribe handles subscription requests
func (c *Client) handleSubscribe(payload json.RawMessage) {
	var req SubscribeRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		c.logger.Error("failed to unmarshal subscribe request", zap.Error(err))
		c.sendError("invalid subscribe request")
		return
	}

	// Validate subscription type
	if req.Type != SubscribeNewBlock && req.Type != SubscribeNewTransaction {
		c.sendError("invalid subscription type")
		return
	}

	c.Subscribe(req.Type)
	c.sendSuccess("subscribed to " + string(req.Type))

	c.logger.Info("client subscribed",
		zap.String("type", string(req.Type)))
}

// handleUnsubscribe handles unsubscribe requests
func (c *Client) handleUnsubscribe(payload json.RawMessage) {
	var req UnsubscribeRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		c.logger.Error("failed to unmarshal unsubscribe request", zap.Error(err))
		c.sendError("invalid unsubscribe request")
		return
	}

	c.Unsubscribe(req.Type)
	c.sendSuccess("unsubscribed from " + string(req.Type))

	c.logger.Info("client unsubscribed",
		zap.String("type", string(req.Type)))
}

// sendMessage sends a message to the client
func (c *Client) sendMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error("failed to marshal message", zap.Error(err))
		return
	}

	select {
	case c.send <- data:
	default:
		c.logger.Warn("client send buffer full, dropping message")
	}
}

// sendError sends an error message to the client
func (c *Client) sendError(errMsg string) {
	msg := Message{
		Type: "error",
	}
	payload, _ := json.Marshal(ErrorMessage{Error: errMsg})
	msg.Payload = payload
	c.sendMessage(msg)
}

// sendSuccess sends a success message to the client
func (c *Client) sendSuccess(message string) {
	msg := Message{
		Type: "success",
	}
	payload, _ := json.Marshal(SuccessMessage{Message: message})
	msg.Payload = payload
	c.sendMessage(msg)
}
