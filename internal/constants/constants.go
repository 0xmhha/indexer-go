package constants

import "time"

// API Server Constants
const (
	// DefaultAPIHost is the default API server host
	DefaultAPIHost = "localhost"

	// DefaultAPIPort is the default API server port
	DefaultAPIPort = 8080

	// MinPort is the minimum valid port number
	MinPort = 1

	// MaxPort is the maximum valid port number
	MaxPort = 65535

	// DefaultReadTimeout is the default HTTP read timeout
	DefaultReadTimeout = 15 * time.Second

	// DefaultWriteTimeout is the default HTTP write timeout
	DefaultWriteTimeout = 15 * time.Second

	// DefaultIdleTimeout is the default HTTP idle timeout
	DefaultIdleTimeout = 60 * time.Second

	// DefaultShutdownTimeout is the default graceful shutdown timeout
	DefaultShutdownTimeout = 30 * time.Second

	// DefaultMaxHeaderBytes is the default maximum request header size (1 MB)
	DefaultMaxHeaderBytes = 1 << 20 // 1 MB

	// DefaultRateLimitPerSecond is the default rate limit (requests per second)
	DefaultRateLimitPerSecond = 1000

	// DefaultRateLimitBurst is the default rate limit burst size
	DefaultRateLimitBurst = 2000
)

// API Paths
const (
	// DefaultGraphQLPath is the default GraphQL endpoint path
	DefaultGraphQLPath = "/graphql"

	// DefaultGraphQLPlaygroundPath is the default GraphQL playground path
	DefaultGraphQLPlaygroundPath = "/playground"

	// DefaultJSONRPCPath is the default JSON-RPC endpoint path
	DefaultJSONRPCPath = "/rpc"

	// DefaultWebSocketPath is the default WebSocket endpoint path
	DefaultWebSocketPath = "/ws"

	// DefaultGraphQLSubscriptionPath is the default GraphQL subscription (WebSocket) path
	DefaultGraphQLSubscriptionPath = "/graphql/ws"
)

// Fetcher Constants
const (
	// DefaultNumWorkers is the default number of worker goroutines for concurrent fetching
	DefaultNumWorkers = 100

	// MinWorkers is the minimum number of workers
	MinWorkers = 1

	// MaxWorkers is the maximum number of workers
	MaxWorkers = 1000

	// DefaultBatchSize is the default batch size for fetching blocks
	DefaultBatchSize = 10

	// DefaultMaxRetries is the default maximum number of retries for failed operations
	DefaultMaxRetries = 3

	// DefaultRetryDelay is the default delay between retries
	DefaultRetryDelay = 1 * time.Second

	// DefaultRetryBackoffMultiplier is the default backoff multiplier for exponential backoff
	DefaultRetryBackoffMultiplier = 2

	// Adaptive Optimization Constants
	// DefaultMetricsWindowSize is the size of the sliding window for metrics averaging
	DefaultMetricsWindowSize = 100

	// DefaultOptimizationInterval is how often to adjust fetcher parameters
	DefaultOptimizationInterval = 30 * time.Second

	// DefaultRateLimitWindow is the time window for rate limit detection
	DefaultRateLimitWindow = 5 * time.Minute

	// Large Block Processing Constants
	// LargeBlockThreshold is the gas threshold to consider a block as "large"
	LargeBlockThreshold = 50000000 // 50M gas

	// DefaultReceiptBatchSize is the number of receipts to process per batch for large blocks
	DefaultReceiptBatchSize = 100

	// DefaultMaxReceiptWorkers is the maximum number of workers for parallel receipt processing
	DefaultMaxReceiptWorkers = 10
)

// Storage Constants
const (
	// DefaultCacheSize is the default cache size in MB for PebbleDB
	DefaultCacheSize = 128 // MB

	// DefaultMaxOpenFiles is the default maximum number of open files for PebbleDB
	DefaultMaxOpenFiles = 1000

	// DefaultWriteBuffer is the default write buffer size in MB for PebbleDB
	DefaultWriteBuffer = 64 // MB

	// DefaultCompactionConcurrency is the default number of concurrent compactions
	DefaultCompactionConcurrency = 4
)

// Pagination Constants
const (
	// DefaultPaginationLimit is the default pagination limit
	DefaultPaginationLimit = 10

	// DefaultMaxPaginationLimit is the default maximum pagination limit
	DefaultMaxPaginationLimit = 100

	// MaxPaginationLimitExtended is the extended maximum pagination limit for specific queries
	MaxPaginationLimitExtended = 1000

	// MinPaginationLimit is the minimum pagination limit
	MinPaginationLimit = 1
)

// Query Constants
const (
	// DefaultQueryTimeout is the default timeout for database queries
	DefaultQueryTimeout = 30 * time.Second

	// DefaultLongQueryTimeout is the timeout for long-running queries
	DefaultLongQueryTimeout = 60 * time.Second
)

// WebSocket Constants
const (
	// DefaultWSReadBufferSize is the default WebSocket read buffer size
	DefaultWSReadBufferSize = 1024

	// DefaultWSWriteBufferSize is the default WebSocket write buffer size
	DefaultWSWriteBufferSize = 1024

	// DefaultWSPingInterval is the default WebSocket ping interval
	DefaultWSPingInterval = 30 * time.Second

	// DefaultWSPongTimeout is the default WebSocket pong timeout
	DefaultWSPongTimeout = 60 * time.Second

	// DefaultWSWriteTimeout is the default WebSocket write timeout
	DefaultWSWriteTimeout = 10 * time.Second
)

// EventBus Constants
const (
	// DefaultEventBufferSize is the default event buffer size
	DefaultEventBufferSize = 100

	// DefaultMaxSubscribers is the default maximum number of subscribers
	DefaultMaxSubscribers = 1000

	// DefaultEventTimeout is the default event delivery timeout
	DefaultEventTimeout = 5 * time.Second
)

// Size Constants
const (
	// BytesPerKB represents bytes in a kilobyte
	BytesPerKB = 1024

	// BytesPerMB represents bytes in a megabyte
	BytesPerMB = 1024 * BytesPerKB

	// BytesPerGB represents bytes in a gigabyte
	BytesPerGB = 1024 * BytesPerMB
)

// Math Constants
const (
	// PercentageMultiplier is used for converting fractions to percentages
	PercentageMultiplier = 100
)

// Bitmap Constants
const (
	// BitsPerByte is the number of bits in a byte
	BitsPerByte = 8
)

// Gas Constants
const (
	// DefaultGasLimit is a typical gas limit for standard transactions
	DefaultGasLimit = 21000

	// DefaultMaxGasLimit is a typical maximum gas limit per block
	DefaultMaxGasLimit = 30000000

	// LargeBlockGasLimit is the gas limit for very large blocks (Stable-One specific)
	LargeBlockGasLimit = 105000000
)

// Blockchain Constants
const (
	// GenesisBlockNumber is the block number of the genesis block
	GenesisBlockNumber = 0

	// DefaultConfirmationBlocks is the default number of confirmations to consider a block final
	DefaultConfirmationBlocks = 12

	// DefaultBlockTime is the typical block time (can vary by chain)
	DefaultBlockTime = 12 * time.Second

	// DefaultEpochLength is the default number of blocks per epoch
	// This matches the default epoch length in go-stablenet/consensus/wbft/config.go
	DefaultEpochLength = 10
)

// Retry and Backoff Constants
const (
	// MaxRetryAttempts is the maximum number of retry attempts
	MaxRetryAttempts = 5

	// InitialRetryDelay is the initial delay for exponential backoff
	InitialRetryDelay = 100 * time.Millisecond

	// MaxRetryDelay is the maximum delay for exponential backoff
	MaxRetryDelay = 30 * time.Second
)

// Monitoring Constants
const (
	// DefaultMetricsInterval is the default interval for metrics collection
	DefaultMetricsInterval = 10 * time.Second

	// DefaultHealthCheckInterval is the default health check interval
	DefaultHealthCheckInterval = 30 * time.Second
)
