package stablenet

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"testing"
)

type epochHeaders map[uint64]*model.Block

func (h epochHeaders) Block(_ context.Context, n uint64) (*model.Block, error) { return h[n], nil }

func TestPreparedEpochRejectsDifferentForkHeaders(t *testing.T) {
	var tracker EpochTracker
	parent := &model.Block{Number: 1, Hash: common.HexToHash("0x11")}
	block := &model.Block{Number: 2, Hash: common.HexToHash("0x22"), ParentHash: common.HexToHash("0x99")}
	_, err := tracker.For(context.Background(), block, epochHeaders{1: parent})
	require.ErrorIs(t, err, chains.ErrAccountingFork)
	_, remembered := tracker.Applied(block.Hash)
	require.False(t, remembered, "a failed speculative epoch must not poison the cache")
}
