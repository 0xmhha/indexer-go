package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.Setenv("INDEXER_RPC_ENDPOINT", "http://testnet:8545"))
	require.NoError(t, os.Setenv("INDEXER_RPC_TIMEOUT", "60s"))
	require.NoError(t, os.Setenv("INDEXER_DB_PATH", "/data/indexer"))
	require.NoError(t, os.Setenv("INDEXER_LOG_LEVEL", "debug"))
	require.NoError(t, os.Setenv("INDEXER_LOG_FORMAT", "console"))
	require.NoError(t, os.Setenv("INDEXER_WORKERS", "200"))
	require.NoError(t, os.Setenv("INDEXER_CHUNK_SIZE", "50"))
	require.NoError(t, os.Setenv("INDEXER_API_CORS_ENABLED", "true"))
	require.NoError(t, os.Setenv("INDEXER_API_CORS_ALLOWED_ORIGINS", "http://localhost:3001,https://app.example.com"))
	defer func() {
		_ = os.Unsetenv("INDEXER_RPC_ENDPOINT")
		_ = os.Unsetenv("INDEXER_RPC_TIMEOUT")
		_ = os.Unsetenv("INDEXER_DB_PATH")
		_ = os.Unsetenv("INDEXER_LOG_LEVEL")
		_ = os.Unsetenv("INDEXER_LOG_FORMAT")
		_ = os.Unsetenv("INDEXER_WORKERS")
		_ = os.Unsetenv("INDEXER_CHUNK_SIZE")
		_ = os.Unsetenv("INDEXER_API_CORS_ENABLED")
		_ = os.Unsetenv("INDEXER_API_CORS_ALLOWED_ORIGINS")
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
	require.NoError(t, os.Setenv("INDEXER_RPC_ENDPOINT", "http://env:8545"))
	defer func() { _ = os.Unsetenv("INDEXER_RPC_ENDPOINT") }()

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
	require.NoError(t, os.Setenv("INDEXER_RPC_ENDPOINT", "http://env:8545"))
	defer func() { _ = os.Unsetenv("INDEXER_RPC_ENDPOINT") }()

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
	require.NoError(t, os.Setenv("INDEXER_RPC_TIMEOUT", "invalid"))
	defer func() { _ = os.Unsetenv("INDEXER_RPC_TIMEOUT") }()

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid timeout, got nil")
	}
}

// TestLoadFromEnvInvalidReadOnly tests loading invalid readonly from env
func TestLoadFromEnvInvalidReadOnly(t *testing.T) {
	require.NoError(t, os.Setenv("INDEXER_DB_READONLY", "invalid"))
	defer func() { _ = os.Unsetenv("INDEXER_DB_READONLY") }()

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid readonly, got nil")
	}
}

// TestLoadFromEnvInvalidWorkers tests loading invalid workers from env
func TestLoadFromEnvInvalidWorkers(t *testing.T) {
	require.NoError(t, os.Setenv("INDEXER_WORKERS", "invalid"))
	defer func() { _ = os.Unsetenv("INDEXER_WORKERS") }()

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid workers, got nil")
	}
}

// TestLoadFromEnvInvalidChunkSize tests loading invalid chunk size from env
func TestLoadFromEnvInvalidChunkSize(t *testing.T) {
	require.NoError(t, os.Setenv("INDEXER_CHUNK_SIZE", "invalid"))
	defer func() { _ = os.Unsetenv("INDEXER_CHUNK_SIZE") }()

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid chunk size, got nil")
	}
}

// TestLoadFromEnvInvalidStartHeight tests loading invalid start height from env
func TestLoadFromEnvInvalidStartHeight(t *testing.T) {
	require.NoError(t, os.Setenv("INDEXER_START_HEIGHT", "invalid"))
	defer func() { _ = os.Unsetenv("INDEXER_START_HEIGHT") }()

	cfg := NewConfig()
	err := cfg.LoadFromEnv()
	if err == nil {
		t.Error("Expected error for invalid start height, got nil")
	}
}

func TestEventBusBufferSizes(t *testing.T) {
	cfg := NewConfig()
	if cfg.EventBus.PublishBufferSize != constants.DefaultEventBusPublishBuffer ||
		cfg.EventBus.SubscriberBufferSize != constants.DefaultEventBusSubscriberBuffer {
		t.Fatalf("defaults: publish %d subscriber %d", cfg.EventBus.PublishBufferSize, cfg.EventBus.SubscriberBufferSize)
	}

	t.Setenv("INDEXER_EVENTBUS_PUBLISH_BUFFER_SIZE", "1234")
	t.Setenv("INDEXER_EVENTBUS_SUBSCRIBER_BUFFER_SIZE", "567")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.EventBus.PublishBufferSize != 1234 || cfg.EventBus.SubscriberBufferSize != 567 {
		t.Fatalf("env: publish %d subscriber %d", cfg.EventBus.PublishBufferSize, cfg.EventBus.SubscriberBufferSize)
	}

	t.Setenv("INDEXER_EVENTBUS_SUBSCRIBER_BUFFER_SIZE", "x")
	if err := cfg.LoadFromEnv(); err == nil {
		t.Fatal("invalid subscriber buffer size accepted")
	}
}

func TestFinalityConfig(t *testing.T) {
	cfg := NewConfig()
	cfg.RPC.Endpoint = "http://localhost:8545"
	cfg.Database.Path = t.TempDir()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default finality rejected: %v", err)
	}
	for _, tc := range []struct {
		finality      string
		confirmations uint64
		ok            bool
	}{
		{"head", 0, true},
		{"finalized", 0, true},
		{"confirmations", 12, true},
		{"confirmations", 0, false},
		{"safe", 0, false},
	} {
		cfg.Indexer.Finality, cfg.Indexer.Confirmations = tc.finality, tc.confirmations
		if err := cfg.Validate(); (err == nil) != tc.ok {
			t.Errorf("finality %q confirmations %d: err %v, want ok %v", tc.finality, tc.confirmations, err, tc.ok)
		}
	}

	t.Setenv("INDEXER_FINALITY", "confirmations")
	t.Setenv("INDEXER_CONFIRMATIONS", "6")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.Indexer.Finality != "confirmations" || cfg.Indexer.Confirmations != 6 {
		t.Fatalf("env: finality %q confirmations %d", cfg.Indexer.Finality, cfg.Indexer.Confirmations)
	}
	t.Setenv("INDEXER_CONFIRMATIONS", "-1")
	if err := cfg.LoadFromEnv(); err == nil {
		t.Fatal("invalid confirmations accepted")
	}
}

func TestOrphanRetention(t *testing.T) {
	cfg := NewConfig()
	if cfg.Indexer.OrphanRetention != 1000 {
		t.Fatalf("default orphan retention = %d, want 1000", cfg.Indexer.OrphanRetention)
	}
	t.Setenv("INDEXER_ORPHAN_RETENTION", "0")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.Indexer.OrphanRetention != 0 {
		t.Fatalf("orphan retention = %d, want 0 (keep all)", cfg.Indexer.OrphanRetention)
	}
	t.Setenv("INDEXER_ORPHAN_RETENTION", "many")
	if err := cfg.LoadFromEnv(); err == nil {
		t.Fatal("expected an error for an invalid INDEXER_ORPHAN_RETENTION")
	}
}

// TestDatabaseDriver checks database.driver: pebble (the default) needs a
// path, postgres a DSN, and other drivers are refused; the environment sets
// the PostgreSQL settings.
func TestDatabaseDriver(t *testing.T) {
	cfg := NewConfig()
	cfg.RPC.Endpoint = "http://localhost:8545"
	cfg.Database.Path = ""
	if err := cfg.Validate(); err == nil {
		t.Error("pebble without a path: expected an error")
	}

	cfg.Database.Driver = DriverPostgres
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "dsn") {
		t.Errorf("postgres without a DSN: got %v", err)
	}
	t.Setenv("INDEXER_DB_POSTGRES_DSN", "postgres://indexer@localhost/indexer")
	t.Setenv("INDEXER_DB_POSTGRES_SCHEMA", "idx")
	t.Setenv("INDEXER_DB_POSTGRES_MAX_CONNS", "8")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("postgres with a DSN and no path: %v", err)
	}
	if cfg.Database.Postgres.Schema != "idx" || cfg.Database.Postgres.MaxConns != 8 {
		t.Errorf("postgres settings from the environment: %+v", cfg.Database.Postgres)
	}

	t.Setenv("INDEXER_DB_DRIVER", "sqlite")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err == nil {
		t.Error("unknown driver: expected an error")
	}
	t.Setenv("INDEXER_DB_POSTGRES_MAX_CONNS", "many")
	if err := cfg.LoadFromEnv(); err == nil {
		t.Error("invalid INDEXER_DB_POSTGRES_MAX_CONNS: expected an error")
	}
}

// TestDatabaseCache checks database.cache_mb from the environment and its
// validation.
func TestDatabaseCache(t *testing.T) {
	cfg := NewConfig()
	cfg.RPC.Endpoint = "http://localhost:8545"
	cfg.Database.Path = "/tmp/indexer"
	t.Setenv("INDEXER_DB_CACHE_MB", "1024")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.Database.CacheMB != 1024 {
		t.Errorf("cache from the environment: %d", cfg.Database.CacheMB)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("a cache size: %v", err)
	}
	cfg.Database.CacheMB = -1
	if err := cfg.Validate(); err == nil {
		t.Error("negative cache: expected an error")
	}
	t.Setenv("INDEXER_DB_CACHE_MB", "big")
	if err := cfg.LoadFromEnv(); err == nil {
		t.Error("invalid INDEXER_DB_CACHE_MB: expected an error")
	}
}

// TestNodeRole checks node.role: all (the default), ingest and api, with the
// former names writer and reader; an API process needs PostgreSQL, and
// multi-chain mode runs every role in one process.
func TestNodeRole(t *testing.T) {
	cfg := NewConfig()
	cfg.RPC.Endpoint = "http://localhost:8545"
	cfg.Database.Path = "/tmp/indexer"
	if cfg.NodeRole() != RoleAll {
		t.Errorf("default role %q, want %q", cfg.NodeRole(), RoleAll)
	}
	for role, want := range map[string]string{"writer": RoleIngest, "reader": RoleAPI, RoleIngest: RoleIngest, RoleAPI: RoleAPI} {
		cfg.Node.Role = role
		if got := cfg.NodeRole(); got != want {
			t.Errorf("role %q is %q, want %q", role, got, want)
		}
	}

	cfg.Node.Role = RoleIngest
	if err := cfg.Validate(); err != nil {
		t.Errorf("ingest on pebble: %v", err)
	}
	cfg.Node.Role = RoleAPI
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), DriverPostgres) {
		t.Errorf("api on pebble: got %v, want an error naming %s", err, DriverPostgres)
	}
	cfg.Database.Driver = DriverPostgres
	cfg.Database.Postgres.DSN = "postgres://indexer@localhost/indexer"
	if err := cfg.Validate(); err != nil {
		t.Errorf("api on postgres: %v", err)
	}
	cfg.Node.Role = "observer"
	if err := cfg.Validate(); err == nil {
		t.Error("unknown role: expected an error")
	}
}

// TestAPISecurity: the API's rate limit and GraphQL bounds are on by
// default, a file or the environment can turn them off, and trusted
// proxies must be addresses or CIDR ranges (refactoring plan R4-3).
func TestAPISecurity(t *testing.T) {
	cfg := NewConfig()
	cfg.RPC.Endpoint = "http://localhost:8545"
	cfg.Database.Path = "/tmp/indexer"
	if !cfg.API.RateLimit.Enabled || cfg.API.RateLimit.PerSecond <= 0 || cfg.API.RateLimit.Burst <= 0 {
		t.Errorf("rate limit is not on by default: %+v", cfg.API.RateLimit)
	}
	if cfg.API.GraphQL.MaxDepth <= 0 || cfg.API.GraphQL.MaxComplexity <= 0 {
		t.Errorf("GraphQL bounds are not on by default: %+v", cfg.API.GraphQL)
	}
	if !cfg.API.EnableREST {
		t.Error("the REST API is not on by default")
	}
	if len(cfg.API.TrustedProxies) != 0 {
		t.Errorf("no proxy is trusted by default: %v", cfg.API.TrustedProxies)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	yml := "api:\n  trusted_proxies: [\"10.0.0.0/8\", \"192.0.2.1\"]\n  rate_limit:\n    enabled: false\n  graphql:\n    max_depth: 0\n    max_complexity: 300\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatal(err)
	}
	if cfg.API.RateLimit.Enabled || cfg.API.GraphQL.MaxDepth != 0 || cfg.API.GraphQL.MaxComplexity != 300 || len(cfg.API.TrustedProxies) != 2 {
		t.Errorf("file settings not applied: %+v", cfg.API)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid settings: %v", err)
	}

	t.Setenv("INDEXER_API_REST", "false")
	t.Setenv("INDEXER_API_TRUSTED_PROXIES", "127.0.0.1, ::1")
	t.Setenv("INDEXER_API_RATE_LIMIT_ENABLED", "true")
	t.Setenv("INDEXER_API_RATE_LIMIT_PER_SECOND", "5")
	t.Setenv("INDEXER_API_RATE_LIMIT_BURST", "10")
	t.Setenv("INDEXER_API_GRAPHQL_MAX_DEPTH", "8")
	t.Setenv("INDEXER_API_GRAPHQL_MAX_COMPLEXITY", "900")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.API.EnableREST {
		t.Error("INDEXER_API_REST=false not applied")
	}
	want := APIRateLimitConfig{Enabled: true, PerSecond: 5, Burst: 10}
	if cfg.API.RateLimit != want || cfg.API.GraphQL != (APIGraphQLConfig{MaxDepth: 8, MaxComplexity: 900}) ||
		strings.Join(cfg.API.TrustedProxies, ",") != "127.0.0.1,::1" {
		t.Errorf("environment not applied: %+v", cfg.API)
	}

	for name, mutate := range map[string]func(*APIConfig){
		"proxy host name": func(a *APIConfig) { a.TrustedProxies = []string{"proxy.local"} },
		"zero rate":       func(a *APIConfig) { a.RateLimit.PerSecond = 0 },
		"negative bound":  func(a *APIConfig) { a.GraphQL.MaxComplexity = -1 },
	} {
		bad := *cfg
		bad.API.TrustedProxies = append([]string(nil), cfg.API.TrustedProxies...)
		mutate(&bad.API)
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestFeatureSettings: a feature reads its own section; "enabled" is not
// one of its settings, unknown keys are an error, and the environment
// turning a feature on keeps the section.
func TestFeatureSettings(t *testing.T) {
	type venue struct {
		Type    string `yaml:"type"`
		Factory string `yaml:"factory"`
	}
	type settings struct {
		Venues []venue `yaml:"venues"`
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	yml := "features:\n  dex.pools:\n    enabled: false\n    venues:\n      - type: uniswap_v3\n        factory: \"0x01\"\n  typo:\n    venuez: []\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatal(err)
	}
	var got settings
	if err := cfg.FeatureSettings("dex.pools", &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Venues) != 1 || got.Venues[0] != (venue{"uniswap_v3", "0x01"}) {
		t.Errorf("settings %+v", got)
	}
	if err := cfg.FeatureSettings("typo", &got); err == nil || !strings.Contains(err.Error(), "venuez") {
		t.Errorf("unknown key: got %v", err)
	}
	var none settings
	if err := cfg.FeatureSettings("absent", &none); err != nil || none.Venues != nil {
		t.Errorf("absent section: %v %+v", err, none)
	}

	t.Setenv("INDEXER_FEATURES", "dex.pools")
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatal(err)
	}
	if !cfg.FeatureOverrides()["dex.pools"] {
		t.Error("the environment did not turn the feature on")
	}
	got = settings{}
	if err := cfg.FeatureSettings("dex.pools", &got); err != nil || len(got.Venues) != 1 {
		t.Errorf("settings lost after the environment: %v %+v", err, got)
	}
}
