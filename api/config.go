package api

import (
	"errors"
	"fmt"
	"time"
)

// Config holds API server configuration
type Config struct {
	// Host is the server host (default: localhost)
	Host string

	// Port is the server port (default: 8080)
	Port int

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes
	WriteTimeout time.Duration

	// IdleTimeout is the maximum duration to wait for the next request
	IdleTimeout time.Duration

	// EnableCORS enables CORS middleware
	EnableCORS bool

	// AllowedOrigins is a list of allowed CORS origins
	AllowedOrigins []string

	// MaxHeaderBytes is the maximum size of request headers
	MaxHeaderBytes int

	// EnableGraphQL enables GraphQL API
	EnableGraphQL bool

	// EnableJSONRPC enables JSON-RPC API
	EnableJSONRPC bool

	// EnableWebSocket enables WebSocket subscriptions
	EnableWebSocket bool

	// GraphQLPath is the GraphQL endpoint path (default: /graphql)
	GraphQLPath string

	// GraphQLPlaygroundPath is the GraphQL playground path (default: /playground)
	GraphQLPlaygroundPath string

	// JSONRPCPath is the JSON-RPC endpoint path (default: /rpc)
	JSONRPCPath string

	// WebSocketPath is the WebSocket endpoint path (default: /ws)
	WebSocketPath string

	// ShutdownTimeout is the graceful shutdown timeout
	ShutdownTimeout time.Duration
}

// DefaultConfig returns a default API server configuration
func DefaultConfig() *Config {
	return &Config{
		Host:                  "localhost",
		Port:                  8080,
		ReadTimeout:           15 * time.Second,
		WriteTimeout:          15 * time.Second,
		IdleTimeout:           60 * time.Second,
		EnableCORS:            true,
		AllowedOrigins:        []string{"*"},
		MaxHeaderBytes:        1 << 20, // 1 MB
		EnableGraphQL:         true,
		EnableJSONRPC:         true,
		EnableWebSocket:       true,
		GraphQLPath:           "/graphql",
		GraphQLPlaygroundPath: "/playground",
		JSONRPCPath:           "/rpc",
		WebSocketPath:         "/ws",
		ShutdownTimeout:       30 * time.Second,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Host == "" {
		return errors.New("host cannot be empty")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if c.ReadTimeout <= 0 {
		return errors.New("read timeout must be positive")
	}
	if c.WriteTimeout <= 0 {
		return errors.New("write timeout must be positive")
	}
	if c.IdleTimeout <= 0 {
		return errors.New("idle timeout must be positive")
	}
	if c.MaxHeaderBytes <= 0 {
		return errors.New("max header bytes must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}

	// At least one API must be enabled
	if !c.EnableGraphQL && !c.EnableJSONRPC && !c.EnableWebSocket {
		return errors.New("at least one API (GraphQL, JSON-RPC, or WebSocket) must be enabled")
	}

	return nil
}

// Address returns the server address in host:port format
func (c *Config) Address() string {
	return c.Host + ":" + fmt.Sprintf("%d", c.Port)
}
