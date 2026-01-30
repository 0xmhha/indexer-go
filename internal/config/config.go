package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/internal/constants"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the indexer
type Config struct {
	RPC             RPCConfig             `yaml:"rpc"`
	Database        DatabaseConfig        `yaml:"database"`
	Log             LogConfig             `yaml:"log"`
	Indexer         IndexerConfig         `yaml:"indexer"`
	API             APIConfig             `yaml:"api"`
	SystemContracts SystemContractsConfig `yaml:"system_contracts"`
	MultiChain      MultiChainConfig      `yaml:"multichain"`
	Watchlist       WatchlistConfig       `yaml:"watchlist"`
	Resilience      ResilienceConfig      `yaml:"resilience"`
	Notifications   NotificationsConfig   `yaml:"notifications"`
	EventBus        EventBusConfig        `yaml:"eventbus"`
	Node            NodeConfig            `yaml:"node"`
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

// SystemContractsConfig holds system contracts verification configuration
type SystemContractsConfig struct {
	// Enabled determines whether to initialize system contract verifications
	Enabled bool `yaml:"enabled"`
	// SourcePath is the path to the directory containing v1/*.sol files
	// e.g., "/path/to/go-stablenet/systemcontracts/solidity"
	SourcePath string `yaml:"source_path"`
	// IncludeAbstracts determines whether to include abstract contracts in the source code
	IncludeAbstracts bool `yaml:"include_abstracts"`
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
	Enabled                  bool     `yaml:"enabled"`
	Host                     string   `yaml:"host"`
	Port                     int      `yaml:"port"`
	EnableGraphQL            bool     `yaml:"enable_graphql"`
	EnableJSONRPC            bool     `yaml:"enable_jsonrpc"`
	EnableWebSocket          bool     `yaml:"enable_websocket"`
	EnableWebSocketKeepAlive bool     `yaml:"enable_websocket_keepalive"`
	EnableCORS               bool     `yaml:"enable_cors"`
	AllowedOrigins           []string `yaml:"allowed_origins"`
}

// MultiChainConfig holds configuration for multi-chain support
type MultiChainConfig struct {
	// Enabled indicates whether multi-chain mode is active
	Enabled bool `yaml:"enabled"`
	// Chains is the list of chain configurations
	Chains []ChainConfig `yaml:"chains"`
	// HealthCheckInterval is how often to check chain health
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
	// MaxUnhealthyDuration is how long a chain can be unhealthy before stopping
	MaxUnhealthyDuration time.Duration `yaml:"max_unhealthy_duration"`
	// AutoRestart indicates whether to automatically restart failed chains
	AutoRestart bool `yaml:"auto_restart"`
	// AutoRestartDelay is the delay before auto-restarting a failed chain
	AutoRestartDelay time.Duration `yaml:"auto_restart_delay"`
}

// ChainConfig defines the configuration for a single blockchain connection
type ChainConfig struct {
	// ID is a unique identifier for this chain instance
	ID string `yaml:"id"`
	// Name is a human-readable name for the chain
	Name string `yaml:"name"`
	// RPCEndpoint is the HTTP(S) JSON-RPC endpoint URL
	RPCEndpoint string `yaml:"rpc_endpoint"`
	// WSEndpoint is the optional WebSocket endpoint URL
	WSEndpoint string `yaml:"ws_endpoint,omitempty"`
	// ChainID is the numeric chain ID
	ChainID uint64 `yaml:"chain_id"`
	// AdapterType specifies which adapter to use: "auto", "evm", "stableone", "anvil"
	AdapterType string `yaml:"adapter_type"`
	// StartHeight is the block height to start indexing from
	StartHeight uint64 `yaml:"start_height"`
	// Enabled indicates whether this chain should be active
	Enabled bool `yaml:"enabled"`
	// Workers is the number of concurrent fetch workers
	Workers int `yaml:"workers,omitempty"`
	// BatchSize is the number of blocks to fetch per batch
	BatchSize int `yaml:"batch_size,omitempty"`
	// RPCTimeout is the timeout for RPC calls
	RPCTimeout time.Duration `yaml:"rpc_timeout,omitempty"`
}

// WatchlistConfig holds configuration for the address watchlist service
type WatchlistConfig struct {
	// Enabled indicates whether the watchlist service is active
	Enabled bool `yaml:"enabled"`
	// BloomFilter holds bloom filter configuration
	BloomFilter BloomFilterConfig `yaml:"bloom_filter"`
	// History holds event history configuration
	History HistoryConfig `yaml:"history"`
}

// BloomFilterConfig holds bloom filter optimization settings
type BloomFilterConfig struct {
	// ExpectedItems is the expected number of addresses to monitor
	ExpectedItems int `yaml:"expected_items"`
	// FalsePositiveRate is the target false positive rate
	FalsePositiveRate float64 `yaml:"false_positive_rate"`
}

// HistoryConfig holds event history retention settings
type HistoryConfig struct {
	// RetentionPeriod is how long to keep historical events
	RetentionPeriod time.Duration `yaml:"retention"`
}

// ResilienceConfig holds WebSocket resilience configuration
type ResilienceConfig struct {
	// Enabled indicates whether WebSocket resilience is active
	Enabled bool `yaml:"enabled"`
	// Session holds session management configuration
	Session SessionConfig `yaml:"session"`
	// EventCache holds event cache configuration
	EventCache EventCacheConfig `yaml:"event_cache"`
}

// SessionConfig holds session management settings
type SessionConfig struct {
	// TTL is the session time-to-live
	TTL time.Duration `yaml:"ttl"`
	// CleanupPeriod is how often to clean up expired sessions
	CleanupPeriod time.Duration `yaml:"cleanup_period"`
}

// EventCacheConfig holds event cache settings
type EventCacheConfig struct {
	// Window is the time window for event caching (for replay)
	Window time.Duration `yaml:"window"`
	// Backend is the cache storage backend: "pebble" or "redis"
	Backend string `yaml:"backend"`
	// Redis holds Redis-specific configuration (if backend is "redis")
	Redis *RedisConfig `yaml:"redis,omitempty"`
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	// Addr is the Redis server address (for standalone mode)
	Addr string `yaml:"addr"`
	// Password is the optional Redis password
	Password string `yaml:"password,omitempty"`
	// DB is the Redis database number
	DB int `yaml:"db"`
}

// EventBusConfig holds EventBus configuration for distributed operations
type EventBusConfig struct {
	// Type is the event bus type: "local", "redis", "kafka", "hybrid"
	Type string `yaml:"type"`
	// PublishBufferSize is the size of the publish buffer
	PublishBufferSize int `yaml:"publish_buffer_size"`
	// HistorySize is the number of events to keep in history for replay
	HistorySize int `yaml:"history_size"`
	// Redis holds Redis EventBus configuration
	Redis EventBusRedisConfig `yaml:"redis"`
	// Kafka holds Kafka EventBus configuration
	Kafka EventBusKafkaConfig `yaml:"kafka"`
}

// EventBusRedisConfig holds Redis Pub/Sub EventBus configuration
type EventBusRedisConfig struct {
	// Enabled indicates whether Redis EventBus is active
	Enabled bool `yaml:"enabled"`
	// Addresses is the list of Redis server addresses (supports cluster mode)
	Addresses []string `yaml:"addresses"`
	// Password is the Redis password
	Password string `yaml:"password,omitempty"`
	// DB is the Redis database number (ignored in cluster mode)
	DB int `yaml:"db"`
	// PoolSize is the maximum number of socket connections
	PoolSize int `yaml:"pool_size"`
	// MinIdleConns is the minimum number of idle connections
	MinIdleConns int `yaml:"min_idle_conns"`
	// MaxRetries is the maximum number of retries before giving up
	MaxRetries int `yaml:"max_retries"`
	// DialTimeout is the timeout for establishing new connections
	DialTimeout time.Duration `yaml:"dial_timeout"`
	// ReadTimeout is the timeout for socket reads
	ReadTimeout time.Duration `yaml:"read_timeout"`
	// WriteTimeout is the timeout for socket writes
	WriteTimeout time.Duration `yaml:"write_timeout"`
	// ChannelPrefix is the prefix for Redis Pub/Sub channels
	ChannelPrefix string `yaml:"channel_prefix"`
	// TLS holds TLS configuration for secure connections
	TLS TLSConfig `yaml:"tls"`
	// ClusterMode indicates whether to use Redis Cluster
	ClusterMode bool `yaml:"cluster_mode"`
}

// EventBusKafkaConfig holds Kafka EventBus configuration
type EventBusKafkaConfig struct {
	// Enabled indicates whether Kafka EventBus is active
	Enabled bool `yaml:"enabled"`
	// Brokers is the list of Kafka broker addresses
	Brokers []string `yaml:"brokers"`
	// Topic is the Kafka topic for events
	Topic string `yaml:"topic"`
	// GroupID is the consumer group ID
	GroupID string `yaml:"group_id"`
	// ClientID is the client ID for this producer
	ClientID string `yaml:"client_id"`
	// SecurityProtocol is the security protocol: "PLAINTEXT", "SSL", "SASL_PLAINTEXT", "SASL_SSL"
	SecurityProtocol string `yaml:"security_protocol"`
	// SASLMechanism is the SASL mechanism: "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"
	SASLMechanism string `yaml:"sasl_mechanism"`
	// SASLUsername is the SASL username
	SASLUsername string `yaml:"sasl_username,omitempty"`
	// SASLPassword is the SASL password
	SASLPassword string `yaml:"sasl_password,omitempty"`
	// BatchSize is the maximum size of a message batch
	BatchSize int `yaml:"batch_size"`
	// LingerMs is the time to wait for the batch to fill
	LingerMs int `yaml:"linger_ms"`
	// Compression is the compression type: "none", "gzip", "snappy", "lz4", "zstd"
	Compression string `yaml:"compression"`
	// RequiredAcks is the number of acknowledgments required: 0, 1, -1 (all)
	RequiredAcks int `yaml:"required_acks"`
	// TLS holds TLS configuration for secure connections
	TLS TLSConfig `yaml:"tls"`
}

// TLSConfig holds TLS configuration for secure connections
type TLSConfig struct {
	// Enabled indicates whether TLS is enabled
	Enabled bool `yaml:"enabled"`
	// CertFile is the path to the client certificate file
	CertFile string `yaml:"cert_file,omitempty"`
	// KeyFile is the path to the client key file
	KeyFile string `yaml:"key_file,omitempty"`
	// CAFile is the path to the CA certificate file
	CAFile string `yaml:"ca_file,omitempty"`
	// InsecureSkipVerify disables server certificate verification
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
	// ServerName is the expected server name for verification
	ServerName string `yaml:"server_name,omitempty"`
}

// NodeConfig holds configuration for multi-node deployment
type NodeConfig struct {
	// ID is the unique identifier for this node
	ID string `yaml:"id"`
	// Role is the node role: "writer", "reader", "all"
	Role string `yaml:"role"`
	// Priority is used for leader election (higher = more likely to be leader)
	Priority int `yaml:"priority"`
}

// NotificationsConfig holds notification service configuration
type NotificationsConfig struct {
	// Enabled indicates whether the notification service is active
	Enabled bool `yaml:"enabled"`
	// Webhook holds webhook-specific configuration
	Webhook WebhookNotificationConfig `yaml:"webhook"`
	// Email holds email-specific configuration
	Email EmailNotificationConfig `yaml:"email"`
	// Slack holds Slack-specific configuration
	Slack SlackNotificationConfig `yaml:"slack"`
	// Retry holds retry behavior configuration
	Retry RetryNotificationConfig `yaml:"retry"`
	// Queue holds notification queue configuration
	Queue QueueNotificationConfig `yaml:"queue"`
	// Storage holds notification storage configuration
	Storage StorageNotificationConfig `yaml:"storage"`
}

// WebhookNotificationConfig holds webhook notification settings
type WebhookNotificationConfig struct {
	// Enabled determines if webhook notifications are available
	Enabled bool `yaml:"enabled"`
	// Timeout for webhook HTTP requests
	Timeout time.Duration `yaml:"timeout"`
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int `yaml:"max_retries"`
	// MaxConcurrent is the maximum concurrent webhook deliveries
	MaxConcurrent int `yaml:"max_concurrent"`
	// AllowedHosts restricts webhook URLs to specific hosts (empty = allow all)
	AllowedHosts []string `yaml:"allowed_hosts"`
	// SignatureHeader is the header name for HMAC signature
	SignatureHeader string `yaml:"signature_header"`
}

// EmailNotificationConfig holds email notification settings
type EmailNotificationConfig struct {
	// Enabled determines if email notifications are available
	Enabled bool `yaml:"enabled"`
	// SMTPHost is the SMTP server hostname
	SMTPHost string `yaml:"smtp_host"`
	// SMTPPort is the SMTP server port
	SMTPPort int `yaml:"smtp_port"`
	// SMTPUsername for authentication
	SMTPUsername string `yaml:"smtp_username"`
	// SMTPPassword for authentication
	SMTPPassword string `yaml:"smtp_password"`
	// FromAddress is the sender email address
	FromAddress string `yaml:"from_address"`
	// FromName is the sender display name
	FromName string `yaml:"from_name"`
	// UseTLS enables TLS for SMTP connection
	UseTLS bool `yaml:"use_tls"`
	// MaxRecipients per email
	MaxRecipients int `yaml:"max_recipients"`
	// RateLimitPerMinute limits emails per minute
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
}

// SlackNotificationConfig holds Slack notification settings
type SlackNotificationConfig struct {
	// Enabled determines if Slack notifications are available
	Enabled bool `yaml:"enabled"`
	// Timeout for Slack API requests
	Timeout time.Duration `yaml:"timeout"`
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int `yaml:"max_retries"`
	// DefaultUsername is the default bot username
	DefaultUsername string `yaml:"default_username"`
	// DefaultIconEmoji is the default bot icon
	DefaultIconEmoji string `yaml:"default_icon_emoji"`
	// RateLimitPerMinute limits Slack messages per minute
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
}

// RetryNotificationConfig holds retry behavior configuration
type RetryNotificationConfig struct {
	// InitialDelay is the initial delay before first retry
	InitialDelay time.Duration `yaml:"initial_delay"`
	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration `yaml:"max_delay"`
	// Multiplier for exponential backoff
	Multiplier float64 `yaml:"multiplier"`
	// MaxAttempts is the maximum total attempts (including initial)
	MaxAttempts int `yaml:"max_attempts"`
}

// QueueNotificationConfig holds notification queue configuration
type QueueNotificationConfig struct {
	// BufferSize is the size of the notification queue buffer
	BufferSize int `yaml:"buffer_size"`
	// Workers is the number of concurrent delivery workers
	Workers int `yaml:"workers"`
	// BatchSize is the maximum batch size for processing
	BatchSize int `yaml:"batch_size"`
	// FlushInterval is how often to flush pending notifications
	FlushInterval time.Duration `yaml:"flush_interval"`
}

// StorageNotificationConfig holds notification storage configuration
type StorageNotificationConfig struct {
	// HistoryRetention is how long to keep delivery history
	HistoryRetention time.Duration `yaml:"history_retention"`
	// MaxSettingsPerUser limits notification settings per user
	MaxSettingsPerUser int `yaml:"max_settings_per_user"`
	// MaxPendingNotifications limits pending notifications
	MaxPendingNotifications int `yaml:"max_pending_notifications"`
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
		c.RPC.Timeout = constants.DefaultQueryTimeout
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
		c.Indexer.Workers = constants.DefaultNumWorkers
	}
	if c.Indexer.ChunkSize == 0 {
		c.Indexer.ChunkSize = constants.DefaultMaxPaginationLimit
	}

	// API defaults
	if c.API.Host == "" {
		c.API.Host = constants.DefaultAPIHost
	}
	if c.API.Port == 0 {
		c.API.Port = constants.DefaultAPIPort
	}
	if c.API.AllowedOrigins == nil {
		c.API.AllowedOrigins = []string{"*"}
	}

	// MultiChain defaults
	if c.MultiChain.HealthCheckInterval == 0 {
		c.MultiChain.HealthCheckInterval = 30 * time.Second
	}
	if c.MultiChain.MaxUnhealthyDuration == 0 {
		c.MultiChain.MaxUnhealthyDuration = 5 * time.Minute
	}
	if c.MultiChain.AutoRestartDelay == 0 {
		c.MultiChain.AutoRestartDelay = 30 * time.Second
	}

	// Watchlist defaults
	if c.Watchlist.BloomFilter.ExpectedItems == 0 {
		c.Watchlist.BloomFilter.ExpectedItems = 100000
	}
	if c.Watchlist.BloomFilter.FalsePositiveRate == 0 {
		c.Watchlist.BloomFilter.FalsePositiveRate = 0.0001
	}
	if c.Watchlist.History.RetentionPeriod == 0 {
		c.Watchlist.History.RetentionPeriod = 720 * time.Hour // 30 days
	}

	// Resilience defaults
	if c.Resilience.Session.TTL == 0 {
		c.Resilience.Session.TTL = 24 * time.Hour
	}
	if c.Resilience.Session.CleanupPeriod == 0 {
		c.Resilience.Session.CleanupPeriod = time.Hour
	}
	if c.Resilience.EventCache.Window == 0 {
		c.Resilience.EventCache.Window = time.Hour
	}
	if c.Resilience.EventCache.Backend == "" {
		c.Resilience.EventCache.Backend = "pebble"
	}

	// Notifications defaults
	if c.Notifications.Webhook.Timeout == 0 {
		c.Notifications.Webhook.Timeout = 10 * time.Second
	}
	if c.Notifications.Webhook.MaxRetries == 0 {
		c.Notifications.Webhook.MaxRetries = 3
	}
	if c.Notifications.Webhook.MaxConcurrent == 0 {
		c.Notifications.Webhook.MaxConcurrent = 10
	}
	if c.Notifications.Webhook.SignatureHeader == "" {
		c.Notifications.Webhook.SignatureHeader = "X-Signature-256"
	}
	if c.Notifications.Email.SMTPPort == 0 {
		c.Notifications.Email.SMTPPort = 587
	}
	if c.Notifications.Email.MaxRecipients == 0 {
		c.Notifications.Email.MaxRecipients = 10
	}
	if c.Notifications.Email.RateLimitPerMinute == 0 {
		c.Notifications.Email.RateLimitPerMinute = 60
	}
	if c.Notifications.Slack.Timeout == 0 {
		c.Notifications.Slack.Timeout = 10 * time.Second
	}
	if c.Notifications.Slack.MaxRetries == 0 {
		c.Notifications.Slack.MaxRetries = 3
	}
	if c.Notifications.Slack.DefaultUsername == "" {
		c.Notifications.Slack.DefaultUsername = "Indexer Bot"
	}
	if c.Notifications.Slack.DefaultIconEmoji == "" {
		c.Notifications.Slack.DefaultIconEmoji = ":robot_face:"
	}
	if c.Notifications.Slack.RateLimitPerMinute == 0 {
		c.Notifications.Slack.RateLimitPerMinute = 30
	}
	if c.Notifications.Retry.InitialDelay == 0 {
		c.Notifications.Retry.InitialDelay = time.Second
	}
	if c.Notifications.Retry.MaxDelay == 0 {
		c.Notifications.Retry.MaxDelay = 5 * time.Minute
	}
	if c.Notifications.Retry.Multiplier == 0 {
		c.Notifications.Retry.Multiplier = 2.0
	}
	if c.Notifications.Retry.MaxAttempts == 0 {
		c.Notifications.Retry.MaxAttempts = 5
	}
	if c.Notifications.Queue.BufferSize == 0 {
		c.Notifications.Queue.BufferSize = 1000
	}
	if c.Notifications.Queue.Workers == 0 {
		c.Notifications.Queue.Workers = 5
	}
	if c.Notifications.Queue.BatchSize == 0 {
		c.Notifications.Queue.BatchSize = 50
	}
	if c.Notifications.Queue.FlushInterval == 0 {
		c.Notifications.Queue.FlushInterval = time.Second
	}
	if c.Notifications.Storage.HistoryRetention == 0 {
		c.Notifications.Storage.HistoryRetention = 7 * 24 * time.Hour
	}
	if c.Notifications.Storage.MaxSettingsPerUser == 0 {
		c.Notifications.Storage.MaxSettingsPerUser = 100
	}
	if c.Notifications.Storage.MaxPendingNotifications == 0 {
		c.Notifications.Storage.MaxPendingNotifications = 10000
	}

	// EventBus defaults
	if c.EventBus.Type == "" {
		c.EventBus.Type = "local"
	}
	if c.EventBus.PublishBufferSize == 0 {
		c.EventBus.PublishBufferSize = 1000
	}
	if c.EventBus.HistorySize == 0 {
		c.EventBus.HistorySize = 100
	}
	// Redis EventBus defaults
	if c.EventBus.Redis.PoolSize == 0 {
		c.EventBus.Redis.PoolSize = 100
	}
	if c.EventBus.Redis.MinIdleConns == 0 {
		c.EventBus.Redis.MinIdleConns = 10
	}
	if c.EventBus.Redis.MaxRetries == 0 {
		c.EventBus.Redis.MaxRetries = 3
	}
	if c.EventBus.Redis.DialTimeout == 0 {
		c.EventBus.Redis.DialTimeout = 5 * time.Second
	}
	if c.EventBus.Redis.ReadTimeout == 0 {
		c.EventBus.Redis.ReadTimeout = 3 * time.Second
	}
	if c.EventBus.Redis.WriteTimeout == 0 {
		c.EventBus.Redis.WriteTimeout = 3 * time.Second
	}
	if c.EventBus.Redis.ChannelPrefix == "" {
		c.EventBus.Redis.ChannelPrefix = "indexer:events"
	}
	// Kafka EventBus defaults
	if c.EventBus.Kafka.Topic == "" {
		c.EventBus.Kafka.Topic = "indexer-events"
	}
	if c.EventBus.Kafka.GroupID == "" {
		c.EventBus.Kafka.GroupID = "indexer-group"
	}
	if c.EventBus.Kafka.SecurityProtocol == "" {
		c.EventBus.Kafka.SecurityProtocol = "PLAINTEXT"
	}
	if c.EventBus.Kafka.BatchSize == 0 {
		c.EventBus.Kafka.BatchSize = 16384
	}
	if c.EventBus.Kafka.LingerMs == 0 {
		c.EventBus.Kafka.LingerMs = 5
	}
	if c.EventBus.Kafka.Compression == "" {
		c.EventBus.Kafka.Compression = "snappy"
	}
	if c.EventBus.Kafka.RequiredAcks == 0 {
		c.EventBus.Kafka.RequiredAcks = -1 // All replicas
	}

	// Node defaults
	if c.Node.ID == "" {
		hostname, err := os.Hostname()
		if err == nil {
			c.Node.ID = hostname
		} else {
			c.Node.ID = "node-1"
		}
	}
	if c.Node.Role == "" {
		c.Node.Role = "all"
	}
	if c.Node.Priority == 0 {
		c.Node.Priority = 1
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
	if enableWebSocketKeepAlive := os.Getenv("INDEXER_API_WEBSOCKET_KEEPALIVE"); enableWebSocketKeepAlive != "" {
		val, err := strconv.ParseBool(enableWebSocketKeepAlive)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_WEBSOCKET_KEEPALIVE: %w", err)
		}
		c.API.EnableWebSocketKeepAlive = val
	}
	if enableCORS := os.Getenv("INDEXER_API_CORS_ENABLED"); enableCORS != "" {
		val, err := strconv.ParseBool(enableCORS)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_CORS_ENABLED: %w", err)
		}
		c.API.EnableCORS = val
	}
	if allowedOrigins := os.Getenv("INDEXER_API_CORS_ALLOWED_ORIGINS"); allowedOrigins != "" {
		origins := make([]string, 0)
		for _, origin := range strings.Split(allowedOrigins, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				origins = append(origins, origin)
			}
		}
		if len(origins) == 0 {
			origins = []string{"*"}
		}
		c.API.AllowedOrigins = origins
	}

	// System contracts configuration
	if enabled := os.Getenv("INDEXER_SYSTEM_CONTRACTS_ENABLED"); enabled != "" {
		val, err := strconv.ParseBool(enabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_SYSTEM_CONTRACTS_ENABLED: %w", err)
		}
		c.SystemContracts.Enabled = val
	}
	if sourcePath := os.Getenv("INDEXER_SYSTEM_CONTRACTS_SOURCE_PATH"); sourcePath != "" {
		c.SystemContracts.SourcePath = sourcePath
	}
	if includeAbstracts := os.Getenv("INDEXER_SYSTEM_CONTRACTS_INCLUDE_ABSTRACTS"); includeAbstracts != "" {
		val, err := strconv.ParseBool(includeAbstracts)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_SYSTEM_CONTRACTS_INCLUDE_ABSTRACTS: %w", err)
		}
		c.SystemContracts.IncludeAbstracts = val
	}

	// Notifications configuration
	if enabled := os.Getenv("INDEXER_NOTIFICATIONS_ENABLED"); enabled != "" {
		val, err := strconv.ParseBool(enabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_NOTIFICATIONS_ENABLED: %w", err)
		}
		c.Notifications.Enabled = val
	}
	if webhookEnabled := os.Getenv("INDEXER_NOTIFICATIONS_WEBHOOK_ENABLED"); webhookEnabled != "" {
		val, err := strconv.ParseBool(webhookEnabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_NOTIFICATIONS_WEBHOOK_ENABLED: %w", err)
		}
		c.Notifications.Webhook.Enabled = val
	}
	if emailEnabled := os.Getenv("INDEXER_NOTIFICATIONS_EMAIL_ENABLED"); emailEnabled != "" {
		val, err := strconv.ParseBool(emailEnabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_NOTIFICATIONS_EMAIL_ENABLED: %w", err)
		}
		c.Notifications.Email.Enabled = val
	}
	if smtpHost := os.Getenv("INDEXER_NOTIFICATIONS_SMTP_HOST"); smtpHost != "" {
		c.Notifications.Email.SMTPHost = smtpHost
	}
	if smtpPort := os.Getenv("INDEXER_NOTIFICATIONS_SMTP_PORT"); smtpPort != "" {
		val, err := strconv.Atoi(smtpPort)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_NOTIFICATIONS_SMTP_PORT: %w", err)
		}
		c.Notifications.Email.SMTPPort = val
	}
	if smtpUser := os.Getenv("INDEXER_NOTIFICATIONS_SMTP_USERNAME"); smtpUser != "" {
		c.Notifications.Email.SMTPUsername = smtpUser
	}
	if smtpPass := os.Getenv("INDEXER_NOTIFICATIONS_SMTP_PASSWORD"); smtpPass != "" {
		c.Notifications.Email.SMTPPassword = smtpPass
	}
	if fromAddr := os.Getenv("INDEXER_NOTIFICATIONS_EMAIL_FROM"); fromAddr != "" {
		c.Notifications.Email.FromAddress = fromAddr
	}
	if slackEnabled := os.Getenv("INDEXER_NOTIFICATIONS_SLACK_ENABLED"); slackEnabled != "" {
		val, err := strconv.ParseBool(slackEnabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_NOTIFICATIONS_SLACK_ENABLED: %w", err)
		}
		c.Notifications.Slack.Enabled = val
	}

	// EventBus configuration
	if ebType := os.Getenv("INDEXER_EVENTBUS_TYPE"); ebType != "" {
		c.EventBus.Type = ebType
	}
	if bufferSize := os.Getenv("INDEXER_EVENTBUS_PUBLISH_BUFFER_SIZE"); bufferSize != "" {
		val, err := strconv.Atoi(bufferSize)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_PUBLISH_BUFFER_SIZE: %w", err)
		}
		c.EventBus.PublishBufferSize = val
	}
	if historySize := os.Getenv("INDEXER_EVENTBUS_HISTORY_SIZE"); historySize != "" {
		val, err := strconv.Atoi(historySize)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_HISTORY_SIZE: %w", err)
		}
		c.EventBus.HistorySize = val
	}
	// Redis EventBus configuration
	if redisEnabled := os.Getenv("INDEXER_EVENTBUS_REDIS_ENABLED"); redisEnabled != "" {
		val, err := strconv.ParseBool(redisEnabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_REDIS_ENABLED: %w", err)
		}
		c.EventBus.Redis.Enabled = val
	}
	if redisAddrs := os.Getenv("INDEXER_EVENTBUS_REDIS_ADDRESSES"); redisAddrs != "" {
		addrs := make([]string, 0)
		for _, addr := range strings.Split(redisAddrs, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				addrs = append(addrs, addr)
			}
		}
		c.EventBus.Redis.Addresses = addrs
	}
	if redisPassword := os.Getenv("INDEXER_EVENTBUS_REDIS_PASSWORD"); redisPassword != "" {
		c.EventBus.Redis.Password = redisPassword
	}
	if redisDB := os.Getenv("INDEXER_EVENTBUS_REDIS_DB"); redisDB != "" {
		val, err := strconv.Atoi(redisDB)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_REDIS_DB: %w", err)
		}
		c.EventBus.Redis.DB = val
	}
	if redisCluster := os.Getenv("INDEXER_EVENTBUS_REDIS_CLUSTER_MODE"); redisCluster != "" {
		val, err := strconv.ParseBool(redisCluster)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_REDIS_CLUSTER_MODE: %w", err)
		}
		c.EventBus.Redis.ClusterMode = val
	}
	// Kafka EventBus configuration
	if kafkaEnabled := os.Getenv("INDEXER_EVENTBUS_KAFKA_ENABLED"); kafkaEnabled != "" {
		val, err := strconv.ParseBool(kafkaEnabled)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_KAFKA_ENABLED: %w", err)
		}
		c.EventBus.Kafka.Enabled = val
	}
	if kafkaBrokers := os.Getenv("INDEXER_EVENTBUS_KAFKA_BROKERS"); kafkaBrokers != "" {
		brokers := make([]string, 0)
		for _, broker := range strings.Split(kafkaBrokers, ",") {
			broker = strings.TrimSpace(broker)
			if broker != "" {
				brokers = append(brokers, broker)
			}
		}
		c.EventBus.Kafka.Brokers = brokers
	}
	if kafkaTopic := os.Getenv("INDEXER_EVENTBUS_KAFKA_TOPIC"); kafkaTopic != "" {
		c.EventBus.Kafka.Topic = kafkaTopic
	}
	if kafkaGroupID := os.Getenv("INDEXER_EVENTBUS_KAFKA_GROUP_ID"); kafkaGroupID != "" {
		c.EventBus.Kafka.GroupID = kafkaGroupID
	}
	if kafkaClientID := os.Getenv("INDEXER_EVENTBUS_KAFKA_CLIENT_ID"); kafkaClientID != "" {
		c.EventBus.Kafka.ClientID = kafkaClientID
	}
	if kafkaSASLUser := os.Getenv("INDEXER_EVENTBUS_KAFKA_SASL_USERNAME"); kafkaSASLUser != "" {
		c.EventBus.Kafka.SASLUsername = kafkaSASLUser
	}
	if kafkaSASLPass := os.Getenv("INDEXER_EVENTBUS_KAFKA_SASL_PASSWORD"); kafkaSASLPass != "" {
		c.EventBus.Kafka.SASLPassword = kafkaSASLPass
	}

	// Node configuration
	if nodeID := os.Getenv("INDEXER_NODE_ID"); nodeID != "" {
		c.Node.ID = nodeID
	}
	if nodeRole := os.Getenv("INDEXER_NODE_ROLE"); nodeRole != "" {
		c.Node.Role = nodeRole
	}
	if nodePriority := os.Getenv("INDEXER_NODE_PRIORITY"); nodePriority != "" {
		val, err := strconv.Atoi(nodePriority)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_NODE_PRIORITY: %w", err)
		}
		c.Node.Priority = val
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

	// Validate EventBus configuration
	validEventBusTypes := map[string]bool{
		"local":  true,
		"redis":  true,
		"kafka":  true,
		"hybrid": true,
	}
	if !validEventBusTypes[c.EventBus.Type] {
		return fmt.Errorf("invalid eventbus type %q, must be one of: local, redis, kafka, hybrid", c.EventBus.Type)
	}
	if c.EventBus.PublishBufferSize <= 0 {
		return fmt.Errorf("eventbus publish buffer size must be positive")
	}
	if c.EventBus.HistorySize < 0 {
		return fmt.Errorf("eventbus history size cannot be negative")
	}
	// Validate Redis configuration if enabled
	if c.EventBus.Redis.Enabled {
		if len(c.EventBus.Redis.Addresses) == 0 {
			return fmt.Errorf("redis eventbus enabled but no addresses configured")
		}
		if c.EventBus.Redis.PoolSize <= 0 {
			return fmt.Errorf("redis pool size must be positive")
		}
	}
	// Validate Kafka configuration if enabled
	if c.EventBus.Kafka.Enabled {
		if len(c.EventBus.Kafka.Brokers) == 0 {
			return fmt.Errorf("kafka eventbus enabled but no brokers configured")
		}
		if c.EventBus.Kafka.Topic == "" {
			return fmt.Errorf("kafka topic is required when kafka is enabled")
		}
	}

	// Validate Node configuration
	validNodeRoles := map[string]bool{
		"writer": true,
		"reader": true,
		"all":    true,
	}
	if !validNodeRoles[c.Node.Role] {
		return fmt.Errorf("invalid node role %q, must be one of: writer, reader, all", c.Node.Role)
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
