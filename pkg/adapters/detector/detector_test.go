package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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

func newTestDetector(t *testing.T, handlers map[string]methodHandler) *Detector {
	t.Helper()
	server := newMockRPCServer(t, handlers)
	rpcClient, err := rpc.DialContext(context.Background(), server.URL)
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)

	return &Detector{
		rpcClient: rpcClient,
		logger:    zap.NewNop(),
		timeout:   5 * 1e9, // 5 seconds
	}
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

// ---- Tests: parseClientVersion (existing, reformatted) ----

func TestParseClientVersion(t *testing.T) {
	d := &Detector{}

	tests := []struct {
		name     string
		version  string
		expected NodeType
	}{
		// Anvil
		{"Anvil standard", "anvil/v0.2.0", NodeTypeAnvil},
		{"Anvil with hash", "anvil/v0.1.0/linux-aarch64/abc123", NodeTypeAnvil},
		{"Foundry anvil", "foundry-anvil/0.2.0", NodeTypeAnvil},
		// Geth
		{"Geth standard", "Geth/v1.14.0-stable/linux-amd64/go1.22", NodeTypeGeth},
		{"go-ethereum", "go-ethereum/v1.13.0/linux-amd64", NodeTypeGeth},
		// StableOne
		{"StableOne standard", "StableOne/v1.0.0", NodeTypeStableOne},
		{"go-stablenet", "go-stablenet/v1.0.0", NodeTypeStableOne},
		// Hardhat
		{"Hardhat Network", "HardhatNetwork/2.22.0", NodeTypeHardhat},
		{"hardhat lowercase", "hardhat/1.0.0", NodeTypeHardhat},
		// Ganache
		{"Ganache standard", "Ganache/v7.9.0", NodeTypeGanache},
		{"EthereumJS TestRPC", "EthereumJS TestRPC/v2.0.0", NodeTypeGanache},
		// Unknown
		{"empty string", "", NodeTypeUnknown},
		{"unknown client", "SomeRandomClient/v1.0.0", NodeTypeUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, d.parseClientVersion(tc.version))
		})
	}
}

// ---- Tests: isLocalChainID ----

func TestIsLocalChainID(t *testing.T) {
	tests := []struct {
		chainID  uint64
		expected bool
	}{
		{31337, true},
		{1337, true},
		{1338, true},
		{9999, true},
		{1234, true},
		{1, false},
		{42161, false},
		{0, false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("chainID_%d", tc.chainID), func(t *testing.T) {
			assert.Equal(t, tc.expected, isLocalChainID(tc.chainID))
		})
	}
}

// ---- Tests: NodeType Constants ----

func TestNodeTypeConstants(t *testing.T) {
	assert.Equal(t, NodeType("anvil"), NodeTypeAnvil)
	assert.Equal(t, NodeType("geth"), NodeTypeGeth)
	assert.Equal(t, NodeType("stableone"), NodeTypeStableOne)
	assert.Equal(t, NodeType("hardhat"), NodeTypeHardhat)
	assert.Equal(t, NodeType("ganache"), NodeTypeGanache)
	assert.Equal(t, NodeType("unknown"), NodeTypeUnknown)
}

// ---- Tests: NodeInfo ----

func TestNodeInfo_Fields(t *testing.T) {
	info := &NodeInfo{
		Type:                 NodeTypeAnvil,
		ClientVersion:        "anvil/v0.2.0",
		ChainID:              31337,
		IsLocal:              true,
		SupportsPendingTx:    true,
		SupportsDebug:        true,
		SupportsAnvilMethods: true,
	}
	assert.Equal(t, NodeTypeAnvil, info.Type)
	assert.Equal(t, "anvil/v0.2.0", info.ClientVersion)
	assert.Equal(t, uint64(31337), info.ChainID)
	assert.True(t, info.IsLocal)
	assert.True(t, info.SupportsAnvilMethods)
}

// ---- Tests: NewDetectorWithClient ----

func TestNewDetectorWithClient(t *testing.T) {
	server := newMockRPCServer(t, map[string]methodHandler{})
	rpcClient, err := rpc.DialContext(context.Background(), server.URL)
	require.NoError(t, err)
	defer rpcClient.Close()

	d := NewDetectorWithClient(rpcClient, zap.NewNop())
	assert.NotNil(t, d)
	assert.NotNil(t, d.rpcClient)
	assert.NotNil(t, d.logger)
	assert.Equal(t, 5*1e9, float64(d.timeout))
}

// ---- Tests: NewDetector ----

func TestNewDetector(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{})
		d, err := NewDetector(server.URL, zap.NewNop())
		require.NoError(t, err)
		require.NotNil(t, d)
		defer d.Close()
	})

	t.Run("invalid URL", func(t *testing.T) {
		d, err := NewDetector("invalid://url", zap.NewNop())
		assert.Error(t, err)
		assert.Nil(t, d)
		assert.Contains(t, err.Error(), "failed to connect to RPC")
	})
}

// ---- Tests: Close ----

func TestDetector_Close(t *testing.T) {
	t.Run("with client", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{})
		d, err := NewDetector(server.URL, zap.NewNop())
		require.NoError(t, err)
		d.Close() // should not panic
	})

	t.Run("nil client", func(t *testing.T) {
		d := &Detector{}
		d.Close() // should not panic
	})
}

// ---- Tests: getClientVersion ----

func TestDetector_GetClientVersion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion": rpcOK(`"Geth/v1.14.0-stable"`),
		})
		version, err := d.getClientVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "Geth/v1.14.0-stable", version)
	})

	t.Run("error", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion": rpcError("not supported"),
		})
		_, err := d.getClientVersion(context.Background())
		assert.Error(t, err)
	})
}

// ---- Tests: getChainID ----

func TestDetector_GetChainID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"eth_chainId": rpcOK(`"0x7a69"`), // 31337
		})
		chainID, err := d.getChainID(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(31337), chainID)
	})

	t.Run("rpc error", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"eth_chainId": rpcError("unavailable"),
		})
		_, err := d.getChainID(context.Background())
		assert.Error(t, err)
	})

	t.Run("invalid hex", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"eth_chainId": rpcOK(`"not-hex"`),
		})
		_, err := d.getChainID(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse chain ID")
	})
}

// ---- Tests: supportsAnvilMethods ----

func TestDetector_SupportsAnvilMethods(t *testing.T) {
	t.Run("anvil_nodeInfo succeeds", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"anvil_nodeInfo": rpcOK(`{}`),
		})
		assert.True(t, d.supportsAnvilMethods(context.Background()))
	})

	t.Run("anvil_nodeInfo fails but anvil_getAutomine succeeds", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"anvil_nodeInfo":    rpcError("not found"),
			"anvil_getAutomine": rpcOK(`true`),
		})
		assert.True(t, d.supportsAnvilMethods(context.Background()))
	})

	t.Run("both fail", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"anvil_nodeInfo":    rpcError("not found"),
			"anvil_getAutomine": rpcError("not found"),
		})
		assert.False(t, d.supportsAnvilMethods(context.Background()))
	})

	t.Run("neither registered (method not found)", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{})
		assert.False(t, d.supportsAnvilMethods(context.Background()))
	})
}

// ---- Tests: supportsPendingTx ----

func TestDetector_SupportsPendingTx(t *testing.T) {
	d := &Detector{}
	assert.True(t, d.supportsPendingTx(context.Background()))
}

// ---- Tests: supportsDebug ----

func TestDetector_SupportsDebug(t *testing.T) {
	t.Run("debug supported (success)", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"debug_traceBlockByNumber": rpcOK(`{}`),
		})
		assert.True(t, d.supportsDebug(context.Background()))
	})

	t.Run("debug supported (non-not-found error)", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"debug_traceBlockByNumber": rpcError("block 0x0 does not exist"),
		})
		assert.True(t, d.supportsDebug(context.Background()))
	})

	t.Run("debug not supported (not found)", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"debug_traceBlockByNumber": rpcError("method not found"),
		})
		assert.False(t, d.supportsDebug(context.Background()))
	})

	t.Run("debug not supported (not supported)", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"debug_traceBlockByNumber": rpcError("debug namespace not supported"),
		})
		assert.False(t, d.supportsDebug(context.Background()))
	})

	t.Run("method not registered", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{})
		// Server returns "method not found" for unregistered methods
		assert.False(t, d.supportsDebug(context.Background()))
	})
}

// ---- Tests: Detect (full orchestration) ----

func TestDetector_Detect(t *testing.T) {
	t.Run("anvil node", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion":       rpcOK(`"anvil/v0.2.0"`),
			"eth_chainId":             rpcOK(`"0x7a69"`), // 31337
			"anvil_nodeInfo":           rpcOK(`{}`),
			"debug_traceBlockByNumber": rpcOK(`{}`),
		})
		info, err := d.Detect(context.Background())
		require.NoError(t, err)
		assert.Equal(t, NodeTypeAnvil, info.Type)
		assert.Equal(t, "anvil/v0.2.0", info.ClientVersion)
		assert.Equal(t, uint64(31337), info.ChainID)
		assert.True(t, info.IsLocal)
		assert.True(t, info.SupportsAnvilMethods)
		assert.True(t, info.SupportsPendingTx)
		assert.True(t, info.SupportsDebug)
	})

	t.Run("geth node", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion":       rpcOK(`"Geth/v1.14.0-stable"`),
			"eth_chainId":             rpcOK(`"0x1"`), // mainnet
			"debug_traceBlockByNumber": rpcOK(`{}`),
		})
		info, err := d.Detect(context.Background())
		require.NoError(t, err)
		assert.Equal(t, NodeTypeGeth, info.Type)
		assert.Equal(t, uint64(1), info.ChainID)
		assert.False(t, info.IsLocal)
		assert.False(t, info.SupportsAnvilMethods)
		assert.True(t, info.SupportsDebug)
	})

	t.Run("unknown node with anvil methods", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion": rpcOK(`"CustomNode/v1.0"`),
			"eth_chainId":       rpcOK(`"0x7a69"`),
			"anvil_nodeInfo":     rpcOK(`{}`),
		})
		info, err := d.Detect(context.Background())
		require.NoError(t, err)
		// Unknown type gets upgraded to Anvil when anvil methods are detected
		assert.Equal(t, NodeTypeAnvil, info.Type)
		assert.True(t, info.SupportsAnvilMethods)
	})

	t.Run("client version error", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion": rpcError("unavailable"),
			"eth_chainId":       rpcOK(`"0x539"`), // 1337
		})
		info, err := d.Detect(context.Background())
		require.NoError(t, err)
		assert.Equal(t, NodeTypeUnknown, info.Type)
		assert.Equal(t, uint64(1337), info.ChainID)
		assert.True(t, info.IsLocal)
	})

	t.Run("chain id error", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion": rpcOK(`"Geth/v1.14.0"`),
			"eth_chainId":       rpcError("unavailable"),
		})
		info, err := d.Detect(context.Background())
		require.NoError(t, err)
		assert.Equal(t, NodeTypeGeth, info.Type)
		assert.Equal(t, uint64(0), info.ChainID)
		assert.False(t, info.IsLocal)
	})

	t.Run("all errors", func(t *testing.T) {
		d := newTestDetector(t, map[string]methodHandler{
			"web3_clientVersion": rpcError("err"),
			"eth_chainId":       rpcError("err"),
		})
		info, err := d.Detect(context.Background())
		require.NoError(t, err)
		assert.Equal(t, NodeTypeUnknown, info.Type)
	})
}

// ---- Tests: DetectFromRPCURL ----

func TestDetectFromRPCURL(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := newMockRPCServer(t, map[string]methodHandler{
			"web3_clientVersion":       rpcOK(`"Geth/v1.14.0"`),
			"eth_chainId":             rpcOK(`"0x1"`),
			"debug_traceBlockByNumber": rpcOK(`{}`),
		})
		info, err := DetectFromRPCURL(context.Background(), server.URL, zap.NewNop())
		require.NoError(t, err)
		assert.Equal(t, NodeTypeGeth, info.Type)
		assert.Equal(t, uint64(1), info.ChainID)
	})

	t.Run("connection error", func(t *testing.T) {
		_, err := DetectFromRPCURL(context.Background(), "invalid://url", zap.NewNop())
		assert.Error(t, err)
	})
}
