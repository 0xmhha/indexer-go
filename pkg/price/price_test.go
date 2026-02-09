package price

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
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

func newTestEthClient(t *testing.T, handlers map[string]methodHandler) *ethclient.Client {
	t.Helper()
	server := newMockRPCServer(t, handlers)
	rpcClient, err := rpc.DialContext(context.Background(), server.URL)
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)
	return ethclient.NewClient(rpcClient)
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

// abiEncodeUint256 returns the ABI-encoded hex string for a uint256 value (32-byte left-padded).
func abiEncodeUint256(val *big.Int) string {
	encoded := common.LeftPadBytes(val.Bytes(), 32)
	return fmt.Sprintf(`"0x%x"`, encoded)
}

// newAvailableOracle constructs a ContractOracle directly with available=true,
// bypassing NewContractOracle (which triggers checkAvailability → GetNativePrice
// → checkAvailability infinite recursion when a real client is provided).
func newAvailableOracle(t *testing.T, client *ethclient.Client, addr common.Address) *ContractOracle {
	t.Helper()
	parsedABI, err := abi.JSON(strings.NewReader(PriceOracleABI))
	require.NoError(t, err)
	return &ContractOracle{
		client:          client,
		contractAddress: addr,
		abi:             parsedABI,
		logger:          zap.NewNop(),
		available:       true,
	}
}

// ---- Oracle interface compliance ----

func TestNoOpOracle_ImplementsOracle(t *testing.T) {
	var _ Oracle = (*NoOpOracle)(nil)
}

func TestContractOracle_ImplementsOracle(t *testing.T) {
	var _ Oracle = (*ContractOracle)(nil)
}

// ---- NoOpOracle tests ----

func TestNewNoOpOracle(t *testing.T) {
	o := NewNoOpOracle()
	assert.NotNil(t, o)
}

func TestNoOpOracle_IsAvailable(t *testing.T) {
	o := NewNoOpOracle()
	assert.False(t, o.IsAvailable())
}

func TestNoOpOracle_GetTokenPrice(t *testing.T) {
	o := NewNoOpOracle()
	price, err := o.GetTokenPrice(context.Background(), common.HexToAddress("0x1234"))
	assert.NoError(t, err)
	assert.Nil(t, price)
}

func TestNoOpOracle_GetNativePrice(t *testing.T) {
	o := NewNoOpOracle()
	price, err := o.GetNativePrice(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, price)
}

func TestNoOpOracle_GetTokenValue(t *testing.T) {
	o := NewNoOpOracle()
	value, err := o.GetTokenValue(context.Background(), common.HexToAddress("0x1234"), big.NewInt(1000), 18)
	assert.NoError(t, err)
	assert.Nil(t, value)
}

// ---- PriceOracleABI tests ----

func TestPriceOracleABI_ValidJSON(t *testing.T) {
	parsedABI, err := abi.JSON(strings.NewReader(PriceOracleABI))
	require.NoError(t, err)

	_, ok := parsedABI.Methods["getPrice"]
	assert.True(t, ok, "ABI should contain getPrice method")

	_, ok = parsedABI.Methods["getNativePrice"]
	assert.True(t, ok, "ABI should contain getNativePrice method")
}

func TestPriceOracleABI_GetPriceInputs(t *testing.T) {
	parsedABI, err := abi.JSON(strings.NewReader(PriceOracleABI))
	require.NoError(t, err)

	method := parsedABI.Methods["getPrice"]
	assert.Len(t, method.Inputs, 1)
	assert.Equal(t, "token", method.Inputs[0].Name)
}

func TestPriceOracleABI_GetNativePriceInputs(t *testing.T) {
	parsedABI, err := abi.JSON(strings.NewReader(PriceOracleABI))
	require.NoError(t, err)

	method := parsedABI.Methods["getNativePrice"]
	assert.Len(t, method.Inputs, 0)
}

// ---- ContractOracle: constructor tests ----
// NOTE: NewContractOracle with a real client and deployed contract triggers
// infinite recursion: checkAvailability() → GetNativePrice() → checkAvailability()
// because available is still false when GetNativePrice re-enters checkAvailability.
// We only test NewContractOracle for cases where checkAvailability returns early.

func TestNewContractOracle_NilClient(t *testing.T) {
	oracle, err := NewContractOracle(nil, common.HexToAddress("0x1234"), nil)
	require.NoError(t, err)
	assert.NotNil(t, oracle)
	assert.False(t, oracle.IsAvailable())
}

func TestNewContractOracle_ZeroAddress(t *testing.T) {
	oracle, err := NewContractOracle(nil, common.Address{}, nil)
	require.NoError(t, err)
	assert.False(t, oracle.IsAvailable())
}

func TestNewContractOracle_WithLogger(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	oracle, err := NewContractOracle(nil, common.Address{}, logger)
	require.NoError(t, err)
	assert.NotNil(t, oracle)
}

func TestNewContractOracle_ContractNotDeployed(t *testing.T) {
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_getCode": rpcOK(`"0x"`),
	})
	oracle, err := NewContractOracle(client, common.HexToAddress("0xdeadbeef"), zap.NewNop())
	require.NoError(t, err)
	assert.False(t, oracle.IsAvailable())
}

func TestNewContractOracle_CodeAtError(t *testing.T) {
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_getCode": rpcError("node error"),
	})
	oracle, err := NewContractOracle(client, common.HexToAddress("0xdeadbeef"), zap.NewNop())
	require.NoError(t, err)
	assert.False(t, oracle.IsAvailable())
}

// ---- ContractOracle: IsAvailable tests ----

func TestContractOracle_IsAvailable(t *testing.T) {
	t.Run("default false", func(t *testing.T) {
		oracle, err := NewContractOracle(nil, common.Address{}, nil)
		require.NoError(t, err)
		assert.False(t, oracle.IsAvailable())
	})

	t.Run("true when set", func(t *testing.T) {
		oracle := &ContractOracle{available: true}
		assert.True(t, oracle.IsAvailable())
	})
}

// ---- ContractOracle: GetTokenPrice tests ----

func TestContractOracle_GetTokenPrice_NotAvailable(t *testing.T) {
	oracle, err := NewContractOracle(nil, common.Address{}, nil)
	require.NoError(t, err)

	price, err := oracle.GetTokenPrice(context.Background(), common.HexToAddress("0xtoken"))
	assert.NoError(t, err)
	assert.Nil(t, price)
}

func TestContractOracle_GetTokenPrice_Success(t *testing.T) {
	priceValue := big.NewInt(500000)
	encodedPrice := abiEncodeUint256(priceValue)

	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
			return json.RawMessage(encodedPrice), nil
		},
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	price, err := oracle.GetTokenPrice(context.Background(), common.HexToAddress("0xtoken"))
	require.NoError(t, err)
	require.NotNil(t, price)
	assert.Equal(t, priceValue, price)
}

func TestContractOracle_GetTokenPrice_CallError(t *testing.T) {
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": rpcError("execution reverted"),
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	price, err := oracle.GetTokenPrice(context.Background(), common.HexToAddress("0xtoken"))
	assert.Error(t, err)
	assert.Nil(t, price)
}

func TestContractOracle_GetTokenPrice_EmptyResult(t *testing.T) {
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": rpcOK(`"0x"`),
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	price, err := oracle.GetTokenPrice(context.Background(), common.HexToAddress("0xtoken"))
	assert.NoError(t, err)
	assert.Nil(t, price)
}

// ---- ContractOracle: GetNativePrice tests ----

func TestContractOracle_GetNativePrice_NilClient(t *testing.T) {
	oracle, err := NewContractOracle(nil, common.Address{}, nil)
	require.NoError(t, err)

	price, err := oracle.GetNativePrice(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, price)
}

func TestContractOracle_GetNativePrice_Success(t *testing.T) {
	priceValue := big.NewInt(2000000000)
	encodedPrice := abiEncodeUint256(priceValue)

	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
			return json.RawMessage(encodedPrice), nil
		},
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	price, err := oracle.GetNativePrice(context.Background())
	require.NoError(t, err)
	require.NotNil(t, price)
	assert.Equal(t, priceValue, price)
}

func TestContractOracle_GetNativePrice_RecheckWithZeroAddr(t *testing.T) {
	// Tests the recheck path: available=false, client!=nil, contractAddress=zero
	// checkAvailability returns early (zero address), so no recursion.
	client := newTestEthClient(t, map[string]methodHandler{})
	parsedABI, err := abi.JSON(strings.NewReader(PriceOracleABI))
	require.NoError(t, err)

	oracle := &ContractOracle{
		client:          client,
		contractAddress: common.Address{},
		abi:             parsedABI,
		logger:          zap.NewNop(),
		available:       false,
	}

	price, err := oracle.GetNativePrice(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, price)
	assert.False(t, oracle.IsAvailable())
}

func TestContractOracle_GetNativePrice_RecheckWithNoCode(t *testing.T) {
	// Tests the recheck path: available=false, client!=nil, contract has no code
	// checkAvailability returns early (no code), so no recursion.
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_getCode": rpcOK(`"0x"`),
	})
	parsedABI, err := abi.JSON(strings.NewReader(PriceOracleABI))
	require.NoError(t, err)

	oracle := &ContractOracle{
		client:          client,
		contractAddress: common.HexToAddress("0xdeadbeef"),
		abi:             parsedABI,
		logger:          zap.NewNop(),
		available:       false,
	}

	price, err := oracle.GetNativePrice(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, price)
	assert.False(t, oracle.IsAvailable())
}

func TestContractOracle_GetNativePrice_CallError(t *testing.T) {
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": rpcError("call failed"),
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	price, err := oracle.GetNativePrice(context.Background())
	assert.Error(t, err)
	assert.Nil(t, price)
}

func TestContractOracle_GetNativePrice_EmptyResult(t *testing.T) {
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": rpcOK(`"0x"`),
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	price, err := oracle.GetNativePrice(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, price)
}

// ---- ContractOracle: GetTokenValue tests ----

func TestContractOracle_GetTokenValue_NotAvailable(t *testing.T) {
	oracle, err := NewContractOracle(nil, common.Address{}, nil)
	require.NoError(t, err)

	value, err := oracle.GetTokenValue(context.Background(), common.HexToAddress("0xtoken"), big.NewInt(1000), 18)
	assert.NoError(t, err)
	assert.Nil(t, value)
}

func TestContractOracle_GetTokenValue_NilAmount(t *testing.T) {
	oracle := &ContractOracle{available: true}
	value, err := oracle.GetTokenValue(context.Background(), common.HexToAddress("0xtoken"), nil, 18)
	assert.NoError(t, err)
	assert.Nil(t, value)
}

func TestContractOracle_GetTokenValue_ZeroAmount(t *testing.T) {
	oracle := &ContractOracle{available: true}
	value, err := oracle.GetTokenValue(context.Background(), common.HexToAddress("0xtoken"), big.NewInt(0), 18)
	assert.NoError(t, err)
	assert.Nil(t, value)
}

func TestContractOracle_GetTokenValue_Success(t *testing.T) {
	priceValue := big.NewInt(1000) // 1000 wei per token
	encodedPrice := abiEncodeUint256(priceValue)

	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": func(_ json.RawMessage) (json.RawMessage, *jrpcError) {
			return json.RawMessage(encodedPrice), nil
		},
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	// 2 tokens (2 * 10^18 smallest units) at 1000 wei/token = 2000 wei
	amount := new(big.Int).Mul(big.NewInt(2), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	value, err := oracle.GetTokenValue(context.Background(), common.HexToAddress("0xtoken"), amount, 18)
	require.NoError(t, err)
	require.NotNil(t, value)
	assert.Equal(t, big.NewInt(2000), value)
}

func TestContractOracle_GetTokenValue_PriceError(t *testing.T) {
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": rpcError("reverted"),
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	_, err := oracle.GetTokenValue(context.Background(), common.HexToAddress("0xtoken"), big.NewInt(1000), 18)
	assert.Error(t, err)
}

func TestContractOracle_GetTokenValue_NilPrice(t *testing.T) {
	// eth_call returns empty → GetTokenPrice returns nil,nil → GetTokenValue returns nil
	client := newTestEthClient(t, map[string]methodHandler{
		"eth_call": rpcOK(`"0x"`),
	})
	contractAddr := common.HexToAddress("0xdeadbeef")
	oracle := newAvailableOracle(t, client, contractAddr)

	value, err := oracle.GetTokenValue(context.Background(), common.HexToAddress("0xtoken"), big.NewInt(1000), 18)
	assert.NoError(t, err)
	assert.Nil(t, value)
}
