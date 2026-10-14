package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateBatchBalancesCorrelateReorderedPartialReplies(t *testing.T) {
	hash := common.HexToHash("0x1234")
	addresses := []common.Address{common.BigToAddress(big.NewInt(1)), common.BigToAddress(big.NewInt(2)), common.BigToAddress(big.NewInt(3)), common.BigToAddress(big.NewInt(4))}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var calls []jrpcRequest
		if json.Unmarshal(raw, &calls) != nil {
			http.Error(w, "batch required", http.StatusBadRequest)
			return
		}
		responses := make([]jrpcResponse, len(calls))
		for i, call := range calls {
			var params []json.RawMessage
			_ = json.Unmarshal(call.Params, &params)
			var at struct{ BlockHash common.Hash }
			_ = json.Unmarshal(params[1], &at)
			assert.Equal(t, hash, at.BlockHash)
			response := jrpcResponse{JSONRPC: "2.0", ID: call.ID}
			switch i {
			case 1:
				response.Error = &jrpcError{Code: -32000, Message: "missing trie node"}
			case 3:
				response.Result = json.RawMessage("null")
			default:
				response.Result = json.RawMessage(fmt.Sprintf("\"0x%x\"", i+1))
			}
			responses[len(calls)-1-i] = response
		}
		_ = json.NewEncoder(w).Encode(responses)
	}))
	t.Cleanup(server.Close)
	rpcClient, err := rpc.DialContext(context.Background(), server.URL)
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)
	c := &Client{rpcClient: rpcClient, ethClient: ethclient.NewClient(rpcClient)}
	values, failures, err := c.BalanceAtHashBatch(t.Context(), addresses, hash)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1), values[0])
	require.Nil(t, values[1], "a failed member does not become zero")
	require.ErrorContains(t, failures[1], "missing trie node")
	require.Equal(t, big.NewInt(3), values[2])
	require.NoError(t, failures[0])
	require.NoError(t, failures[2])
	require.Nil(t, values[3], "a null response must not become zero")
	require.ErrorContains(t, failures[3], "no result")
}
