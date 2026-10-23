package postgres

import (
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func contractFixture(t *testing.T) (*Store, port.ExecutionConfig, execwire.Address, execwire.Account, []byte) {
	t.Helper()
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	addr := execwire.Address{1}
	code := []byte{0x60, 0x01, 0x00}
	a := execwire.Account{Exists: true, CodeHash: execwire.Hash(crypto.Keccak256Hash(code)), StorageRoot: execwire.Hash(types.EmptyRootHash)}
	seed := port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot, Accounts: []execwire.AccountChange{baselineAccount(addr, a)}, Code: []execwire.Code{{Hash: a.CodeHash, Bytes: code}}}
	require.NoError(t, s.SeedExecution(t.Context(), seed))
	return s, cfg, addr, a, code
}

func TestExecutionCodeBaselineHistoryAndIndependentWriter(t *testing.T) {
	s, cfg, addr, a, old := contractFixture(t)
	got, err := s.ExecutionCodeAt(t.Context(), addr, 0)
	require.NoError(t, err)
	require.Equal(t, old, got)
	var created *int64
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT created_height FROM state_contracts WHERE address=$1", addr[:]).Scan(&created))
	require.Nil(t, created, "baseline does not invent original creation height")
	code := []byte{0x60, 0x02, 0x00}
	b := a
	b.CodeHash = execwire.Hash(crypto.Keccak256Hash(code))
	r := accountResult(cfg, addr, a, b, b)
	r.Steps[0].Code = []execwire.Code{{Hash: b.CodeHash, Bytes: code}}
	require.NoError(t, applyStatePart(t, s, port.ExecutionContracts, r), "contracts writer is independent of accounts writer")
	got, err = s.ExecutionCodeAt(t.Context(), addr, 1)
	require.NoError(t, err)
	require.Equal(t, code, got)
	got, err = s.ExecutionCodeAt(t.Context(), addr, 0)
	require.NoError(t, err)
	require.Equal(t, old, got)
	_, err = s.ExecutionCodeAt(t.Context(), execwire.Address{9}, 1)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
}

func TestExecutionCodeRejectsHashMismatchAndStoredCollision(t *testing.T) {
	s, cfg, addr, a, _ := contractFixture(t)
	b := a
	b.CodeHash = execwire.Hash(crypto.Keccak256Hash([]byte{0x60, 0x02}))
	r := accountResult(cfg, addr, a, b, b)
	r.Steps[0].Code = []execwire.Code{{Hash: b.CodeHash, Bytes: []byte{0x60, 0x03}}}
	require.ErrorIs(t, applyStatePart(t, s, port.ExecutionContracts, r), port.ErrInvalidData)
	r.Steps[0].Code[0].Bytes = []byte{0x60, 0x02}
	_, err := s.pool.Exec(t.Context(), "INSERT INTO state_code(code_hash,code,first_height) VALUES($1,$2,0)", b.CodeHash[:], []byte{0x60, 0x03})
	require.NoError(t, err)
	require.ErrorIs(t, applyStatePart(t, s, port.ExecutionContracts, r), port.ErrExecutionConflict, "never overwrite a conflicting content-addressed blob")
	cursor, err := s.ExecutionCursor(t.Context(), port.ExecutionContracts)
	require.NoError(t, err)
	require.Equal(t, cfg.Anchor, cursor.Ref)
}

func TestExecutionCreationEvidenceAndSameBlockDestruction(t *testing.T) {
	s, cfg, _, a := accountFixture(t)
	addr := execwire.Address{4}
	code := []byte{0x60, 0x03, 0xff}
	b := a
	b.CodeHash = execwire.Hash(crypto.Keccak256Hash(code))
	r := accountResult(cfg, addr, execwire.Account{}, b, execwire.Account{})
	r.Steps[0].Code = []execwire.Code{{Hash: b.CodeHash, Bytes: code}}
	evidence := port.ExecutionCreation{Address: addr, Creator: execwire.Address{5}, TxIndex: 0, TxHash: r.Steps[0].TxHash}
	p := statePart(t, port.ExecutionContracts, r)
	bad := evidence
	bad.TxHash[0]++
	require.ErrorIs(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionState(ctx, p.Writer, r, bad) }), port.ErrExecutionConflict)
	require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionState(ctx, p.Writer, r, evidence) }))
	got, err := s.ExecutionCodeAt(t.Context(), addr, 1)
	require.NoError(t, err)
	require.Empty(t, got, "final tombstone is covered absence")
	var created, destroyed *int64
	var creator []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT created_height,destroyed_height,creator FROM state_contracts WHERE address=$1", addr[:]).Scan(&created, &destroyed, &creator))
	require.Equal(t, int64(1), *created)
	require.Equal(t, int64(1), *destroyed)
	require.Equal(t, evidence.Creator[:], creator)
	var history int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM state_contract_changes WHERE address=$1", addr[:]).Scan(&history))
	require.Equal(t, 2, history, "intermediate code activation survives final deletion")
	var stored []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT code FROM state_code WHERE code_hash=$1", b.CodeHash[:]).Scan(&stored))
	require.Equal(t, code, stored)
}

func TestExecutionEmptyCodeCreationEvidence(t *testing.T) {
	s, cfg, _, a := accountFixture(t)
	addr := execwire.Address{4}
	a.Nonce = 1
	r := accountResult(cfg, addr, execwire.Account{}, a, a)
	e := port.ExecutionCreation{Address: addr, Creator: execwire.Address{5}, TxIndex: 0, TxHash: r.Steps[0].TxHash}
	p := statePart(t, port.ExecutionContracts, r)
	require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionState(ctx, p.Writer, r, e) }), "a successful CREATE may deploy empty runtime code")
	var created, destroyed *int64
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT created_height,destroyed_height FROM state_contracts WHERE address=$1", addr[:]).Scan(&created, &destroyed))
	require.NotNil(t, created)
	require.Equal(t, int64(1), *created)
	require.Nil(t, destroyed)
	code, err := s.ExecutionCodeAt(t.Context(), addr, 1)
	require.NoError(t, err)
	require.Empty(t, code)
}

func TestExecutionCreationEndsWithinItsTransaction(t *testing.T) {
	s, cfg, _, _ := accountFixture(t)
	addr := execwire.Address{4}
	r := accountResult(cfg, addr, execwire.Account{}, execwire.Account{}, execwire.Account{})
	r.Steps[0].Accounts[0].StorageCleared = true
	e := port.ExecutionCreation{Address: addr, Creator: execwire.Address{5}, TxIndex: 0, TxHash: r.Steps[0].TxHash}
	p := statePart(t, port.ExecutionContracts, r)
	require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error { return s.ApplyExecutionState(ctx, p.Writer, r, e) }), "a verified successful creation receipt can outlive its account's constructor-time destruction")
	var created, destroyed *int64
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT created_height,destroyed_height FROM state_contracts WHERE address=$1", addr[:]).Scan(&created, &destroyed))
	require.NotNil(t, created)
	require.NotNil(t, destroyed)
	require.Equal(t, int64(1), *created)
	require.Equal(t, int64(1), *destroyed)
}
