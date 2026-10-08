package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The schema is defined by the files in migrations/, named
// NNNN_description.sql and applied in order, each once, in a transaction of
// its own. schema_migrations records the applied versions. A migration is
// never edited once released; a change is a new file. A migration that adds
// a table of indexed data calls undo_track for it, so rollbacks restore it
// (migration 0006, TestEveryTableIsUndoTracked).

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration is one schema change.
type migration struct {
	version int
	name    string
	sql     string
}

// migrations returns the embedded migrations in version order.
func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		num, _, ok := strings.Cut(name, "_")
		if !ok || !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("postgres: migration %q is not named NNNN_description.sql", name)
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("postgres: migration %q: %w", name, err)
		}
		data, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: name, sql: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("postgres: migration versions must be 1, 2, ...; found %d at position %d", m.version, i+1)
		}
	}
	return out, nil
}

// SchemaVersion is the schema version this build needs: its last
// migration.
func SchemaVersion() int {
	ms, err := migrations()
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].version
}

// migrateLock is the advisory lock that serializes migrations of a
// database, so processes started together do not apply one twice.
const migrateLock = 0x696e6478 // "indx"

// Migrate applies the migrations the schema has not had yet. A schema newer
// than this build is refused.
func (s *Store) Migrate(ctx context.Context) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLock); err != nil {
			return fmt.Errorf("postgres: lock migrations: %w", err)
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			version    integer PRIMARY KEY,
			name       text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return fmt.Errorf("postgres: create schema_migrations: %w", err)
		}
		current, err := appliedVersion(ctx, tx)
		if err != nil {
			return err
		}
		if current > len(ms) {
			return fmt.Errorf("postgres: schema version %d is newer than this build (%d)", current, len(ms))
		}
		for _, m := range ms[current:] {
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return fmt.Errorf("postgres: migration %s: %w", m.name, err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", m.version, m.name); err != nil {
				return fmt.Errorf("postgres: record migration %s: %w", m.name, err)
			}
		}
		return nil
	})
}

// appliedVersion returns the last applied migration, 0 for none.
func appliedVersion(ctx context.Context, q querier) (int, error) {
	var v int
	if err := q.QueryRow(ctx, "SELECT COALESCE(max(version), 0) FROM schema_migrations").Scan(&v); err != nil {
		return 0, fmt.Errorf("postgres: read schema version: %w", err)
	}
	return v, nil
}

// checkVersion fails unless the schema is at this build's version (a
// read-only store does not migrate).
func (s *Store) checkVersion(ctx context.Context) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return fmt.Errorf("postgres: read schema version: %w", err)
	}
	v := 0
	if exists {
		var err error
		if v, err = appliedVersion(ctx, s.pool); err != nil {
			return err
		}
	}
	if want := SchemaVersion(); v != want {
		return fmt.Errorf("postgres: schema version %d, this build needs %d (start an ingest process to migrate)", v, want)
	}
	return nil
}
