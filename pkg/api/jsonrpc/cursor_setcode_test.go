package jsonrpc

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

// TestSetCodeListsPageByCursor follows nextCursor through the SetCode
// address lists on a Pebble storage: every authorization comes once, newest
// first, and a cursor that is not one of the list's is invalid params.
func TestSetCodeListsPageByCursor(t *testing.T) {
	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	target := common.HexToAddress("0x0000000000000000000000000000000000007b01")
	authority := common.HexToAddress("0x0000000000000000000000000000000000007a01")
	for b := uint64(1); b <= 5; b++ {
		require.NoError(t, st.SaveSetCodeAuthorization(ctx, &port.SetCodeAuthorizationRecord{
			TxHash: common.BigToHash(new(big.Int).SetUint64(b)), BlockNumber: b,
			TargetAddress: target, AuthorityAddress: authority,
			ChainID: big.NewInt(1), R: big.NewInt(1), S: big.NewInt(1), Applied: true,
		}))
	}
	srv := NewServer(st, zap.NewNop())

	for method, arg := range map[string]string{
		"getSetCodeAuthorizationsByTarget":    fmt.Sprintf(`"target": "%s"`, target.Hex()),
		"getSetCodeAuthorizationsByAuthority": fmt.Sprintf(`"authority": "%s"`, authority.Hex()),
	} {
		t.Run(method, func(t *testing.T) {
			var got []string
			after := ""
			for i := 0; ; i++ {
				require.Less(t, i, 5, "cursor paging does not end")
				params := fmt.Sprintf(`{%s, "limit": 2, "after": %q}`, arg, after)
				res, rpcErr := srv.HandleMethodDirect(ctx, method, json.RawMessage(params))
				require.Nil(t, rpcErr)
				m := res.(map[string]interface{})
				for _, a := range m["authorizations"].([]interface{}) {
					got = append(got, fmt.Sprint(a.(map[string]interface{})["blockNumber"]))
				}
				after = m["nextCursor"].(string)
				if after == "" {
					break
				}
			}
			assert.Equal(t, []string{"5", "4", "3", "2", "1"}, got)

			// Offset clients are unchanged.
			res, rpcErr := srv.HandleMethodDirect(ctx, method, json.RawMessage(fmt.Sprintf(`{%s, "limit": 2, "offset": 4}`, arg)))
			require.Nil(t, rpcErr)
			m := res.(map[string]interface{})
			assert.Len(t, m["authorizations"], 1)
			assert.Equal(t, "", m["nextCursor"])

			_, rpcErr = srv.HandleMethodDirect(ctx, method, json.RawMessage(fmt.Sprintf(`{%s, "after": "not a cursor!"}`, arg)))
			require.NotNil(t, rpcErr)
			assert.Equal(t, InvalidParams, rpcErr.Code)
		})
	}
}
