package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestFeeDelegationTxOutput serves live StableNet block 33 (a type 2 and a
// fee delegation 0x16 transaction) and checks the fields nodes return: the
// fee payer signature as fv/fr/fs and, for mined fee-market transactions,
// the effective gas price as gasPrice.
func TestFeeDelegationTxOutput(t *testing.T) {
	raw, err := os.ReadFile("../../chains/stablenet/testdata/live_vectors.json")
	require.NoError(t, err)
	var v struct {
		Blocks          map[string]json.RawMessage `json:"blocks"`
		Receipts        map[string]json.RawMessage `json:"receipts"`
		RawTransactions map[string]string          `json:"rawTransactions"`
	}
	require.NoError(t, json.Unmarshal(raw, &v))
	p := stablenet.New()
	b, err := p.DecodeBlock(v.Blocks["33"])
	require.NoError(t, err)
	rs, err := p.DecodeReceipts(v.Receipts["33"])
	require.NoError(t, err)

	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	require.NoError(t, st.SetModelBlock(ctx, b))
	require.NoError(t, st.SetLatestHeight(ctx, 33))
	for _, r := range rs {
		require.NoError(t, st.SetModelReceipt(ctx, r))
	}

	srv := NewServer(st, zap.NewNop())
	var nodeTxs []map[string]any
	var block struct {
		Transactions []map[string]any `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(v.Blocks["33"], &block))
	nodeTxs = block.Transactions

	for i, tx := range b.Transactions {
		params, _ := json.Marshal(map[string]string{"hash": tx.Hash.Hex()})
		res, rpcErr := srv.HandleMethodDirect(ctx, "getTxResult", params)
		require.Nil(t, rpcErr)
		got := res.(map[string]interface{})
		require.Equal(t, fmt.Sprintf("0x%x", rs[i].EffectiveGasPrice), got["gasPrice"], "tx %d gasPrice", i)
		for _, f := range []string{"fv", "fr", "fs", "feePayer"} {
			want, ok := nodeTxs[i][f]
			if !ok {
				require.NotContains(t, got, f, "tx %d", i)
				continue
			}
			require.True(t, strings.EqualFold(want.(string), got[f].(string)), "tx %d %s: node %v, indexer %v", i, f, want, got[f])
		}
	}
}
