package postgres

import (
	"context"
	"crypto/sha256"
	"math"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func baselineAccount(addr execwire.Address, a execwire.Account) execwire.AccountChange {
	return execwire.AccountChange{Address: addr, Before: a, After: a, Baseline: true}
}
func accountFixture(t *testing.T) (*Store, port.ExecutionConfig, execwire.Address, execwire.Account) {
	t.Helper()
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	a := execwire.Account{Exists: true, Nonce: math.MaxUint64, Extra: math.MaxUint64, CodeHash: execwire.Hash(types.EmptyCodeHash), StorageRoot: execwire.Hash(types.EmptyRootHash)}
	for i := range a.Balance {
		a.Balance[i] = 0xff
	}
	addr := execwire.Address{1}
	seed := port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot, Accounts: []execwire.AccountChange{baselineAccount(addr, a)}}
	require.NoError(t, s.SeedExecution(t.Context(), seed))
	return s, cfg, addr, a
}
func accountResult(cfg port.ExecutionConfig, addr execwire.Address, before, after, final execwire.Account) execwire.Result {
	return execwire.Result{Ref: execwire.Ref{Generation: cfg.Anchor.Generation, Height: cfg.Anchor.Height + 1, Hash: execwire.Hash{2}}, Parent: cfg.Anchor.Hash, StateRoot: execwire.Hash{20}, ReceiptsRoot: execwire.Hash{21}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, TxCount: 1, Steps: []execwire.Step{
		{Ordinal: 0, Cause: execwire.Transaction, TxIndex: 0, TxHash: execwire.Hash{7}, Accounts: []execwire.AccountChange{{Address: addr, Before: before, After: after}}},
		{Ordinal: 1, Cause: execwire.Finalize, TxIndex: -1, Accounts: []execwire.AccountChange{{Address: addr, Before: after, After: final}}},
	}}
}
func statePart(t *testing.T, w port.ExecutionWriter, r execwire.Result) port.ExecutionPart {
	t.Helper()
	p, err := execwire.MarshalResult(r)
	require.NoError(t, err)
	return port.ExecutionPart{Writer: w, Ref: r.Ref, Parent: r.Parent, Digest: execwire.Hash(sha256.Sum256(p))}
}
func applyStatePart(t *testing.T, s *Store, w port.ExecutionWriter, r execwire.Result) error {
	t.Helper()
	return s.WriteExecutionPart(t.Context(), statePart(t, w, r), func(ctx context.Context) error { return s.ApplyExecutionState(ctx, w, r) })
}
func TestExecutionBaselineIsExplicitAndIdempotent(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	seed := port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot, Accounts: []execwire.AccountChange{baselineAccount(addr, a)}}
	require.NoError(t, s.SeedExecution(t.Context(), seed))
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.BaselineReady)
	got, err := s.ExecutionAccountAt(t.Context(), addr, cfg.Anchor.Height)
	require.NoError(t, err)
	require.Equal(t, a, got, "uint256 balance and uint64 nonce/Extra retain precision")
	_, err = s.ExecutionAccountAt(t.Context(), execwire.Address{9}, cfg.Anchor.Height)
	require.ErrorIs(t, err, port.ErrExecutionCoverage, "unknown account is not an invented zero")
	seed.Accounts[0].After.Nonce--
	seed.Accounts[0].Before = seed.Accounts[0].After
	require.ErrorIs(t, s.SeedExecution(t.Context(), seed), port.ErrExecutionConflict)
}
func TestExecutionAccountsCASOrdinalAndAsOfHistory(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	b := a
	b.Nonce--
	b.Balance[31]--
	c := b
	c.Extra--
	r := accountResult(cfg, addr, a, b, c)
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r))
	for h, want := range map[uint64]execwire.Account{0: a, 1: c} {
		got, err := s.ExecutionAccountAt(t.Context(), addr, h)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	d := c
	d.Balance[31]--
	r2 := accountResult(cfg, addr, c, d, d)
	r2.Parent = r.Ref.Hash
	r2.Ref.Height++
	r2.Ref.Hash[0]++
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r2))
	got, err := s.ExecutionAccountAt(t.Context(), addr, 1)
	require.NoError(t, err)
	require.Equal(t, c, got, "an ahead latest row must not hide the visible historical value")
	cursor, err := s.ExecutionCursor(t.Context(), port.ExecutionAccounts)
	require.NoError(t, err)
	bad := r2
	bad.Ref.Height++
	bad.Parent = r2.Ref.Hash
	bad.Ref.Hash[0]++
	bad.Steps = append([]execwire.Step(nil), bad.Steps...)
	bad.Steps[0].Accounts = []execwire.AccountChange{{Address: addr, Before: a, After: d}}
	require.ErrorIs(t, applyStatePart(t, s, port.ExecutionAccounts, bad), port.ErrExecutionConflict)
	after, err := s.ExecutionCursor(t.Context(), port.ExecutionAccounts)
	require.NoError(t, err)
	require.Equal(t, cursor, after)
}
func TestExecutionMissingBeforeAndKnownNewAccount(t *testing.T) {
	s, cfg, _, a := accountFixture(t)
	addr := execwire.Address{4}
	r := accountResult(cfg, addr, a, a, a)
	require.ErrorIs(t, applyStatePart(t, s, port.ExecutionAccounts, r), port.ErrExecutionCoverage)
	r = accountResult(cfg, addr, execwire.Account{}, a, a)
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r), "verified absent Before identifies a new account")
	got, err := s.ExecutionAccountAt(t.Context(), addr, 1)
	require.NoError(t, err)
	require.Equal(t, a, got)
}

func TestExecutionHistoryPartitionsOwnedByCoordinator(t *testing.T) {
	s, _, _, _ := accountFixture(t)
	for table, tracked := range trackedTables(t, s) {
		require.True(t, tracked, "table %s must belong to legacy undo or the explicit execution coordinator", table)
	}
}

func TestExecutionAccountDeletionRecreationAndCancelledCAS(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	r := accountResult(cfg, addr, a, execwire.Account{}, execwire.Account{})
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r))
	got, err := s.ExecutionAccountAt(t.Context(), addr, 1)
	require.NoError(t, err)
	require.False(t, got.Exists, "known tombstone is distinct from unknown coverage")
	b := a
	b.Nonce--
	r2 := accountResult(cfg, addr, execwire.Account{}, b, b)
	r2.Parent = r.Ref.Hash
	r2.Ref.Height++
	r2.Ref.Hash[0]++
	ctx, cancel := context.WithCancel(t.Context())
	err = s.WriteExecutionPart(ctx, statePart(t, port.ExecutionAccounts, r2), func(bound context.Context) error {
		if err := s.ApplyExecutionState(bound, port.ExecutionAccounts, r2); err != nil {
			return err
		}
		cancel()
		return nil
	})
	cancel()
	require.ErrorIs(t, err, context.Canceled)
	got, err = s.ExecutionAccountAt(t.Context(), addr, 2)
	require.NoError(t, err)
	require.False(t, got.Exists, "cancel rolled back recreated current/history")
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, r2))
	got, err = s.ExecutionAccountAt(t.Context(), addr, 2)
	require.NoError(t, err)
	require.Equal(t, b, got)
}

func TestExecutionStateScopeAndInputIdentity(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	r := accountResult(cfg, addr, a, a, a)
	require.ErrorIs(t, s.ApplyExecutionState(t.Context(), port.ExecutionAccounts, r), port.ErrClosed)
	p := statePart(t, port.ExecutionAccounts, r)
	wrong := p
	wrong.Digest[0]++
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), wrong, func(ctx context.Context) error { return s.ApplyExecutionState(ctx, p.Writer, r) }), port.ErrExecutionConflict)
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionState(ctx, port.ExecutionStorage, r) }), port.ErrExecutionConflict)
	var expired context.Context
	require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { expired = ctx; return s.ApplyExecutionState(ctx, p.Writer, r) }))
	require.ErrorIs(t, s.ApplyExecutionState(expired, p.Writer, r), port.ErrClosed)
}
