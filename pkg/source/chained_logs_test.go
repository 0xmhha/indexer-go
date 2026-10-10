package source

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// fakeLogs answers one log per block and records the ranges and heights
// it was asked for.
type fakeLogs struct {
	name        string
	first, last uint64
	ranges      [][2]uint64
	heights     []uint64
}

func (f *fakeLogs) Profile() chains.Profile                             { return nil }
func (f *fakeLogs) Head(context.Context) (uint64, error)                { return f.last, nil }
func (f *fakeLogs) Range() (uint64, uint64)                             { return f.first, f.last }
func (f *fakeLogs) HashAt(context.Context, uint64) (common.Hash, error) { return common.Hash{}, nil }
func (f *fakeLogs) BlockWithReceipts(context.Context, uint64) (*model.Block, []*model.Receipt, error) {
	return nil, nil, nil
}
func (f *fakeLogs) LogsInRange(_ context.Context, from, to uint64) ([]*model.Log, error) {
	f.ranges = append(f.ranges, [2]uint64{from, to})
	var out []*model.Log
	for n := from; n <= to; n++ {
		out = append(out, &model.Log{BlockNumber: n})
	}
	return out, nil
}
func (f *fakeLogs) Headers(_ context.Context, heights []uint64) (map[uint64]*model.Block, error) {
	f.heights = append(f.heights, heights...)
	out := map[uint64]*model.Block{}
	for _, n := range heights {
		out[n] = &model.Block{Number: n, Extra: []byte(f.name)}
	}
	return out, nil
}

// TestChainedLogsSplitsAtTheArchive: a range is read from the archive
// inside its range and from the node outside it, in chain order, and each
// header from the source that holds it.
func TestChainedLogsSplitsAtTheArchive(t *testing.T) {
	ctx := context.Background()
	archive := &fakeLogs{name: "archive", first: 10, last: 19}
	node := &fakeLogs{name: "node", last: 100}
	c := NewChainedLogs(archive, node)

	logs, err := c.LogsInRange(ctx, 5, 25)
	require.NoError(t, err)
	require.Len(t, logs, 21)
	for i, l := range logs {
		require.Equal(t, uint64(5+i), l.BlockNumber, "chain order")
	}
	assert.Equal(t, [][2]uint64{{10, 19}}, archive.ranges)
	assert.Equal(t, [][2]uint64{{5, 9}, {20, 25}}, node.ranges)

	archive.ranges, node.ranges = nil, nil
	_, err = c.LogsInRange(ctx, 12, 15)
	require.NoError(t, err)
	assert.Equal(t, [][2]uint64{{12, 15}}, archive.ranges)
	assert.Empty(t, node.ranges, "inside the archive only")

	hs, err := c.Headers(ctx, []uint64{3, 10, 19, 20})
	require.NoError(t, err)
	require.Len(t, hs, 4)
	assert.Equal(t, "archive", string(hs[10].Extra))
	assert.Equal(t, "archive", string(hs[19].Extra))
	assert.Equal(t, "node", string(hs[3].Extra))
	assert.Equal(t, "node", string(hs[20].Extra))
}
