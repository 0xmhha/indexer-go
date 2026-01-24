package rpcproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Proxy is the main RPC proxy service
type Proxy struct {
	config         *Config
	logger         *zap.Logger
	ethClient      *ethclient.Client
	rpcClient      *rpc.Client
	storage        storage.ContractVerificationReader
	cache          *Cache
	keyBuilder     *CacheKeyBuilder
	workerPool     *WorkerPool
	circuitBreaker *CircuitBreaker
	rateLimiter    *rate.Limiter
	ipLimiters     sync.Map // map[string]*rate.Limiter
	mu             sync.RWMutex
	started        bool
	totalRequests  int64
	latencySum     int64
	latencyCount   int64
}

// NewProxy creates a new RPC proxy service
func NewProxy(
	ethClient *ethclient.Client,
	rpcClient *rpc.Client,
	storage storage.ContractVerificationReader,
	config *Config,
	logger *zap.Logger,
) *Proxy {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	p := &Proxy{
		config:         config,
		logger:         logger,
		ethClient:      ethClient,
		rpcClient:      rpcClient,
		storage:        storage,
		cache:          NewCache(config.Cache),
		keyBuilder:     NewCacheKeyBuilder("rpcproxy"),
		circuitBreaker: NewCircuitBreaker(config.CircuitBreaker),
		rateLimiter:    rate.NewLimiter(rate.Limit(config.RateLimit.RequestsPerSecond), config.RateLimit.BurstSize),
	}

	// Create worker pool with request handler
	p.workerPool = NewWorkerPool(config.Worker, p.handleRequest, logger)

	return p
}

// Start starts the proxy service
func (p *Proxy) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return nil
	}

	p.workerPool.Start()
	p.started = true

	p.logger.Info("RPC Proxy service started")
	return nil
}

// Stop gracefully stops the proxy service
func (p *Proxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return nil
	}

	p.workerPool.Stop()
	p.started = false

	p.logger.Info("RPC Proxy service stopped")
	return nil
}

// ContractCall executes a contract call and returns decoded result
func (p *Proxy) ContractCall(ctx context.Context, req *ContractCallRequest) (*ContractCallResponse, error) {
	// Check rate limit
	if !p.rateLimiter.Allow() {
		return nil, ErrRateLimited
	}

	// Check circuit breaker
	if !p.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}

	// Build cache key
	paramsStr := string(req.Params)
	blockStr := "latest"
	if req.BlockNumber != nil {
		blockStr = req.BlockNumber.String()
	}
	cacheKey := p.keyBuilder.ContractCall(req.ContractAddress.Hex(), req.MethodName, paramsStr+blockStr)

	// Check cache
	if cached, ok := p.cache.Get(cacheKey); ok {
		resp := cached.(*ContractCallResponse)
		return resp, nil
	}

	// Get ABI
	var contractABI abi.ABI
	var err error

	if req.ABI != "" {
		// Use provided ABI
		contractABI, err = abi.JSON(strings.NewReader(req.ABI))
		if err != nil {
			return nil, fmt.Errorf("failed to parse provided ABI: %w", err)
		}
	} else {
		// Get ABI from storage
		verification, err := p.storage.GetContractVerification(ctx, req.ContractAddress)
		if err != nil {
			return nil, ErrContractNotVerified
		}
		if verification.ABI == "" {
			return nil, ErrABINotFound
		}
		contractABI, err = abi.JSON(strings.NewReader(verification.ABI))
		if err != nil {
			return nil, fmt.Errorf("failed to parse stored ABI: %w", err)
		}
	}

	// Find method in ABI
	method, exists := contractABI.Methods[req.MethodName]
	if !exists {
		return nil, ErrMethodNotFound
	}

	// Parse parameters
	var params []interface{}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("failed to parse parameters: %w", err)
		}
	}

	// Convert params to correct types based on ABI
	convertedParams, err := p.convertParams(method.Inputs, params)
	if err != nil {
		return nil, fmt.Errorf("failed to convert parameters: %w", err)
	}

	// Pack the call data
	callData, err := contractABI.Pack(req.MethodName, convertedParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack call data: %w", err)
	}

	// Create call message
	msg := ethereum.CallMsg{
		To:   &req.ContractAddress,
		Data: callData,
	}

	// Execute call
	start := time.Now()
	result, err := p.ethClient.CallContract(ctx, msg, req.BlockNumber)
	latency := time.Since(start)

	p.recordLatency(latency)

	if err != nil {
		p.circuitBreaker.RecordFailure()
		return nil, fmt.Errorf("contract call failed: %w", err)
	}

	p.circuitBreaker.RecordSuccess()

	// Unpack result
	var decoded interface{}
	decodedOk := false

	if len(method.Outputs) > 0 {
		outputs, err := method.Outputs.Unpack(result)
		if err == nil && len(outputs) > 0 {
			if len(outputs) == 1 {
				decoded = p.formatOutput(outputs[0])
			} else {
				formattedOutputs := make([]interface{}, len(outputs))
				for i, out := range outputs {
					formattedOutputs[i] = p.formatOutput(out)
				}
				decoded = formattedOutputs
			}
			decodedOk = true
		}
	}

	response := &ContractCallResponse{
		Result:    decoded,
		RawResult: fmt.Sprintf("0x%x", result),
		Decoded:   decodedOk,
	}

	// Determine cache TTL based on method name
	ttl := p.config.Cache.DefaultTTL
	methodLower := strings.ToLower(req.MethodName)
	if methodLower == "name" || methodLower == "symbol" || methodLower == "decimals" {
		ttl = p.config.Cache.TokenMetadataTTL
	} else if strings.Contains(methodLower, "balance") {
		ttl = p.config.Cache.BalanceTTL
	}

	p.cache.Set(cacheKey, response, ttl)

	return response, nil
}

// GetTransactionStatus returns the current status of a transaction
func (p *Proxy) GetTransactionStatus(ctx context.Context, txHash common.Hash) (*TransactionStatusResponse, error) {
	// Check rate limit
	if !p.rateLimiter.Allow() {
		return nil, ErrRateLimited
	}

	// Check circuit breaker
	if !p.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}

	// Check cache (short TTL for pending transactions)
	cacheKey := p.keyBuilder.TransactionStatus(txHash.Hex())
	if cached, ok := p.cache.Get(cacheKey); ok {
		resp := cached.(*TransactionStatusResponse)
		// Don't return cached pending status
		if resp.Status != TxStatusPending {
			return resp, nil
		}
	}

	start := time.Now()

	// Get transaction
	tx, isPending, err := p.ethClient.TransactionByHash(ctx, txHash)
	if err != nil {
		p.circuitBreaker.RecordFailure()
		if err.Error() == "not found" {
			response := &TransactionStatusResponse{
				TxHash: txHash,
				Status: TxStatusNotFound,
			}
			return response, nil
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	p.circuitBreaker.RecordSuccess()
	_ = tx // Used for future extensions

	if isPending {
		response := &TransactionStatusResponse{
			TxHash: txHash,
			Status: TxStatusPending,
		}
		// Short cache for pending
		p.cache.Set(cacheKey, response, 5*time.Second)
		p.recordLatency(time.Since(start))
		return response, nil
	}

	// Get receipt for confirmed transaction
	receipt, err := p.ethClient.TransactionReceipt(ctx, txHash)
	if err != nil {
		// Transaction exists but receipt not yet available
		response := &TransactionStatusResponse{
			TxHash: txHash,
			Status: TxStatusPending,
		}
		p.cache.Set(cacheKey, response, 5*time.Second)
		p.recordLatency(time.Since(start))
		return response, nil
	}

	// Get current block for confirmations
	currentBlock, err := p.ethClient.BlockNumber(ctx)
	if err != nil {
		currentBlock = 0
	}

	blockNum := receipt.BlockNumber.Uint64()
	confirmations := uint64(0)
	if currentBlock >= blockNum {
		confirmations = currentBlock - blockNum + 1
	}

	status := TxStatusSuccess
	if receipt.Status == 0 {
		status = TxStatusFailed
	}

	// Consider confirmed after 12 blocks
	if confirmations >= 12 {
		status = TxStatusConfirmed
	}

	gasUsed := receipt.GasUsed
	blockHash := receipt.BlockHash

	response := &TransactionStatusResponse{
		TxHash:        txHash,
		Status:        status,
		BlockNumber:   &blockNum,
		BlockHash:     &blockHash,
		Confirmations: confirmations,
		GasUsed:       &gasUsed,
	}

	// Cache confirmed transactions longer
	ttl := p.config.Cache.DefaultTTL
	if status == TxStatusConfirmed || status == TxStatusFailed {
		ttl = p.config.Cache.ImmutableTTL
	}
	p.cache.Set(cacheKey, response, ttl)

	p.recordLatency(time.Since(start))
	return response, nil
}

// GetBalance returns the balance of an address at a specific block from the chain RPC
func (p *Proxy) GetBalance(ctx context.Context, req *BalanceRequest) (*BalanceResponse, error) {
	// Check rate limit
	if !p.rateLimiter.Allow() {
		return nil, ErrRateLimited
	}

	// Check circuit breaker
	if !p.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}

	// Build cache key
	blockStr := "latest"
	if req.BlockNumber != nil {
		blockStr = req.BlockNumber.String()
	}
	cacheKey := p.keyBuilder.Balance(req.Address.Hex(), blockStr)

	// Check cache
	if cached, ok := p.cache.Get(cacheKey); ok {
		return cached.(*BalanceResponse), nil
	}

	start := time.Now()

	// Get balance from chain RPC
	balance, err := p.ethClient.BalanceAt(ctx, req.Address, req.BlockNumber)
	if err != nil {
		p.circuitBreaker.RecordFailure()
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	p.circuitBreaker.RecordSuccess()
	p.recordLatency(time.Since(start))

	// Get block number for response
	var blockNumber uint64
	if req.BlockNumber != nil {
		blockNumber = req.BlockNumber.Uint64()
	} else {
		// Get latest block number
		currentBlock, err := p.ethClient.BlockNumber(ctx)
		if err == nil {
			blockNumber = currentBlock
		}
	}

	response := &BalanceResponse{
		Address:     req.Address,
		Balance:     balance,
		BlockNumber: blockNumber,
	}

	// Cache with balance TTL
	p.cache.Set(cacheKey, response, p.config.Cache.BalanceTTL)

	return response, nil
}

// GetInternalTransactions returns internal transactions for a tx hash
func (p *Proxy) GetInternalTransactions(ctx context.Context, txHash common.Hash) (*InternalTransactionResponse, error) {
	// Check rate limit
	if !p.rateLimiter.Allow() {
		return nil, ErrRateLimited
	}

	// Check circuit breaker
	if !p.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}

	// Check cache (internal txs are immutable once confirmed)
	cacheKey := p.keyBuilder.InternalTransactions(txHash.Hex())
	if cached, ok := p.cache.Get(cacheKey); ok {
		return cached.(*InternalTransactionResponse), nil
	}

	start := time.Now()

	// Use debug_traceTransaction to get internal transactions
	var traceResult map[string]interface{}
	err := p.rpcClient.CallContext(ctx, &traceResult, "debug_traceTransaction", txHash, map[string]interface{}{
		"tracer": "callTracer",
	})

	p.recordLatency(time.Since(start))

	if err != nil {
		p.circuitBreaker.RecordFailure()
		return nil, fmt.Errorf("failed to trace transaction: %w", err)
	}

	p.circuitBreaker.RecordSuccess()

	// Parse trace result
	internalTxs := p.parseTraceResult(traceResult, []int{})

	response := &InternalTransactionResponse{
		TxHash:               txHash,
		InternalTransactions: internalTxs,
		TotalCount:           len(internalTxs),
	}

	// Cache immutable data
	p.cache.SetImmutable(cacheKey, response)

	return response, nil
}

// parseTraceResult parses the callTracer result into internal transactions
func (p *Proxy) parseTraceResult(trace map[string]interface{}, traceAddr []int) []InternalTransaction {
	var result []InternalTransaction

	txType, _ := trace["type"].(string)
	from, _ := trace["from"].(string)
	to, _ := trace["to"].(string)
	valueStr, _ := trace["value"].(string)
	gasStr, _ := trace["gas"].(string)
	gasUsedStr, _ := trace["gasUsed"].(string)
	input, _ := trace["input"].(string)
	output, _ := trace["output"].(string)
	errorMsg, _ := trace["error"].(string)

	value := new(big.Int)
	if valueStr != "" {
		value.SetString(strings.TrimPrefix(valueStr, "0x"), 16)
	}

	gas := uint64(0)
	if gasStr != "" {
		fmt.Sscanf(strings.TrimPrefix(gasStr, "0x"), "%x", &gas)
	}

	gasUsed := uint64(0)
	if gasUsedStr != "" {
		fmt.Sscanf(strings.TrimPrefix(gasUsedStr, "0x"), "%x", &gasUsed)
	}

	// Add current call
	internalTx := InternalTransaction{
		Type:         txType,
		From:         common.HexToAddress(from),
		To:           common.HexToAddress(to),
		Value:        value,
		Gas:          gas,
		GasUsed:      gasUsed,
		Input:        input,
		Output:       output,
		Error:        errorMsg,
		Depth:        len(traceAddr),
		TraceAddress: append([]int{}, traceAddr...),
	}
	result = append(result, internalTx)

	// Process nested calls
	if calls, ok := trace["calls"].([]interface{}); ok {
		for i, call := range calls {
			if callMap, ok := call.(map[string]interface{}); ok {
				nestedAddr := append(traceAddr, i)
				nestedTxs := p.parseTraceResult(callMap, nestedAddr)
				result = append(result, nestedTxs...)
			}
		}
	}

	return result
}

// handleRequest is the worker pool request handler
func (p *Proxy) handleRequest(ctx context.Context, req *Request) *Response {
	atomic.AddInt64(&p.totalRequests, 1)

	var data interface{}
	var err error

	switch req.Type {
	case RequestTypeContractCall:
		payload := req.Payload.(*ContractCallRequest)
		data, err = p.ContractCall(ctx, payload)

	case RequestTypeTransactionStatus:
		payload := req.Payload.(*TransactionStatusRequest)
		data, err = p.GetTransactionStatus(ctx, payload.TxHash)

	case RequestTypeInternalTransaction:
		payload := req.Payload.(*InternalTransactionRequest)
		data, err = p.GetInternalTransactions(ctx, payload.TxHash)

	default:
		err = ErrInvalidRequest
	}

	return &Response{
		ID:      req.ID,
		Success: err == nil,
		Data:    data,
		Error:   err,
	}
}

// convertParams converts JSON params to ABI-compatible types
func (p *Proxy) convertParams(inputs abi.Arguments, params []interface{}) ([]interface{}, error) {
	if len(params) != len(inputs) {
		return nil, fmt.Errorf("parameter count mismatch: expected %d, got %d", len(inputs), len(params))
	}

	result := make([]interface{}, len(params))
	for i, input := range inputs {
		converted, err := p.convertParam(input.Type, params[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert parameter %d (%s): %w", i, input.Name, err)
		}
		result[i] = converted
	}

	return result, nil
}

// convertParam converts a single parameter to ABI-compatible type
func (p *Proxy) convertParam(abiType abi.Type, value interface{}) (interface{}, error) {
	switch abiType.T {
	case abi.AddressTy:
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string for address type")
		}
		return common.HexToAddress(str), nil

	case abi.UintTy, abi.IntTy:
		switch v := value.(type) {
		case string:
			n := new(big.Int)
			if strings.HasPrefix(v, "0x") {
				n.SetString(v[2:], 16)
			} else {
				n.SetString(v, 10)
			}
			return n, nil
		case float64:
			return big.NewInt(int64(v)), nil
		case int:
			return big.NewInt(int64(v)), nil
		default:
			return nil, fmt.Errorf("cannot convert %T to uint/int", value)
		}

	case abi.BoolTy:
		b, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool type")
		}
		return b, nil

	case abi.StringTy:
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string type")
		}
		return str, nil

	case abi.BytesTy, abi.FixedBytesTy:
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string for bytes type")
		}
		return common.FromHex(str), nil

	default:
		return value, nil
	}
}

// formatOutput formats ABI output for JSON serialization
func (p *Proxy) formatOutput(value interface{}) interface{} {
	switch v := value.(type) {
	case *big.Int:
		return v.String()
	case common.Address:
		return v.Hex()
	case []byte:
		return fmt.Sprintf("0x%x", v)
	case [32]byte:
		return fmt.Sprintf("0x%x", v[:])
	default:
		return v
	}
}

// recordLatency records request latency for metrics
func (p *Proxy) recordLatency(latency time.Duration) {
	atomic.AddInt64(&p.latencySum, int64(latency))
	atomic.AddInt64(&p.latencyCount, 1)
}

// GetMetrics returns current proxy metrics
func (p *Proxy) GetMetrics() *Metrics {
	total, success, failed, active, queueDepth := p.workerPool.Stats()
	hits, misses, _, _ := p.cache.Stats()

	avgLatency := time.Duration(0)
	count := atomic.LoadInt64(&p.latencyCount)
	if count > 0 {
		avgLatency = time.Duration(atomic.LoadInt64(&p.latencySum) / count)
	}

	return &Metrics{
		TotalRequests:      total,
		SuccessfulRequests: success,
		FailedRequests:     failed,
		CacheHits:          hits,
		CacheMisses:        misses,
		AverageLatency:     avgLatency,
		QueueDepth:         queueDepth,
		ActiveWorkers:      active,
		CircuitState:       p.circuitBreaker.State().String(),
	}
}

// GetIPRateLimiter returns or creates a rate limiter for an IP
func (p *Proxy) GetIPRateLimiter(ip string) *rate.Limiter {
	if limiter, ok := p.ipLimiters.Load(ip); ok {
		return limiter.(*rate.Limiter)
	}

	limiter := rate.NewLimiter(rate.Limit(p.config.RateLimit.PerIPRequestsPerSecond), p.config.RateLimit.BurstSize/10)
	p.ipLimiters.Store(ip, limiter)
	return limiter
}

// AllowIP checks if a request from an IP should be allowed
func (p *Proxy) AllowIP(ip string) bool {
	if !p.config.RateLimit.PerIPLimit {
		return true
	}
	return p.GetIPRateLimiter(ip).Allow()
}
