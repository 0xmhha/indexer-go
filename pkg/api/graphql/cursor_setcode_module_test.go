package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestSetCodeAndModuleListsPageByCursor follows pageInfo.endCursor through
// the SetCode and module list queries on a Pebble storage: every item comes
// once, in list order, and a cursor that is not one of the list's is
// rejected.
func TestSetCodeAndModuleListsPageByCursor(t *testing.T) {
	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	target := common.HexToAddress("0x0000000000000000000000000000000000007b01")
	authority := common.HexToAddress("0x0000000000000000000000000000000000007a01")
	account := common.HexToAddress("0x0000000000000000000000000000000000005a01")
	for b := uint64(1); b <= 5; b++ {
		require.NoError(t, st.SaveSetCodeAuthorization(ctx, &port.SetCodeAuthorizationRecord{
			TxHash: common.BigToHash(new(big.Int).SetUint64(b)), BlockNumber: b,
			TargetAddress: target, AuthorityAddress: authority,
			ChainID: big.NewInt(1), R: big.NewInt(1), S: big.NewInt(1), Applied: true,
		}))
		module := common.BigToAddress(new(big.Int).SetUint64(0x5b00 + b))
		require.NoError(t, st.SaveInstalledModule(ctx, &port.InstalledModule{
			Account: account, Module: module, ModuleType: port.ModuleTypeValidator, InstalledAt: b, Active: true,
		}))
		require.NoError(t, st.UpdateModuleStats(ctx, &port.ModuleStats{Module: module, ModuleType: port.ModuleTypeValidator, TotalInstalls: b}))
	}
	h, err := NewHandler(st, zap.NewNop())
	require.NoError(t, err)

	for _, c := range []struct {
		field, args, node string
		want              []string
	}{
		{"setCodeAuthorizationsByTarget", fmt.Sprintf(`target: "%s"`, target.Hex()), "blockNumber", []string{"5", "4", "3", "2", "1"}},
		{"setCodeAuthorizationsByAuthority", fmt.Sprintf(`authority: "%s"`, authority.Hex()), "blockNumber", []string{"5", "4", "3", "2", "1"}},
		{"installedModules", fmt.Sprintf(`account: "%s"`, account.Hex()), "installedAt", []string{"5", "4", "3", "2", "1"}},
		{"installedModules", `moduleType: "VALIDATOR"`, "installedAt", []string{"5", "4", "3", "2", "1"}},
		{"listModuleStats", "", "totalInstalls", nil},
	} {
		t.Run(c.field+"("+c.args+")", func(t *testing.T) {
			query := func(pagination string) (map[string]interface{}, error) {
				args := "pagination: {" + pagination + "}"
				if c.args != "" {
					args = c.args + ", " + args
				}
				res := h.ExecuteQuery(fmt.Sprintf(`{ %s(%s) { nodes { %s } totalCount pageInfo { hasNextPage endCursor } } }`, c.field, args, c.node), nil)
				if len(res.Errors) > 0 {
					return nil, res.Errors[0]
				}
				raw, err := json.Marshal(res.Data)
				require.NoError(t, err)
				var out map[string]map[string]interface{}
				require.NoError(t, json.Unmarshal(raw, &out))
				return out[c.field], nil
			}

			var got []string
			pagination := "limit: 2"
			for i := 0; ; i++ {
				require.Less(t, i, 5, "cursor paging does not end")
				conn, err := query(pagination)
				require.NoError(t, err)
				nodes := conn["nodes"].([]interface{})
				assert.Equal(t, float64(len(nodes)), conn["totalCount"], "totalCount is the page's count")
				for _, n := range nodes {
					got = append(got, n.(map[string]interface{})[c.node].(string))
				}
				info := conn["pageInfo"].(map[string]interface{})
				if !info["hasNextPage"].(bool) {
					assert.Nil(t, info["endCursor"])
					break
				}
				pagination = fmt.Sprintf(`limit: 2, after: "%s"`, info["endCursor"])
			}
			if c.want != nil {
				assert.Equal(t, c.want, got)
			} else {
				assert.ElementsMatch(t, []string{"1", "2", "3", "4", "5"}, got)
			}

			_, err := query(`limit: 2, after: "not a cursor!"`)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid pagination cursor")
		})
	}

	t.Run("RecentModulesTakeNoCursor", func(t *testing.T) {
		res := h.ExecuteQuery(`{ installedModules(pagination: {limit: 2, after: "x"}) { totalCount } }`, nil)
		require.NotEmpty(t, res.Errors)
		assert.Contains(t, res.Errors[0].Message, "invalid pagination cursor")
	})
}
