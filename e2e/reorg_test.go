package e2e

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/client"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestLiveLoopFollowsAnvilReorg injects a reorganization into a real node
// (refactoring plan R2-4): the indexer follows Anvil to block 5, Anvil
// reverts to block 2 and mines a different branch to block 6, and the live
// loop must roll back blocks 3..5, keep them as orphans and index the new
// branch with the node's hashes.
func TestLiveLoopFollowsAnvilReorg(t *testing.T) {
	instance, ctx := startAnvil(t, 2*time.Minute)

	require.NoError(t, instance.MineBlocks(ctx, 2))
	snapshot, err := instance.Snapshot(ctx)
	require.NoError(t, err)
	require.NoError(t, instance.MineBlocks(ctx, 3)) // head 5

	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	c, err := client.NewClient(&client.Config{Endpoint: instance.RPCURL(), Timeout: 5 * time.Second, Logger: zap.NewNop()})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	src, err := sourcerpc.Detect(ctx, instance.RPCClient())
	require.NoError(t, err)

	f := fetch.NewFetcher(c, db, &fetch.Config{
		MaxRetries: 3, RetryDelay: 10 * time.Millisecond, PollInterval: 10 * time.Millisecond, BatchSize: 10,
	}, zap.NewNop(), nil)
	f.SetSource(src)
	t.Cleanup(f.Close)

	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- f.Run(loopCtx) }()
	defer func() {
		stop()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("live loop: %v", err)
		}
	}()

	waitIndexed := func(head uint64) {
		t.Helper()
		require.Eventually(t, func() bool {
			h, err := db.GetLatestHeight(ctx)
			return err == nil && h == head
		}, time.Minute, 20*time.Millisecond, "indexed to %d", head)
	}
	waitIndexed(5)
	old := make(map[uint64]common.Hash)
	for n := uint64(3); n <= 5; n++ {
		b, err := db.GetBlock(ctx, n)
		require.NoError(t, err)
		old[n] = b.Hash
	}

	// Another branch from block 2: later timestamps make every block differ.
	require.NoError(t, instance.Revert(ctx, snapshot))
	latest, err := instance.GetLatestBlockNumber(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), latest)
	require.NoError(t, instance.SetNextBlockTimestamp(ctx, uint64(time.Now().Unix())+1000))
	require.NoError(t, instance.MineBlocks(ctx, 4)) // head 6
	waitIndexed(6)

	for n := uint64(0); n <= 6; n++ {
		want, err := instance.GetBlockHashByNumber(ctx, new(big.Int).SetUint64(n))
		require.NoError(t, err)
		b, err := db.GetBlock(ctx, n)
		require.NoError(t, err)
		require.Equal(t, want, b.Hash, "block %d follows the node", n)
	}
	for n := uint64(3); n <= 5; n++ {
		ob, err := db.GetOrphanedBlock(ctx, old[n])
		require.NoError(t, err, "block %d of the old branch is kept as an orphan", n)
		require.Equal(t, n, ob.Block.Number)
	}
	reorgs, _, err := db.GetReorgs(ctx, port.FirstPage(10))
	require.NoError(t, err)
	require.Len(t, reorgs, 1)
	require.Equal(t, uint64(2), reorgs[0].ForkNumber)
	require.Equal(t, uint64(5), reorgs[0].OldHead)
	count, depth := f.Reorgs()
	require.Equal(t, uint64(1), count)
	require.Equal(t, uint64(3), depth)
}
