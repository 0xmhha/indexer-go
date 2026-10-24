package postgres

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestExecutionBalanceJournalIsSoleAuthorityAndHistoryIsVisible(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	ctx := t.Context()
	address := common.Address(addr)
	// Old projection data is deliberately wrong; it must neither override the
	// state journal nor be deleted implicitly by an upgrade.
	require.NoError(t, s.SetBalance(ctx, address, 0, big.NewInt(123)))
	got, err := s.GetAddressBalance(ctx, address, 0)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).SetBytes(a.Balance[:]), got)
	b := a
	b.Balance[31]--
	c := b
	c.Balance[31]--
	r := accountResult(cfg, addr, a, b, c)
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r))
	history, next, err := s.GetBalanceHistory(ctx, address, 0, 99, port.FirstPage(20))
	require.NoError(t, err)
	require.Empty(t, next)
	require.Len(t, history, 1)
	require.Zero(t, history[0].BlockNumber)
	require.Zero(t, history[0].Delta.Sign())
	require.Equal(t, new(big.Int).SetBytes(a.Balance[:]), history[0].Balance)
	// Publication markers here are synthetic; full seven-writer integration is
	// covered in pkg/app. Only the public read boundary is advanced in this fixture.
	_, err = s.pool.Exec(ctx, "UPDATE exec_control SET visible_height=$1,visible_hash=$2,visible_root=$3 WHERE id", int64(r.Ref.Height), r.Ref.Hash[:], r.StateRoot[:])
	require.NoError(t, err)
	var all []port.BalanceSnapshot
	for {
		page, cursor, e := s.GetBalanceHistory(ctx, address, 0, 99, port.Page{Limit: 1, After: next})
		require.NoError(t, e)
		all = append(all, page...)
		next = cursor
		if next == "" {
			break
		}
	}
	require.Len(t, all, 3)
	require.Equal(t, common.Hash{7}, all[1].TxHash)
	require.Zero(t, all[2].TxHash)
	require.Equal(t, int64(-1), all[1].Delta.Int64())
	require.Equal(t, int64(-1), all[2].Delta.Int64())
	got, err = s.GetAddressBalance(ctx, address, 0)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).SetBytes(c.Balance[:]), got)
	readonly := openTestStore(t, s.schema, true)
	got, err = readonly.GetAddressBalance(ctx, address, 0)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).SetBytes(c.Balance[:]), got)
	_, _, err = s.GetBalanceHistory(ctx, common.Address{99}, 0, 99, port.FirstPage(1))
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
	var count int
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM balances").Scan(&count))
	require.Equal(t, 1, count, "old projection rows are preserved")
}

func TestExecutionBalanceJournalRequiresBoundExactStoredInput(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	b := a
	b.Balance[31]--
	r := accountResult(cfg, addr, a, b, b)
	p := statePart(t, port.ExecutionAccounts, r)
	block := &model.Block{Number: r.Ref.Height, Hash: common.Hash(r.Ref.Hash), ParentHash: common.Hash(r.Parent), StateRoot: common.Hash(r.StateRoot)}
	for _, w := range port.ExecutionWriters() {
		p.Writer = w
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
			if w == port.ExecutionAccounts {
				return s.ApplyExecutionState(ctx, w, r)
			}
			if w == port.ExecutionBlocks {
				return s.ApplyExecutionCore(ctx, w, block, nil)
			}
			return nil
		}))
	}
	changes := []port.ExecutionBalanceChange{{Address: common.Address(addr), Before: new(big.Int).SetBytes(a.Balance[:]), After: new(big.Int).SetBytes(b.Balance[:]), TxHash: common.Hash{7}}, {Address: common.Address(addr), Before: new(big.Int).SetBytes(b.Balance[:]), After: new(big.Int).SetBytes(b.Balance[:]), Ordinal: 1}}
	require.Error(t, s.ValidateExecutionBalances(t.Context(), r.Ref.Height, common.Hash(r.Ref.Hash), changes))
	pub := port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, Digest: p.Digest}
	committed, err := s.CommitExecution(t.Context(), pub, func(ctx context.Context) error {
		bad := append([]port.ExecutionBalanceChange(nil), changes...)
		bad[0].TxHash = common.Hash{9}
		require.ErrorIs(t, s.ValidateExecutionBalances(ctx, r.Ref.Height, common.Hash(r.Ref.Hash), bad), port.ErrExecutionConflict)
		require.ErrorIs(t, s.ValidateExecutionBalances(ctx, r.Ref.Height, common.Hash(r.Ref.Hash), changes[:1]), port.ErrExecutionConflict)
		return s.ValidateExecutionBalances(ctx, r.Ref.Height, common.Hash(r.Ref.Hash), changes)
	}, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.True(t, committed)
}

func TestExecutionBalanceHistorySkipsNonceOnlyAndRejectsOldOrStaleCursor(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	b := a
	b.Nonce--
	c := b
	c.Balance[31]--
	r := accountResult(cfg, addr, a, b, c)
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r))
	_, err := s.pool.Exec(t.Context(), "UPDATE exec_control SET visible_height=$1,visible_hash=$2,visible_root=$3 WHERE id", int64(r.Ref.Height), r.Ref.Hash[:], r.StateRoot[:])
	require.NoError(t, err)
	address := common.Address(addr)
	history, next, err := s.GetBalanceHistory(t.Context(), address, 0, 99, port.FirstPage(1))
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.NotEmpty(t, next)
	last, _, err := s.GetBalanceHistory(t.Context(), address, 0, 99, port.Page{After: next, Limit: 1})
	require.NoError(t, err)
	require.Len(t, last, 1)
	require.Zero(t, last[0].TxHash, "only Finalize changed the balance")
	_, _, err = s.GetBalanceHistory(t.Context(), address, 0, 99, port.Page{After: encodeCursor("balances:"+address.Hex(), "1"), Limit: 1})
	require.ErrorIs(t, err, port.ErrInvalidCursor)
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_control SET generation=generation+1 WHERE id")
	require.NoError(t, err)
	_, _, err = s.GetBalanceHistory(t.Context(), address, 0, 99, port.Page{After: next, Limit: 1})
	require.ErrorIs(t, err, port.ErrInvalidCursor)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = s.GetBalanceHistory(ctx, address, 0, 99, port.FirstPage(1))
	require.ErrorIs(t, err, context.Canceled)
}
