// Package postgres is the PostgreSQL adapter of the storage ports in
// pkg/core/port (refactoring plan R4-2). Every process that opens the same
// database sees the same data, so one ingest process and many API
// processes can share it (R4-1).
//
// Each port's data lives in tables with the columns its queries filter and
// sort on; rows also keep the chain-neutral model encoded with
// pkg/core/model, so they read back exactly as the chain profile decoded
// them. The schema is created and upgraded by the migrations under
// migrations/ when a store opens (Migrate).
//
// A block transaction (BeginBlock) is a PostgreSQL transaction bound to the
// returned context: storage calls made with that context belong to it, see
// its writes, and commit or roll back together. Like a Pebble batch, a block
// transaction is used by one goroutine at a time.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Options configure a Store.
type Options struct {
	// DSN is the connection string (postgres://user:pass@host:port/db).
	DSN string
	// Schema is the PostgreSQL schema the store's tables live in; empty
	// uses "public". It is created when missing.
	Schema string
	// MaxConns caps the connection pool; 0 keeps the pgxpool default.
	MaxConns int32
	// ReadOnly refuses writes (an API process, R4-1) and does not migrate:
	// the schema must be current.
	ReadOnly bool
}

// Store implements the storage ports on PostgreSQL.
type Store struct {
	pool     *pgxpool.Pool
	schema   string
	readOnly bool

	// orphanRetention is how many reorganization records are kept with
	// their orphaned blocks; 0 keeps all (SetOrphanRetention).
	orphanRetention atomic.Uint64
}

var schemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Open connects to the database, creates the schema when missing, and
// brings it to the current version unless the store is read-only.
func Open(ctx context.Context, opts Options) (*Store, error) {
	schema := opts.Schema
	if schema == "" {
		schema = "public"
	}
	if !schemaName.MatchString(schema) {
		return nil, fmt.Errorf("postgres: invalid schema name %q", schema)
	}
	cfg, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN: %w", err)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	if !opts.ReadOnly {
		// Create the schema before the pool's connections use it.
		conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
		if err != nil {
			return nil, fmt.Errorf("postgres: connect: %w", err)
		}
		// CREATE SCHEMA IF NOT EXISTS is not safe against itself running
		// concurrently (a unique violation), so processes opening a new
		// schema together take the migration lock first.
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrateLock); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize())
			return err
		})
		_ = conn.Close(ctx)
		if err != nil {
			return nil, fmt.Errorf("postgres: create schema %s: %w", schema, err)
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	s := &Store{pool: pool, schema: schema, readOnly: opts.ReadOnly}
	if opts.ReadOnly {
		if err := s.checkVersion(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return s, nil
	}
	if err := s.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// querier is a connection pool or a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// blockTxKey is the context key a block transaction is bound under.
type blockTxKey struct{}

// q returns where a call made with ctx runs: the block transaction of this
// store bound to ctx, or the pool.
func (s *Store) q(ctx context.Context) querier {
	if tx := s.boundTx(ctx); tx != nil {
		return tx.tx
	}
	return s.pool
}

func (s *Store) boundTx(ctx context.Context) *blockTx {
	if ctx == nil {
		return nil
	}
	if tx, ok := ctx.Value(blockTxKey{}).(*blockTx); ok && tx.owner == s && !tx.done {
		return tx
	}
	return nil
}

// write checks that the store accepts writes.
func (s *Store) write() error {
	if s.readOnly {
		return port.ErrReadOnly
	}
	return nil
}

// inTx runs fn in the block transaction bound to ctx, or in a transaction
// of its own, so a method's writes apply together.
func (s *Store) inTx(ctx context.Context, fn func(q querier) error) error {
	if err := s.write(); err != nil {
		return err
	}
	if tx := s.boundTx(ctx); tx != nil {
		return fn(tx.tx)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(tx) })
}

// blockTx is an open block transaction.
type blockTx struct {
	owner  *Store
	tx     pgx.Tx
	done   bool
	height *uint64
}

var _ port.BlockTransactor = (*Store)(nil)

// BeginBlock implements port.BlockTransactor.
func (s *Store) BeginBlock(ctx context.Context) (context.Context, port.BlockTx, error) {
	if err := s.write(); err != nil {
		return nil, nil, err
	}
	// The transaction outlives the call's context: it ends with Commit or
	// Rollback.
	tx, err := s.pool.Begin(context.WithoutCancel(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: begin block transaction: %w", err)
	}
	// The tracked tables record their changes for undo (migration 0006).
	if _, err := tx.Exec(ctx, "SELECT set_config('indexer.undo', 'on', true)"); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, nil, fmt.Errorf("postgres: begin block transaction: %w", err)
	}
	bt := &blockTx{owner: s, tx: tx}
	return context.WithValue(ctx, blockTxKey{}, bt), bt, nil
}

// SetHeight implements port.BlockTx.
func (t *blockTx) SetHeight(height uint64) { t.height = &height }

// UndoWindow is how many recent blocks can be rolled back, as in the
// Pebble store.
const UndoWindow = 128

// Commit implements port.BlockTx. With a height, the changes the
// transaction recorded become the block's undo (replacing an earlier record
// of the height) and the record that leaves the window is dropped; without
// one they are discarded.
func (t *blockTx) Commit() error {
	if t.done {
		return port.ErrBlockTxDone
	}
	t.done = true
	ctx := context.Background()
	if err := t.recordUndo(ctx); err != nil {
		_ = t.tx.Rollback(ctx)
		return fmt.Errorf("postgres: record undo: %w", err)
	}
	if err := t.tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit block transaction: %w", err)
	}
	return nil
}

func (t *blockTx) recordUndo(ctx context.Context) error {
	if t.height == nil {
		_, err := t.tx.Exec(ctx, "DELETE FROM undo_log WHERE height IS NULL AND txid = txid_current()")
		return err
	}
	h := i64(*t.height)
	b := &pgx.Batch{}
	b.Queue("DELETE FROM undo_log WHERE height = $1", h)
	b.Queue("UPDATE undo_log SET height = $1 WHERE height IS NULL AND txid = txid_current()", h)
	b.Queue("INSERT INTO undo_blocks (height) VALUES ($1) ON CONFLICT DO NOTHING", h)
	if h >= UndoWindow {
		b.Queue("DELETE FROM undo_log WHERE height <= $1", h-UndoWindow)
		b.Queue("DELETE FROM undo_blocks WHERE height <= $1", h-UndoWindow)
	}
	return sendBatch(ctx, t.tx, b)
}

// Rollback implements port.BlockTx.
func (t *blockTx) Rollback() {
	if t.done {
		return
	}
	t.done = true
	_ = t.tx.Rollback(context.Background())
}

// notFound maps pgx.ErrNoRows to port.ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return port.ErrNotFound
	}
	return err
}

// i64 converts a height or index to the bigint PostgreSQL stores.
func i64(v uint64) int64 { return int64(v) }
