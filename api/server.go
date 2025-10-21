package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/0xmhha/indexer-go/api/graphql"
	"github.com/0xmhha/indexer-go/api/jsonrpc"
	apimiddleware "github.com/0xmhha/indexer-go/api/middleware"
	"github.com/0xmhha/indexer-go/api/websocket"
	"github.com/0xmhha/indexer-go/events"
	"github.com/0xmhha/indexer-go/storage"
	"go.uber.org/zap"
)

// Server represents the API server
type Server struct {
	config    *Config
	logger    *zap.Logger
	storage   storage.Storage
	eventBus  *events.EventBus
	router    *chi.Mux
	server    *http.Server
	wsServer  *websocket.Server
}

// NewServer creates a new API server
func NewServer(config *Config, logger *zap.Logger, store storage.Storage) (*Server, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	s := &Server{
		config:  config,
		logger:  logger,
		storage: store,
		router:  chi.NewRouter(),
	}

	// Setup middleware
	s.setupMiddleware()

	// Setup routes
	s.setupRoutes()

	// Create HTTP server
	s.server = &http.Server{
		Addr:           config.Address(),
		Handler:        s.router,
		ReadTimeout:    config.ReadTimeout,
		WriteTimeout:   config.WriteTimeout,
		IdleTimeout:    config.IdleTimeout,
		MaxHeaderBytes: config.MaxHeaderBytes,
	}

	return s, nil
}

// SetEventBus sets the EventBus for the server (optional)
func (s *Server) SetEventBus(bus *events.EventBus) {
	s.eventBus = bus
}

// setupMiddleware configures the middleware stack
func (s *Server) setupMiddleware() {
	// Recovery middleware (must be first)
	s.router.Use(apimiddleware.Recovery(s.logger))

	// Request ID middleware
	s.router.Use(middleware.RequestID)

	// Real IP middleware
	s.router.Use(middleware.RealIP)

	// Logger middleware
	s.router.Use(apimiddleware.LoggerWithLevel(s.logger))

	// Recoverer middleware (chi's built-in)
	s.router.Use(middleware.Recoverer)

	// Timeout middleware (30 seconds)
	s.router.Use(middleware.Timeout(30 * time.Second))

	// CORS middleware
	if s.config.EnableCORS {
		s.router.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.config.AllowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: true,
			MaxAge:           300, // Maximum value not ignored by any major browsers
		}))
	}

	// Compressor middleware
	s.router.Use(middleware.Compress(5))
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Health check endpoint
	s.router.Get("/health", s.handleHealth)

	// API version endpoint
	s.router.Get("/version", s.handleVersion)

	// Prometheus metrics endpoint
	s.router.Handle("/metrics", promhttp.Handler())

	// EventBus subscriber stats endpoint (if EventBus is configured)
	s.router.Get("/subscribers", s.handleSubscribers)

	// GraphQL endpoints
	if s.config.EnableGraphQL {
		s.logger.Info("GraphQL API enabled", zap.String("path", s.config.GraphQLPath))

		// Create GraphQL handler
		graphqlHandler, err := graphql.NewHandler(s.storage, s.logger)
		if err != nil {
			s.logger.Error("failed to create GraphQL handler", zap.Error(err))
		} else {
			s.router.Handle(s.config.GraphQLPath, graphqlHandler)
			s.router.Get(s.config.GraphQLPlaygroundPath, graphqlHandler.PlaygroundHandler())
			s.logger.Info("GraphQL playground enabled", zap.String("path", s.config.GraphQLPlaygroundPath))
		}
	}

	// JSON-RPC endpoints
	if s.config.EnableJSONRPC {
		s.logger.Info("JSON-RPC API enabled", zap.String("path", s.config.JSONRPCPath))

		// Create JSON-RPC handler
		jsonrpcServer := jsonrpc.NewServer(s.storage, s.logger)
		s.router.Post(s.config.JSONRPCPath, jsonrpcServer.ServeHTTP)
	}

	// WebSocket endpoints
	if s.config.EnableWebSocket {
		s.logger.Info("WebSocket API enabled", zap.String("path", s.config.WebSocketPath))

		// Create WebSocket server
		s.wsServer = websocket.NewServer(s.logger)
		s.router.Get(s.config.WebSocketPath, s.wsServer.ServeHTTP)
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string              `json:"status"`
	Timestamp string              `json:"timestamp"`
	EventBus  *EventBusHealthInfo `json:"eventbus,omitempty"`
}

// EventBusHealthInfo contains EventBus health information
type EventBusHealthInfo struct {
	Subscribers     int    `json:"subscribers"`
	TotalEvents     uint64 `json:"total_events"`
	TotalDeliveries uint64 `json:"total_deliveries"`
	DroppedEvents   uint64 `json:"dropped_events"`
}

// handleHealth handles the health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	response := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Add EventBus health info if available
	if s.eventBus != nil {
		totalEvents, totalDeliveries, droppedEvents := s.eventBus.Stats()
		response.EventBus = &EventBusHealthInfo{
			Subscribers:     s.eventBus.SubscriberCount(),
			TotalEvents:     totalEvents,
			TotalDeliveries: totalDeliveries,
			DroppedEvents:   droppedEvents,
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleVersion handles the version endpoint
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"version":"1.0.0","name":"indexer-go"}`)
}

// SubscribersResponse represents the subscribers list response
type SubscribersResponse struct {
	TotalCount  int                      `json:"total_count"`
	Subscribers []events.SubscriberInfo  `json:"subscribers"`
}

// handleSubscribers handles the subscribers endpoint
func (s *Server) handleSubscribers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check if EventBus is configured
	if s.eventBus == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "EventBus not configured",
		})
		return
	}

	// Get all subscriber info
	subscribers := s.eventBus.GetAllSubscriberInfo()

	response := SubscribersResponse{
		TotalCount:  len(subscribers),
		Subscribers: subscribers,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// Start starts the API server
func (s *Server) Start() error {
	s.logger.Info("starting API server",
		zap.String("address", s.config.Address()),
		zap.Bool("graphql", s.config.EnableGraphQL),
		zap.Bool("jsonrpc", s.config.EnableJSONRPC),
		zap.Bool("websocket", s.config.EnableWebSocket),
	)

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

// Stop gracefully stops the API server
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("stopping API server")

	// Stop WebSocket server first
	if s.wsServer != nil {
		s.wsServer.Stop()
	}

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, s.config.ShutdownTimeout)
	defer cancel()

	// Shutdown server
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	s.logger.Info("API server stopped gracefully")
	return nil
}

// Router returns the underlying chi router (for testing)
func (s *Server) Router() *chi.Mux {
	return s.router
}
