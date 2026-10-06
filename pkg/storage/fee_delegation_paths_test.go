package storage

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestLegacyReadsFindFeeDelegationTxs stores live StableNet block 33 (a
// type 2 and a fee delegation 0x16 transaction) and reads it back through
// the paths that walk a block's transactions. The go-ethereum view of the
// block rebuilds 0x16 as type 2 under another hash; these paths must use
// the hash the chain reports, or the 0x16 receipt is missed.
func TestLegacyReadsFindFeeDelegationTxs(t *testing.T) {
	raw, err := os.ReadFile("../chains/stablenet/testdata/live_vectors.json")
	require.NoError(t, err)
	var v struct {
		Blocks   map[string]json.RawMessage `json:"blocks"`
		Receipts map[string]json.RawMessage `json:"receipts"`
	}
	require.NoError(t, json.Unmarshal(raw, &v))
	p := stablenet.New()
	b, err := p.DecodeBlock(v.Blocks["33"])
	require.NoError(t, err)
	rs, err := p.DecodeReceipts(v.Receipts["33"])
	require.NoError(t, err)

	s := newTestPebble(t)
	ctx := context.Background()
	require.NoError(t, s.SetBlock(ctx, b))
	require.NoError(t, s.SetLatestHeight(ctx, 33))
	for _, r := range rs {
		require.NoError(t, s.SetReceipt(ctx, r))
	}
	fdTx := b.Transactions[1]
	_, ok := stablenet.FeeDelegationOf(fdTx)
	require.True(t, ok)
	for _, tx := range b.Transactions {
		require.NoError(t, s.AddTransactionToAddressIndex(ctx, tx.From, tx.Hash))
	}

	receipts, err := s.GetReceiptsByBlockNumber(ctx, 33)
	require.NoError(t, err)
	require.Len(t, receipts, 2, "the 0x16 receipt is found")
	require.Equal(t, fdTx.Hash, receipts[1].TxHash)

	missing, err := s.GetMissingReceipts(ctx, 33)
	require.NoError(t, err)
	require.Empty(t, missing, "gap detection does not report the 0x16 receipt as missing")

	found, err := s.Search(ctx, fdTx.Hash.Hex(), []string{"transaction"}, 10)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, fdTx.Hash.Hex(), found[0].Value)
	require.Equal(t, fdTx.From.Hex(), found[0].Metadata["from"])

	// The fee delegation filter follows the transaction as the chain
	// profile decoded it. Results are compared by position: the go-ethereum
	// view of a 0x16 transaction has another hash.
	for _, tx := range b.Transactions {
		for _, want := range []bool{true, false} {
			filter := port.DefaultTransactionFilter()
			filter.IsFeeDelegated = &want
			got, err := s.GetTransactionsByAddressFiltered(ctx, tx.From, filter, 10, 0)
			require.NoError(t, err)
			var gotIdx, wantIdx []uint64
			for _, r := range got {
				gotIdx = append(gotIdx, r.Location.TxIndex)
			}
			for i, other := range b.Transactions {
				if other.From == tx.From && (other.Hash == fdTx.Hash) == want {
					wantIdx = append(wantIdx, uint64(i))
				}
			}
			require.ElementsMatch(t, wantIdx, gotIdx, "sender %s, fee delegated=%v", tx.From.Hex(), want)
		}
	}
}
