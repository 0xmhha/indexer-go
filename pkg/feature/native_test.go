package feature

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestNativeBalanceContextPinsBlockAndOwnsValues(t *testing.T) {
	hash := common.Hash{1}
	changes := []NativeBalanceChange{{Address: common.Address{2}, Before: big.NewInt(100), After: big.NewInt(110), TxHash: common.Hash{3}}}
	ctx, err := WithNativeBalances(t.Context(), 7, hash, changes)
	require.NoError(t, err)
	changes[0].After.SetInt64(999)
	b := &Block{Model: &model.Block{Number: 7, Hash: hash}}
	got, err := NativeBalancesFromContext(ctx, b)
	require.NoError(t, err)
	require.Equal(t, "110", got[0].After.String())
	got[0].Before.SetInt64(999)
	again, err := NativeBalancesFromContext(ctx, b)
	require.NoError(t, err)
	require.Equal(t, "100", again[0].Before.String())
	b.Model.Hash[0]++
	_, err = NativeBalancesFromContext(ctx, b)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	_, err = NativeBalancesFromContext(t.Context(), b)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = NativeBalancesFromContext(canceled, b)
	require.ErrorIs(t, err, context.Canceled)
}
