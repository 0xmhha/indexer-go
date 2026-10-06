// Package e2e provides end-to-end tests for the indexer against real blockchain nodes.
// These tests require Anvil to be installed and available in the PATH.
package e2e

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/e2e/anvil"
	_ "github.com/0xmhha/indexer-go/pkg/chains/evm" // generic profile
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
)

// startAnvil starts an Anvil node for one test.
func startAnvil(t *testing.T, timeout time.Duration) (*anvil.TestInstance, context.Context) {
	t.Helper()
	anvil.SkipIfNoAnvil(t.Skip)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	instance := anvil.NewTestInstance(nil, zap.NewNop())
	require.NoError(t, instance.Start(ctx))
	t.Cleanup(instance.Stop)
	return instance, ctx
}

// TestProfileDetection: an Anvil node is indexed with the generic EVM
// profile.
func TestProfileDetection(t *testing.T) {
	instance, ctx := startAnvil(t, 30*time.Second)
	src, err := sourcerpc.Detect(ctx, instance.RPCClient())
	require.NoError(t, err)
	require.Equal(t, "evm", src.Profile().ID())
}

// TestForcedProfile: --adapter names a profile; a name that is no profile
// (anvil) falls back to detection.
func TestForcedProfile(t *testing.T) {
	instance, ctx := startAnvil(t, 30*time.Second)
	for _, name := range []string{"evm", "anvil", ""} {
		src, err := sourcerpc.Select(ctx, instance.RPCClient(), name)
		require.NoError(t, err, name)
		require.Equal(t, "evm", src.Profile().ID(), name)
	}
}

// TestSourceBlockFetching reads the head block through the profile source
// with the hash the node reports.
func TestSourceBlockFetching(t *testing.T) {
	instance, ctx := startAnvil(t, 30*time.Second)
	require.NoError(t, instance.MineBlocks(ctx, 5))
	src, err := sourcerpc.Detect(ctx, instance.RPCClient())
	require.NoError(t, err)

	head, err := src.Head(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, head, uint64(5))

	block, receipts, err := src.BlockWithReceipts(ctx, head)
	require.NoError(t, err)
	require.Equal(t, head, block.Number)
	require.Len(t, receipts, len(block.Transactions))

	want, err := instance.GetBlockHashByNumber(ctx, new(big.Int).SetUint64(head))
	require.NoError(t, err)
	require.Equal(t, want, block.Hash)
	hash, err := src.HashAt(ctx, head)
	require.NoError(t, err)
	require.Equal(t, want, hash)
}

// TestAnvilSpecificFeatures tests Anvil-specific RPC methods
func TestAnvilSpecificFeatures(t *testing.T) {
	instance, ctx := startAnvil(t, 30*time.Second)

	snapshotID, err := instance.Snapshot(ctx)
	require.NoError(t, err)
	require.NoError(t, instance.MineBlocks(ctx, 5))
	blockAfterMine, err := instance.GetLatestBlockNumber(ctx)
	require.NoError(t, err)

	require.NoError(t, instance.Revert(ctx, snapshotID))
	blockAfterRevert, err := instance.GetLatestBlockNumber(ctx)
	require.NoError(t, err)
	require.Less(t, blockAfterRevert, blockAfterMine, "block number decreases after revert")
}

// TestMultipleBlocks reads every mined block through the profile source.
func TestMultipleBlocks(t *testing.T) {
	instance, ctx := startAnvil(t, 60*time.Second)
	const blockCount = 20
	require.NoError(t, instance.MineBlocks(ctx, blockCount))
	src, err := sourcerpc.Detect(ctx, instance.RPCClient())
	require.NoError(t, err)

	for i := uint64(1); i <= blockCount; i++ {
		block, _, err := src.BlockWithReceipts(ctx, i)
		require.NoError(t, err, "block %d", i)
		require.Equal(t, i, block.Number)
		require.NotEqual(t, common.Hash{}, block.Hash, "block %d", i)
	}
}
