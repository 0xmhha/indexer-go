package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// testDSN is the database the tests use, from INDEXER_TEST_POSTGRES
// (postgres://user@host:port/db). Without it the tests are skipped.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set INDEXER_TEST_POSTGRES to a PostgreSQL database to test the adapter")
	}
	return dsn
}

// newTestStore opens a store in a schema of its own, dropped after the
// test, so tests do not see each other's data.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return openTestStore(t, newTestSchema(t), false)
}

// newTestSchema returns a fresh schema name that is dropped after the test.
func newTestSchema(t *testing.T) string {
	t.Helper()
	dsn := testDSN(t)
	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := "t_" + hex.EncodeToString(b[:])
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(context.Background()) }()
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})
	return schema
}

func openTestStore(t *testing.T, schema string, readOnly bool) *Store {
	t.Helper()
	s, err := Open(context.Background(), Options{DSN: testDSN(t), Schema: schema, MaxConns: 4, ReadOnly: readOnly})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}
