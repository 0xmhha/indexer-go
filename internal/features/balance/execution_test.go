package balance

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type executionRecords struct {
	port.Reader
	preparationRecords
	history []feature.NativeBalanceChange
}

func (s *executionRecords) GetAddressBalance(_ context.Context, a common.Address, _ uint64) (*big.Int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v := s.balances[a]; v != nil {
		return new(big.Int).Set(v), nil
	}
	return nil, port.ErrNotFound
}
func (s *executionRecords) UpdateBalance(ctx context.Context, a common.Address, n uint64, d *big.Int, h common.Hash) error {
	before, _ := s.GetAddressBalance(ctx, a, n)
	if err := s.preparationRecords.UpdateBalance(ctx, a, n, d, h); err != nil {
		return err
	}
	after, _ := s.GetAddressBalance(ctx, a, n)
	s.history = append(s.history, feature.NativeBalanceChange{Address: a, Before: before, After: after, TxHash: h})
	return nil
}

type forbiddenAccounting struct{}

func (forbiddenAccounting) ConcurrentAccounting() bool { return true }
func (forbiddenAccounting) NativeDeltas(context.Context, *model.Block, []*model.Receipt, chains.AccountingEnv) ([]chains.BalanceDelta, error) {
	return nil, errors.New("execution must not recompute profile deltas")
}
func nativeHandler(changes []feature.NativeBalanceChange) (*handler, *executionRecords) {
	s := &executionRecords{preparationRecords: preparationRecords{balances: map[common.Address]*big.Int{}}}
	h := &handler{w: s, r: s, records: s, accounting: forbiddenAccounting{}}
	h.deps.NativeBalances = func(context.Context, *feature.Block) ([]feature.NativeBalanceChange, error) { return changes, nil }
	h.deps.BalanceAtHash = func(context.Context, common.Address, common.Hash) (*big.Int, error) {
		return nil, errors.New("execution must not read RPC balances")
	}
	h.balanceAt = func(context.Context, common.Address, *big.Int) (*big.Int, error) {
		return nil, errors.New("execution must not resync RPC")
	}
	return h, s
}
func TestExecutionBalancesUseJournalAndFinalizeWithoutAccountingOrRPC(t *testing.T) {
	a, b := common.Address{1}, common.Address{2}
	tx1, tx2 := common.Hash{1}, common.Hash{2}
	changes := []feature.NativeBalanceChange{{Address: a, Before: big.NewInt(100), After: big.NewInt(110), TxHash: tx1, Ordinal: 0}, {Address: a, Before: big.NewInt(110), After: big.NewInt(90), TxHash: tx2, Ordinal: 1}, {Address: b, Before: big.NewInt(7), After: big.NewInt(0), Ordinal: 2}}
	h, s := nativeHandler(changes)
	block := &feature.Block{Model: &model.Block{Number: 7, Hash: common.Hash{7}}}
	_, err := h.PrepareBlock(t.Context(), block)
	require.NoError(t, err)
	require.NoError(t, h.HandleBlock(t.Context(), block))
	require.Equal(t, "90", s.balances[a].String())
	require.Zero(t, s.balances[b].Sign())
	require.Len(t, s.history, 3)
	require.Equal(t, tx1, s.history[0].TxHash)
	require.Equal(t, tx2, s.history[1].TxHash)
	require.Zero(t, s.history[2].TxHash)
}
func TestExecutionBalanceMismatchAndMissingContextNeverFallBack(t *testing.T) {
	a := common.Address{1}
	h, s := nativeHandler([]feature.NativeBalanceChange{{Address: a, Before: big.NewInt(100), After: big.NewInt(110)}})
	s.balances[a] = big.NewInt(99)
	block := &feature.Block{Model: &model.Block{Number: 7}}
	require.ErrorIs(t, h.HandleBlock(t.Context(), block), port.ErrExecutionConflict)
	require.Empty(t, s.history)
	require.Equal(t, "99", s.balances[a].String())
	h.deps.NativeBalances = feature.NativeBalancesFromContext
	require.ErrorIs(t, h.HandleBlock(t.Context(), block), port.ErrExecutionCoverage)
	require.Empty(t, s.history)
}
func TestExecutionBalanceRegistrationNeedsNoBalanceRPC(t *testing.T) {
	_, s := nativeHandler(nil)
	_, err := feature.Build([]string{Name}, feature.Deps{Storage: s, NativeBalances: feature.NativeBalancesFromContext})
	require.NoError(t, err)
}
