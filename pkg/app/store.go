package app

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
)

// The store is chosen by database.driver: Pebble files under database.path
// (the default) or PostgreSQL (database.postgres, refactoring plan R4-2).
// Both implement every storage port; this file is the only place that
// tells them apart.

var (
	_ storage.Storage = (*storage.PebbleStorage)(nil)
	_ storage.Storage = (*postgres.Store)(nil)
)

// usesPostgres reports whether db selects the PostgreSQL store.
func usesPostgres(db *config.DatabaseConfig) bool { return db.Driver == config.DriverPostgres }

// openStore opens the store db selects: for indexing, or read-only for an
// API process (node.role api, PostgreSQL only).
func openStore(ctx context.Context, db *config.DatabaseConfig, readOnly bool, logger *zap.Logger) (storage.Storage, error) {
	if usesPostgres(db) {
		s, err := postgres.Open(ctx, postgres.Options{
			DSN:      db.Postgres.DSN,
			Schema:   db.Postgres.Schema,
			MaxConns: db.Postgres.MaxConns,
			ReadOnly: readOnly,
		})
		if err != nil {
			return nil, err
		}
		s.SetLogger(logger)
		return s, nil
	}
	if readOnly {
		return nil, fmt.Errorf("a read-only process needs database.driver %s", config.DriverPostgres)
	}
	cfg := storage.DefaultConfig(db.Path)
	cfg.ReadOnly = false
	s, err := storage.NewPebbleStorage(cfg)
	if err != nil {
		return nil, err
	}
	s.SetLogger(logger)
	return s, nil
}

func openPostgres(ctx context.Context, db *config.DatabaseConfig) (*postgres.Store, error) {
	return postgres.Open(ctx, postgres.Options{
		DSN:      db.Postgres.DSN,
		Schema:   db.Postgres.Schema,
		MaxConns: db.Postgres.MaxConns,
	})
}

// storeLocation describes where db keeps the index, for logs: the Pebble
// path or the PostgreSQL schema (never the DSN, which may hold a password).
func storeLocation(db *config.DatabaseConfig) zap.Field {
	if usesPostgres(db) {
		schema := db.Postgres.Schema
		if schema == "" {
			schema = "public"
		}
		return zap.String("postgres_schema", schema)
	}
	return zap.String("path", db.Path)
}

// chainSchema returns the PostgreSQL schema of a chain in multichain mode:
// <schema>_<chain id> (chain_<id> without a schema), lower case, with every
// character a schema name cannot hold replaced by '_'.
func chainSchema(base, chainID string) string {
	if base == "" {
		base = "chain"
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base + "_" + chainID) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// chainDatabase returns the database configuration of one chain in
// multichain mode: its own Pebble directory or PostgreSQL schema.
func chainDatabase(db config.DatabaseConfig, chainID string) config.DatabaseConfig {
	db.Path = chainDBPath(db.Path, chainID)
	if usesPostgres(&db) {
		db.Postgres.Schema = chainSchema(db.Postgres.Schema, chainID)
	}
	return db
}

// databases returns the databases the configuration indexes into: one per
// chain in multichain mode, else the configured one. Two chains mapping to
// the same PostgreSQL schema is an error.
func databases(cfg *config.Config) ([]config.DatabaseConfig, error) {
	if !cfg.MultiChainMode() {
		return []config.DatabaseConfig{cfg.Database}, nil
	}
	out := make([]config.DatabaseConfig, 0, len(cfg.MultiChain.Chains))
	schemas := map[string]string{}
	for _, cc := range cfg.MultiChain.Chains {
		db := chainDatabase(cfg.Database, cc.ID)
		if usesPostgres(&db) {
			if other, dup := schemas[db.Postgres.Schema]; dup {
				return nil, fmt.Errorf("chains %q and %q both map to PostgreSQL schema %q", other, cc.ID, db.Postgres.Schema)
			}
			schemas[db.Postgres.Schema] = cc.ID
		}
		out = append(out, db)
	}
	return out, nil
}

// clearDatabases deletes every indexed and user record (--clear-data).
func clearDatabases(cfg *config.Config, log *zap.Logger) error {
	if !usesPostgres(&cfg.Database) {
		return clearDataFolder(cfg.Database.Path, log)
	}
	dbs, err := databases(cfg)
	if err != nil {
		return err
	}
	for i := range dbs {
		if err := clearPostgres(&dbs[i], true, log); err != nil {
			return err
		}
	}
	return nil
}

// reindexDatabases deletes the indexed chain data and keeps user data
// (--reindex).
func reindexDatabases(cfg *config.Config, log *zap.Logger) error {
	dbs, err := databases(cfg)
	if err != nil {
		return err
	}
	for i := range dbs {
		if usesPostgres(&dbs[i]) {
			err = clearPostgres(&dbs[i], false, log)
		} else {
			err = reindexData(dbs[i].Path, log)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// clearPostgres clears one PostgreSQL schema: the chain data, and with all
// the user data too.
func clearPostgres(db *config.DatabaseConfig, all bool, log *zap.Logger) error {
	ctx := context.Background()
	s, err := openPostgres(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() { _ = s.Close() }()
	log.Warn("Clearing PostgreSQL data", storeLocation(db), zap.Bool("user_data", all))
	tables, err := s.ClearChainData(ctx, storage.PrefixesOf(storage.ChainData), all)
	if err != nil {
		return err
	}
	log.Info("PostgreSQL data cleared", storeLocation(db), zap.Strings("tables", tables))
	return nil
}
