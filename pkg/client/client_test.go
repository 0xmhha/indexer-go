package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ---- Mock JSON-RPC Server Infrastructure ----

type jrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

type jrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jrpcError      `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

type jrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type methodHandler func(params json.RawMessage) (json.RawMessage, *jrpcError)

func newMockRPCServer(t *testing.T, handlers map[string]methodHandler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		defer r.Body.Close()

		w.Header().Set("Content-Type", "application/json")

		trimmed := strings.TrimSpace(string(body))
		if strings.HasPrefix(trimmed, "[") {
			var reqs []jrpcRequest
			if err := json.Unmarshal(body, &reqs); err != nil {
				http.Error(w, "invalid batch", 400)
				return
			}
			var responses []jrpcResponse
			for _, req := range reqs {
				responses = append(responses, dispatchRequest(req, handlers))
			}
			json.NewEncoder(w).Encode(responses)
			return
		}

		var req jrpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		json.NewEncoder(w).Encode(dispatchRequest(req, handlers))
	}))
	t.Cleanup(server.Close)
	return server
}

func dispatchRequest(req jrpcRequest, handlers map[string]methodHandler) jrpcResponse {
	resp := jrpcResponse{JSONRPC: "2.0", ID: req.ID}
	handler, ok := handlers[req.Method]
	if !ok {
		resp.Error = &jrpcError{Code: -32601, Message: "method not found: " + req.Method}
		return resp
	}
	result, rpcErr := handler(req.Params)
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return resp
}

func newTestClient(t *testing.T, handlers map[string]methodHandler) *Client {
	t.Helper()
	server := newMockRPCServer(t, handlers)
	rpcClient, err := rpc.DialContext(context.Background(), server.URL)
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)

	return &Client{
		ethClient: ethclient.NewClient(rpcClient),
		rpcClient: rpcClient,
		endpoint:  server.URL,
		logger:    zap.NewNop(),
	}
}

// ---- JSON Response Helpers ----

func zeroLogsBloom() string {
	return "0x" + strings.Repeat("00", 256)
}

func makeBlockJSON(number uint64) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"hash":"0xb903239f8543d04b5dc1ba6579132b143087c68db1b2168786408fcbce568238",
		"parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner":"0x0000000000000000000000000000000000000000",
		"stateRoot":"0x0000000000000000000000000000000000000000000000000000000000000000",
		"transactionsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"receiptsRoot":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"logsBloom":"%s",
		"difficulty":"0x0",
		"number":"0x%x",
		"gasLimit":"0x1000000",
		"gasUsed":"0x0",
		"timestamp":"0x0",
		"extraData":"0x",
		"mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce":"0x0000000000000000",
		"baseFeePerGas":"0x0",
		"totalDifficulty":"0x0",
		"transactions":[],
		"uncles":[],
		"size":"0x0"
	}`, zeroLogsBloom(), number))
}

func makeReceiptJSON(txHash string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"blockHash":"0xb903239f8543d04b5dc1ba6579132b143087c68db1b2168786408fcbce568238",
		"blockNumber":"0x1",
		"contractAddress":null,
		"cumulativeGasUsed":"0x5208",
		"effectiveGasPrice":"0x3b9aca00",
		"from":"0x0000000000000000000000000000000000000001",
		"gasUsed":"0x5208",
		"logs":[],
		"logsBloom":"%s",
		"root":"0x",
		"status":"0x1",
		"to":"0x0000000000000000000000000000000000000002",
		"transactionHash":"%s",
		"transactionIndex":"0x0",
		"type":"0x0"
	}`, zeroLogsBloom(), txHash))
}

func chainIDHandler() methodHandler {
	return func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
		return json.RawMessage(`"0x1"`), nil
	}
}

func rpcErrorHandler(msg string) methodHandler {
	return func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
		return nil, &jrpcError{Code: -32000, Message: msg}
	}
}

// ---- Tests: BatchReceiptResult ----

func TestBatchReceiptResult_HasErrors(t *testing.T) {
	t.Run("no errors", func(t *testing.T) {
		r := &BatchReceiptResult{Errors: nil}
		assert.False(t, r.HasErrors())
	})
	t.Run("with errors", func(t *testing.T) {
		r := &BatchReceiptResult{
			Errors: []BatchReceiptError{{TxHash: common.Hash{}, Error: fmt.Errorf("fail")}},
		}
		assert.True(t, r.HasErrors())
	})
}

func TestBatchReceiptResult_AllSucceeded(t *testing.T) {
	t.Run("all succeeded", func(t *testing.T) {
		r := &BatchReceiptResult{FailureCount: 0}
		assert.True(t, r.AllSucceeded())
	})
	t.Run("some failed", func(t *testing.T) {
		r := &BatchReceiptResult{FailureCount: 2}
		assert.False(t, r.AllSucceeded())
	})
}

// ---- Tests: NewClient ----

func TestNewClient(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		client, err := NewClient(nil)
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("empty endpoint", func(t *testing.T) {
		client, err := NewClient(&Config{Endpoint: ""})
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "endpoint cannot be empty")
	})

	t.Run("invalid endpoint", func(t *testing.T) {
		client, err := NewClient(&Config{
			Endpoint: "invalid://endpoint",
			Timeout:  5 * time.Second,
		})
		assert.Error(t, err)
		assert.Nil(t, client)
	})

	t.Run("success with nil logger", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{
			"eth_chainId": chainIDHandler(),
		})
		client, err := NewClient(&Config{
			Endpoint: server.URL,
			Timeout:  5 * time.Second,
		})
		require.NoError(t, err)
		require.NotNil(t, client)
		defer client.Close()

		assert.NotNil(t, client.EthClient())
		assert.NotNil(t, client.RPCClient())
	})

	t.Run("success with logger", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{
			"eth_chainId": chainIDHandler(),
		})
		logger, _ := zap.NewDevelopment()
		client, err := NewClient(&Config{
			Endpoint: server.URL,
			Timeout:  5 * time.Second,
			Logger:   logger,
		})
		require.NoError(t, err)
		require.NotNil(t, client)
		defer client.Close()
	})

	t.Run("success without timeout", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{
			"eth_chainId": chainIDHandler(),
		})
		client, err := NewClient(&Config{
			Endpoint: server.URL,
		})
		require.NoError(t, err)
		require.NotNil(t, client)
		defer client.Close()
	})

	t.Run("ping failure", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{
			"eth_chainId": rpcErrorHandler("connection refused"),
		})
		client, err := NewClient(&Config{
			Endpoint: server.URL,
			Timeout:  5 * time.Second,
		})
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to ping")
	})
}

// ---- Tests: Close & Accessors ----

func TestClient_Close(t *testing.T) {
	t.Run("normal close", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_chainId": chainIDHandler(),
		})
		client.Close() // should not panic
	})

	t.Run("close with nil ethClient", func(t *testing.T) {
		c := &Client{}
		c.Close() // should not panic
	})
}

func TestClient_Accessors(t *testing.T) {
	client := newTestClient(t, map[string]methodHandler{
		"eth_chainId": chainIDHandler(),
	})

	assert.NotNil(t, client.EthClient())
	assert.NotNil(t, client.RPCClient())
}

// ---- Tests: Ping ----

func TestClient_Ping(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_chainId": chainIDHandler(),
		})
		err := client.Ping(context.Background())
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_chainId": rpcErrorHandler("node unavailable"),
		})
		err := client.Ping(context.Background())
		assert.Error(t, err)
	})
}

// ---- Tests: GetLatestBlockNumber ----

func TestClient_GetLatestBlockNumber(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_blockNumber": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return json.RawMessage(`"0xa"`), nil // 10
			},
		})
		num, err := client.GetLatestBlockNumber(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(10), num)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_blockNumber": rpcErrorHandler("internal error"),
		})
		_, err := client.GetLatestBlockNumber(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get latest block number")
	})
}

// ---- Tests: GetBlockByNumber ----

func TestClient_GetBlockByNumber(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockByNumber": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return makeBlockJSON(1), nil
			},
		})
		block, err := client.GetBlockByNumber(context.Background(), 1)
		require.NoError(t, err)
		assert.NotNil(t, block)
		assert.Equal(t, uint64(1), block.Number().Uint64())
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockByNumber": rpcErrorHandler("block not found"),
		})
		_, err := client.GetBlockByNumber(context.Background(), 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get block 999")
	})
}

// ---- Tests: GetBlockByHash ----

func TestClient_GetBlockByHash(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockByHash": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return makeBlockJSON(1), nil
			},
		})
		hash := common.HexToHash("0xb903239f8543d04b5dc1ba6579132b143087c68db1b2168786408fcbce568238")
		block, err := client.GetBlockByHash(context.Background(), hash)
		require.NoError(t, err)
		assert.NotNil(t, block)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockByHash": rpcErrorHandler("not found"),
		})
		hash := common.HexToHash("0xdead")
		_, err := client.GetBlockByHash(context.Background(), hash)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get block")
	})
}

// ---- Tests: GetTransactionByHash ----

func TestClient_GetTransactionByHash(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionByHash": rpcErrorHandler("tx not found"),
		})
		hash := common.HexToHash("0xdead")
		_, _, err := client.GetTransactionByHash(context.Background(), hash)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get transaction")
	})
}

// ---- Tests: GetTransactionReceipt ----

func TestClient_GetTransactionReceipt(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": rpcErrorHandler("receipt not found"),
		})
		hash := common.HexToHash("0xdead")
		_, err := client.GetTransactionReceipt(context.Background(), hash)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get receipt")
	})
}

// ---- Tests: GetBlockReceipts ----

func TestClient_GetBlockReceipts(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		txHash := "0xabc0000000000000000000000000000000000000000000000000000000000001"
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockReceipts": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return json.RawMessage(fmt.Sprintf(`[%s]`, string(makeReceiptJSON(txHash)))), nil
			},
		})
		receipts, err := client.GetBlockReceipts(context.Background(), 1)
		require.NoError(t, err)
		assert.Len(t, receipts, 1)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockReceipts": rpcErrorHandler("block not found"),
		})
		_, err := client.GetBlockReceipts(context.Background(), 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get receipts for block 999")
	})
}

// ---- Tests: GetChainID ----

func TestClient_GetChainID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_chainId": chainIDHandler(),
		})
		chainID, err := client.GetChainID(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(1), chainID.Int64())
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_chainId": rpcErrorHandler("chain id unavailable"),
		})
		_, err := client.GetChainID(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get chain ID")
	})
}

// ---- Tests: GetNetworkID ----

func TestClient_GetNetworkID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"net_version": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return json.RawMessage(`"1"`), nil
			},
		})
		netID, err := client.GetNetworkID(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(1), netID.Int64())
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"net_version": rpcErrorHandler("network error"),
		})
		_, err := client.GetNetworkID(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get network ID")
	})
}

// ---- Tests: BalanceAt ----

func TestClient_BalanceAt(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBalance": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return json.RawMessage(`"0xde0b6b3a7640000"`), nil // 1 ETH
			},
		})
		addr := common.HexToAddress("0x1234")
		balance, err := client.BalanceAt(context.Background(), addr, nil)
		require.NoError(t, err)
		assert.Equal(t, "1000000000000000000", balance.String())
	})

	t.Run("error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBalance": rpcErrorHandler("account not found"),
		})
		addr := common.HexToAddress("0x1234")
		_, err := client.BalanceAt(context.Background(), addr, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get balance")
	})
}

// ---- Tests: BatchGetBlocks ----

func TestClient_BatchGetBlocks(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{})
		blocks, err := client.BatchGetBlocks(context.Background(), nil)
		assert.NoError(t, err)
		assert.Nil(t, blocks)
	})

	t.Run("success", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockByNumber": func(params json.RawMessage) (json.RawMessage, *jrpcError) {
				var args []json.RawMessage
				json.Unmarshal(params, &args)
				// Parse block number from params
				if len(args) > 0 {
					numStr := strings.Trim(string(args[0]), `"`)
					var num uint64
					fmt.Sscanf(numStr, "0x%x", &num)
					return makeBlockJSON(num), nil
				}
				return makeBlockJSON(0), nil
			},
		})
		blocks, err := client.BatchGetBlocks(context.Background(), []uint64{1, 2})
		require.NoError(t, err)
		assert.Len(t, blocks, 2)
	})

	t.Run("individual element error", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockByNumber": func(params json.RawMessage) (json.RawMessage, *jrpcError) {
				var args []json.RawMessage
				json.Unmarshal(params, &args)
				if len(args) > 0 {
					numStr := strings.Trim(string(args[0]), `"`)
					if numStr == "0x63" { // block 99
						return nil, &jrpcError{Code: -32000, Message: "block not found"}
					}
				}
				return makeBlockJSON(1), nil
			},
		})
		_, err := client.BatchGetBlocks(context.Background(), []uint64{1, 99})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch block 99")
	})

	t.Run("batch call error with cancelled context", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{
			"eth_getBlockByNumber": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return makeBlockJSON(1), nil
			},
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately
		_, err := client.BatchGetBlocks(ctx, []uint64{1})
		assert.Error(t, err)
	})
}

// ---- Tests: BatchGetReceiptsWithDetails ----

func TestClient_BatchGetReceiptsWithDetails(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{})
		result, err := client.BatchGetReceiptsWithDetails(context.Background(), nil)
		require.NoError(t, err)
		assert.Equal(t, 0, result.TotalRequests)
		assert.True(t, result.AllSucceeded())
	})

	t.Run("success", func(t *testing.T) {
		hash1 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return makeReceiptJSON(hash1.Hex()), nil
			},
		})
		result, err := client.BatchGetReceiptsWithDetails(context.Background(), []common.Hash{hash1})
		require.NoError(t, err)
		assert.Equal(t, 1, result.TotalRequests)
		assert.Equal(t, 1, result.SuccessCount)
		assert.Equal(t, 0, result.FailureCount)
		assert.True(t, result.AllSucceeded())
		assert.False(t, result.HasErrors())
	})

	t.Run("individual error", func(t *testing.T) {
		hash1 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": rpcErrorHandler("receipt error"),
		})
		result, err := client.BatchGetReceiptsWithDetails(context.Background(), []common.Hash{hash1})
		require.NoError(t, err) // batch call itself succeeds
		assert.Equal(t, 1, result.TotalRequests)
		assert.Equal(t, 0, result.SuccessCount)
		assert.Equal(t, 1, result.FailureCount)
		assert.True(t, result.HasErrors())
		assert.False(t, result.AllSucceeded())
	})

	t.Run("null receipt (not found)", func(t *testing.T) {
		hash1 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000099")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return json.RawMessage("null"), nil
			},
		})
		result, err := client.BatchGetReceiptsWithDetails(context.Background(), []common.Hash{hash1})
		require.NoError(t, err)
		assert.Equal(t, 1, result.TotalRequests)
		assert.Equal(t, 0, result.SuccessCount)
		assert.Equal(t, 1, result.FailureCount)
		assert.True(t, result.HasErrors())
		assert.Nil(t, result.Receipts[0])
	})

	t.Run("batch call error with cancelled context", func(t *testing.T) {
		hash1 := common.HexToHash("0x01")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return makeReceiptJSON(hash1.Hex()), nil
			},
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.BatchGetReceiptsWithDetails(ctx, []common.Hash{hash1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "batch call failed")
	})

	t.Run("mixed results", func(t *testing.T) {
		hash1 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
		hash2 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": func(params json.RawMessage) (json.RawMessage, *jrpcError) {
				var args []json.RawMessage
				json.Unmarshal(params, &args)
				if len(args) > 0 {
					hashStr := strings.Trim(string(args[0]), `"`)
					if hashStr == hash2.Hex() {
						return nil, &jrpcError{Code: -32000, Message: "not found"}
					}
				}
				return makeReceiptJSON(hash1.Hex()), nil
			},
		})
		result, err := client.BatchGetReceiptsWithDetails(context.Background(), []common.Hash{hash1, hash2})
		require.NoError(t, err)
		assert.Equal(t, 2, result.TotalRequests)
		assert.Equal(t, 1, result.SuccessCount)
		assert.Equal(t, 1, result.FailureCount)
		assert.True(t, result.HasErrors())
	})
}

// ---- Tests: BatchGetReceipts ----

func TestClient_BatchGetReceipts(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		client := newTestClient(t, map[string]methodHandler{})
		receipts, err := client.BatchGetReceipts(context.Background(), nil)
		assert.NoError(t, err)
		assert.Nil(t, receipts)
	})

	t.Run("success", func(t *testing.T) {
		hash1 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return makeReceiptJSON(hash1.Hex()), nil
			},
		})
		receipts, err := client.BatchGetReceipts(context.Background(), []common.Hash{hash1})
		require.NoError(t, err)
		assert.Len(t, receipts, 1)
	})

	t.Run("returns error on any failure", func(t *testing.T) {
		hash1 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": rpcErrorHandler("receipt not found"),
		})
		_, err := client.BatchGetReceipts(context.Background(), []common.Hash{hash1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch receipt")
	})

	t.Run("batch call error", func(t *testing.T) {
		hash1 := common.HexToHash("0x01")
		client := newTestClient(t, map[string]methodHandler{
			"eth_getTransactionReceipt": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
				return makeReceiptJSON(hash1.Hex()), nil
			},
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.BatchGetReceipts(ctx, []common.Hash{hash1})
		assert.Error(t, err)
	})
}
