package graphql

import (
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	gql "github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type lazyBlocksStore struct {
	*mockStorage
	block            *model.Block
	counts, receipts int
}

func (s *lazyBlocksStore) GetBlock(context.Context, uint64) (*model.Block, error) {
	return s.block, nil
}
func (s *lazyBlocksStore) GetBlocks(_ context.Context, from, to uint64) ([]*model.Block, error) {
	if from <= 1 && to >= 1 {
		return []*model.Block{s.block}, nil
	}
	return []*model.Block{}, nil
}
func (s *lazyBlocksStore) GetBlockCount(context.Context) (uint64, error) { s.counts++; return 37, nil }
func (s *lazyBlocksStore) GetReceipt(context.Context, common.Hash) (*model.Receipt, error) {
	s.receipts++
	addr := common.Address{7}
	return &model.Receipt{ContractAddress: &addr}, nil
}
func newLazyStore() *lazyBlocksStore {
	return &lazyBlocksStore{mockStorage: &mockStorage{latestHeight: 1}, block: &model.Block{Number: 1, Hash: common.Hash{1}, Transactions: []*model.Transaction{{Hash: common.Hash{9}}}}}
}
func lazyQuery(t *testing.T, store *lazyBlocksStore, query string) map[string]interface{} {
	t.Helper()
	schema, err := NewSchema(store, zap.NewNop())
	require.NoError(t, err)
	result := gql.Do(gql.Params{Schema: schema.Schema(), Context: context.Background(), RequestString: query})
	require.Empty(t, result.Errors)
	return result.Data.(map[string]interface{})
}
func TestBlockFieldsAreResolvedOnDemand(t *testing.T) {
	for _, query := range []string{
		`{blocks(pagination:{limit:8}) {nodes{number hash transactionCount txCount}}}`,
		`{block(number:"1"){number hash transactionCount}}`,
		`{blocksRange(startNumber:"1",endNumber:"1",includeTransactions:false){blocks{number transactions{hash} transactionCount}}}`,
	} {
		store := newLazyStore()
		lazyQuery(t, store, query)
		require.Zero(t, store.counts)
		require.Zero(t, store.receipts, "metadata must not load contract-creation receipts")
	}
	store := newLazyStore()
	data := lazyQuery(t, store, `{blocks {a:totalCount ...Count nodes{transactionCount a:transactions{hash contractAddress} ...Transactions}}} fragment Count on BlockConnection {totalCount} fragment Transactions on Block {transactions{hash contractAddress}}`)
	require.Equal(t, 1, store.counts, "count aliases share one read")
	require.Equal(t, 1, store.receipts, "transaction aliases share one conversion")
	connection := data["blocks"].(map[string]interface{})
	require.Equal(t, 37, connection["a"])
	require.Equal(t, 37, connection["totalCount"])
	node := connection["nodes"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, 1, node["transactionCount"])
	require.Equal(t, node["a"], node["transactions"])
	tx := node["a"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, common.Address{7}.Hex(), tx["contractAddress"])
}
