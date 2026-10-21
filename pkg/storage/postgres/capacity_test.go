package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// insufficientPrivilege is SQLSTATE 42501.
const insufficientPrivilege = "42501"

// TestCapacityStatistics: the database size is readable by the store's own
// role; the WAL size needs a monitoring privilege and otherwise fails with
// a privilege error, never a wrong value.
func TestCapacityStatistics(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	db, err := s.DatabaseBytes(ctx)
	require.NoError(t, err)
	require.Positive(t, db)
	wal, err := s.WALBytes(ctx)
	if err != nil {
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr), "%v", err)
		require.Equal(t, insufficientPrivilege, pgErr.Code)
		return
	}
	require.Positive(t, wal, "a running server has WAL segments")

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.DatabaseBytes(canceled)
	require.ErrorIs(t, err, context.Canceled)
}

// TestWALStatisticGrant: an ordinary role reads the WAL size after
// GRANT EXECUTE ON FUNCTION pg_ls_waldir(), as deployments/testnets
// init-indexer.sql grants it, and is refused without it. Needs a role that
// can create roles; the transaction is rolled back.
func TestWALStatisticGrant(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN(t))
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	var canCreate bool
	require.NoError(t, conn.QueryRow(ctx, "SELECT rolsuper OR rolcreaterole FROM pg_roles WHERE rolname = current_user").Scan(&canCreate))
	if !canCreate {
		t.Skip("the test role cannot create roles")
	}
	query := "SELECT COALESCE(sum(size), 0)::bigint FROM pg_ls_waldir()"
	role := pgx.Identifier{newTestSchema(t) + "_role"}.Sanitize()
	for _, granted := range []bool{false, true} {
		tx, err := conn.Begin(ctx)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, "CREATE ROLE "+role+" NOLOGIN")
		require.NoError(t, err)
		if granted {
			_, err = tx.Exec(ctx, "GRANT EXECUTE ON FUNCTION pg_ls_waldir() TO "+role)
			require.NoError(t, err)
		}
		_, err = tx.Exec(ctx, "SET LOCAL ROLE "+role)
		require.NoError(t, err)
		var n int64
		err = tx.QueryRow(ctx, query).Scan(&n)
		if granted {
			require.NoError(t, err)
			require.Positive(t, n)
		} else {
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, insufficientPrivilege, pgErr.Code)
		}
		require.NoError(t, tx.Rollback(ctx))
	}
}
