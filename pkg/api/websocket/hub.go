package websocket

import (
	"encoding/json"
	"sync"

	"go.uber.org/zap"
)

const (
	// DefaultMaxClients is the maximum number of concurrent WebSocket clients
	DefaultMaxClients = 10000
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

	// done signals the Run goroutine to exit
	done chan struct{}

	// maxClients limits concurrent connections to prevent unbounded growth
	maxClients int

	logger *zap.Logger
}

// NewHub creates a new Hub
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *Event, 256),
		done:       make(chan struct{}),
		maxClients: DefaultMaxClients,
		logger:     logger,
	}
}

// Run runs the hub event loop. It exits when Stop() is called.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			return

		case client := <-h.register:
			h.mu.Lock()
			if len(h.clients) >= h.maxClients {
				h.mu.Unlock()
				h.logger.Warn("max clients reached, rejecting connection",
					zap.Int("max_clients", h.maxClients))
				close(client.send)
				continue
			}
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Info("client registered",
				zap.Int("total_clients", h.ClientCount()))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Info("client unregistered",
				zap.Int("total_clients", h.ClientCount()))

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

// Stop stops the hub and closes all client connections.
// It signals the Run goroutine to exit and cleans up all clients.
func (h *Hub) Stop() {
	// Signal Run() to exit
	close(h.done)

	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		close(client.send)
		delete(h.clients, client)
	}

	h.logger.Info("hub stopped")
}
