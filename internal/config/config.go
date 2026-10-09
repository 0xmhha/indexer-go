package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/internal/constants"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the indexer
type Config struct {
	RPC                RPCConfig                `yaml:"rpc"`
	Source             SourceConfig             `yaml:"source"`
	Database           DatabaseConfig           `yaml:"database"`
	Log                LogConfig                `yaml:"log"`
	Indexer            IndexerConfig            `yaml:"indexer"`
	API                APIConfig                `yaml:"api"`
	SystemContracts    SystemContractsConfig    `yaml:"system_contracts"`
	MultiChain         MultiChainConfig         `yaml:"multichain"`
	Watchlist          WatchlistConfig          `yaml:"watchlist"`
	Resilience         ResilienceConfig         `yaml:"resilience"`
	Notifications      NotificationsConfig      `yaml:"notifications"`
	EventBus           EventBusConfig           `yaml:"eventbus"`
	Node               NodeConfig               `yaml:"node"`
	Verifier           VerifierConfig           `yaml:"verifier"`
	AccountAbstraction AccountAbstractionConfig `yaml:"account_abstraction"`
	// Features turns registered features on or off (pkg/feature). Features
	// not listed keep the chain profile's default.
	Features map[string]FeatureConfig `yaml:"features"`
}

// FeatureConfig configures one feature: enabled, and the feature's own
// settings, which the feature reads from its section (FeatureSettings).
type FeatureConfig struct {
	Enabled *bool `yaml:"enabled"`

	settings yaml.Node // the whole section as written
}

// UnmarshalYAML keeps the section for the feature's own settings.
func (f *FeatureConfig) UnmarshalYAML(n *yaml.Node) error {
	var plain struct {
		Enabled *bool `yaml:"enabled"`
	}
	if err := n.Decode(&plain); err != nil {
		return err
	}
	f.Enabled, f.settings = plain.Enabled, *n
	return nil
}

// SetFeatureSettings sets the settings of a feature as if its section held
// settings (a struct with yaml tags), keeping whether it is enabled.
func (c *Config) SetFeatureSettings(name string, settings any) error {
	var node yaml.Node
	if err := node.Encode(settings); err != nil {
		return fmt.Errorf("features.%s: %w", name, err)
	}
	if c.Features == nil {
		c.Features = map[string]FeatureConfig{}
	}
	fc := c.Features[name]
	fc.settings = node
	c.Features[name] = fc
	return nil
}

// FeatureSettings decodes the section of a feature into into (a pointer to
// the feature's settings struct); a feature without a section leaves into
// as it is. Keys the struct does not name are an error, so a misspelled
// setting is not silently ignored ("enabled" is always allowed).
func (c *Config) FeatureSettings(name string, into any) error {
	fc, ok := c.Features[name]
	if !ok || fc.settings.Kind == 0 {
		return nil
	}
	node := fc.settings
	if node.Kind == yaml.MappingNode {
		// Leave out "enabled", which is not the feature's.
		trimmed := node
		trimmed.Content = nil
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value != "enabled" {
				trimmed.Content = append(trimmed.Content, node.Content[i], node.Content[i+1])
			}
		}
		node = trimmed
	}
	data, err := yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("features.%s: %w", name, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("features.%s: %w", name, err)
	}
	return nil
}

// FeatureOverrides returns the features explicitly turned on (true) or off
// (false) by the configuration.
func (c *Config) FeatureOverrides() map[string]bool {
	out := map[string]bool{}
	for name, fc := range c.Features {
		if fc.Enabled != nil {
			out[name] = *fc.Enabled
		}
	}
	return out
}

// RPCConfig holds RPC client configuration
type RPCConfig struct {
	// Endpoint is the node's JSON-RPC URL, or "replay:///dir" to serve a
	// recorded archive instead of a node (pkg/source/replay).
	Endpoint string        `yaml:"endpoint"`
	Timeout  time.Duration `yaml:"timeout"`
	// RecordDir, when set, records every JSON-RPC call to the node into an
	// archive in this directory while indexing.
	RecordDir string `yaml:"record_dir"`
	// FallbackEndpoints are further HTTP(S) JSON-RPC URLs of the same chain.
	// Calls go to Endpoint and fail over to them in order when it does not
	// answer (pkg/rpcpool).
	FallbackEndpoints []string `yaml:"fallback_endpoints"`
	// RateLimit caps JSON-RPC requests per second to the nodes (0: none).
	RateLimit float64 `yaml:"rate_limit"`
	// WSEndpoint, when set, is a WebSocket URL whose newHeads subscription
	// wakes the live loop as soon as a block arrives; polling continues.
	WSEndpoint string `yaml:"ws_endpoint"`
}

// SourceConfig selects where block data comes from besides the node.
type SourceConfig struct {
	// EraDir, when set, reads the blocks held by the era1 archives in this
	// directory (exported by the node client) from the files, and every
	// later block from the node. Requires indexer.profile_source.
	EraDir string `yaml:"era_dir"`
}

// Database drivers (database.driver).
const (
	// DriverPebble stores the index in a Pebble database under
	// database.path (the default).
	DriverPebble = "pebble"
	// DriverPostgres stores the index in PostgreSQL (database.postgres,
	// refactoring plan R4-2).
	DriverPostgres = "postgres"
)

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path     string `yaml:"path"`
	ReadOnly bool   `yaml:"readonly"`
	// Driver selects the store: DriverPebble (empty) or DriverPostgres.
	Driver   string         `yaml:"driver"`
	Postgres PostgresConfig `yaml:"postgres"`
}

// PostgresConfig configures the PostgreSQL store (database.driver
// postgres).
type PostgresConfig struct {
	// DSN is the connection string (postgres://user:pass@host:port/db).
	// It may hold a password; it is never logged.
	DSN string `yaml:"dsn"`
	// Schema is the PostgreSQL schema of the tables; empty uses "public".
	// In multi-chain mode each chain uses <schema>_<chain id>.
	Schema string `yaml:"schema"`
	// MaxConns caps the connection pool; 0 keeps the pgxpool default.
	MaxConns int32 `yaml:"max_conns"`
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
	// AtomicBlock and ProfileSource selected the legacy write path and the
	// legacy go-ethereum client path when false. Both paths were removed
	// after v0.1.0: blocks are always indexed in one storage transaction
	// and decoded by the node's chain profile. The settings are kept only
	// so that a configuration still setting them to false is rejected
	// instead of silently ignored.
	AtomicBlock   *bool `yaml:"atomic_block"`
	ProfileSource *bool `yaml:"profile_source"`
	// PollInterval is how long the live loop waits before asking the node
	// for a new head once it has caught up (default 50ms). It bounds the
	// delay between a block appearing on the node and indexing starting.
	PollInterval time.Duration `yaml:"poll_interval"`
	// Finality selects how far the live loop indexes: "head" (default,
	// newest block; reorganizations are rolled back), "confirmations"
	// (head minus Confirmations) or "finalized" (the node's finalized
	// block). Rollback stays active under every policy.
	Finality string `yaml:"finality"`
	// Confirmations is how many blocks behind the head the live loop stays
	// with finality "confirmations".
	Confirmations uint64 `yaml:"confirmations"`
	// OrphanRetention is how many reorganization records are kept with the
	// blocks they removed (default 1000); older ones are deleted when a new
	// reorganization is recorded. 0 keeps them all.
	OrphanRetention uint64 `yaml:"orphan_retention"`
	// Mode selects what is ingested: "full" (default) stores every block,
	// transaction, receipt and log for the explorer; "declared" reads only
	// the headers and the logs of the tables declared in features.records
	// and stores those headers and records (refactoring plan R6-1).
	Mode string `yaml:"mode"`
}

// Ingest modes (indexer.mode).
const (
	ModeFull     = "full"
	ModeDeclared = "declared"
)

// DeclaredMode reports whether only declared data is ingested.
func (c *Config) DeclaredMode() bool { return c.Indexer.Mode == ModeDeclared }

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

	// EnableREST serves the REST API of the most polled paths under /v1
	// (/chains/<id>/v1 in multi-chain mode).
	EnableREST bool `yaml:"enable_rest"`

	// TrustedProxies are the reverse proxies (addresses or CIDR ranges)
	// whose X-Forwarded-For and X-Real-IP headers name the client. The
	// headers of any other peer are ignored: clients can write them.
	TrustedProxies []string `yaml:"trusted_proxies"`
	// RateLimit limits the requests of each client address.
	RateLimit APIRateLimitConfig `yaml:"rate_limit"`
	// GraphQL bounds what one GraphQL request may ask for.
	GraphQL APIGraphQLConfig `yaml:"graphql"`

	// SubscriptionEngine delivers GraphQL subscriptions through the
	// subscription engine (refactoring plan R3-3): events encoded once per
	// subscription kind, slow connections disconnected with the sequence to
	// resubscribe from. false subscribes each client subscription to the
	// event bus directly, as before.
	SubscriptionEngine bool `yaml:"subscription_engine"`
}

// APIRateLimitConfig limits the API requests of each client address.
type APIRateLimitConfig struct {
	Enabled   bool    `yaml:"enabled"`
	PerSecond float64 `yaml:"per_second"`
	Burst     int     `yaml:"burst"`
}

// APIGraphQLConfig bounds GraphQL requests; a request over a bound is
// refused before it runs. 0 turns a bound off.
type APIGraphQLConfig struct {
	// MaxDepth is the deepest field nesting allowed.
	MaxDepth int `yaml:"max_depth"`
	// MaxComplexity is the highest complexity allowed: every field counts
	// 1, and the fields under a page count once per row asked for.
	MaxComplexity int `yaml:"max_complexity"`
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

// WatchlistConfig is kept so that a configuration enabling the watchlist,
// which was removed after v0.1.0, is reported (UnsupportedSettings).
type WatchlistConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ResilienceConfig is kept so that a configuration enabling WebSocket
// resilience, which was removed after v0.1.0, is reported.
type ResilienceConfig struct {
	Enabled bool `yaml:"enabled"`
}

// EventBusConfig holds EventBus configuration for distributed operations
type EventBusConfig struct {
	// Type is the event bus type: "local", "redis", "kafka", "hybrid"
	Type string `yaml:"type"`
	// PublishBufferSize is the size of the publish buffer
	PublishBufferSize int `yaml:"publish_buffer_size"`
	// SubscriberBufferSize is the channel size of each API subscription
	// (GraphQL subscriptions, JSON-RPC pending pool). Events are dropped for
	// a subscriber only when its channel is full.
	SubscriberBufferSize int `yaml:"subscriber_buffer_size"`
	// HistorySize is the number of events to keep in history for replay
	HistorySize int `yaml:"history_size"`
	// Outbox records the events of indexed blocks in the block's storage
	// transaction and delivers them through a relay in sequence order
	// (default). false publishes them directly after the commit, the
	// earlier behaviour, without sequence numbers.
	Outbox bool `yaml:"outbox"`
	// OutboxRetention is how many delivered events the outbox keeps in the
	// database after the relay delivered them; 0 keeps all.
	OutboxRetention uint64 `yaml:"outbox_retention"`
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

// Node roles (node.role).
const (
	RoleAll    = "all"
	RoleIngest = "ingest"
	RoleAPI    = "api"
)

// NodeRole returns the configured role, with the former names writer and
// reader mapped to ingest and api.
func (c *Config) NodeRole() string {
	switch c.Node.Role {
	case "", RoleAll:
		return RoleAll
	case "writer":
		return RoleIngest
	case "reader":
		return RoleAPI
	}
	return c.Node.Role
}

// NodeConfig holds configuration for multi-node deployment
type NodeConfig struct {
	// ID is the unique identifier for this node. It names the node's
	// consumer group of the change stream (refactoring plan R3-2), so a
	// restarted node resumes where it stopped. Defaults to the hostname.
	ID string `yaml:"id"`
	// Role is what the process runs (refactoring plan R4-1): RoleAll
	// (indexing and the API), RoleIngest (indexing; the HTTP server serves
	// only health and metrics) or RoleAPI (the API over a database another
	// process indexes, opened read-only). "writer" and "reader" are the
	// former names of ingest and api.
	Role string `yaml:"role"`
	// Priority is used for leader election (higher = more likely to be leader)
	Priority int `yaml:"priority"`
}

// VerifierConfig holds contract verification configuration
type VerifierConfig struct {
	// Enabled indicates whether contract verification is active
	Enabled bool `yaml:"enabled"`
	// SolcBinDir is the directory for Solidity compiler binaries
	SolcBinDir string `yaml:"solc_bin_dir"`
	// SolcCacheDir is the directory for caching compiled results
	SolcCacheDir string `yaml:"solc_cache_dir"`
	// MaxCompilationTime is the maximum time for a single compilation (in seconds)
	MaxCompilationTime int `yaml:"max_compilation_time"`
	// AutoDownload automatically downloads missing compiler versions
	AutoDownload bool `yaml:"auto_download"`
	// AllowMetadataVariance allows metadata hash differences in bytecode comparison
	AllowMetadataVariance bool `yaml:"allow_metadata_variance"`
}

// AccountAbstractionConfig holds EIP-4337 Account Abstraction indexing configuration
type AccountAbstractionConfig struct {
	// Enabled indicates whether AA event indexing is active
	Enabled bool `yaml:"enabled"`
	// EntryPointAddresses is a list of known EntryPoint contract addresses
	// If empty, the processor will detect EntryPoint events by signature matching
	EntryPointAddresses []string `yaml:"entry_point_addresses"`
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
	// Account abstraction indexing is on unless a config file sets
	// account_abstraction.enabled: false. This is set here, not in
	// SetDefaults, because SetDefaults runs again after the file is loaded
	// and cannot tell an omitted bool from an explicit false.
	cfg.AccountAbstraction.Enabled = true
	// Same reasoning: atomic block indexing is the default, and an explicit
	// false in the file or INDEXER_ATOMIC_BLOCK=false selects the legacy path.
	cfg.Indexer.OrphanRetention = 1000
	// Likewise the outbox is on unless a file or INDEXER_EVENTBUS_OUTBOX
	// turns it off, and an explicit 0 keeps every outbox entry.
	cfg.EventBus.Outbox = true
	cfg.EventBus.OutboxRetention = constants.DefaultOutboxRetention
	// WebSocket keep-alive pings keep idle subscribers connected; it can be
	// disabled with api.enable_websocket_keepalive: false.
	cfg.API.EnableWebSocketKeepAlive = true
	// The subscription engine is on unless a file or
	// INDEXER_API_SUBSCRIPTION_ENGINE turns it off.
	cfg.API.SubscriptionEngine = true
	// The REST API is on unless a file or INDEXER_API_REST turns it off.
	cfg.API.EnableREST = true
	// Likewise the API's rate limit and GraphQL bounds are on unless a file
	// or the environment turns them off (enabled: false, a bound of 0).
	cfg.API.RateLimit = APIRateLimitConfig{
		Enabled:   true,
		PerSecond: constants.DefaultRateLimitPerSecond,
		Burst:     constants.DefaultRateLimitBurst,
	}
	cfg.API.GraphQL = APIGraphQLConfig{
		MaxDepth:      constants.DefaultGraphQLMaxDepth,
		MaxComplexity: constants.DefaultGraphQLMaxComplexity,
	}
	return cfg
}

// SetDefaults sets default values for the configuration
func (c *Config) SetDefaults() {
	// RPC defaults
	if c.Indexer.PollInterval == 0 {
		c.Indexer.PollInterval = 50 * time.Millisecond
	}
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
		c.EventBus.PublishBufferSize = constants.DefaultEventBusPublishBuffer
	}
	if c.EventBus.SubscriberBufferSize == 0 {
		c.EventBus.SubscriberBufferSize = constants.DefaultEventBusSubscriberBuffer
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

	// Verifier defaults
	if c.Verifier.SolcBinDir == "" {
		c.Verifier.SolcBinDir = "./solc-bin"
	}
	if c.Verifier.SolcCacheDir == "" {
		c.Verifier.SolcCacheDir = "./solc-cache"
	}
	if c.Verifier.MaxCompilationTime == 0 {
		c.Verifier.MaxCompilationTime = 30 // 30 seconds
	}
	// AllowMetadataVariance defaults to true for compatibility
	// (can be explicitly set to false in config)
}

// LoadFromEnv loads configuration from environment variables
// Environment variables take precedence over file configuration
func (c *Config) LoadFromEnv() error {
	// RPC configuration
	if endpoint := os.Getenv("INDEXER_RPC_ENDPOINT"); endpoint != "" {
		c.RPC.Endpoint = endpoint
	}
	if v := os.Getenv("INDEXER_FINALITY"); v != "" {
		c.Indexer.Finality = v
	}
	if v := os.Getenv("INDEXER_MODE"); v != "" {
		c.Indexer.Mode = v
	}
	if v := os.Getenv("INDEXER_CONFIRMATIONS"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_CONFIRMATIONS: %w", err)
		}
		c.Indexer.Confirmations = n
	}
	if v := os.Getenv("INDEXER_ORPHAN_RETENTION"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_ORPHAN_RETENTION: %w", err)
		}
		c.Indexer.OrphanRetention = n
	}
	if v := os.Getenv("INDEXER_EVENTBUS_OUTBOX"); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_OUTBOX: %w", err)
		}
		c.EventBus.Outbox = on
	}
	if v := os.Getenv("INDEXER_EVENTBUS_OUTBOX_RETENTION"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_OUTBOX_RETENTION: %w", err)
		}
		c.EventBus.OutboxRetention = n
	}
	if v := os.Getenv("INDEXER_POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_POLL_INTERVAL: %w", err)
		}
		c.Indexer.PollInterval = d
	}
	if v := os.Getenv("INDEXER_RPC_FALLBACK_ENDPOINTS"); v != "" {
		c.RPC.FallbackEndpoints = splitList(v)
	}
	if v := os.Getenv("INDEXER_RPC_RATE_LIMIT"); v != "" {
		r, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_RPC_RATE_LIMIT: %w", err)
		}
		c.RPC.RateLimit = r
	}
	if v := os.Getenv("INDEXER_RPC_WS_ENDPOINT"); v != "" {
		c.RPC.WSEndpoint = v
	}
	if v := os.Getenv("INDEXER_RPC_RECORD_DIR"); v != "" {
		c.RPC.RecordDir = v
	}
	if v := os.Getenv("INDEXER_SOURCE_ERA_DIR"); v != "" {
		c.Source.EraDir = v
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
	if driver := os.Getenv("INDEXER_DB_DRIVER"); driver != "" {
		c.Database.Driver = driver
	}
	if dsn := os.Getenv("INDEXER_DB_POSTGRES_DSN"); dsn != "" {
		c.Database.Postgres.DSN = dsn
	}
	if schema := os.Getenv("INDEXER_DB_POSTGRES_SCHEMA"); schema != "" {
		c.Database.Postgres.Schema = schema
	}
	if conns := os.Getenv("INDEXER_DB_POSTGRES_MAX_CONNS"); conns != "" {
		val, err := strconv.ParseInt(conns, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_DB_POSTGRES_MAX_CONNS: %w", err)
		}
		c.Database.Postgres.MaxConns = int32(val)
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
	if atomic := os.Getenv("INDEXER_ATOMIC_BLOCK"); atomic != "" {
		val, err := strconv.ParseBool(atomic)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_ATOMIC_BLOCK: %w", err)
		}
		c.Indexer.AtomicBlock = &val
	}
	if v := os.Getenv("INDEXER_PROFILE_SOURCE"); v != "" {
		val, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_PROFILE_SOURCE: %w", err)
		}
		c.Indexer.ProfileSource = &val
	}
	// INDEXER_FEATURES=name1,-name2 turns name1 on and name2 off.
	if v := os.Getenv("INDEXER_FEATURES"); v != "" {
		if c.Features == nil {
			c.Features = map[string]FeatureConfig{}
		}
		for _, item := range strings.Split(v, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			on := !strings.HasPrefix(item, "-")
			name := strings.TrimPrefix(item, "-")
			fc := c.Features[name] // keeps the section's settings
			fc.Enabled = &on
			c.Features[name] = fc
		}
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
	if v := os.Getenv("INDEXER_API_REST"); v != "" {
		val, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_REST: %w", err)
		}
		c.API.EnableREST = val
	}
	if v := os.Getenv("INDEXER_API_SUBSCRIPTION_ENGINE"); v != "" {
		val, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_SUBSCRIPTION_ENGINE: %w", err)
		}
		c.API.SubscriptionEngine = val
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
	if v := os.Getenv("INDEXER_API_TRUSTED_PROXIES"); v != "" {
		var proxies []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				proxies = append(proxies, p)
			}
		}
		c.API.TrustedProxies = proxies
	}
	if v := os.Getenv("INDEXER_API_RATE_LIMIT_ENABLED"); v != "" {
		val, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_RATE_LIMIT_ENABLED: %w", err)
		}
		c.API.RateLimit.Enabled = val
	}
	if v := os.Getenv("INDEXER_API_RATE_LIMIT_PER_SECOND"); v != "" {
		val, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_RATE_LIMIT_PER_SECOND: %w", err)
		}
		c.API.RateLimit.PerSecond = val
	}
	if v := os.Getenv("INDEXER_API_RATE_LIMIT_BURST"); v != "" {
		val, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_RATE_LIMIT_BURST: %w", err)
		}
		c.API.RateLimit.Burst = val
	}
	if v := os.Getenv("INDEXER_API_GRAPHQL_MAX_DEPTH"); v != "" {
		val, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_GRAPHQL_MAX_DEPTH: %w", err)
		}
		c.API.GraphQL.MaxDepth = val
	}
	if v := os.Getenv("INDEXER_API_GRAPHQL_MAX_COMPLEXITY"); v != "" {
		val, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_API_GRAPHQL_MAX_COMPLEXITY: %w", err)
		}
		c.API.GraphQL.MaxComplexity = val
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
	if v := os.Getenv("INDEXER_EVENTBUS_SUBSCRIBER_BUFFER_SIZE"); v != "" {
		val, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid INDEXER_EVENTBUS_SUBSCRIBER_BUFFER_SIZE: %w", err)
		}
		c.EventBus.SubscriberBufferSize = val
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

// MultiChainMode reports whether the indexer runs the chains listed in
// multichain.chains, each indexed into its own database under
// <database.path>/chains/<id> (refactoring plan R2-8).
func (c *Config) MultiChainMode() bool {
	return c.MultiChain.Enabled && len(c.MultiChain.Chains) > 0
}

// chainIDPattern is the form of a chain id: it names the chain's database
// directory and appears in API paths, so it has no path separators and
// does not start with a dot (multichain.ValidChainID).
var chainIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// validateMultiChain checks the chain entries of multichain mode.
func (c *Config) validateMultiChain() error {
	if !c.MultiChainMode() {
		return nil
	}
	seen := map[string]bool{}
	for i, ch := range c.MultiChain.Chains {
		if !chainIDPattern.MatchString(ch.ID) {
			return fmt.Errorf("multichain.chains[%d]: invalid id %q: use letters, digits, '.', '_' and '-', starting with a letter or digit", i, ch.ID)
		}
		if seen[ch.ID] {
			return fmt.Errorf("multichain.chains[%d]: duplicate id %q", i, ch.ID)
		}
		seen[ch.ID] = true
		if !strings.HasPrefix(ch.RPCEndpoint, "http://") && !strings.HasPrefix(ch.RPCEndpoint, "https://") {
			return fmt.Errorf("multichain.chains[%d] (%s): rpc_endpoint %q is not an HTTP(S) URL", i, ch.ID, ch.RPCEndpoint)
		}
		if ch.WSEndpoint != "" && !strings.HasPrefix(ch.WSEndpoint, "ws://") && !strings.HasPrefix(ch.WSEndpoint, "wss://") {
			return fmt.Errorf("multichain.chains[%d] (%s): ws_endpoint %q is not a WebSocket URL", i, ch.ID, ch.WSEndpoint)
		}
	}
	return nil
}

// isFalse reports whether an optional setting was set to false.
func isFalse(b *bool) bool { return b != nil && !*b }

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate RPC configuration (multichain mode reads each chain's
	// endpoint from its entry)
	if c.RPC.Endpoint == "" && !c.MultiChainMode() {
		return fmt.Errorf("RPC endpoint is required")
	}
	if err := c.validateMultiChain(); err != nil {
		return err
	}
	if c.RPC.RateLimit < 0 {
		return fmt.Errorf("rpc.rate_limit must not be negative")
	}
	for _, e := range c.RPC.FallbackEndpoints {
		if !strings.HasPrefix(e, "http://") && !strings.HasPrefix(e, "https://") {
			return fmt.Errorf("rpc.fallback_endpoints: %q is not an HTTP(S) URL", e)
		}
	}
	if c.RPC.WSEndpoint != "" && !strings.HasPrefix(c.RPC.WSEndpoint, "ws://") && !strings.HasPrefix(c.RPC.WSEndpoint, "wss://") {
		return fmt.Errorf("rpc.ws_endpoint: %q is not a WebSocket URL", c.RPC.WSEndpoint)
	}
	if c.RPC.Timeout <= 0 {
		return fmt.Errorf("RPC timeout must be positive")
	}
	if isFalse(c.Indexer.AtomicBlock) || isFalse(c.Indexer.ProfileSource) {
		return fmt.Errorf("indexer.atomic_block: false and indexer.profile_source: false select ingest paths removed after v0.1.0; remove these settings (or run v0.1.0)")
	}
	switch c.Indexer.Finality {
	case "", "head", "finalized":
	case "confirmations":
		if c.Indexer.Confirmations == 0 {
			return fmt.Errorf("indexer.finality confirmations requires indexer.confirmations > 0")
		}
	default:
		return fmt.Errorf("invalid indexer.finality %q, must be one of: head, confirmations, finalized", c.Indexer.Finality)
	}
	switch c.Indexer.Mode {
	case "", ModeFull:
	case ModeDeclared:
		if c.MultiChainMode() || c.Source.EraDir != "" {
			return fmt.Errorf("indexer.mode declared reads the declared logs from one node: multichain and source.era_dir are not supported")
		}
	default:
		return fmt.Errorf("invalid indexer.mode %q, must be one of: full, declared", c.Indexer.Mode)
	}

	// Validate database configuration
	switch c.Database.Driver {
	case "", DriverPebble:
		if c.Database.Path == "" {
			return fmt.Errorf("database path is required")
		}
	case DriverPostgres:
		if c.Database.Postgres.DSN == "" {
			return fmt.Errorf("database.postgres.dsn is required with database.driver %s", DriverPostgres)
		}
		if c.Database.Postgres.MaxConns < 0 {
			return fmt.Errorf("database.postgres.max_conns cannot be negative")
		}
	default:
		return fmt.Errorf("invalid database.driver %q, must be one of: %s, %s", c.Database.Driver, DriverPebble, DriverPostgres)
	}

	if err := c.API.validate(); err != nil {
		return err
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
	switch c.NodeRole() {
	case RoleAll, RoleIngest:
	case RoleAPI:
		// Pebble locks its directory even when opened read-only, so only
		// a database other processes can open serves an API process.
		if c.Database.Driver != DriverPostgres {
			return fmt.Errorf("node.role %s needs database.driver %s: the indexing process holds a Pebble database alone", RoleAPI, DriverPostgres)
		}
	default:
		return fmt.Errorf("invalid node role %q, must be one of: %s, %s, %s", c.Node.Role, RoleAll, RoleIngest, RoleAPI)
	}
	if c.NodeRole() != RoleAll && c.MultiChainMode() {
		return fmt.Errorf("node.role %s is not supported in multi-chain mode yet; use %s", c.Node.Role, RoleAll)
	}

	return nil
}

// validate checks the API's security settings.
func (a *APIConfig) validate() error {
	for _, p := range a.TrustedProxies {
		if _, err := netip.ParsePrefix(p); err == nil {
			continue
		}
		if _, err := netip.ParseAddr(p); err != nil {
			return fmt.Errorf("api.trusted_proxies: %q is neither an IP address nor a CIDR range", p)
		}
	}
	if a.RateLimit.Enabled && (a.RateLimit.PerSecond <= 0 || a.RateLimit.Burst <= 0) {
		return fmt.Errorf("api.rate_limit.per_second and burst must be positive (or set api.rate_limit.enabled: false)")
	}
	if a.GraphQL.MaxDepth < 0 || a.GraphQL.MaxComplexity < 0 {
		return fmt.Errorf("api.graphql.max_depth and max_complexity cannot be negative (0 turns a bound off)")
	}
	return nil
}

// Load is a convenience method that loads configuration in the following order:
// 1. Set defaults
// 2. Load from file (if provided)
// 3. Load from environment variables (override file)
// 4. Validate
func Load(configFile string) (*Config, error) {
	cfg, err := LoadUnvalidated(configFile)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// LoadUnvalidated loads defaults, the config file and environment variables
// without validating, so that command-line flags can still supply required
// values before Validate runs.
func LoadUnvalidated(configFile string) (*Config, error) {
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

	return cfg, nil
}

// UnsupportedSettings lists settings that are read but have no effect yet,
// so startup can warn instead of silently ignoring them.
func (c *Config) UnsupportedSettings() []string {
	var out []string
	if c.EventBus.Type != "" && c.EventBus.Type != "local" {
		out = append(out, fmt.Sprintf("eventbus.type=%q is not wired; the in-process event bus is used (node.priority is ignored too)", c.EventBus.Type))
	}
	if c.Watchlist.Enabled {
		out = append(out, "watchlist.enabled has no effect: the watchlist was removed after v0.1.0")
	}
	if c.Resilience.Enabled {
		out = append(out, "resilience.enabled has no effect: WebSocket resilience was removed after v0.1.0")
	}
	if len(c.AccountAbstraction.EntryPointAddresses) > 0 {
		out = append(out, "account_abstraction.entry_point_addresses is not supported yet; known EntryPoint addresses are used")
	}
	if c.MultiChainMode() {
		ignored := map[string]bool{
			"rpc.endpoint":           c.RPC.Endpoint != "",
			"rpc.fallback_endpoints": len(c.RPC.FallbackEndpoints) > 0,
			"rpc.ws_endpoint":        c.RPC.WSEndpoint != "",
			"rpc.record_dir":         c.RPC.RecordDir != "",
			"source.era_dir":         c.Source.EraDir != "",
			"notifications.enabled":  c.Notifications.Enabled,
			"verifier.enabled":       c.Verifier.Enabled,
		}
		for _, name := range []string{"rpc.endpoint", "rpc.fallback_endpoints", "rpc.ws_endpoint", "rpc.record_dir", "source.era_dir", "notifications.enabled", "verifier.enabled"} {
			if ignored[name] {
				out = append(out, name+" has no effect in multichain mode: chains are configured by their multichain.chains entries")
			}
		}
	}
	return out
}

// splitList splits a comma-separated value, dropping empty items and
// surrounding spaces.
func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
