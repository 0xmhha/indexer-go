package websocket

import (
	"net/http"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Server handles WebSocket connections
type Server struct {
	hub      *Hub
	logger   *zap.Logger
	upgrader websocket.Upgrader
}

// NewServer creates a new WebSocket server
func NewServer(logger *zap.Logger) *Server {
	hub := NewHub(logger)
	go hub.Run()

	return &Server{
		hub:    hub,
		logger: logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

// SetCheckOrigin sets the check of the Origin header of upgrade requests
// (api.allowed_origins); every origin is allowed until it is set.
func (s *Server) SetCheckOrigin(check func(r *http.Request) bool) {
	s.upgrader.CheckOrigin = check
}

// ServeHTTP handles WebSocket upgrade requests
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade connection", zap.Error(err))
		return
	}

	client := NewClient(s.hub, conn, s.logger)
	s.hub.register <- client

	// Start client goroutines
	go client.WritePump()
	go client.ReadPump()

	s.logger.Info("new websocket connection",
		zap.String("remote_addr", r.RemoteAddr))
}

// Hub returns the underlying hub (for broadcasting events)
func (s *Server) Hub() *Hub {
	return s.hub
}

// Stop stops the WebSocket server
func (s *Server) Stop() {
	s.hub.Stop()
}
