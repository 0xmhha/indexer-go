package graphql

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// TestUserOpListsFollowCursors: the UserOperation and bundler lists return
// endCursor for the next page, continue after a cursor, and reject a cursor
// they did not issue.
func TestUserOpListsFollowCursors(t *testing.T) {
	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	sender := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	for i := int64(1); i <= 3; i++ {
		require.NoError(t, st.SaveUserOp(ctx, &userop.UserOperation{
			Hash: common.BigToHash(common.Big1.SetInt64(i)), Sender: sender,
			TransactionHash: common.BigToHash(common.Big1.SetInt64(100 + i)), BlockNumber: uint64(i),
		}))
		require.NoError(t, st.UpdateBundlerStats(ctx, &userop.BundlerStats{Address: common.BigToAddress(common.Big1.SetInt64(i))}))
	}
	h, err := NewHandler(st, zap.NewNop())
	require.NoError(t, err)

	query := func(field, args, after string) (nodes []interface{}, info map[string]interface{}, errs []string) {
		pagination := `{limit: 2}`
		if after != "" {
			pagination = fmt.Sprintf(`{limit: 2, after: %q}`, after)
		}
		res := h.ExecuteQuery(fmt.Sprintf(`{ %s(%spagination: %s) { nodes { __typename } pageInfo { hasNextPage hasPreviousPage startCursor endCursor } } }`, field, args, pagination), nil)
		for _, e := range res.Errors {
			errs = append(errs, e.Message)
		}
		if len(errs) > 0 {
			return nil, nil, errs
		}
		conn := res.Data.(map[string]interface{})[field].(map[string]interface{})
		return conn["nodes"].([]interface{}), conn["pageInfo"].(map[string]interface{}), nil
	}

	for _, l := range []struct{ field, args string }{
		{"userOperationsBySender", fmt.Sprintf("sender: %q, ", sender.Hex())},
		{"userOperations", fmt.Sprintf("sender: %q, ", sender.Hex())},
		{"bundlers", ""},
	} {
		t.Run(l.field, func(t *testing.T) {
			nodes, info, errs := query(l.field, l.args, "")
			require.Empty(t, errs)
			assert.Len(t, nodes, 2)
			assert.Equal(t, true, info["hasNextPage"])
			assert.Nil(t, info["startCursor"])
			cursor, ok := info["endCursor"].(string)
			require.True(t, ok, "endCursor continues after the first page")

			nodes, info, errs = query(l.field, l.args, cursor)
			require.Empty(t, errs)
			assert.Len(t, nodes, 1)
			assert.Equal(t, false, info["hasNextPage"])
			assert.Equal(t, true, info["hasPreviousPage"])
			assert.Equal(t, cursor, info["startCursor"])
			assert.Nil(t, info["endCursor"])

			_, _, errs = query(l.field, l.args, "not a cursor!")
			assert.Equal(t, []string{"invalid pagination cursor"}, errs)
		})
	}

	_, _, errs := query("userOperations", "", "some cursor")
	assert.Equal(t, []string{"invalid pagination cursor"}, errs, "the recent list issues no cursors")
}
