package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/storage"
	"go.uber.org/zap"
)

// mockStorage is a mock implementation of storage.Storage for testing
type mockStorage struct {
	storage.Storage
}

func TestNewServer(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid port",
			config: &Config{
				Host:            "localhost",
				Port:            0,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "no API enabled",
			config: &Config{
				Host:            "localhost",
				Port:            8080,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   false,
				EnableJSONRPC:   false,
				EnableWebSocket: false,
			},
			wantErr: true,
		},
	}

	logger := zap.NewNop()
	store := &mockStorage{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(tt.config, logger, store)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewServer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && server == nil {
				t.Error("NewServer() returned nil server")
			}
		})
	}
}

func TestServerHealthEndpoint(t *testing.T) {
	config := DefaultConfig()
	logger := zap.NewNop()
	store := &mockStorage{}

	server, err := NewServer(config, logger, store)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health endpoint returned wrong status code: got %v want %v",
			w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("health endpoint returned wrong content type: got %v want %v",
			contentType, "application/json")
	}
}

func TestServerVersionEndpoint(t *testing.T) {
	config := DefaultConfig()
	logger := zap.NewNop()
	store := &mockStorage{}

	server, err := NewServer(config, logger, store)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()

	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("version endpoint returned wrong status code: got %v want %v",
			w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("version endpoint returned wrong content type: got %v want %v",
			contentType, "application/json")
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	config := DefaultConfig()
	config.Port = 8081 // Use different port to avoid conflicts
	logger := zap.NewNop()
	store := &mockStorage{}

	server, err := NewServer(config, logger, store)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Test graceful shutdown without actually starting the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = server.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "empty host",
			config: &Config{
				Host:            "",
				Port:            8080,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "negative port",
			config: &Config{
				Host:            "localhost",
				Port:            -1,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "port too large",
			config: &Config{
				Host:            "localhost",
				Port:            70000,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "zero read timeout",
			config: &Config{
				Host:            "localhost",
				Port:            8080,
				ReadTimeout:     0,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "negative write timeout",
			config: &Config{
				Host:            "localhost",
				Port:            8080,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    -1 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "zero idle timeout",
			config: &Config{
				Host:            "localhost",
				Port:            8080,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     0,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "negative shutdown timeout",
			config: &Config{
				Host:            "localhost",
				Port:            8080,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  1 << 20,
				ShutdownTimeout: -1 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
		{
			name: "zero max header bytes",
			config: &Config{
				Host:            "localhost",
				Port:            8080,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     60 * time.Second,
				MaxHeaderBytes:  0,
				ShutdownTimeout: 30 * time.Second,
				EnableGraphQL:   true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServerMiddleware(t *testing.T) {
	config := DefaultConfig()
	config.EnableCORS = true
	config.AllowedOrigins = []string{"http://localhost:3000"}

	logger := zap.NewNop()
	store := &mockStorage{}

	server, err := NewServer(config, logger, store)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Test CORS headers
	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()

	server.Router().ServeHTTP(w, req)

	// CORS middleware should handle OPTIONS requests
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS request returned wrong status code: got %v", w.Code)
	}
}

func TestConfigDefaults(t *testing.T) {
	config := DefaultConfig()

	// Verify default values
	if config.Host != "localhost" {
		t.Errorf("expected default host to be localhost, got %s", config.Host)
	}

	if config.Port != 8080 {
		t.Errorf("expected default port to be 8080, got %d", config.Port)
	}

	if !config.EnableGraphQL {
		t.Error("expected GraphQL to be enabled by default")
	}

	if !config.EnableJSONRPC {
		t.Error("expected JSON-RPC to be enabled by default")
	}

	if !config.EnableWebSocket {
		t.Error("expected WebSocket to be enabled by default")
	}

	// Test Address() method
	expectedAddr := "localhost:8080"
	if config.Address() != expectedAddr {
		t.Errorf("expected address %s, got %s", expectedAddr, config.Address())
	}
}
