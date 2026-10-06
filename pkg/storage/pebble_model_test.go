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

// TestModelStorageKeepsStableNetIdentity stores a block captured from
// go-stablenet (WBFT block hash, one fee delegation transaction) through the
// model methods and finds everything again under the hashes the node reports
// (D13, D16).
func TestModelStorageKeepsStableNetIdentity(t *testing.T) {
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

	s, cleanup := setupTestStorage(t)
	defer cleanup()
	st := s.(*PebbleStorage)
	ctx := context.Background()

	require.NoError(t, st.SetBlock(ctx, b))
	for _, r := range rs {
		require.NoError(t, st.SetReceipt(ctx, r))
	}

	got, err := st.GetBlockByHash(ctx, b.Hash)
	require.NoError(t, err, "the block is found under its WBFT hash")
	require.Equal(t, b.Hash, got.Hash)
	byHeight, err := st.GetBlock(ctx, 33)
	require.NoError(t, err)
	require.Equal(t, b.Hash, byHeight.Hash)

	fdTx := b.Transactions[1]
	tx, loc, err := st.GetTransaction(ctx, fdTx.Hash)
	require.NoError(t, err, "the fee delegation tx is found under its canonical hash")
	require.Equal(t, uint8(stablenet.FeeDelegationTxType), tx.Type)
	require.Equal(t, stablenet.FeePayerOf(fdTx), stablenet.FeePayerOf(tx))
	require.Equal(t, uint64(1), loc.TxIndex)
	require.Equal(t, b.Hash, loc.BlockHash)

	r, err := st.GetReceipt(ctx, fdTx.Hash)
	require.NoError(t, err)
	require.Equal(t, uint8(stablenet.FeeDelegationTxType), r.Type)
	require.Equal(t, rs[1].GasUsed, r.GasUsed)

	// Legacy readers still work through the bridge, with its known limits.
	legacyTx, _, err := st.GetTransaction(ctx, fdTx.Hash)
	require.NoError(t, err)
	require.Equal(t, fdTx.Nonce, legacyTx.Nonce)
	legacyReceipt, err := st.GetReceipt(ctx, fdTx.Hash)
	require.NoError(t, err)
	require.Equal(t, fdTx.Hash, legacyReceipt.TxHash)
	_, err = st.GetBlockByHash(ctx, b.Hash)
	require.NoError(t, err)
}

func TestModelReceiptValidation(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()
	st := s.(*PebbleStorage)
	require.ErrorIs(t, st.SetReceipt(context.Background(), nil), port.ErrInvalidReceipt)
}
