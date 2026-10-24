package fetch

import (
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestExecutionPublicationBindsOwnedNativeJournalToFeatureContext(t *testing.T) {
	h := newChainHarness(t, &Config{StatePrefetch: true}, nil)
	b, rs, err := h.src.BlockWithReceipts(t.Context(), 1)
	require.NoError(t, err)
	h.f.storage = &executionFeatureStore{PebbleStorage: h.db}
	h.f.SetExecutionCanonical(&executionCanonicalProbe{hash: b.Hash})
	var before, after execwire.Word
	before[31], after[31] = 100, 110
	r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: b.Number, Hash: execwire.Hash(b.Hash)}, Parent: execwire.Hash(b.ParentHash), StateRoot: execwire.Hash(b.StateRoot), Steps: []execwire.Step{{Cause: execwire.Finalize, TxIndex: -1, Accounts: []execwire.AccountChange{{Address: execwire.Address{1}, Before: execwire.Account{Exists: true, Balance: before}, After: execwire.Account{Exists: true, Balance: after}}}}}}
	checked := 0
	check := func(ctx context.Context, block *feature.Block) error {
		changes, err := feature.NativeBalancesFromContext(ctx, block)
		if err != nil {
			return err
		}
		require.Len(t, changes, 1)
		require.Equal(t, common.Address{1}, changes[0].Address)
		require.Equal(t, "100", changes[0].Before.String())
		require.Equal(t, "110", changes[0].After.String())
		require.Zero(t, changes[0].TxHash)
		changes[0].After.SetInt64(999)
		checked++
		return nil
	}
	setPreparingFeature(t, h, func(ctx context.Context, b *feature.Block) (feature.PreparedHandler, error) {
		if err := check(ctx, b); err != nil {
			return feature.PreparedHandler{}, err
		}
		return feature.PreparedHandler{Handler: feature.BlockHandlerFunc(check)}, nil
	})
	require.NoError(t, h.f.PublishExecution(t.Context(), r, b, rs, execwire.Hash{1}))
	require.Equal(t, 2, checked)
}
