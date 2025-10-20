package websocket

import (
	"encoding/json"
	"sync"

	"go.uber.org/zap"
)

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	// Registered clients
	clients map[*Client]bool
	mu      sync.RWMutex

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Broadcast events to clients
	broadcast chan *Event

	logger *zap.Logger
}

// NewHub creates a new Hub
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *Event, 256),
		logger:     logger,
	}
}

// Run runs the hub
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Info("client registered",
				zap.Int("total_clients", len(h.clients)))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Info("client unregistered",
				zap.Int("total_clients", len(h.clients)))

		case event := <-h.broadcast:
			h.broadcastEvent(event)
		}
	}
}

// broadcastEvent broadcasts an event to all subscribed clients
func (h *Hub) broadcastEvent(event *Event) {
	message := Message{
		Type: "event",
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("failed to marshal event", zap.Error(err))
		return
	}
	message.Payload = eventData

	messageBytes, err := json.Marshal(message)
	if err != nil {
		h.logger.Error("failed to marshal message", zap.Error(err))
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	sentCount := 0
	for client := range h.clients {
		if client.IsSubscribed(event.Type) {
			select {
			case client.send <- messageBytes:
				sentCount++
			default:
				// Client buffer full, close the connection
				h.logger.Warn("client buffer full, closing connection")
				close(client.send)
				delete(h.clients, client)
			}
		}
	}

	h.logger.Debug("event broadcasted",
		zap.String("type", string(event.Type)),
		zap.Int("recipients", sentCount))
}

// BroadcastNewBlock broadcasts a new block event
func (h *Hub) BroadcastNewBlock(blockData interface{}) {
	event := &Event{
		Type: SubscribeNewBlock,
		Data: blockData,
	}

	select {
	case h.broadcast <- event:
	default:
		h.logger.Warn("broadcast channel full, dropping event")
	}
}

// BroadcastNewTransaction broadcasts a new transaction event
func (h *Hub) BroadcastNewTransaction(txData interface{}) {
	event := &Event{
		Type: SubscribeNewTransaction,
		Data: txData,
	}

	select {
	case h.broadcast <- event:
	default:
		h.logger.Warn("broadcast channel full, dropping event")
	}
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Stop stops the hub and closes all client connections
func (h *Hub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		close(client.send)
		delete(h.clients, client)
	}

	h.logger.Info("hub stopped")
}
