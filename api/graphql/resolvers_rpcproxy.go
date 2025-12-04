package graphql

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/rpcproxy"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// schemaBuilder is a helper struct for building GraphQL schema parts
type schemaBuilder struct {
	schema  *Schema
	queries graphql.Fields
}

// resolveContractCall handles the contractCall query
func (s *Schema) resolveContractCall(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Check if RPC proxy is available
	if s.rpcProxy == nil {
		return nil, fmt.Errorf("RPC proxy is not available")
	}

	// Extract parameters
	addressStr, ok := p.Args["address"].(string)
	if !ok || addressStr == "" {
		return nil, fmt.Errorf("address is required")
	}

	methodName, ok := p.Args["method"].(string)
	if !ok || methodName == "" {
		return nil, fmt.Errorf("method is required")
	}

	// Validate address
	if !common.IsHexAddress(addressStr) {
		return nil, fmt.Errorf("invalid address format")
	}
	address := common.HexToAddress(addressStr)

	// Get optional parameters
	var params json.RawMessage
	if paramsArg, ok := p.Args["params"].(string); ok && paramsArg != "" {
		params = json.RawMessage(paramsArg)
	} else {
		params = json.RawMessage("[]")
	}

	var abiStr string
	if abiArg, ok := p.Args["abi"].(string); ok {
		abiStr = abiArg
	}

	// Create request
	req := &rpcproxy.ContractCallRequest{
		ContractAddress: address,
		MethodName:      methodName,
		Params:          params,
		ABI:             abiStr,
	}

	// Execute contract call
	resp, err := s.rpcProxy.ContractCall(ctx, req)
	if err != nil {
		s.logger.Error("contract call failed",
			zap.String("address", addressStr),
			zap.String("method", methodName),
			zap.Error(err))
		return nil, err
	}

	// Format result for JSON
	var resultStr string
	if resp.Result != nil {
		resultBytes, err := json.Marshal(resp.Result)
		if err == nil {
			resultStr = string(resultBytes)
		}
	}

	return map[string]interface{}{
		"result":    resultStr,
		"rawResult": resp.RawResult,
		"decoded":   resp.Decoded,
	}, nil
}

// resolveTransactionStatus handles the transactionStatus query
func (s *Schema) resolveTransactionStatus(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Check if RPC proxy is available
	if s.rpcProxy == nil {
		return nil, fmt.Errorf("RPC proxy is not available")
	}

	// Extract txHash parameter
	txHashStr, ok := p.Args["txHash"].(string)
	if !ok || txHashStr == "" {
		return nil, fmt.Errorf("txHash is required")
	}

	txHash := common.HexToHash(txHashStr)

	// Get transaction status
	resp, err := s.rpcProxy.GetTransactionStatus(ctx, txHash)
	if err != nil {
		s.logger.Error("get transaction status failed",
			zap.String("txHash", txHashStr),
			zap.Error(err))
		return nil, err
	}

	result := map[string]interface{}{
		"txHash":        resp.TxHash.Hex(),
		"status":        string(resp.Status),
		"confirmations": fmt.Sprintf("%d", resp.Confirmations),
	}

	if resp.BlockNumber != nil {
		result["blockNumber"] = fmt.Sprintf("%d", *resp.BlockNumber)
	}

	if resp.BlockHash != nil {
		result["blockHash"] = resp.BlockHash.Hex()
	}

	if resp.GasUsed != nil {
		result["gasUsed"] = fmt.Sprintf("%d", *resp.GasUsed)
	}

	return result, nil
}

// resolveInternalTransactionsRPC handles the internalTransactionsRPC query
// This uses RPC proxy to get internal transactions from debug_traceTransaction
func (s *Schema) resolveInternalTransactionsRPC(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Check if RPC proxy is available
	if s.rpcProxy == nil {
		return nil, fmt.Errorf("RPC proxy is not available")
	}

	// Extract txHash parameter
	txHashStr, ok := p.Args["txHash"].(string)
	if !ok || txHashStr == "" {
		return nil, fmt.Errorf("txHash is required")
	}

	txHash := common.HexToHash(txHashStr)

	// Get internal transactions
	resp, err := s.rpcProxy.GetInternalTransactions(ctx, txHash)
	if err != nil {
		s.logger.Error("get internal transactions failed",
			zap.String("txHash", txHashStr),
			zap.Error(err))
		return nil, err
	}

	// Convert to GraphQL format
	internalTxs := make([]map[string]interface{}, len(resp.InternalTransactions))
	for i, tx := range resp.InternalTransactions {
		internalTxs[i] = map[string]interface{}{
			"type":         tx.Type,
			"from":         tx.From.Hex(),
			"to":           tx.To.Hex(),
			"value":        tx.Value.String(),
			"gas":          fmt.Sprintf("%d", tx.Gas),
			"gasUsed":      fmt.Sprintf("%d", tx.GasUsed),
			"input":        tx.Input,
			"output":       tx.Output,
			"error":        tx.Error,
			"depth":        tx.Depth,
			"traceAddress": tx.TraceAddress,
		}
	}

	return map[string]interface{}{
		"txHash":               resp.TxHash.Hex(),
		"internalTransactions": internalTxs,
		"totalCount":           resp.TotalCount,
	}, nil
}

// resolveRPCProxyMetrics handles the rpcProxyMetrics query
func (s *Schema) resolveRPCProxyMetrics(p graphql.ResolveParams) (interface{}, error) {
	// Check if RPC proxy is available
	if s.rpcProxy == nil {
		return nil, fmt.Errorf("RPC proxy is not available")
	}

	metrics := s.rpcProxy.GetMetrics()

	return map[string]interface{}{
		"totalRequests":      fmt.Sprintf("%d", metrics.TotalRequests),
		"successfulRequests": fmt.Sprintf("%d", metrics.SuccessfulRequests),
		"failedRequests":     fmt.Sprintf("%d", metrics.FailedRequests),
		"cacheHits":          fmt.Sprintf("%d", metrics.CacheHits),
		"cacheMisses":        fmt.Sprintf("%d", metrics.CacheMisses),
		"averageLatencyMs":   fmt.Sprintf("%d", metrics.AverageLatency.Milliseconds()),
		"queueDepth":         metrics.QueueDepth,
		"activeWorkers":      metrics.ActiveWorkers,
		"circuitState":       metrics.CircuitState,
	}, nil
}

// resolveLiveBalance handles the liveBalance query
func (s *Schema) resolveLiveBalance(p graphql.ResolveParams) (interface{}, error) {
	// Check if RPC proxy is available
	if s.rpcProxy == nil {
		return nil, fmt.Errorf("RPC proxy is not available")
	}

	// Get address parameter
	addressStr, ok := p.Args["address"].(string)
	if !ok || addressStr == "" {
		return nil, fmt.Errorf("address is required")
	}

	address := common.HexToAddress(addressStr)

	// Get optional block number
	var blockNumber *big.Int
	if blockNumArg, ok := p.Args["blockNumber"].(string); ok && blockNumArg != "" {
		blockNumber = new(big.Int)
		if _, success := blockNumber.SetString(blockNumArg, 10); !success {
			return nil, fmt.Errorf("invalid block number: %s", blockNumArg)
		}
	}

	// Create balance request
	req := &rpcproxy.BalanceRequest{
		Address:     address,
		BlockNumber: blockNumber,
	}

	// Get balance from RPC proxy
	resp, err := s.rpcProxy.GetBalance(p.Context, req)
	if err != nil {
		s.logger.Error("get live balance failed",
			zap.String("address", addressStr),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get live balance: %w", err)
	}

	return map[string]interface{}{
		"address":     resp.Address.Hex(),
		"balance":     resp.Balance.String(),
		"blockNumber": fmt.Sprintf("%d", resp.BlockNumber),
	}, nil
}

// SetRPCProxy sets the RPC proxy service for the schema
func (s *Schema) SetRPCProxy(proxy *rpcproxy.Proxy) {
	s.rpcProxy = proxy
}

// buildRPCProxyQueries builds the RPC proxy related GraphQL queries
func (b *schemaBuilder) buildRPCProxyQueries() {
	// ContractCallResult type
	contractCallResultType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "ContractCallResult",
		Description: "Result of a contract call",
		Fields: graphql.Fields{
			"result": &graphql.Field{
				Type:        graphql.String,
				Description: "Decoded result as JSON string",
			},
			"rawResult": &graphql.Field{
				Type:        graphql.String,
				Description: "Raw hex result",
			},
			"decoded": &graphql.Field{
				Type:        graphql.Boolean,
				Description: "Whether the result was successfully decoded",
			},
		},
	})

	// TransactionStatusResult type
	transactionStatusResultType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "TransactionStatusResult",
		Description: "Transaction status information",
		Fields: graphql.Fields{
			"txHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
			"status": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Transaction status (pending, success, failed, not_found, confirmed)",
			},
			"blockNumber": &graphql.Field{
				Type:        bigIntType,
				Description: "Block number (if confirmed)",
			},
			"blockHash": &graphql.Field{
				Type:        hashType,
				Description: "Block hash (if confirmed)",
			},
			"confirmations": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of confirmations",
			},
			"gasUsed": &graphql.Field{
				Type:        bigIntType,
				Description: "Gas used (if confirmed)",
			},
		},
	})

	// InternalTransactionRPC type (from debug_traceTransaction)
	internalTransactionRPCType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "InternalTransactionRPC",
		Description: "Internal transaction from RPC trace",
		Fields: graphql.Fields{
			"type": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Call type (CALL, CREATE, DELEGATECALL, etc.)",
			},
			"from": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "From address",
			},
			"to": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "To address",
			},
			"value": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Value transferred",
			},
			"gas": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Gas provided",
			},
			"gasUsed": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Gas used",
			},
			"input": &graphql.Field{
				Type:        graphql.String,
				Description: "Input data",
			},
			"output": &graphql.Field{
				Type:        graphql.String,
				Description: "Output data",
			},
			"error": &graphql.Field{
				Type:        graphql.String,
				Description: "Error message if failed",
			},
			"depth": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Call depth",
			},
			"traceAddress": &graphql.Field{
				Type:        graphql.NewList(graphql.Int),
				Description: "Trace address path",
			},
		},
	})

	// InternalTransactionsRPCResult type
	internalTransactionsRPCResultType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "InternalTransactionsRPCResult",
		Description: "Internal transactions from RPC trace",
		Fields: graphql.Fields{
			"txHash": &graphql.Field{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
			"internalTransactions": &graphql.Field{
				Type:        graphql.NewList(internalTransactionRPCType),
				Description: "List of internal transactions",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count of internal transactions",
			},
		},
	})

	// RPCProxyMetrics type
	rpcProxyMetricsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "RPCProxyMetrics",
		Description: "RPC Proxy service metrics",
		Fields: graphql.Fields{
			"totalRequests": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total number of requests",
			},
			"successfulRequests": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of successful requests",
			},
			"failedRequests": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of failed requests",
			},
			"cacheHits": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of cache hits",
			},
			"cacheMisses": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of cache misses",
			},
			"averageLatencyMs": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Average latency in milliseconds",
			},
			"queueDepth": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Current queue depth",
			},
			"activeWorkers": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of active workers",
			},
			"circuitState": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Circuit breaker state (closed, open, half-open)",
			},
		},
	})

	// Add queries
	b.queries["contractCall"] = &graphql.Field{
		Type:        contractCallResultType,
		Description: "Execute a read-only contract call",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Contract address",
			},
			"method": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Method name to call",
			},
			"params": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Method parameters as JSON array string",
			},
			"abi": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Contract ABI as JSON string (optional, uses stored ABI if not provided)",
			},
		},
		Resolve: b.schema.resolveContractCall,
	}

	b.queries["transactionStatus"] = &graphql.Field{
		Type:        transactionStatusResultType,
		Description: "Get real-time transaction status",
		Args: graphql.FieldConfigArgument{
			"txHash": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
		},
		Resolve: b.schema.resolveTransactionStatus,
	}

	b.queries["internalTransactionsRPC"] = &graphql.Field{
		Type:        internalTransactionsRPCResultType,
		Description: "Get internal transactions using debug_traceTransaction RPC",
		Args: graphql.FieldConfigArgument{
			"txHash": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(hashType),
				Description: "Transaction hash",
			},
		},
		Resolve: b.schema.resolveInternalTransactionsRPC,
	}

	b.queries["rpcProxyMetrics"] = &graphql.Field{
		Type:        rpcProxyMetricsType,
		Description: "Get RPC proxy service metrics",
		Resolve:     b.schema.resolveRPCProxyMetrics,
	}

	// LiveBalance result type
	liveBalanceResultType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "LiveBalanceResult",
		Description: "Live balance result from chain RPC",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Account address",
			},
			"balance": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Account balance in wei",
			},
			"blockNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block number at which balance was queried",
			},
		},
	})

	b.queries["liveBalance"] = &graphql.Field{
		Type:        liveBalanceResultType,
		Description: "Get live balance from chain RPC (real-time, not from indexed storage)",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Account address",
			},
			"blockNumber": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Block number (optional, defaults to latest)",
			},
		},
		Resolve: b.schema.resolveLiveBalance,
	}
}
