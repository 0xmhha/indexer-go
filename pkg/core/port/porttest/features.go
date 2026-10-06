package porttest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// testFeatureState checks FeatureStateStore: FeatureStates returns exactly
// the states set, by name, with the last SetFeatureState of a name winning;
// inside a block transaction the states commit and roll back with the block.
func testFeatureState(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("EmptyStore", func(t *testing.T) {
		s := open[port.FeatureStateStore](t, newStore)
		states, err := s.FeatureStates(ctx)
		require.NoError(t, err)
		assert.Empty(t, states)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[port.FeatureStateStore](t, newStore)
		want := map[string]port.FeatureState{
			"address.index":   {Active: true},
			"token.transfers": {Through: 41},
			"aa.erc4337":      {Active: true, Through: 9, Gap: &port.BlockRange{From: 10, To: 20}},
		}
		for name, st := range want {
			require.NoError(t, s.SetFeatureState(ctx, name, st))
		}
		got, err := s.FeatureStates(ctx)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("SetReplaces", func(t *testing.T) {
		s := open[port.FeatureStateStore](t, newStore)
		require.NoError(t, s.SetFeatureState(ctx, "aa.erc4337", port.FeatureState{Active: true, Gap: &port.BlockRange{From: 1, To: 5}}))
		require.NoError(t, s.SetFeatureState(ctx, "aa.erc4337", port.FeatureState{Through: 5}))
		got, err := s.FeatureStates(ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]port.FeatureState{"aa.erc4337": {Through: 5}}, got,
			"the whole state is replaced, the gap included")
	})

	t.Run("NamesAreExact", func(t *testing.T) {
		s := open[port.FeatureStateStore](t, newStore)
		require.NoError(t, s.SetFeatureState(ctx, "aa", port.FeatureState{Through: 1}))
		require.NoError(t, s.SetFeatureState(ctx, "aa.erc4337", port.FeatureState{Through: 2}))
		got, err := s.FeatureStates(ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]port.FeatureState{"aa": {Through: 1}, "aa.erc4337": {Through: 2}}, got,
			"a name that prefixes another is a different feature")
	})

	t.Run("BlockTransaction", func(t *testing.T) {
		s := open[port.FeatureStateStore](t, newStore)
		bt, ok := s.(port.BlockTransactor)
		if !ok {
			t.Skip("store has no block transactions")
		}
		require.NoError(t, s.SetFeatureState(ctx, "address.index", port.FeatureState{Through: 3}))

		txCtx, tx, err := bt.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.SetFeatureState(txCtx, "address.index", port.FeatureState{Active: true}))
		inside, err := s.FeatureStates(txCtx)
		require.NoError(t, err)
		assert.Equal(t, port.FeatureState{Active: true}, inside["address.index"], "the transaction reads its own writes")
		outside, err := s.FeatureStates(ctx)
		require.NoError(t, err)
		assert.Equal(t, port.FeatureState{Through: 3}, outside["address.index"], "invisible outside before Commit")
		require.NoError(t, tx.Commit())
		after, err := s.FeatureStates(ctx)
		require.NoError(t, err)
		assert.Equal(t, port.FeatureState{Active: true}, after["address.index"])

		txCtx, tx, err = bt.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.SetFeatureState(txCtx, "token.transfers", port.FeatureState{Active: true}))
		tx.Rollback()
		after, err = s.FeatureStates(ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]port.FeatureState{"address.index": {Active: true}}, after, "Rollback discards the state")
	})
}
