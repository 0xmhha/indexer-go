package postgres

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type executionBalanceRPCProbe struct{ calls int }

func (p *executionBalanceRPCProbe) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	p.calls++
	return new(big.Int), nil
}
func TestExecutionBalanceReadsCoveredBaselineAndHidesUnpublishedWithoutRPC(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	probe := &executionBalanceRPCProbe{}
	s.SetGenesisBalanceResolver(probe)
	want := new(big.Int).SetBytes(a.Balance[:])
	got, err := s.GetAddressBalance(t.Context(), common.Address(addr), 0)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Zero(t, probe.calls)
	changed := a
	changed.Balance[31]--
	r := accountResult(cfg, addr, a, changed, changed)
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r))
	got, err = s.GetAddressBalance(t.Context(), common.Address(addr), 0)
	require.NoError(t, err)
	require.Equal(t, want, got)
	_, err = s.GetAddressBalance(t.Context(), common.Address(addr), 1)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
	_, err = s.GetAddressBalance(t.Context(), common.Address{99}, 0)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
	require.Zero(t, probe.calls)
	readonly := openTestStore(t, s.schema, true)
	got, err = readonly.GetAddressBalance(t.Context(), common.Address(addr), 0)
	require.NoError(t, err)
	require.Equal(t, want, got)
}
