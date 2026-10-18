package graphql

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	gql "github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type windowBlocksStore struct {
	*lazyBlocksStore
	windows, latest, ranges int
	offset, limit           int
	err                     error
}

func (s *windowBlocksStore) GetLatestHeight(ctx context.Context) (uint64, error) {
	s.latest++
	return s.lazyBlocksStore.GetLatestHeight(ctx)
}
func (s *windowBlocksStore) GetBlocks(ctx context.Context, from, to uint64) ([]*model.Block, error) {
	s.ranges++
	return s.lazyBlocksStore.GetBlocks(ctx, from, to)
}
func (s *windowBlocksStore) GetLatestBlockWindow(ctx context.Context, offset, limit int) (uint64, []*model.Block, error) {
	s.windows++
	s.offset, s.limit = offset, limit
	if s.err != nil {
		return 0, nil, s.err
	}
	height, err := s.lazyBlocksStore.GetLatestHeight(ctx)
	if err != nil {
		return 0, nil, err
	}
	range_, ok := calculateBlockRangeReverse(height, offset, limit)
	if !ok {
		return height, nil, nil
	}
	blocks, err := s.lazyBlocksStore.GetBlocks(ctx, range_.StartBlock, range_.EndBlock)
	return height, blocks, err
}

func TestLatestBlockWindowGraphQLParity(t *testing.T) {
	for _, args := range []string{
		`(pagination:{limit:8})`,
		`(pagination:{limit:1,offset:1})`,
		`(pagination:{limit:8,offset:2})`,
		`(filter:{timestampFrom:"1"})`,
		`(filter:{miner:"0x0000000000000000000000000000000000000000"})`,
		`(filter:{numberFrom:"1",numberTo:"1"})`,
	} {
		t.Run(args, func(t *testing.T) {
			query := `{blocks` + args + `{nodes{number hash transactionCount} pageInfo{hasNextPage hasPreviousPage startCursor endCursor} totalCount}}`
			legacy := newLazyStore()
			want := lazyQuery(t, legacy, query)
			store := &windowBlocksStore{lazyBlocksStore: newLazyStore()}
			schema, err := NewSchema(store, zap.NewNop())
			require.NoError(t, err)
			result := gql.Do(gql.Params{Schema: schema.Schema(), Context: t.Context(), RequestString: query})
			require.Empty(t, result.Errors)
			require.Equal(t, want, result.Data)
			if store.windows == 0 {
				require.Equal(t, 1, store.latest)
				require.Equal(t, 1, store.ranges, "explicit number filters keep the generic path")
			} else {
				require.Equal(t, 1, store.windows)
				require.Zero(t, store.latest)
				require.Zero(t, store.ranges, "no duplicate tip or block query")
			}
		})
	}
}

func TestLatestBlockWindowGraphQLErrors(t *testing.T) {
	for _, err := range []error{port.ErrNotFound, errors.New("window read failed")} {
		store := &windowBlocksStore{lazyBlocksStore: newLazyStore(), err: err}
		schema, e := NewSchema(store, zap.NewNop())
		require.NoError(t, e)
		result := gql.Do(gql.Params{Schema: schema.Schema(), Context: t.Context(), RequestString: `{blocks{nodes{number}}}`})
		if errors.Is(err, port.ErrNotFound) {
			require.Empty(t, result.Errors)
		} else {
			require.NotEmpty(t, result.Errors)
			require.Contains(t, result.Errors[0].Message, err.Error())
		}
		require.Zero(t, store.latest, "errors are not hidden by a fallback read")
		require.Zero(t, store.ranges)
	}
}
