package feedelegation

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestStatsCountFeeDelegationTxs stores live StableNet block 33 (a type 2
// and a fee delegation 0x16 transaction) and computes the statistics: only
// the 0x16 transaction is sponsored, by its fee payer, for gasUsed times its
// effective gas price.
func TestStatsCountFeeDelegationTxs(t *testing.T) {
	raw, err := os.ReadFile("../testdata/live_vectors.json")
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

	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	require.NoError(t, db.SetBlock(ctx, b))
	require.NoError(t, db.SetLatestHeight(ctx, 33))
	for _, r := range rs {
		require.NoError(t, db.SetReceipt(ctx, r))
	}
	fdTx, fdReceipt := b.Transactions[1], rs[1]
	fd, ok := stablenet.FeeDelegationOf(fdTx)
	require.True(t, ok)
	fee := new(big.Int).Mul(new(big.Int).SetUint64(fdReceipt.GasUsed), fdReceipt.EffectiveGasPrice)

	s := NewStats(db)
	stats, err := s.GetFeeDelegationStats(ctx, 33, 33)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stats.TotalFeeDelegatedTxs)
	require.Equal(t, 0, fee.Cmp(stats.TotalFeesSaved))
	require.InDelta(t, 50.0, stats.AdoptionRate, 1e-9)

	top, total, err := s.GetTopFeePayers(ctx, 10, 33, 33)
	require.NoError(t, err)
	require.Equal(t, uint64(1), total)
	require.Equal(t, fd.FeePayer, top[0].Address)

	payer, err := s.GetFeePayerStats(ctx, fd.FeePayer, 33, 33)
	require.NoError(t, err)
	require.Equal(t, uint64(1), payer.TxCount)
	require.Equal(t, 0, fee.Cmp(payer.TotalFeesPaid))
	require.InDelta(t, 100.0, payer.Percentage, 1e-9)
}
