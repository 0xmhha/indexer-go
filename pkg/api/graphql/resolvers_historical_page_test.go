package graphql

import (
	"context"
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

func TestBalanceHistoryCursorPages(t *testing.T) {
	store := &cursorHistoryStorage{&mockHistoricalStorage{mockStorage: &mockStorage{}}}
	handler, err := NewHandler(store, zap.NewNop())
	require.NoError(t, err)

	query := func(after string) map[string]interface{} {
		t.Helper()
		result := handler.ExecuteQuery(`query($after: String) {
			balanceHistory(address: "0x456", fromBlock: "0", toBlock: "100", pagination: {limit: 1, after: $after}) {
				nodes { blockNumber }
				pageInfo { hasNextPage hasPreviousPage startCursor endCursor }
			}
		}`, map[string]interface{}{"after": after})
		require.Empty(t, result.Errors)
		return result.Data.(map[string]interface{})["balanceHistory"].(map[string]interface{})
	}

	first := query("")
	info := first["pageInfo"].(map[string]interface{})
	assert.Equal(t, true, info["hasNextPage"])
	assert.Equal(t, false, info["hasPreviousPage"])
	assert.Equal(t, "c1", info["endCursor"], "endCursor is the next page's cursor")

	second := query("c1")
	assert.Equal(t, "2", second["nodes"].([]interface{})[0].(map[string]interface{})["blockNumber"])
	info = second["pageInfo"].(map[string]interface{})
	assert.Equal(t, false, info["hasNextPage"])
	assert.Equal(t, true, info["hasPreviousPage"])
	assert.Equal(t, "c1", info["startCursor"])
	assert.Nil(t, info["endCursor"])

	result := handler.ExecuteQuery(`{
		balanceHistory(address: "0x456", fromBlock: "0", toBlock: "100", pagination: {after: "bogus"}) { totalCount }
	}`, nil)
	require.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0].Message, "invalid pagination cursor")
}
