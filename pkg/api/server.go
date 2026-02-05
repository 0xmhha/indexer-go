package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/0xmhha/indexer-go/pkg/api/etherscan"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
	apimiddleware "github.com/0xmhha/indexer-go/pkg/api/middleware"
	"github.com/0xmhha/indexer-go/pkg/api/websocket"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Server represents the API server
type Server struct {
	config              *Config
	logger              *zap.Logger
	storage             storage.Storage
	eventBus            *events.EventBus
	router              *chi.Mux
	server              *http.Server
	wsServer            *websocket.Server
	gqlSubServer        *graphql.SubscriptionServer
	rpcProxy            *rpcproxy.Proxy
	verifier            verifier.Verifier
	notificationService notifications.Service
}

// ServerOptions contains optional configuration for the API server
type ServerOptions struct {
	RPCProxy            *rpcproxy.Proxy
	Verifier            verifier.Verifier
	NotificationService notifications.Service
}

// NewServer creates a new API server
func NewServer(config *Config, logger *zap.Logger, store storage.Storage) (*Server, error) {
	return NewServerWithOptions(config, logger, store, nil)
}

// NewServerWithOptions creates a new API server with optional configurations
func NewServerWithOptions(config *Config, logger *zap.Logger, store storage.Storage, opts *ServerOptions) (*Server, error) {
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

	// Set optional RPC Proxy before setting up routes
	if opts != nil && opts.RPCProxy != nil {
		s.rpcProxy = opts.RPCProxy
		logger.Info("RPC Proxy configured for API server")
	}

	// Set optional Verifier for Etherscan API
	if opts != nil && opts.Verifier != nil {
		s.verifier = opts.Verifier
		logger.Info("Verifier configured for Etherscan API")
	}

	// Set optional Notification Service
	if opts != nil && opts.NotificationService != nil {
		s.notificationService = opts.NotificationService
		logger.Info("Notification service configured for API server")
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

	// Set EventBus for GraphQL Subscription server if it exists
	if s.gqlSubServer != nil {
		s.gqlSubServer.SetEventBus(bus)
		s.logger.Info("EventBus set for GraphQL subscriptions")
	}
}

// SetRPCProxy sets the RPC Proxy for the server (enables contract call queries)
func (s *Server) SetRPCProxy(proxy *rpcproxy.Proxy) {
	s.rpcProxy = proxy
	s.logger.Info("RPC Proxy set for API server")
}

// SetNotificationService sets the notification service for the server
func (s *Server) SetNotificationService(service notifications.Service) {
	s.notificationService = service
	s.logger.Info("Notification service set for API server")
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

	// Rate limiting middleware (if enabled)
	if s.config.EnableRateLimit {
		s.router.Use(apimiddleware.RateLimit(
			s.config.RateLimitPerSecond,
			s.config.RateLimitBurst,
			s.logger,
		))
		s.logger.Info("rate limiting enabled",
			zap.Float64("rate_per_second", s.config.RateLimitPerSecond),
			zap.Int("burst", s.config.RateLimitBurst),
		)
	}

	// Custom CORS middleware that adds headers to ALL responses
	if s.config.EnableCORS {
		s.router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origin := r.Header.Get("Origin")
				if origin == "" {
					origin = "*"
				}

				// Check if origin is allowed
				allowed := false
				for _, allowedOrigin := range s.config.AllowedOrigins {
					if allowedOrigin == "*" || allowedOrigin == origin {
						allowed = true
						break
					}
				}

				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, Upgrade, Connection")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Max-Age", "300")
				}

				// Handle preflight requests
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}

				next.ServeHTTP(w, r)
			})
		})
	}
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// WebSocket endpoints - registered directly without timeout/compress
	if s.config.EnableWebSocket {
		s.logger.Info("WebSocket API enabled", zap.String("path", s.config.WebSocketPath))

		// Create WebSocket server
		s.wsServer = websocket.NewServer(s.logger)
		s.router.Get(s.config.WebSocketPath, s.wsServer.ServeHTTP)
	}

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

		// Create GraphQL handler with optional RPC Proxy and Notification Service
		opts := &graphql.HandlerOptions{
			RPCProxy:            s.rpcProxy,
			NotificationService: s.notificationService,
		}
		graphqlHandler, err := graphql.NewHandlerWithOptions(s.storage, s.logger, opts)
		if err != nil {
			s.logger.Error("failed to create GraphQL handler", zap.Error(err))
		} else {
			s.router.Handle(s.config.GraphQLPath, graphqlHandler)
			s.router.Get(s.config.GraphQLPlaygroundPath, graphqlHandler.PlaygroundHandler())
			s.logger.Info("GraphQL playground enabled", zap.String("path", s.config.GraphQLPlaygroundPath))
		}

		// Create GraphQL Subscription server (EventBus will be set later via SetEventBus)
		s.gqlSubServer = graphql.NewSubscriptionServer(nil, s.logger, s.config.EnableWebSocketKeepAlive)
		s.router.Get("/graphql/ws", s.gqlSubServer.Handler())
		s.logger.Info("GraphQL subscriptions endpoint registered",
			zap.String("path", "/graphql/ws"),
			zap.Bool("keep_alive", s.config.EnableWebSocketKeepAlive))
	}

	// JSON-RPC endpoints
	if s.config.EnableJSONRPC {
		s.logger.Info("JSON-RPC API enabled", zap.String("path", s.config.JSONRPCPath))

		// Create JSON-RPC handler
		jsonrpcServer := jsonrpc.NewServer(s.storage, s.logger)

		// Set notification service if available
		if s.notificationService != nil {
			jsonrpcServer.SetNotificationService(s.notificationService)
			s.logger.Info("Notification service configured for JSON-RPC")
		}

		s.router.Post(s.config.JSONRPCPath, jsonrpcServer.ServeHTTP)
	}

	// Etherscan-compatible API endpoints (for Forge verification)
	etherscanHandler := etherscan.NewHandler(s.storage, s.verifier, s.logger)
	s.router.Get("/api", etherscanHandler.ServeHTTP)
	s.router.Post("/api", etherscanHandler.ServeHTTP)
	s.logger.Info("Etherscan-compatible API enabled", zap.String("path", "/api"))
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
	_ = json.NewEncoder(w).Encode(response)
}

// handleVersion handles the version endpoint
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"version":"1.0.0","name":"indexer-go"}`)
}

// SubscribersResponse represents the subscribers list response
type SubscribersResponse struct {
	TotalCount  int                     `json:"total_count"`
	Subscribers []events.SubscriberInfo `json:"subscribers"`
}

// handleSubscribers handles the subscribers endpoint
func (s *Server) handleSubscribers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check if EventBus is configured
	if s.eventBus == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
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
	_ = json.NewEncoder(w).Encode(response)
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
