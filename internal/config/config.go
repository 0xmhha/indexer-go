package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the indexer
type Config struct {
	RPC      RPCConfig      `yaml:"rpc"`
	Database DatabaseConfig `yaml:"database"`
	Log      LogConfig      `yaml:"log"`
	Indexer  IndexerConfig  `yaml:"indexer"`
	API      APIConfig      `yaml:"api"`
}

// RPCConfig holds RPC client configuration
type RPCConfig struct {
	Endpoint string        `yaml:"endpoint"`
	Timeout  time.Duration `yaml:"timeout"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path     string `yaml:"path"`
	ReadOnly bool   `yaml:"readonly"`
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// IndexerConfig holds indexer-specific configuration
type IndexerConfig struct {
	Workers     int    `yaml:"workers"`
	ChunkSize   int    `yaml:"chunk_size"`
	StartHeight uint64 `yaml:"start_height"`
}

// APIConfig holds API server configuration
type APIConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Host            string   `yaml:"host"`
	Port            int      `yaml:"port"`
	EnableGraphQL   bool     `yaml:"enable_graphql"`
	EnableJSONRPC   bool     `yaml:"enable_jsonrpc"`
	EnableWebSocket bool     `yaml:"enable_websocket"`
	EnableCORS      bool     `yaml:"enable_cors"`
	AllowedOrigins  []string `yaml:"allowed_origins"`
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	cfg := &Config{}
	cfg.SetDefaults()
	return cfg
}

// SetDefaults sets default values for the configuration
func (c *Config) SetDefaults() {
	// RPC defaults
	if c.RPC.Timeout == 0 {
		c.RPC.Timeout = 30 * time.Second
	}

	// Log defaults
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}

	// Indexer defaults
	if c.Indexer.Workers == 0 {
		c.Indexer.Workers = 100
	}
	if c.Indexer.ChunkSize == 0 {
		c.Indexer.ChunkSize = 100
	}

	// API defaults
	if c.API.Host == "" {
		c.API.Host = "localhost"
	}
	if c.API.Port == 0 {
		c.API.Port = 8080
	}
	if c.API.AllowedOrigins == nil {
		c.API.AllowedOrigins = []string{"*"}
	}
}

// LoadFromEnv loads configuration from environment variables
// Environment variables take precedence over file configuration
func (c *Config) LoadFromEnv() error {
	// RPC configuration
	if endpoint := os.Getenv("INDEXER_RPC_ENDPOINT"); endpoint != "" {
		c.RPC.Endpoint = endpoint
	}
	if timeout := os.Getenv("INDEXER_RPC_TIMEOUT"); timeout != "" {
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_RPC_TIMEOUT: %w", err)
		}
		c.RPC.Timeout = duration
	}

	// Database configuration
	if path := os.Getenv("INDEXER_DB_PATH"); path != "" {
		c.Database.Path = path
	}
	if readonly := os.Getenv("INDEXER_DB_READONLY"); readonly != "" {
		val, err := strconv.ParseBool(readonly)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_DB_READONLY: %w", err)
		}
		c.Database.ReadOnly = val
	}

	// Log configuration
	if level := os.Getenv("INDEXER_LOG_LEVEL"); level != "" {
		c.Log.Level = level
	}
	if format := os.Getenv("INDEXER_LOG_FORMAT"); format != "" {
		c.Log.Format = format
	}

	// Indexer configuration
	if workers := os.Getenv("INDEXER_WORKERS"); workers != "" {
		val, err := strconv.Atoi(workers)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_WORKERS: %w", err)
		}
		c.Indexer.Workers = val
	}
	if chunkSize := os.Getenv("INDEXER_CHUNK_SIZE"); chunkSize != "" {
		val, err := strconv.Atoi(chunkSize)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_CHUNK_SIZE: %w", err)
		}
		c.Indexer.ChunkSize = val
	}
	if startHeight := os.Getenv("INDEXER_START_HEIGHT"); startHeight != "" {
		val, err := strconv.ParseUint(startHeight, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_START_HEIGHT: %w", err)
		}
		c.Indexer.StartHeight = val
	}

	// API configuration
	if enabled := os.Getenv("INDEXER_API_ENABLED"); enabled != "" {
		val, err := strconv.ParseBool(enabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_ENABLED: %w", err)
		}
		c.API.Enabled = val
	}
	if host := os.Getenv("INDEXER_API_HOST"); host != "" {
		c.API.Host = host
	}
	if port := os.Getenv("INDEXER_API_PORT"); port != "" {
		val, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_PORT: %w", err)
		}
		c.API.Port = val
	}
	if enableGraphQL := os.Getenv("INDEXER_API_GRAPHQL"); enableGraphQL != "" {
		val, err := strconv.ParseBool(enableGraphQL)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_GRAPHQL: %w", err)
		}
		c.API.EnableGraphQL = val
	}
	if enableJSONRPC := os.Getenv("INDEXER_API_JSONRPC"); enableJSONRPC != "" {
		val, err := strconv.ParseBool(enableJSONRPC)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_JSONRPC: %w", err)
		}
		c.API.EnableJSONRPC = val
	}
	if enableWebSocket := os.Getenv("INDEXER_API_WEBSOCKET"); enableWebSocket != "" {
		val, err := strconv.ParseBool(enableWebSocket)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_WEBSOCKET: %w", err)
		}
		c.API.EnableWebSocket = val
	}

	return nil
}

// LoadFromFile loads configuration from a YAML file
func (c *Config) LoadFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate RPC configuration
	if c.RPC.Endpoint == "" {
		return fmt.Errorf("RPC endpoint is required")
	}
	if c.RPC.Timeout <= 0 {
		return fmt.Errorf("RPC timeout must be positive")
	}

	// Validate database configuration
	if c.Database.Path == "" {
		return fmt.Errorf("database path is required")
	}

	// Validate log configuration
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.Log.Level] {
		return fmt.Errorf("invalid log level %q, must be one of: debug, info, warn, error", c.Log.Level)
	}

	validLogFormats := map[string]bool{
		"json":    true,
		"console": true,
	}
	if !validLogFormats[c.Log.Format] {
		return fmt.Errorf("invalid log format %q, must be one of: json, console", c.Log.Format)
	}

	// Validate indexer configuration
	if c.Indexer.Workers <= 0 {
		return fmt.Errorf("worker count must be positive")
	}
	if c.Indexer.ChunkSize <= 0 {
		return fmt.Errorf("chunk size must be positive")
	}

	return nil
}

// Load is a convenience method that loads configuration in the following order:
// 1. Set defaults
// 2. Load from file (if provided)
// 3. Load from environment variables (override file)
// 4. Validate
func Load(configFile string) (*Config, error) {
	cfg := NewConfig()

	// Load from file if provided
	if configFile != "" {
		if err := cfg.LoadFromFile(configFile); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	}

	// Load from environment variables (override file)
	if err := cfg.LoadFromEnv(); err != nil {
		return nil, fmt.Errorf("failed to load config from environment: %w", err)
	}

	// Set defaults for any missing values
	cfg.SetDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}
