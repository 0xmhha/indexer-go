package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/features/aa"
	"github.com/0xmhha/indexer-go/pkg/features/address"
	"github.com/0xmhha/indexer-go/pkg/features/balance"
	"github.com/0xmhha/indexer-go/pkg/features/token"
)

// The end-to-end tests index into Pebble. With INDEXER_TEST_DRIVER=postgres
// they index into the PostgreSQL database INDEXER_TEST_POSTGRES instead
// (refactoring plan R4-2), every database directory a schema of its own,
// and compare dumps of those schemas where they compare Pebble dumps, so
// the same assertions check both stores:
//
//	INDEXER_TEST_DRIVER=postgres INDEXER_TEST_POSTGRES=postgres://... go test ./cmd/indexer
//
// Tests of Pebble itself (the golden keyspace files, opening Pebble
// directly) call pebbleOnly.

// testOnPostgres reports whether the end-to-end tests run on PostgreSQL.
func testOnPostgres() bool { return os.Getenv("INDEXER_TEST_DRIVER") == config.DriverPostgres }

// testPostgresDSN returns the database the tests use on PostgreSQL.
func testPostgresDSN(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Fatal("INDEXER_TEST_DRIVER=postgres needs INDEXER_TEST_POSTGRES")
	}
	return dsn
}

// pebbleOnly skips a test of the Pebble store itself on PostgreSQL.
func pebbleOnly(t testing.TB) {
	t.Helper()
	if testOnPostgres() {
		t.Skip("checks the Pebble store itself")
	}
}

// testSchema is the PostgreSQL schema of a test database directory, and
// chainSchema of it for the chain databases <dir>/chains/<id>.
func testSchema(dir string) string {
	if root, id := filepath.Split(dir); filepath.Base(filepath.Clean(root)) == "chains" {
		return chainSchema(testSchema(filepath.Dir(filepath.Clean(root))), id)
	}
	sum := sha256.Sum256([]byte(dir))
	return "e2e_" + hex.EncodeToString(sum[:8])
}

// setTestDatabase points cfg at the test database dir: the Pebble directory,
// or its PostgreSQL schema (with those of its chains dropped after the
// test).
func setTestDatabase(t testing.TB, cfg *config.Config, dir string) {
	t.Helper()
	cfg.Database.Path = dir
	if !testOnPostgres() {
		return
	}
	dsn := testPostgresDSN(t)
	schema := testSchema(dir)
	cfg.Database.Driver = config.DriverPostgres
	cfg.Database.Postgres = config.PostgresConfig{DSN: dsn, Schema: schema, MaxConns: 8}
	t.Cleanup(func() { dropTestSchemas(dsn, schema) })
}

// dropTestSchemas drops schema and the schemas of its chains.
func dropTestSchemas(dsn, schema string) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(ctx) }()
	rows, err := conn.Query(ctx, "SELECT nspname FROM pg_namespace WHERE nspname = $1 OR nspname LIKE $1 || '\\_%'", schema)
	if err != nil {
		return
	}
	names, _ := pgx.CollectRows(rows, pgx.RowTo[string])
	for _, n := range names {
		_, _ = conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{n}.Sanitize()+" CASCADE")
	}
}

// pgDumpSkipped are the tables a PostgreSQL dump leaves out, as
// normalizeVolatile does for Pebble: undo, orphans and reorganizations, and
// the outbox depend on how blocks were processed, not only on the chain.
var pgDumpSkipped = map[string]bool{
	"schema_migrations": true, "undo_log": true, "undo_blocks": true,
	"reorgs": true, "orphaned_blocks": true, "orphaned_transactions": true,
	"outbox": true, "outbox_sequence": true, "outbox_cursors": true,
}

// pgSerialLists are the tables whose bigserial id only orders an address's
// list: a rolled-back insert still uses an id, so a dump shows the position
// within the address instead.
var pgSerialLists = map[string]bool{"address_transactions": true, "balance_history": true}

// dumpPostgres returns the rows of the test database dir as dump entries
// in key order: key <table>/<primary key as JSON>, value the row as JSON.
func dumpPostgres(t testing.TB, dir string) []testchain.Entry {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testPostgresDSN(t))
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	schema := testSchema(dir)

	rows, err := conn.Query(ctx, `SELECT c.relname,
			(SELECT string_agg(quote_ident(a.attname), ', ' ORDER BY k.ord)
			 FROM pg_index i CROSS JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
			 JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
			 WHERE i.indrelid = c.oid AND i.indisprimary)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind = 'r'`, schema)
	require.NoError(t, err)
	type table struct{ name, pk string }
	tables, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (table, error) {
		var tb table
		err := r.Scan(&tb.name, &tb.pk)
		return tb, err
	})
	require.NoError(t, err)
	require.NotEmpty(t, tables, "schema %s of %s has no tables", schema, dir)

	var out []testchain.Entry
	for _, tb := range tables {
		if pgDumpSkipped[tb.name] {
			continue
		}
		from := pgx.Identifier{schema, tb.name}.Sanitize()
		key := "jsonb_build_array(" + tb.pk + ")"
		row := "to_jsonb(t)"
		if pgSerialLists[tb.name] {
			key = "jsonb_build_array(address, row_number() OVER (PARTITION BY address ORDER BY id))"
			row = "to_jsonb(t) - 'id'"
		}
		q := "SELECT '" + tb.name + "/' || (" + key + ")::text, (" + row + ")::text FROM " + from + " t"
		if tb.name == "meta" {
			q += " WHERE name <> 'reorg_seq'" // the reorganizations, skipped above
		}
		rows, err := conn.Query(ctx, q)
		require.NoError(t, err, tb.name)
		entries, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (testchain.Entry, error) {
			var k, v string
			err := r.Scan(&k, &v)
			return testchain.Entry{Key: []byte(k), Value: []byte(v)}, err
		})
		require.NoError(t, err, tb.name)
		out = append(out, entries...)
	}
	sort.Slice(out, func(i, j int) bool { return strings.Compare(string(out[i].Key), string(out[j].Key)) < 0 })
	return out
}

// featureStateKey is the dump key of a feature's state.
func featureStateKey(name string) string {
	if testOnPostgres() {
		return `feature_states/["` + name + `"]`
	}
	return "/meta/features/" + name
}

// pgFeatureKeys lists, per feature, the PostgreSQL dump key prefixes only
// that feature writes (featureKeys lists the Pebble ones).
var pgFeatureKeys = map[string][]string{
	address.Name:         {"address_transactions/", "contract_creations/"},
	balance.Name:         {"balance_history/", "balances/"},
	token.TransfersName:  {"erc20_transfers/", "erc721_transfers/", "nft_owners/"},
	aa.EIP7702:           {"setcode_authorizations/", "setcode_delegations/", "setcode_stats/"},
	aa.ERC4337:           {"user_operations/", "bundler_stats/", "factory_stats/", "paymaster_stats/", "smart_accounts/"},
	aa.ERC7579:           {"installed_modules/", "module_stats/"},
	systemcontracts.Name: {kvDumpPrefix("/data/syscontracts/"), kvDumpPrefix("/index/syscontracts/")},
}

// kvDumpPrefix is the PostgreSQL dump key prefix of the kv rows under a key
// prefix (a bytea key in JSON is "\\x" and hex).
func kvDumpPrefix(p string) string { return `kv/["\\x` + hex.EncodeToString([]byte(p)) }

// featureKeyPrefixes returns the dump key prefixes only a feature writes.
func featureKeyPrefixes(name string) []string {
	if testOnPostgres() {
		return pgFeatureKeys[name]
	}
	return featureKeys[name]
}

// reindexTestDatabase reindexes the test database dir the way --reindex
// does with its driver.
func reindexTestDatabase(t testing.TB, dir string) {
	t.Helper()
	cfg := config.NewConfig()
	setTestDatabase(t, cfg, dir)
	require.NoError(t, reindexDatabases(cfg, zap.NewNop()))
}
