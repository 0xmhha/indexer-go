package source_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
)

type blockOnlySource struct {
	*fakeSource
	reads int
	err   error
}

func (s *blockOnlySource) Block(ctx context.Context, n uint64) (*model.Block, error) {
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	b, _, err := s.BlockWithReceipts(ctx, n)
	return b, err
}

func TestChainedBlockOnly(t *testing.T) {
	archive := &fakeSource{tag: 'a', head: 99, first: 0, last: 99}
	node := &blockOnlySource{fakeSource: &fakeSource{tag: 'n', head: 150}}
	c := &source.Chained{First: archive, Then: node}
	for _, n := range []uint64{0, 99, 100, 150} {
		b, err := source.ReadBlock(context.Background(), c, n)
		require.NoError(t, err)
		want := byte('a')
		if n > 99 {
			want = 'n'
		}
		require.Equal(t, want, b.Hash[0])
	}
	require.Equal(t, 2, node.reads, "the joined node keeps its receipt-free path")
	_, err := source.ReadBlock(context.Background(), c, 151)
	require.ErrorIs(t, err, source.ErrNotFound)
	wantErr := errors.New("block RPC unavailable")
	node.err = wantErr
	_, err = source.ReadBlock(context.Background(), c, 100)
	require.ErrorIs(t, err, wantErr, "do not hide a block-only error by fetching receipts")
}
