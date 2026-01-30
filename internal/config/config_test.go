package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestNewConfig tests creating a config with defaults
func TestNewConfig(t *testing.T) {
	cfg := NewConfig()
	if cfg == nil {
		t.Fatal("NewConfig() returned nil")
	}

	// Check defaults
	if cfg.Log.Level != "info" {
		t.Errorf("Expected default log level 'info', got %q", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Expected default log format 'json', got %q", cfg.Log.Format)
	}
	if cfg.Indexer.Workers != 100 {
		t.Errorf("Expected default workers 100, got %d", cfg.Indexer.Workers)
	}
	if cfg.Indexer.ChunkSize != 100 {
		t.Errorf("Expected default chunk size 100, got %d", cfg.Indexer.ChunkSize)
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				RPC: RPCConfig{
					Endpoint: "http://localhost:8545",
					Timeout:  30 * time.Second,
				},
				Database: DatabaseConfig{
					Path: "/tmp/indexer-test",
				},
				Log: LogConfig{
					Level:  "info",
					Format: "json",
				},
				Indexer: IndexerConfig{
					Workers:   100,
					ChunkSize: 100,
				},
				EventBus: EventBusConfig{
					Type:              "local",
					PublishBufferSize: 1000,
					HistorySize:       100,
				},
				Node: NodeConfig{
					ID:   "test-node",
					Role: "all",
				},
			},
			wantErr: false,
		},
		{
			name: "missing RPC endpoint",
			config: &Config{
				RPC: RPCConfig{
					Endpoint: "",
					Timeout:  30 * time.Second,
				},
				Database: DatabaseConfig{
					Path: "/tmp/indexer-test",
				},
			},
			wantErr: true,
			errMsg:  "RPC endpoint is required",
		},
		{
			name: "missing database path",
			config: &Config{
				RPC: RPCConfig{
					Endpoint: "http://localhost:8545",
					Timeout:  30 * time.Second,
				},
				Database: DatabaseConfig{
					Path: "",
				},
			},
			wantErr: true,
			errMsg:  "database path is required",
		},
		{
			name: "invalid worker count",
			config: &Config{
				RPC: RPCConfig{
					Endpoint: "http://localhost:8545",
					Timeout:  30 * time.Second,
				},
				Database: DatabaseConfig{
					Path: "/tmp/indexer-test",
				},
				Log: LogConfig{
					Level:  "info",
					Format: "json",
				},
				Indexer: IndexerConfig{
					Workers:   0,
					ChunkSize: 100,
				},
			},
			wantErr: true,
			errMsg:  "worker count must be positive",
		},
		{
			name: "invalid chunk size",
			config: &Config{
				RPC: RPCConfig{
					Endpoint: "http://localhost:8545",
					Timeout:  30 * time.Second,
				},
				Database: DatabaseConfig{
					Path: "/tmp/indexer-test",
				},
				Log: LogConfig{
					Level:  "info",
					Format: "json",
				},
				Indexer: IndexerConfig{
					Workers:   100,
					ChunkSize: 0,
				},
			},
			wantErr: true,
			errMsg:  "chunk size must be positive",
		},
		{
			name: "invalid RPC timeout",
			config: &Config{
				RPC: RPCConfig{
					Endpoint: "http://localhost:8545",
					Timeout:  0,
				},
				Database: DatabaseConfig{
					Path: "/tmp/indexer-test",
				},
			},
			wantErr: true,
			errMsg:  "RPC timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.errMsg {
				t.Errorf("Validate() error message = %q, want %q", err.Error(), tt.errMsg)
			}
		})
	}
}

// TestLoadFromEnv tests loading configuration from environment variables
func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("INDEXER_RPC_ENDPOINT", "http://testnet:8545")
	os.Setenv("INDEXER_RPC_TIMEOUT", "60s")
	os.Setenv("INDEXER_DB_PATH", "/data/indexer")
	os.Setenv("INDEXER_LOG_LEVEL", "debug")
	os.Setenv("INDEXER_LOG_FORMAT", "console")
	os.Setenv("INDEXER_WORKERS", "200")
	os.Setenv("INDEXER_CHUNK_SIZE", "50")
	os.Setenv("INDEXER_API_CORS_ENABLED", "true")
	os.Setenv("INDEXER_API_CORS_ALLOWED_ORIGINS", "http://localhost:3001,https://app.example.com")
	defer func() {
		os.Unsetenv("INDEXER_RPC_ENDPOINT")
		os.Unsetenv("INDEXER_RPC_TIMEOUT")
		os.Unsetenv("INDEXER_DB_PATH")
		os.Unsetenv("INDEXER_LOG_LEVEL")
		os.Unsetenv("INDEXER_LOG_FORMAT")
		os.Unsetenv("INDEXER_WORKERS")
		os.Unsetenv("INDEXER_CHUNK_SIZE")
		os.Unsetenv("INDEXER_API_CORS_ENABLED")
		os.Unsetenv("INDEXER_API_CORS_ALLOWED_ORIGINS")
	}()

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.RPC.Endpoint != "http://testnet:8545" {
		t.Errorf("Expected RPC endpoint 'http://testnet:8545', got %q", cfg.RPC.Endpoint)
	}
	if cfg.RPC.Timeout != 60*time.Second {
		t.Errorf("Expected RPC timeout 60s, got %v", cfg.RPC.Timeout)
	}
	if cfg.Database.Path != "/data/indexer" {
		t.Errorf("Expected database path '/data/indexer', got %q", cfg.Database.Path)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Expected log level 'debug', got %q", cfg.Log.Level)
	}
	if cfg.Log.Format != "console" {
		t.Errorf("Expected log format 'console', got %q", cfg.Log.Format)
	}
	if cfg.Indexer.Workers != 200 {
		t.Errorf("Expected workers 200, got %d", cfg.Indexer.Workers)
	}
	if cfg.Indexer.ChunkSize != 50 {
		t.Errorf("Expected chunk size 50, got %d", cfg.Indexer.ChunkSize)
	}
	if !cfg.API.EnableCORS {
		t.Errorf("Expected API CORS enabled")
	}
	wantOrigins := []string{"http://localhost:3001", "https://app.example.com"}
	if !reflect.DeepEqual(cfg.API.AllowedOrigins, wantOrigins) {
		t.Errorf("Expected allowed origins %v, got %v", wantOrigins, cfg.API.AllowedOrigins)
	}
}

// TestLoadFromFile tests loading configuration from YAML file
func TestLoadFromFile(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
rpc:
  endpoint: http://localhost:9545
  timeout: 45s

database:
  path: /tmp/test-db
  readonly: false

log:
  level: warn
  format: json

indexer:
  workers: 150
  chunk_size: 75
  start_height: 0
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg := NewConfig()
	err = cfg.LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.RPC.Endpoint != "http://localhost:9545" {
		t.Errorf("Expected RPC endpoint 'http://localhost:9545', got %q", cfg.RPC.Endpoint)
	}
	if cfg.RPC.Timeout != 45*time.Second {
		t.Errorf("Expected RPC timeout 45s, got %v", cfg.RPC.Timeout)
	}
	if cfg.Database.Path != "/tmp/test-db" {
		t.Errorf("Expected database path '/tmp/test-db', got %q", cfg.Database.Path)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("Expected log level 'warn', got %q", cfg.Log.Level)
	}
	if cfg.Indexer.Workers != 150 {
		t.Errorf("Expected workers 150, got %d", cfg.Indexer.Workers)
	}
	if cfg.Indexer.ChunkSize != 75 {
		t.Errorf("Expected chunk size 75, got %d", cfg.Indexer.ChunkSize)
	}
}

// TestLoadFromFileNotFound tests loading from non-existent file
func TestLoadFromFileNotFound(t *testing.T) {
	cfg := NewConfig()
	err := cfg.LoadFromFile("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent file, got nil")
	}
}

// TestLoadFromFileInvalidYAML tests loading from invalid YAML file
func TestLoadFromFileInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid.yaml")

	invalidYAML := `
rpc:
  endpoint: "http://localhost:8545
  timeout: invalid
`

	err := os.WriteFile(configFile, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid config file: %v", err)
	}

	cfg := NewConfig()
	err = cfg.LoadFromFile(configFile)
	if err == nil {
		t.Error("Expected error when loading invalid YAML, got nil")
	}
}

// TestConfigPriority tests configuration priority (env > file > defaults)
func TestConfigPriority(t *testing.T) {
	// Create config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
rpc:
  endpoint: http://file:8545
  timeout: 30s

database:
  path: /file/db

log:
  level: info
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set environment variable (should override file)
	os.Setenv("INDEXER_RPC_ENDPOINT", "http://env:8545")
	defer os.Unsetenv("INDEXER_RPC_ENDPOINT")

	cfg := NewConfig()

	// Load from file first
	err = cfg.LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Then load from env (should override)
	err = cfg.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	// RPC endpoint should be from env
	if cfg.RPC.Endpoint != "http://env:8545" {
		t.Errorf("Expected RPC endpoint from env 'http://env:8545', got %q", cfg.RPC.Endpoint)
	}

	// Database path should be from file (no env override)
	if cfg.Database.Path != "/file/db" {
		t.Errorf("Expected database path from file '/file/db', got %q", cfg.Database.Path)
	}

	// Log level should be from file (no env override)
	if cfg.Log.Level != "info" {
		t.Errorf("Expected log level from file 'info', got %q", cfg.Log.Level)
	}
}

// TestSetDefaults tests setting default values
func TestSetDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	if cfg.Log.Level != "info" {
		t.Errorf("Expected default log level 'info', got %q", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Expected default log format 'json', got %q", cfg.Log.Format)
	}
	if cfg.Indexer.Workers != 100 {
		t.Errorf("Expected default workers 100, got %d", cfg.Indexer.Workers)
	}
	if cfg.Indexer.ChunkSize != 100 {
		t.Errorf("Expected default chunk size 100, got %d", cfg.Indexer.ChunkSize)
	}
	if cfg.RPC.Timeout != 30*time.Second {
		t.Errorf("Expected default RPC timeout 30s, got %v", cfg.RPC.Timeout)
	}
}

// TestLoadValidConfig tests the Load convenience function with valid config
func TestLoadValidConfig(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
rpc:
  endpoint: http://localhost:8545
  timeout: 30s

database:
  path: /tmp/test-db

log:
  level: info
  format: json

indexer:
  workers: 100
  chunk_size: 100
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RPC.Endpoint != "http://localhost:8545" {
		t.Errorf("Expected RPC endpoint 'http://localhost:8545', got %q", cfg.RPC.Endpoint)
	}
}

// TestLoadInvalidConfig tests the Load convenience function with invalid config
func TestLoadInvalidConfig(t *testing.T) {
	// Create temporary config file with missing required fields
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
log:
  level: info
  format: json
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err = Load(configFile)
	if err == nil {
		t.Error("Expected error when loading invalid config, got nil")
	}
}

// TestLoadWithEmptyFile tests Load with empty config file
func TestLoadWithEmptyFile(t *testing.T) {
	_, err := Load("")
	if err == nil {
		t.Error("Expected error when loading with no config and no env vars, got nil")
	}
}

// TestLoadWithEnvOverride tests Load with environment variable override
func TestLoadWithEnvOverride(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
rpc:
  endpoint: http://file:8545
  timeout: 30s

database:
  path: /file/db

log:
  level: info
  format: json
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set environment variable
	os.Setenv("INDEXER_RPC_ENDPOINT", "http://env:8545")
	defer os.Unsetenv("INDEXER_RPC_ENDPOINT")

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should use env value
	if cfg.RPC.Endpoint != "http://env:8545" {
		t.Errorf("Expected RPC endpoint from env 'http://env:8545', got %q", cfg.RPC.Endpoint)
	}
}

// TestValidateInvalidLogLevel tests validation with invalid log level
func TestValidateInvalidLogLevel(t *testing.T) {
	cfg := &Config{
		RPC: RPCConfig{
			Endpoint: "http://localhost:8545",
			Timeout:  30 * time.Second,
		},
		Database: DatabaseConfig{
			Path: "/tmp/test",
		},
		Log: LogConfig{
			Level:  "invalid",
			Format: "json",
		},
		Indexer: IndexerConfig{
			Workers:   100,
			ChunkSize: 100,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid log level, got nil")
	}
}

// TestValidateInvalidLogFormat tests validation with invalid log format
func TestValidateInvalidLogFormat(t *testing.T) {
	cfg := &Config{
		RPC: RPCConfig{
			Endpoint: "http://localhost:8545",
			Timeout:  30 * time.Second,
		},
		Database: DatabaseConfig{
			Path: "/tmp/test",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "invalid",
		},
		Indexer: IndexerConfig{
			Workers:   100,
			ChunkSize: 100,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid log format, got nil")
	}
}

// TestLoadFromEnvInvalidTimeout tests loading invalid timeout from env
func TestLoadFromEnvInvalidTimeout(t *testing.T) {
	os.Setenv("INDEXER_RPC_TIMEOUT", "invalid")
	defer os.Unsetenv("INDEXER_RPC_TIMEOUT")

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid timeout, got nil")
	}
}

// TestLoadFromEnvInvalidReadOnly tests loading invalid readonly from env
func TestLoadFromEnvInvalidReadOnly(t *testing.T) {
	os.Setenv("INDEXER_DB_READONLY", "invalid")
	defer os.Unsetenv("INDEXER_DB_READONLY")

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid readonly, got nil")
	}
}

// TestLoadFromEnvInvalidWorkers tests loading invalid workers from env
func TestLoadFromEnvInvalidWorkers(t *testing.T) {
	os.Setenv("INDEXER_WORKERS", "invalid")
	defer os.Unsetenv("INDEXER_WORKERS")

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid workers, got nil")
	}
}

// TestLoadFromEnvInvalidChunkSize tests loading invalid chunk size from env
func TestLoadFromEnvInvalidChunkSize(t *testing.T) {
	os.Setenv("INDEXER_CHUNK_SIZE", "invalid")
	defer os.Unsetenv("INDEXER_CHUNK_SIZE")

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid chunk size, got nil")
	}
}

// TestLoadFromEnvInvalidStartHeight tests loading invalid start height from env
func TestLoadFromEnvInvalidStartHeight(t *testing.T) {
	os.Setenv("INDEXER_START_HEIGHT", "invalid")
	defer os.Unsetenv("INDEXER_START_HEIGHT")

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid start height, got nil")
	}
}
