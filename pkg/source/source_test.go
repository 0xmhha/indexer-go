package source_test

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// fakeSource answers with blocks whose hash encodes the source and height.
type fakeSource struct {
	tag         byte
	hashTag     byte // tag used by HashAt when set
	head        uint64
	first, last uint64
}

func (f *fakeSource) Profile() chains.Profile              { return nil }
func (f *fakeSource) Head(context.Context) (uint64, error) { return f.head, nil }
func (f *fakeSource) Range() (uint64, uint64)              { return f.first, f.last }
func (f *fakeSource) HashAt(_ context.Context, n uint64) (common.Hash, error) {
	if f.hashTag != 0 {
		return common.Hash{f.hashTag, byte(n)}, nil
	}
	return common.Hash{f.tag, byte(n)}, nil
}
func (f *fakeSource) BlockWithReceipts(_ context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	if n > f.head {
		return nil, nil, source.ErrNotFound
	}
	return &model.Block{Number: n, Hash: common.Hash{f.tag, byte(n)}}, nil, nil
}

func TestChained(t *testing.T) {
	ctx := context.Background()
	archive := &fakeSource{tag: 'a', head: 99, first: 0, last: 99}
	node := &fakeSource{tag: 'n', head: 150}
	c := &source.Chained{First: archive, Then: node}

	for n, want := range map[uint64]byte{0: 'a', 99: 'a', 100: 'n', 150: 'n'} {
		b, _, err := c.BlockWithReceipts(ctx, n)
		require.NoError(t, err)
		require.Equal(t, want, b.Hash[0], "block %d", n)
		h, err := c.HashAt(ctx, n)
		require.NoError(t, err)
		require.Equal(t, want, h[0])
	}
	head, err := c.Head(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(150), head)

	// A node behind the archive: the head is the archive's end.
	c = &source.Chained{First: archive, Then: &fakeSource{tag: 'n', head: 10}}
	head, err = c.Head(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(99), head)
}

func TestChainedCheckJoin(t *testing.T) {
	ctx := context.Background()
	archive := &fakeSource{tag: 'a', hashTag: 'x', first: 10, last: 99}
	same := &fakeSource{tag: 'n', hashTag: 'x', head: 150}
	other := &fakeSource{tag: 'n', head: 150}

	require.NoError(t, (&source.Chained{First: archive, Then: same}).CheckJoin(ctx))
	require.ErrorIs(t, (&source.Chained{First: archive, Then: other}).CheckJoin(ctx), source.ErrDifferentChain)
	// The node has not reached the archive yet: nothing to compare.
	require.NoError(t, (&source.Chained{First: archive, Then: &fakeSource{tag: 'n', head: 5}}).CheckJoin(ctx))
	// The node is inside the archive range: compared at its head.
	require.ErrorIs(t, (&source.Chained{First: archive, Then: &fakeSource{tag: 'n', head: 50}}).CheckJoin(ctx), source.ErrDifferentChain)
}
