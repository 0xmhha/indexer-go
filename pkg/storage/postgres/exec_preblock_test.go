package postgres

import (
	"fmt"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestExecutionPreBlockCauseSurvivesStateWriters(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	b := a
	b.Nonce--
	r := accountResult(cfg, addr, a, b, b)
	r.TxCount = 0
	r.Steps[0].Cause = execwire.PreBlock
	r.Steps[0].TxIndex = -1
	r.Steps[0].TxHash = execwire.Hash{}
	for _, w := range []port.ExecutionWriter{port.ExecutionAccounts, port.ExecutionStorage, port.ExecutionContracts} {
		require.NoError(t, applyStatePart(t, s, w, r), "pre-block is a system transition, not a transaction")
	}
	var cause, txIndex int
	var txHash []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT cause,tx_index,tx_hash FROM state_account_changes WHERE height=$1 AND ordinal=0", int64(r.Ref.Height)).Scan(&cause, &txIndex, &txHash))
	require.Equal(t, int(execwire.PreBlock), cause)
	require.Equal(t, -1, txIndex)
	require.Nil(t, txHash)
	got, err := s.ExecutionAccountAt(t.Context(), addr, r.Ref.Height)
	require.NoError(t, err)
	require.Equal(t, b, got)
}

func TestExecutionPreBlockMigrationPreservesExistingPartitions(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	b := a
	b.Nonce--
	old := accountResult(cfg, addr, a, b, b)
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, old))
	restoreExecutionV16(t, s)
	require.NoError(t, s.Migrate(t.Context()))
	c := b
	c.Nonce--
	next := accountResult(cfg, addr, b, c, c)
	next.Parent, next.Ref.Height, next.Ref.Hash = old.Ref.Hash, old.Ref.Height+1, execwire.Hash{3}
	next.TxCount, next.Steps[0].Cause, next.Steps[0].TxIndex, next.Steps[0].TxHash = 0, execwire.PreBlock, -1, execwire.Hash{}
	require.NoError(t, applyStatePart(t, s, port.ExecutionAccounts, next))
	got, err := s.ExecutionAccountAt(t.Context(), addr, old.Ref.Height)
	require.NoError(t, err)
	require.Equal(t, b, got, "the existing partition and its history survive the additive migration")
	_, err = s.pool.Exec(t.Context(), "UPDATE state_account_changes SET cause=5 WHERE height=$1", int64(next.Ref.Height))
	require.Error(t, err, "unknown future causes remain rejected")
}

// Reconstruct the released v16 fixture shape, removing later additive
// tables/columns, before replaying upgrades. Deleting only one version entry
// leaves a newer max(version), causing Migrate to skip the intended upgrade.
// This is test-only reconstruction, not a supported production downgrade.
func restoreExecutionV16(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(t.Context(), `DROP TABLE exec_call_orphan_frames,exec_call_orphan_blocks,exec_state_proofs,exec_blob_entries,exec_blob_blocks,exec_call_frames,exec_call_blocks; ALTER TABLE exec_publications DROP COLUMN call_traces,DROP COLUMN blob_sidecars,DROP COLUMN state_proofs`)
	require.NoError(t, err)
	// Restore the four exact v16 checks on an existing partitioned schema.
	for _, table := range []string{"state_account_changes", "state_storage_changes", "state_storage_clears", "state_contract_changes"} {
		identifier := pgx.Identifier{table}.Sanitize()
		constraint := pgx.Identifier{table + "_cause_check"}.Sanitize()
		_, err := s.pool.Exec(t.Context(), fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s; ALTER TABLE %s ADD CONSTRAINT %s CHECK(cause BETWEEN 0 AND 3)", identifier, constraint, identifier, constraint))
		require.NoError(t, err)
	}
	_, err = s.pool.Exec(t.Context(), "DELETE FROM schema_migrations WHERE version>=17")
	require.NoError(t, err)
	version, err := appliedVersion(t.Context(), s.pool)
	require.NoError(t, err)
	require.Equal(t, 16, version)
}
