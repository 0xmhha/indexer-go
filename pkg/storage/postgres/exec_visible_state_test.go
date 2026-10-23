package postgres

import (
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecutionVisibleStateUsesPublishedHistoryAndReportsCoverage(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	b := a
	b.Nonce--
	b.StorageRoot = execwire.Hash{9}
	r := accountResult(cfg, addr, a, b, b)
	slot, value := execwire.Word{1}, execwire.Word{2}
	r.Steps[0].Storage = []execwire.StorageChange{{Address: addr, Slot: slot, After: value}}
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r))
	require.NoError(t, applyStatePart(t, s, port.ExecutionStorage, r))
	require.NoError(t, applyStatePart(t, s, port.ExecutionContracts, r))
	account, err := s.VisibleExecutionAccount(t.Context(), addr)
	require.NoError(t, err)
	require.Equal(t, a, account.Account)
	require.Equal(t, cfg.Anchor, account.Ref)
	storage, err := s.VisibleExecutionStorage(t.Context(), addr, slot)
	require.NoError(t, err)
	require.Zero(t, storage.Value)
	code, err := s.VisibleExecutionCode(t.Context(), addr)
	require.NoError(t, err)
	require.Empty(t, code.Code)
	require.Equal(t, cfg.Anchor, code.Ref)
	_, err = s.VisibleExecutionAccount(t.Context(), execwire.Address{99})
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
	// Isolate the as-of query contract; publication atomicity has separate tests.
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_control SET visible_height=$1,visible_hash=$2,visible_root=$3 WHERE id", int64(r.Ref.Height), r.Ref.Hash[:], r.StateRoot[:])
	require.NoError(t, err)
	account, err = s.VisibleExecutionAccount(t.Context(), addr)
	require.NoError(t, err)
	require.Equal(t, b, account.Account)
	storage, err = s.VisibleExecutionStorage(t.Context(), addr, slot)
	require.NoError(t, err)
	require.Equal(t, value, storage.Value)
}
