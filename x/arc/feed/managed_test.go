package arcfeed_test

import (
	"context"
	"math/big"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/0xmhha/indexer-go/pkg/source"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/x/arc"
	arcfeed "github.com/0xmhha/indexer-go/x/arc/feed"
)

type managedOracle struct{ *oracle }

func (o managedOracle) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	if o.chain.Block(n) == nil {
		return common.Hash{}, source.ErrNotFound
	}
	return o.oracle.HashAt(ctx, n)
}
func (o managedOracle) FinalizedCheckpoint(context.Context) (uint64, common.Hash, error) {
	return 0, o.chain.Block(0).Block.Hash(), nil
}

func TestArcManagedReorgRetainsForkAndResumes(t *testing.T) {
	testManagedReorg(t, db(t), false)
}

func TestArcManagedReorgPostgres(t *testing.T) {
	testManagedReorg(t, postgresDB(t), false)
}

// Minimal restart at an explicit recent bootstrap exercises the same real
// RLPx/window/generation path without repeating unrelated historical blocks.
func TestArcManagedReorgAtBootstrap(t *testing.T) {
	testManagedReorg(t, db(t), true)
}

func testManagedReorg(t *testing.T, store port.CaptureDatabase, recentBootstrap bool) {
	t.Helper()
	c, _, fresh, peer, session := fixtures(t)
	last := uint64(3)
	if recentBootstrap {
		c.Reorg(2)
		last = 2
		anchor := model.FeedBlock{Chain: peer.Chain, Number: 1, Hash: c.Block(1).Block.Hash()}
		peer.Checkpoint, session.Anchor = anchor, anchor
	}
	old := c.Block(2).Block.Hash()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var switched atomic.Bool
	opts := capture.RuntimeOptions{ControlInterval: 20 * time.Millisecond, RestartDelay: time.Millisecond, Pipeline: capture.PipelineOptions{PollInterval: time.Millisecond, ResolveInterval: 20 * time.Millisecond, OnCaptured: func(id model.CaptureIdentity) {
		if id.Generation == 1 && id.Block.Number == last && switched.CompareAndSwap(false, true) {
			c.Reorg(1)
			for i := 0; i < int(last-1); i++ {
				to := testchain.NewAccount(uint64(20 + i)).Address
				c.AddBlock(testchain.TxSpec{From: testchain.NewAccount(1), Tx: &types.DynamicFeeTx{To: &to, Gas: 21000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(99)}, GasUsed: 21000})
			}
			c.SetHead(last)
		}
		if id.Generation == 2 && id.Block.Number == last {
			cancel()
		}
	}}}
	err := arcfeed.RunManaged(ctx, store, managedOracle{fresh}, peer, session, opts, "fixture-authority")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, context.Canceled, ctx.Err(), "unexpected early return: %v", err)
	facts, err := capture.New(store, peer.Chain, managedOracle{fresh})
	require.NoError(t, err)
	state, err := facts.State(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, state.Generation)
	require.Equal(t, c.Block(last).Block.Hash(), state.Through.Hash)
	digest, _ := session.Plan.Digest()
	oldID := model.CaptureIdentity{Block: model.FeedBlock{Chain: peer.Chain, Number: 2, Hash: old}, Generation: 1, PlanVersion: 1, PlanDigest: digest}
	retained, err := facts.Read(context.Background(), oldID)
	require.NoError(t, err)
	require.Len(t, retained.Payloads, 3)
	newID, err := facts.CanonicalAt(context.Background(), 2, 2)
	require.NoError(t, err)
	require.NotEqual(t, oldID.Block.Hash, newID.Block.Hash)
	require.EqualValues(t, 2, newID.Generation)
	events, err := facts.Events(context.Background(), 0, 20)
	require.NoError(t, err)
	oldCount := last - session.Anchor.Number
	require.Len(t, events, int(oldCount+1+last-1))
	require.Equal(t, "capture_reset", events[oldCount].Kind)
	optsRaw, err := session.Storage.OptionsFor(session.Instance, peer.Chain)
	require.NoError(t, err)
	raw, err := facts.OpenWindow(context.Background(), optsRaw)
	require.NoError(t, err)
	require.Equal(t, int(3*(oldCount+last-1)), raw.Stats().Records)
	require.NoError(t, raw.Close())
}

func TestArcManagedFinalityUsesExistingLimitedRPC(t *testing.T) {
	c, _, _, peer, session := fixtures(t)
	c.SetFinalized(3)
	server := testchain.NewServer(c)
	t.Cleanup(server.Close)
	pool, err := rpcpool.New(rpcpool.Config{Endpoints: []string{server.URL()}, RateLimit: 5, Burst: 1, CoalesceReads: true})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rpc, err := gethrpc.DialOptions(ctx, pool.Primary(), gethrpc.WithHTTPClient(&http.Client{Transport: pool}))
	require.NoError(t, err)
	defer rpc.Close()
	fresh := sourcerpc.New(rpc, arc.New())
	store := db(t)
	err = arcfeed.RunManaged(ctx, store, fresh, peer, session, capture.RuntimeOptions{Collect: true, ControlInterval: time.Second, OnControl: func(s capture.ControlStatus) {
		if s.Finalized != nil && s.Finalized.Number == 3 {
			cancel()
		}
	}}, "local-fixture")
	require.ErrorIs(t, err, context.Canceled)
	facts, err := capture.New(store, peer.Chain, fresh)
	require.NoError(t, err)
	state, err := facts.State(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 3, state.Finalized.Number)
	require.Equal(t, "arc.rpc-finalized/6e764023/local-fixture", state.FinalityPolicy)
	calls := server.Calls()
	require.Positive(t, pool.Status()[0].Requests)
	require.LessOrEqual(t, pool.Status()[0].Requests, uint64(calls["eth_getBlockByNumber"]), "independent finalized reads may share an HTTP batch")
	require.Zero(t, calls["eth_getBlockReceipts"])
}
