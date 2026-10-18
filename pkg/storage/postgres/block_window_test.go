package postgres

import (
	"context"
	"math"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestLatestBlockWindowHeightSemanticsAndBindings(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	_, _, err := s.GetLatestBlockWindow(ctx, 0, 8)
	require.ErrorIs(t, err, port.ErrNotFound)
	for _, height := range []uint64{0, 1, 4, 5, 6, 9} {
		require.NoError(t, s.SetBlock(ctx, &model.Block{Number: height, Hash: common.Hash{byte(height + 1)}}))
	}
	require.NoError(t, s.SetLatestHeight(ctx, 6))
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	require.NotNil(t, lease)
	defer lease.Close()
	for _, window := range [][2]int{{0, 8}, {0, 2}, {1, 3}, {2, 2}, {6, 8}, {7, 8}, {math.MaxInt, math.MaxInt}} {
		queries := 0
		observed := ObserveQueries(lease.Context(ctx), func(q QueryObservation) {
			if q.Stage == "query" {
				queries++
			}
		})
		height, got, err := s.GetLatestBlockWindow(observed, window[0], window[1])
		require.NoError(t, err)
		require.Equal(t, uint64(6), height)
		require.Equal(t, 1, queries, "one statement even for a gap or empty range")
		var want []*model.Block
		if height >= uint64(window[0]) {
			end := height - uint64(window[0])
			start := uint64(0)
			if end >= uint64(window[1]-1) {
				start = end - uint64(window[1]-1)
			}
			want, err = s.GetBlocks(ctx, start, end)
			require.NoError(t, err)
		}
		require.Equal(t, len(want), len(got))
		for i := range want {
			require.Equal(t, want[i].Number, got[i].Number)
			require.Equal(t, want[i].Hash, got[i].Hash)
		}
	}
	for _, window := range [][2]int{{-1, 1}, {0, 0}, {0, -1}} {
		_, _, err := s.GetLatestBlockWindow(ctx, window[0], window[1])
		require.Error(t, err)
	}
}

func TestLatestBlockWindowSameTransactionAndCorruptData(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	require.NoError(t, s.SetBlock(ctx, &model.Block{Number: 1, Hash: common.Hash{1}}))
	require.NoError(t, s.SetLatestHeight(ctx, 1))
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	defer lease.Close()
	bound, tx, err := s.BeginBlock(lease.Context(ctx))
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, s.SetBlock(bound, &model.Block{Number: 2, Hash: common.Hash{2}}))
	require.NoError(t, s.SetLatestHeight(bound, 2))
	height, blocks, err := s.GetLatestBlockWindow(bound, 0, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)
	require.Len(t, blocks, 1)
	require.Equal(t, uint64(2), blocks[0].Number)
	height, blocks, err = s.GetLatestBlockWindow(lease.Context(ctx), 0, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), height, "uncommitted tip must not escape")
	require.Equal(t, uint64(1), blocks[0].Number)
	_, err = s.q(bound).Exec(bound, "UPDATE meta SET value = $1 WHERE name = $2", []byte{1}, metaLatestHeight)
	require.NoError(t, err)
	_, _, err = s.GetLatestBlockWindow(bound, 0, 8)
	require.ErrorContains(t, err, "has 1 bytes, want 8")
	tx.Rollback()
	_, err = s.pool.Exec(ctx, "UPDATE blocks SET data = $1 WHERE number = $2", []byte("invalid block"), int64(1))
	require.NoError(t, err)
	_, _, err = s.GetLatestBlockWindow(lease.Context(ctx), 0, 8)
	require.Error(t, err, "decode errors must not become missing blocks")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = s.GetLatestBlockWindow(lease.Context(cancelled), 0, 8)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLatestBlockWindowUnsignedBoundary(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	for i, height := range []uint64{math.MaxUint64 - 1, math.MaxUint64} {
		require.NoError(t, s.SetBlock(ctx, &model.Block{Number: height, Hash: common.Hash{byte(i + 1)}}))
	}
	require.NoError(t, s.SetLatestHeight(ctx, math.MaxUint64))
	height, got, err := s.GetLatestBlockWindow(ctx, 0, 2)
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint64), height)
	want, err := s.GetBlocks(ctx, math.MaxUint64-1, math.MaxUint64)
	require.NoError(t, err)
	require.Equal(t, want, got)
}
