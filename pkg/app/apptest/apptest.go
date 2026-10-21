// Package apptest runs the indexer engine (pkg/app) against a test chain
// (pkg/testchain) for tests outside pkg/app, such as the end-to-end tests
// of a chain under x/<chain>. Tests index into Pebble; with
// INDEXER_TEST_DRIVER=postgres they index into a schema of the PostgreSQL
// database INDEXER_TEST_POSTGRES, dropped after the test, as pkg/app's own
// tests do.
package apptest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/app"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// rpcTimeout bounds each test RPC call.
const rpcTimeout = 5 * time.Second

// indexTimeout bounds indexing a test chain to its head.
const indexTimeout = time.Minute

// OnPostgres reports whether the tests run on PostgreSQL.
func OnPostgres() bool { return os.Getenv("INDEXER_TEST_DRIVER") == config.DriverPostgres }

// PebbleOnly skips a test of the Pebble store itself on PostgreSQL.
func PebbleOnly(t testing.TB) {
	t.Helper()
	if OnPostgres() {
		t.Skip("checks the Pebble store itself")
	}
}

// Config returns the configuration of a test indexer of the node at
// endpoint: a fresh test database, the API off.
func Config(t testing.TB, endpoint string) *config.Config {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.Timeout = rpcTimeout
	cfg.API.Enabled = false
	database(t, cfg, filepath.Join(t.TempDir(), "db"))
	return cfg
}

// Enable turns features on in cfg.
func Enable(cfg *config.Config, features ...string) {
	on := map[string]bool{}
	for _, name := range features {
		on[name] = true
	}
	SetFeatures(cfg, on)
}

// SetFeatures turns each feature in features on or off in cfg.
func SetFeatures(cfg *config.Config, features map[string]bool) {
	if cfg.Features == nil {
		cfg.Features = map[string]config.FeatureConfig{}
	}
	for name, on := range features {
		cfg.Features[name] = config.FeatureConfig{Enabled: &on}
	}
}

// Start builds an App of cfg, shut down after the test.
func Start(t testing.TB, cfg *config.Config) *app.App {
	t.Helper()
	a, err := app.NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	t.Cleanup(a.Shutdown)
	return a
}

// Index serves chain, indexes it to its head with the features given on,
// and returns the App.
func Index(t testing.TB, chain *testchain.Chain, features ...string) *app.App {
	t.Helper()
	srv := testchain.NewServer(chain)
	t.Cleanup(srv.Close)
	cfg := Config(t, srv.URL())
	Enable(cfg, features...)
	a := Start(t, cfg)
	FetchAll(t, a, 0, chain.Head())
	return a
}

// FetchAll indexes the blocks from..to of a and fails the test on error.
func FetchAll(t testing.TB, a *app.App, from, to uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), indexTimeout)
	defer cancel()
	require.NoError(t, a.FetchRange(ctx, from, to))
}

// database points cfg at the test database dir: the Pebble directory, or
// its PostgreSQL schema, dropped after the test.
func database(t testing.TB, cfg *config.Config, dir string) {
	t.Helper()
	cfg.Database.Path = dir
	if !OnPostgres() {
		return
	}
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	require.NotEmpty(t, dsn, "INDEXER_TEST_DRIVER=postgres needs INDEXER_TEST_POSTGRES")
	sum := sha256.Sum256([]byte(dir))
	schema := "e2e_" + hex.EncodeToString(sum[:8])
	cfg.Database.Driver = config.DriverPostgres
	cfg.Database.Postgres = config.PostgresConfig{DSN: dsn, Schema: schema, MaxConns: 4}
	t.Cleanup(func() { dropSchema(dsn, schema) })
}

// dropSchema drops a test schema; a failure leaves it for the next run's
// database to ignore.
func dropSchema(dsn, schema string) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(ctx) }()
	_, _ = conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
}
