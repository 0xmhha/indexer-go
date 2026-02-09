package anvil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/adapters/evm"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ---- Mock EVM Client ----

type mockEVMClient struct{}

func (m *mockEVMClient) GetLatestBlockNumber(_ context.Context) (uint64, error) {
	return 100, nil
}
func (m *mockEVMClient) GetBlockByNumber(_ context.Context, _ uint64) (*types.Block, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockEVMClient) GetBlockByHash(_ context.Context, _ common.Hash) (*types.Block, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockEVMClient) GetBlockReceipts(_ context.Context, _ uint64) (types.Receipts, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockEVMClient) GetTransactionByHash(_ context.Context, _ common.Hash) (*types.Transaction, bool, error) {
	return nil, false, fmt.Errorf("not implemented")
}
func (m *mockEVMClient) BalanceAt(_ context.Context, _ common.Address, _ *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockEVMClient) Close() {}

// Verify mockEVMClient implements evm.Client
var _ evm.Client = (*mockEVMClient)(nil)

// ---- Mock JSON-RPC Server ----

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

		var req jrpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		resp := jrpcResponse{JSONRPC: "2.0", ID: req.ID}
		handler, ok := handlers[req.Method]
		if !ok {
			resp.Error = &jrpcError{Code: -32601, Message: "method not found"}
		} else {
			result, rpcErr := handler(req.Params)
			if rpcErr != nil {
				resp.Error = rpcErr
			} else {
				resp.Result = result
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server
}

func rpcOK(result string) methodHandler {
	return func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
		return json.RawMessage(result), nil
	}
}

func rpcError(msg string) methodHandler {
	return func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
		return nil, &jrpcError{Code: -32000, Message: msg}
	}
}

func newTestAnvilClient(t *testing.T, handlers map[string]methodHandler) *AnvilClient {
	t.Helper()
	server := newMockRPCServer(t, handlers)
	rpcClient, err := rpc.DialContext(context.Background(), server.URL)
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)

	return &AnvilClient{
		rpcClient: rpcClient,
		logger:    zap.NewNop(),
	}
}

// ---- Tests: Config ----

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	require.NotNil(t, config)
	assert.Equal(t, 0, config.ChainID.Cmp(big.NewInt(DefaultChainID)))
	assert.Equal(t, DefaultNativeCurrency, config.NativeCurrency)
	assert.True(t, config.EnableAnvilFeatures)
}

func TestConfig_CustomValues(t *testing.T) {
	config := &Config{
		ChainID:             big.NewInt(1337),
		RPCEndpoint:         "http://localhost:8545",
		NativeCurrency:      "MATIC",
		EnableAnvilFeatures: false,
	}
	assert.Equal(t, 0, config.ChainID.Cmp(big.NewInt(1337)))
	assert.Equal(t, "MATIC", config.NativeCurrency)
	assert.False(t, config.EnableAnvilFeatures)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 31337, DefaultChainID)
	assert.Equal(t, "ETH", DefaultNativeCurrency)
	assert.Equal(t, 18, DefaultNativeDecimals)
}

// ---- Tests: NewAdapter ----

func TestNewAdapter(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		adapter, err := NewAdapter(&mockEVMClient{}, nil, zap.NewNop())
		require.NoError(t, err)
		require.NotNil(t, adapter)
		assert.NotNil(t, adapter.consensusParser)
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &Config{
			ChainID:             big.NewInt(1337),
			NativeCurrency:      "MATIC",
			EnableAnvilFeatures: false,
		}
		adapter, err := NewAdapter(&mockEVMClient{}, config, zap.NewNop())
		require.NoError(t, err)
		require.NotNil(t, adapter)
		info := adapter.Info()
		assert.Equal(t, "MATIC", info.NativeCurrency)
		assert.Equal(t, 0, info.ChainID.Cmp(big.NewInt(1337)))
	})
}

// ---- Tests: NewAdapterWithRPC ----

func TestNewAdapterWithRPC(t *testing.T) {
	server := newMockRPCServer(t, map[string]methodHandler{})
	rpcClient, err := rpc.DialContext(context.Background(), server.URL)
	require.NoError(t, err)
	defer rpcClient.Close()

	adapter, err := NewAdapterWithRPC(&mockEVMClient{}, rpcClient, nil, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, adapter)
	assert.NotNil(t, adapter.rpcClient)

	// Should return non-nil AnvilClient
	anvilClient := adapter.GetAnvilClient()
	assert.NotNil(t, anvilClient)
}

// ---- Tests: Adapter Methods ----

func TestAdapter_Info(t *testing.T) {
	adapter, err := NewAdapter(&mockEVMClient{}, nil, zap.NewNop())
	require.NoError(t, err)

	info := adapter.Info()
	require.NotNil(t, info)
	assert.Equal(t, 0, info.ChainID.Cmp(big.NewInt(DefaultChainID)))
	assert.Equal(t, chain.ChainTypeEVM, info.ChainType)
	assert.Equal(t, chain.ConsensusTypePoA, info.ConsensusType)
	assert.Equal(t, "Anvil", info.Name)
	assert.Equal(t, "ETH", info.NativeCurrency)
	assert.Equal(t, DefaultNativeDecimals, info.Decimals)
}

func TestAdapter_ConsensusParser(t *testing.T) {
	t.Run("with parser", func(t *testing.T) {
		adapter, err := NewAdapter(&mockEVMClient{}, nil, zap.NewNop())
		require.NoError(t, err)
		assert.NotNil(t, adapter.ConsensusParser())
	})

	t.Run("nil parser", func(t *testing.T) {
		adapter := &Adapter{
			config:          DefaultConfig(),
			logger:          zap.NewNop(),
			consensusParser: nil,
		}
		assert.Nil(t, adapter.ConsensusParser())
	})
}

func TestAdapter_SystemContracts(t *testing.T) {
	adapter := &Adapter{config: DefaultConfig(), logger: zap.NewNop()}
	assert.Nil(t, adapter.SystemContracts())
}

func TestAdapter_GetAnvilClient(t *testing.T) {
	t.Run("nil rpcClient", func(t *testing.T) {
		adapter := &Adapter{config: DefaultConfig(), logger: zap.NewNop(), rpcClient: nil}
		assert.Nil(t, adapter.GetAnvilClient())
	})

	t.Run("with rpcClient", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{})
		rpcClient, err := rpc.DialContext(context.Background(), server.URL)
		require.NoError(t, err)
		defer rpcClient.Close()

		adapter := &Adapter{config: DefaultConfig(), logger: zap.NewNop(), rpcClient: rpcClient}
		anvilClient := adapter.GetAnvilClient()
		assert.NotNil(t, anvilClient)
	})
}

// ---- Tests: AnvilClient RPC Methods ----

func TestAnvilClient_Mine(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_mine": rpcOK("null"),
		})
		err := client.Mine(context.Background(), 10)
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_mine": rpcError("mine failed"),
		})
		err := client.Mine(context.Background(), 1)
		assert.Error(t, err)
	})
}

func TestAnvilClient_SetBalance(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_setBalance": rpcOK("null"),
		})
		err := client.SetBalance(context.Background(), "0x1234", big.NewInt(1000))
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_setBalance": rpcError("failed"),
		})
		err := client.SetBalance(context.Background(), "0x1234", big.NewInt(0))
		assert.Error(t, err)
	})
}

func TestAnvilClient_SetCode(t *testing.T) {
	client := newTestAnvilClient(t, map[string]methodHandler{
		"anvil_setCode": rpcOK("null"),
	})
	err := client.SetCode(context.Background(), "0x1234", []byte("60806040"))
	assert.NoError(t, err)
}

func TestAnvilClient_Snapshot(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"evm_snapshot": rpcOK(`"0x1"`),
		})
		id, err := client.Snapshot(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "0x1", id)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"evm_snapshot": rpcError("snapshot failed"),
		})
		_, err := client.Snapshot(context.Background())
		assert.Error(t, err)
	})
}

func TestAnvilClient_Revert(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"evm_revert": rpcOK(`true`),
		})
		err := client.Revert(context.Background(), "0x1")
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"evm_revert": rpcError("invalid snapshot"),
		})
		err := client.Revert(context.Background(), "0xbad")
		assert.Error(t, err)
	})
}

func TestAnvilClient_SetNextBlockTimestamp(t *testing.T) {
	client := newTestAnvilClient(t, map[string]methodHandler{
		"evm_setNextBlockTimestamp": rpcOK("null"),
	})
	err := client.SetNextBlockTimestamp(context.Background(), 1700000000)
	assert.NoError(t, err)
}

func TestAnvilClient_IncreaseTime(t *testing.T) {
	client := newTestAnvilClient(t, map[string]methodHandler{
		"evm_increaseTime": rpcOK("null"),
	})
	err := client.IncreaseTime(context.Background(), 3600)
	assert.NoError(t, err)
}

func TestAnvilClient_ImpersonateAccount(t *testing.T) {
	client := newTestAnvilClient(t, map[string]methodHandler{
		"anvil_impersonateAccount": rpcOK("null"),
	})
	err := client.ImpersonateAccount(context.Background(), "0xdead")
	assert.NoError(t, err)
}

func TestAnvilClient_StopImpersonatingAccount(t *testing.T) {
	client := newTestAnvilClient(t, map[string]methodHandler{
		"anvil_stopImpersonatingAccount": rpcOK("null"),
	})
	err := client.StopImpersonatingAccount(context.Background(), "0xdead")
	assert.NoError(t, err)
}

func TestAnvilClient_SetAutomine(t *testing.T) {
	client := newTestAnvilClient(t, map[string]methodHandler{
		"evm_setAutomine": rpcOK("null"),
	})
	err := client.SetAutomine(context.Background(), true)
	assert.NoError(t, err)
}

func TestAnvilClient_GetAutomine(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_getAutomine": rpcOK(`true`),
		})
		result, err := client.GetAutomine(context.Background())
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("disabled", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_getAutomine": rpcOK(`false`),
		})
		result, err := client.GetAutomine(context.Background())
		require.NoError(t, err)
		assert.False(t, result)
	})
}

func TestAnvilClient_Reset(t *testing.T) {
	t.Run("without fork", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_reset": rpcOK("null"),
		})
		err := client.Reset(context.Background(), "", 0)
		assert.NoError(t, err)
	})

	t.Run("with fork", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_reset": rpcOK("null"),
		})
		err := client.Reset(context.Background(), "http://mainnet.example.com", 18000000)
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_reset": rpcError("reset failed"),
		})
		err := client.Reset(context.Background(), "", 0)
		assert.Error(t, err)
	})
}

func TestAnvilClient_NodeInfo(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_nodeInfo": rpcOK(`{"currentBlockNumber":"0x1","forkConfig":null}`),
		})
		info, err := client.NodeInfo(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "0x1", info["currentBlockNumber"])
	})

	t.Run("error", func(t *testing.T) {
		client := newTestAnvilClient(t, map[string]methodHandler{
			"anvil_nodeInfo": rpcError("not available"),
		})
		_, err := client.NodeInfo(context.Background())
		assert.Error(t, err)
	})
}
