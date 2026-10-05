package main

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/consensus"
)

// TestWBFTSigningFromCanonicalSeals indexes the StableNet scenario, whose
// blocks carry the previous block's seals and epoch blocks every 4 blocks.
// Each block's signers must be the validators set in the Prev seals of the
// next block, against the validator set of the block's epoch, with the
// round recorded there; epochs must be numbered in order from genesis.
func TestWBFTSigningFromCanonicalSeals(t *testing.T) {
	sc := testchain.BuildStableNet()
	app := indexAll(t, sc.Chain)
	r := consensus.NewStore(app.storage.(consensus.Backend), nil)
	cfg := sc.Chain.StableNetConfig()
	ctx := context.Background()

	head := sc.Chain.Head()
	for n := uint64(1); n < head; n++ {
		preparers, committers, err := r.GetBlockSigners(ctx, n)
		require.NoError(t, err)
		var want []common.Address
		for _, i := range cfg.Committers(n) {
			want = append(want, cfg.Candidates[i].Address)
		}
		require.ElementsMatch(t, want, committers, "commit signers of block %d", n)
		require.Len(t, preparers, len(cfg.Candidates), "prepare signers of block %d", n)

		for _, c := range cfg.Candidates {
			acts, err := r.GetValidatorSigningActivity(ctx, c.Address, n, n, 10, 0)
			require.NoError(t, err)
			require.Len(t, acts, 1, "activity of %s at %d", c.Address.Hex(), n)
			require.Equal(t, cfg.PrevRound(n), acts[0].Round, "round of block %d", n)
		}
	}
	_, committers, err := r.GetBlockSigners(ctx, head)
	require.NoError(t, err)
	require.Empty(t, committers, "the head's seals arrive with the next block")

	epochs, total, err := r.GetEpochsList(ctx, 100, 0)
	require.NoError(t, err)
	require.Equal(t, int(head/cfg.EpochLength)+1, total)
	for _, e := range epochs {
		require.Equal(t, e.BlockNumber/cfg.EpochLength, e.EpochNumber, "epoch at block %d", e.BlockNumber)
		require.Zero(t, e.BlockNumber%cfg.EpochLength)
	}
}
