package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestFeeDelegationQueries runs the fee delegation queries through the
// GraphQL handler, which serves them only because this package registered
// them. An index without fee delegation transactions answers with zero statistics.
func TestFeeDelegationQueries(t *testing.T) {
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.SetLatestHeight(context.Background(), 0))
	handler, err := graphql.NewHandler(db, zap.NewNop())
	require.NoError(t, err)

	for _, tc := range []struct {
		field string
		query string
	}{
		{"feeDelegationStats", `{ feeDelegationStats { totalFeeDelegatedTxs totalFeesSaved adoptionRate avgFeeSaved } }`},
		{"feeDelegationStats", `{ feeDelegationStats(fromBlock: "0", toBlock: "100") { totalFeeDelegatedTxs } }`},
		{"topFeePayers", `{ topFeePayers { nodes { address txCount totalFeesPaid percentage } totalCount } }`},
		{"topFeePayers", `{ topFeePayers(limit: 5, fromBlock: "0", toBlock: "100") { nodes { address } } }`},
		{"feePayerStats", `{ feePayerStats(address: "0x0000000000000000000000000000000000000001") { address txCount totalFeesPaid percentage } }`},
		{"feePayerStats", `{ feePayerStats(address: "0x0000000000000000000000000000000000000001", fromBlock: "0", toBlock: "100") { address } }`},
	} {
		result := handler.ExecuteQuery(tc.query, nil)
		require.Empty(t, result.Errors, tc.query)
		data, _ := result.Data.(map[string]interface{})
		require.NotNil(t, data[tc.field], tc.query)
	}
}
