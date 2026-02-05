package rpcproxy

import (
	"context"
	"encoding/json"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Priority levels for request queue
type Priority int

const (
	PriorityCritical Priority = iota // Transaction status checks (immediate)
	PriorityHigh                     // Contract calls (fast)
	PriorityNormal                   // Internal transactions (can wait)
)

// RequestType defines the type of RPC proxy request
type RequestType string

const (
	RequestTypeContractCall        RequestType = "contract_call"
	RequestTypeTransactionStatus   RequestType = "transaction_status"
	RequestTypeInternalTransaction RequestType = "internal_transaction"
	RequestTypeBalance             RequestType = "balance"
)

// Request represents an RPC proxy request
type Request struct {
	ID        string
	Type      RequestType
	Priority  Priority
	Payload   interface{}
	ResultCh  chan *Response
	CreatedAt time.Time
	Timeout   time.Duration
}

// Response represents an RPC proxy response
type Response struct {
	ID      string
	Success bool
	Data    interface{}
	Error   error
	Cached  bool
	Latency time.Duration
}

// ContractCallRequest represents a contract call request
type ContractCallRequest struct {
	ContractAddress common.Address  `json:"contractAddress"`
	MethodName      string          `json:"methodName"`
	Params          json.RawMessage `json:"params"`
	ABI             string          `json:"abi"`
	BlockNumber     *big.Int        `json:"blockNumber,omitempty"` // nil for latest
}

// ContractCallResponse represents a contract call response
type ContractCallResponse struct {
	Result    interface{} `json:"result"`
	RawResult string      `json:"rawResult"`
	Decoded   bool        `json:"decoded"`
}

// TransactionStatusRequest represents a transaction status request
type TransactionStatusRequest struct {
	TxHash common.Hash `json:"txHash"`
}

// TransactionStatus represents the status of a transaction
type TransactionStatus string

const (
	TxStatusPending   TransactionStatus = "pending"
	TxStatusSuccess   TransactionStatus = "success"
	TxStatusFailed    TransactionStatus = "failed"
	TxStatusNotFound  TransactionStatus = "not_found"
	TxStatusConfirmed TransactionStatus = "confirmed"
)

// TransactionStatusResponse represents a transaction status response
type TransactionStatusResponse struct {
	TxHash        common.Hash       `json:"txHash"`
	Status        TransactionStatus `json:"status"`
	BlockNumber   *uint64           `json:"blockNumber,omitempty"`
	BlockHash     *common.Hash      `json:"blockHash,omitempty"`
	Confirmations uint64            `json:"confirmations"`
	GasUsed       *uint64           `json:"gasUsed,omitempty"`
}

// InternalTransactionRequest represents an internal transaction trace request
type InternalTransactionRequest struct {
	TxHash common.Hash `json:"txHash"`
}

// InternalTransaction represents a single internal transaction
type InternalTransaction struct {
	Type         string         `json:"type"` // CALL, CREATE, DELEGATECALL, etc.
	From         common.Address `json:"from"`
	To           common.Address `json:"to"`
	Value        *big.Int       `json:"value"`
	Gas          uint64         `json:"gas"`
	GasUsed      uint64         `json:"gasUsed"`
	Input        string         `json:"input"`
	Output       string         `json:"output"`
	Error        string         `json:"error,omitempty"`
	Depth        int            `json:"depth"`
	TraceAddress []int          `json:"traceAddress"`
}

// InternalTransactionResponse represents internal transactions response
type InternalTransactionResponse struct {
	TxHash               common.Hash           `json:"txHash"`
	InternalTransactions []InternalTransaction `json:"internalTransactions"`
	TotalCount           int                   `json:"totalCount"`
}

// BalanceRequest represents a balance query request
type BalanceRequest struct {
	Address     common.Address `json:"address"`
	BlockNumber *big.Int       `json:"blockNumber,omitempty"` // nil for latest
}

// BalanceResponse represents a balance query response
type BalanceResponse struct {
	Address     common.Address `json:"address"`
	Balance     *big.Int       `json:"balance"`
	BlockNumber uint64         `json:"blockNumber"`
}

// CodeRequest represents a code query request
type CodeRequest struct {
	Address     common.Address `json:"address"`
	BlockNumber *big.Int       `json:"blockNumber,omitempty"` // nil for latest
}

// CodeResponse represents a code query response
type CodeResponse struct {
	Address     common.Address `json:"address"`
	Code        []byte         `json:"code"`
	IsContract  bool           `json:"isContract"`
	BlockNumber uint64         `json:"blockNumber"`
}

// NonceRequest represents a nonce (transaction count) query request
type NonceRequest struct {
	Address     common.Address `json:"address"`
	BlockNumber *big.Int       `json:"blockNumber,omitempty"` // nil for latest
}

// NonceResponse represents a nonce query response
type NonceResponse struct {
	Address     common.Address `json:"address"`
	Nonce       uint64         `json:"nonce"`
	BlockNumber uint64         `json:"blockNumber"`
}

// CacheConfig holds cache configuration
type CacheConfig struct {
	// MaxSize is the maximum number of entries in the cache
	MaxSize int
	// DefaultTTL is the default time-to-live for cache entries
	DefaultTTL time.Duration
	// ImmutableTTL is the TTL for immutable data (e.g., completed tx traces)
	ImmutableTTL time.Duration
	// BalanceTTL is the TTL for balance data
	BalanceTTL time.Duration
	// TokenMetadataTTL is the TTL for token metadata (name, symbol)
	TokenMetadataTTL time.Duration
}

// DefaultCacheConfig returns the default cache configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		MaxSize:          10000,
		DefaultTTL:       30 * time.Second,
		ImmutableTTL:     24 * time.Hour,
		BalanceTTL:       15 * time.Second,
		TokenMetadataTTL: 24 * time.Hour,
	}
}

// WorkerConfig holds worker pool configuration
type WorkerConfig struct {
	// NumWorkers is the number of worker goroutines
	NumWorkers int
	// QueueSize is the size of the request queue buffer
	QueueSize int
	// RequestTimeout is the default timeout for requests
	RequestTimeout time.Duration
	// MaxRetries is the maximum number of retries for failed requests
	MaxRetries int
	// RetryDelay is the initial delay between retries (exponential backoff)
	RetryDelay time.Duration
}

// DefaultWorkerConfig returns the default worker configuration
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		NumWorkers:     10,
		QueueSize:      1000,
		RequestTimeout: 10 * time.Second,
		MaxRetries:     3,
		RetryDelay:     100 * time.Millisecond,
	}
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	// RequestsPerSecond is the rate limit for requests
	RequestsPerSecond float64
	// BurstSize is the maximum burst size
	BurstSize int
	// PerIPLimit enables per-IP rate limiting
	PerIPLimit bool
	// PerIPRequestsPerSecond is the per-IP rate limit
	PerIPRequestsPerSecond float64
}

// DefaultRateLimitConfig returns the default rate limit configuration
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RequestsPerSecond:      100,
		BurstSize:              200,
		PerIPLimit:             true,
		PerIPRequestsPerSecond: 10,
	}
}

// CircuitBreakerConfig holds circuit breaker configuration
type CircuitBreakerConfig struct {
	// MaxFailures is the number of failures before opening the circuit
	MaxFailures int
	// ResetTimeout is the time before attempting to close the circuit
	ResetTimeout time.Duration
	// HalfOpenRequests is the number of requests to allow in half-open state
	HalfOpenRequests int
}

// DefaultCircuitBreakerConfig returns the default circuit breaker configuration
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxFailures:      5,
		ResetTimeout:     30 * time.Second,
		HalfOpenRequests: 3,
	}
}

// Config holds the complete RPC proxy configuration
type Config struct {
	Cache          *CacheConfig
	Worker         *WorkerConfig
	RateLimit      *RateLimitConfig
	CircuitBreaker *CircuitBreakerConfig
}

// DefaultConfig returns the default RPC proxy configuration
func DefaultConfig() *Config {
	return &Config{
		Cache:          DefaultCacheConfig(),
		Worker:         DefaultWorkerConfig(),
		RateLimit:      DefaultRateLimitConfig(),
		CircuitBreaker: DefaultCircuitBreakerConfig(),
	}
}

// Metrics holds RPC proxy metrics
type Metrics struct {
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64
	CacheHits          int64
	CacheMisses        int64
	AverageLatency     time.Duration
	QueueDepth         int
	ActiveWorkers      int
	CircuitState       string
}

// RPCProxyService defines the interface for the RPC proxy service
type RPCProxyService interface {
	// ContractCall executes a contract call and returns decoded result
	ContractCall(ctx context.Context, req *ContractCallRequest) (*ContractCallResponse, error)

	// GetTransactionStatus returns the current status of a transaction
	GetTransactionStatus(ctx context.Context, txHash common.Hash) (*TransactionStatusResponse, error)

	// GetInternalTransactions returns internal transactions for a tx hash
	GetInternalTransactions(ctx context.Context, txHash common.Hash) (*InternalTransactionResponse, error)

	// GetBalance returns the balance of an address at a specific block from the chain RPC
	GetBalance(ctx context.Context, req *BalanceRequest) (*BalanceResponse, error)

	// GetNonce returns the nonce (transaction count) of an address at a specific block from the chain RPC
	GetNonce(ctx context.Context, req *NonceRequest) (*NonceResponse, error)

	// GetCode returns the bytecode at an address to check if it's a contract
	GetCode(ctx context.Context, req *CodeRequest) (*CodeResponse, error)

	// GetMetrics returns current proxy metrics
	GetMetrics() *Metrics

	// Start starts the proxy service
	Start() error

	// Stop gracefully stops the proxy service
	Stop() error
}
