package api

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/consensus"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestWBFTResolversWithData runs every WBFT query through the GraphQL
// handler, which serves them only because this package registered them.
func TestWBFTResolversWithData(t *testing.T) {
	ctx := context.Background()
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	store := consensus.NewStore(db, zap.NewNop())

	validator := common.HexToAddress("0x10")
	require.NoError(t, db.SetBlock(ctx, modelBlock(createBlock(1, validator))))
	require.NoError(t, store.SaveWBFTBlockExtra(ctx, &consensus.WBFTBlockExtra{
		BlockNumber: 1, BlockHash: common.HexToHash("0xw1"), Round: 1,
		PreparedSeal:  &consensus.WBFTAggregatedSeal{Sealers: []byte{0xFF}, Signature: make([]byte, 96)},
		CommittedSeal: &consensus.WBFTAggregatedSeal{Sealers: []byte{0xFF}, Signature: make([]byte, 96)},
		GasTip:        big.NewInt(100),
		Timestamp:     1700000000,
	}))
	require.NoError(t, store.SaveEpochInfo(ctx, &consensus.EpochInfo{
		EpochNumber: 1, BlockNumber: 1, Validators: []uint32{0},
		Candidates: []consensus.Candidate{{Address: validator, Diligence: 999999}},
	}))
	require.NoError(t, store.UpdateValidatorSigningStats(ctx, 1, []*consensus.ValidatorSigningActivity{
		{BlockNumber: 1, ValidatorAddress: validator, SignedPrepare: true, SignedCommit: true, Timestamp: 1700000000},
	}))

	handler, err := graphql.NewHandler(db, zap.NewNop())
	require.NoError(t, err)

	tests := []struct {
		field string
		query string
	}{
		{"wbftBlockExtra", `{ wbftBlockExtra(blockNumber: "1") { blockNumber round gasTip preparedSeal { signature } committedSeal { signature } } }`},
		{"wbftBlock", `{ wbftBlock(number: "1") { blockNumber round } }`},
		{"epochInfo", `{ epochInfo(epochNumber: "1") { epochNumber blockNumber validators candidates { address diligence } } }`},
		{"epochByNumber", `{ epochByNumber(number: "1") { epochNumber } }`},
		{"latestEpochInfo", `{ latestEpochInfo { epochNumber blockNumber } }`},
		{"epochs", `{ epochs { nodes { epochNumber blockNumber } totalCount } }`},
		{"validatorSigningStats", `{ validatorSigningStats(validatorAddress: "0x0000000000000000000000000000000000000010", fromBlock: "1", toBlock: "100") { validatorAddress prepareSignCount commitSignCount signingRate blocksProposed proposalRate } }`},
		{"allValidatorsSigningStats", `{ allValidatorsSigningStats(fromBlock: "1", toBlock: "100") { nodes { validatorAddress signingRate } totalCount } }`},
		{"validatorSigningActivity", `{ validatorSigningActivity(validatorAddress: "0x0000000000000000000000000000000000000010", fromBlock: "1", toBlock: "100") { nodes { blockNumber signedPrepare signedCommit round } totalCount } }`},
		{"blockSigners", `{ blockSigners(blockNumber: "1") { preparers committers } }`},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			require.Empty(t, result.Errors)
			data, _ := result.Data.(map[string]interface{})
			assert.NotNil(t, data[tc.field])
		})
	}
}

func createBlock(number uint64, miner common.Address) *types.Block {
	return types.NewBlockWithHeader(&types.Header{Number: new(big.Int).SetUint64(number), Coinbase: miner, Time: 1700000000})
}
