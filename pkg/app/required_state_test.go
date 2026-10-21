package app

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestManagedRequiredStateCheckpointRestart(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long: restarts a managed feed with required state")
	}
	for _, bootstrap := range []bool{true, false} {
		name := "captured-parent"
		if bootstrap {
			name = "bootstrap-header"
		}
		t.Run(name, func(t *testing.T) {
			c := testchain.NewChain(5042002, nil)
			c.AddStateRootBlock(types.EmptyRootHash, nil)
			c.AddStateRootBlock(types.EmptyRootHash, nil)
			c.AddStateRootBlock(types.EmptyRootHash, nil)
			cfg, _, rpc := feedAppConfig(t, c)
			cfg.Source.Feed.StateProofs = stateProofConfig()
			cfg.Source.Feed.StateProofs.Required = true
			if bootstrap {
				cfg.Source.Feed.AnchorHeight = 1
				cfg.Source.Feed.AnchorHash = c.Block(1).Block.Hash().Hex()
				cfg.Indexer.StartHeight = 2
			}
			a, e := NewApp(cfg, zap.NewNop(), false, "")
			require.NoError(t, e)
			initial, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
			require.NoError(t, e)
			require.Equal(t, "checkpoint_pending", initial.StateProofs.Status)
			cancel, done := runFeedApp(t, a)
			status := waitStateProofs(t, a, 3)
			waitFeedProjection(t, a, c, 3)
			require.True(t, status.StateProofs.Descriptor.Required)
			require.True(t, status.StateProofs.Descriptor.ParentCheckpoint)
			require.EqualValues(t, 1, status.StateProofs.Parent.Height)
			require.Equal(t, c.Block(1).Block.Hash(), status.StateProofs.Parent.Hash)
			require.EqualValues(t, 3, rpc.Calls()["eth_getProof"])
			epoch := status.StateProofs.Epoch
			d := status.StateProofs.Descriptor
			before, after, e := a.managedFeed.stateSupplier.ReadTransition(t.Context(), epoch, d, 2)
			require.NoError(t, e)
			require.EqualValues(t, 1, before.Block.Number)
			require.EqualValues(t, 2, after.Block.Number)
			require.False(t, before.Accounts[0].Exists)
			require.False(t, after.Accounts[0].Exists)
			require.NoError(t, a.managedFeed.store.RequiredSupplementReady(t.Context(), epoch, after.Block))
			cancel()
			require.ErrorIs(t, <-done, context.Canceled)
			a.Shutdown()
			rpc.PruneStateBefore(4)
			a, e = NewApp(cfg, zap.NewNop(), false, "")
			require.NoError(t, e)
			cancel, done = runFeedApp(t, a)
			// Wait for actual proof worker recovery, not simply the persisted status.
			require.Eventually(t, func() bool { return a.managedFeed.stateSupplier.Metrics().Verify.Count > 0 }, 10*time.Second, 20*time.Millisecond)
			count := rpc.Calls()["eth_getProof"]
			c.AddStateRootBlock(types.EmptyRootHash, nil)
			waitStateProofs(t, a, 4)
			require.Equal(t, count+1, rpc.Calls()["eth_getProof"])
			beforeCalls := rpc.Calls()
			before, after, e = evmstate.ReadTransition(t.Context(), a.managedFeed.store, a.managedFeed.program.Profile, epoch, d, 2)
			require.NoError(t, e)
			require.EqualValues(t, 1, before.Block.Number)
			require.EqualValues(t, 2, after.Block.Number)
			// Cancel before comparing counters to exclude unrelated poll/control requests.
			cancel()
			require.ErrorIs(t, <-done, context.Canceled)
			require.Equal(t, beforeCalls["eth_getProof"], rpc.Calls()["eth_getProof"])
			a.Shutdown()
			required := cfg.Source.Feed.StateProofs
			cfg.Source.Feed.StateProofs = nil
			_, e = NewApp(cfg, zap.NewNop(), false, "")
			require.ErrorIs(t, e, capture.ErrConflict)
			cfg.Source.Feed.StateProofs = required
			required.Required = false
			_, e = NewApp(cfg, zap.NewNop(), false, "")
			require.ErrorIs(t, e, capture.ErrConflict)
			required.Required = true
			required.Name = "renamed"
			_, e = NewApp(cfg, zap.NewNop(), false, "")
			require.ErrorIs(t, e, capture.ErrConflict)
		})
	}
}
func TestManagedRequiredParentFailure(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.StateProofs.Required = true
	rpc.PruneStateBefore(2)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	_, done := runFeedApp(t, a)
	select {
	case e = <-done:
		require.ErrorIs(t, e, evmstate.ErrUnavailable)
	case <-time.After(20 * time.Second):
		t.Fatal("parent failure did not join workers")
	}
	status, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.Nil(t, status.StateProofs.Parent)
	require.Nil(t, status.StateProofs.Complete)
	require.Equal(t, "checkpoint_pending", status.StateProofs.Status)
}
func TestManagedRequiredParentReorg(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	cfg, _, _ := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.StateProofs.StartHeight = 3
	cfg.Source.Feed.StateProofs.Required = true
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	old := waitStateProofs(t, a, 3)
	c.Reorg(1)
	c.AddStateRootBlock(types.EmptyRootHash, []byte("new parent"))
	c.AddStateRootBlock(types.EmptyRootHash, []byte("new child"))
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
		return e == nil && s.StateProofs != nil && s.StateProofs.Epoch > old.StateProofs.Epoch && s.StateProofs.Parent != nil && s.StateProofs.Parent.Hash == c.Block(2).Block.Hash() && s.StateProofs.Complete != nil && s.StateProofs.Complete.Hash == c.Block(3).Block.Hash()
	}, 30*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
