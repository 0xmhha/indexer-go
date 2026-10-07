package jsonrpc

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// cursorHistoryStorage lists two balance snapshots one per page: the first
// page returns the cursor "c1", which continues to the second.
type cursorHistoryStorage struct {
	*mockHistoricalStorage
}

func (m *cursorHistoryStorage) GetBalanceHistory(_ context.Context, _ common.Address, _, _ uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	snap := func(n uint64) port.BalanceSnapshot {
		return port.BalanceSnapshot{BlockNumber: n, Balance: big.NewInt(int64(n)), Delta: big.NewInt(1)}
	}
	switch page.After {
	case "":
		return []port.BalanceSnapshot{snap(1)}, "c1", nil
	case "c1":
		return []port.BalanceSnapshot{snap(2)}, "", nil
	default:
		return nil, "", port.ErrInvalidCursor
	}
}

func TestGetBalanceHistoryCursorPages(t *testing.T) {
	ctx := context.Background()
	server := NewServer(&cursorHistoryStorage{&mockHistoricalStorage{mockStorage: &mockStorage{}}}, zap.NewNop())
	call := func(params string) (map[string]interface{}, *Error) {
		t.Helper()
		result, err := server.HandleMethodDirect(ctx, "getBalanceHistory", json.RawMessage(params))
		if err != nil {
			return nil, err
		}
		return result.(map[string]interface{}), nil
	}

	first, rpcErr := call(`{"address": "0x456", "fromBlock": 0, "toBlock": 100, "limit": 1}`)
	require.Nil(t, rpcErr)
	assert.Equal(t, "c1", first["nextCursor"], "the cursor of the next page")
	assert.Equal(t, true, first["pageInfo"].(map[string]interface{})["hasNextPage"])

	second, rpcErr := call(`{"address": "0x456", "fromBlock": 0, "toBlock": 100, "limit": 1, "after": "c1"}`)
	require.Nil(t, rpcErr)
	assert.Equal(t, "0x2", second["nodes"].([]interface{})[0].(map[string]interface{})["blockNumber"])
	assert.Nil(t, second["nextCursor"], "no cursor after the last page")
	info := second["pageInfo"].(map[string]interface{})
	assert.Equal(t, false, info["hasNextPage"])
	assert.Equal(t, true, info["hasPreviousPage"])

	_, rpcErr = call(`{"address": "0x456", "fromBlock": 0, "toBlock": 100, "after": "bogus"}`)
	require.NotNil(t, rpcErr)
	assert.Equal(t, InvalidParams, rpcErr.Code)
	assert.Equal(t, "invalid pagination cursor", rpcErr.Message)
}
