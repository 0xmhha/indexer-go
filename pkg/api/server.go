package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/api/etherscan"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
	apimiddleware "github.com/0xmhha/indexer-go/pkg/api/middleware"
	"github.com/0xmhha/indexer-go/pkg/api/rest"
	"github.com/0xmhha/indexer-go/pkg/api/websocket"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
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
	storage             port.QueryStore
	eventBus            *events.EventBus
	router              *chi.Mux
	server              *http.Server
	wsServer            *websocket.Server
	gqlSubServer        *graphql.SubscriptionServer
	rpcProxy            *rpcproxy.Proxy
	verifier            verifier.Verifier
	notificationService notifications.Service
	chains              ChainStores
	origins             apimiddleware.Origins
	trustedProxies      []netip.Prefix

	// Background work the server stops: the rate limiter's cleanup, the
	// JSON-RPC filter managers (root and per chain).
	rateLimiter *apimiddleware.RateLimiter
	rpcServer   *jsonrpc.Server
	chainRoutes *chainRoutes
}

// ServerOptions contains optional configuration for the API server
type ServerOptions struct {
	RPCProxy            *rpcproxy.Proxy
	Verifier            verifier.Verifier
	NotificationService notifications.Service
	// Chains serves the chains of multichain mode under /chains/{id}/.
	// The server then has no store of its own (store is nil) and serves no
	// GraphQL, JSON-RPC or Etherscan API at the root.
	Chains ChainStores
}

// NewServer creates a new API server
func NewServer(config *Config, logger *zap.Logger, store port.QueryStore) (*Server, error) {
	return NewServerWithOptions(config, logger, store, nil)
}

// NewServerWithOptions creates a new API server with optional configurations
func NewServerWithOptions(config *Config, logger *zap.Logger, store port.QueryStore, opts *ServerOptions) (*Server, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	trusted, err := apimiddleware.ParseTrustedProxies(config.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	s := &Server{
		config:         config,
		logger:         logger,
		storage:        store,
		router:         chi.NewRouter(),
		origins:        apimiddleware.NewOrigins(config.AllowedOrigins),
		trustedProxies: trusted,
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

	if opts != nil && opts.Chains != nil {
		s.chains = opts.Chains
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

// graphqlLimits are the bounds of GraphQL requests.
func (s *Server) graphqlLimits() graphql.Limits {
	return graphql.Limits{MaxDepth: s.config.GraphQLMaxDepth, MaxComplexity: s.config.GraphQLMaxComplexity}
}

// streamOutbox returns the change stream's outbox of store when the server
// serves it (StreamResume), nil otherwise.
func (s *Server) streamOutbox(store port.QueryStore) port.Outbox {
	if !s.config.StreamResume || store == nil {
		return nil
	}
	ob, _ := store.(port.Outbox)
	return ob
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

	// Client address: the peer, or the client a trusted proxy forwarded
	// for (api.trusted_proxies); logs and the rate limit use it
	s.router.Use(apimiddleware.ClientIP(s.trustedProxies, s.logger))

	// Logger middleware
	s.router.Use(apimiddleware.LoggerWithLevel(s.logger))

	// Recoverer middleware (chi's built-in)
	s.router.Use(middleware.Recoverer)

	// Rate limiting middleware (if enabled)
	if s.config.EnableRateLimit {
		s.rateLimiter = apimiddleware.NewRateLimiter(s.config.RateLimitPerSecond, s.config.RateLimitBurst, s.logger)
		s.router.Use(s.rateLimiter.Middleware())
		s.logger.Info("rate limiting enabled",
			zap.Float64("rate_per_second", s.config.RateLimitPerSecond),
			zap.Int("burst", s.config.RateLimitBurst),
		)
	}

	// API key authentication middleware (if enabled)
	if s.config.EnableAPIKeyAuth {
		authCfg := apimiddleware.AuthConfig{
			APIKeys: s.config.APIKeys,
			AllowedPaths: map[string]bool{
				"/health":  true,
				"/version": true,
				"/metrics": true,
			},
		}
		s.router.Use(apimiddleware.APIKeyAuth(authCfg, s.logger))
		s.logger.Info("API key authentication enabled",
			zap.Int("configured_keys", len(s.config.APIKeys)),
		)
	}

	// CORS headers on every response to an allowed origin
	if s.config.EnableCORS {
		s.router.Use(apimiddleware.CORS(s.origins))
	}
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// WebSocket endpoints - registered directly without timeout/compress
	if s.config.EnableWebSocket {
		s.logger.Info("WebSocket API enabled", zap.String("path", s.config.WebSocketPath))

		// Create WebSocket server
		s.wsServer = websocket.NewServer(s.logger)
		s.wsServer.SetCheckOrigin(s.origins.CheckWebSocketOrigin)
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

	if s.config.HealthOnly {
		s.logger.Info("API endpoints are served by other processes (health and metrics only)")
		return
	}

	if s.chains != nil {
		s.mountChainRoutes(s.chains)
	}
	if s.storage == nil {
		// Multichain mode: every chain's API is under /chains/{id}/.
		return
	}

	// GraphQL endpoints
	if s.config.EnableGraphQL {
		s.logger.Info("GraphQL API enabled", zap.String("path", s.config.GraphQLPath))

		// Create GraphQL handler with optional RPC Proxy and Notification Service
		outbox := s.streamOutbox(s.storage)
		opts := &graphql.HandlerOptions{
			RPCProxy:            s.rpcProxy,
			NotificationService: s.notificationService,
			Limits:              s.graphqlLimits(),
			ExtensionsOnly:      s.config.DeclaredOnly,
		}
		if outbox != nil {
			opts.Stream = outbox
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
		s.gqlSubServer.SetDirect(s.config.DirectSubscriptions)
		s.gqlSubServer.SetCheckOrigin(s.origins.CheckWebSocketOrigin)
		if outbox != nil {
			s.gqlSubServer.SetOutbox(outbox)
		}
		s.router.Get("/graphql/ws", s.gqlSubServer.Handler())
		s.logger.Info("GraphQL subscriptions endpoint registered",
			zap.String("path", "/graphql/ws"),
			zap.Bool("keep_alive", s.config.EnableWebSocketKeepAlive))
	}

	s.mountRoutes()

	if s.config.DeclaredOnly {
		s.logger.Info("Declared data only: no explorer, REST, JSON-RPC or Etherscan API")
		return
	}

	// REST API of the most polled paths, over the same resolvers
	if s.config.EnableREST {
		restPath := strings.TrimRight(s.config.RESTPath, "/")
		if restPath == "" {
			restPath = constants.DefaultRESTPath
		}
		gql, err := graphql.NewHandler(s.storage, s.logger)
		if err != nil {
			s.logger.Error("failed to create the REST API", zap.Error(err))
		} else {
			s.router.Handle(restPath+"/*", rest.NewHandler(gql))
			s.logger.Info("REST API enabled", zap.String("path", restPath))
		}
	}

	// JSON-RPC endpoints
	if s.config.EnableJSONRPC {
		s.logger.Info("JSON-RPC API enabled", zap.String("path", s.config.JSONRPCPath))

		// Create JSON-RPC handler
		jsonrpcServer := jsonrpc.NewServer(s.storage, s.logger)
		s.rpcServer = jsonrpcServer

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
	defer s.stopBackground()
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	s.logger.Info("API server stopped gracefully")
	return nil
}

// stopBackground stops the goroutines the server started besides the HTTP
// server: the rate limiter's cleanup and the JSON-RPC filter managers.
func (s *Server) stopBackground() {
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
	if s.rpcServer != nil {
		s.rpcServer.Close()
	}
	if s.chainRoutes != nil {
		s.chainRoutes.close()
	}
}

// Router returns the underlying chi router (for testing)
func (s *Server) Router() *chi.Mux {
	return s.router
}
