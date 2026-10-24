package postgres

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type executionReadKey struct{}

func (s *Store) loadExecutionMode(ctx context.Context) error {
	var active bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM exec_control WHERE id) OR EXISTS(SELECT 1 FROM exec_rewinds)").Scan(&active); err != nil {
		return err
	}
	s.executionMode.Store(active)
	return nil
}
func (s *Store) executionReadBound(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(executionReadKey{}).(*executionReadView)
	return ok && v.owner == s
}

// ReadSnapshot borrows the existing API reservation or pool; it opens no extra
// pool. Execution mode is selected before API startup, never hot-enabled.
func (s *Store) ReadSnapshot(ctx context.Context, read func(context.Context) error) error {
	if read == nil {
		return port.ErrInvalidData
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.executionReadBound(ctx) || s.boundTx(ctx) != nil || !s.executionMode.Load() {
		return read(ctx)
	}
	var conn *pgxpool.Conn
	var connRelease func()
	var err error
	if reader, ok := ctx.Value(queryReaderKey{}).(*reservedQueryReader); ok && reader.owner == s {
		conn, connRelease, err = reader.acquire(ctx)
	} else {
		conn, err = s.pool.Acquire(ctx)
		if err == nil {
			connRelease = conn.Release
		}
	}
	if err != nil {
		return err
	}
	defer connRelease()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock_shared("+executionLockSQL+")").Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return port.ErrExecutionRewinding
	}
	c, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL))
	if err != nil {
		return err
	}
	if c.Rewinding {
		return port.ErrExecutionRewinding
	}
	viewCtx, cancel := context.WithCancel(ctx)
	v := &executionReadView{owner: s, tx: tx, ctx: viewCtx, cancel: cancel, gate: make(chan struct{}, 1)}
	v.gate <- struct{}{}
	defer v.stop()
	return read(context.WithValue(viewCtx, executionReadKey{}, v))
}

type executionReadView struct {
	owner   *Store
	tx      pgx.Tx
	ctx     context.Context
	cancel  context.CancelFunc
	gate    chan struct{}
	mu      sync.Mutex
	closing bool
	active  *executionReadRows
	wg      sync.WaitGroup
}

func (v *executionReadView) admit(ctx context.Context) (func(), error) {
	v.mu.Lock()
	if v.closing {
		v.mu.Unlock()
		return nil, port.ErrClosed
	}
	v.wg.Add(1)
	v.mu.Unlock()
	select {
	case <-ctx.Done():
		v.wg.Done()
		return nil, ctx.Err()
	case <-v.ctx.Done():
		v.wg.Done()
		return nil, v.ctx.Err()
	case <-v.gate:
	}
	if err := v.ctx.Err(); err != nil {
		v.gate <- struct{}{}
		v.wg.Done()
		return nil, err
	}
	return func() { v.gate <- struct{}{}; v.wg.Done() }, nil
}
func (v *executionReadView) stop() {
	v.mu.Lock()
	v.closing = true
	active := v.active
	v.mu.Unlock()
	v.cancel()
	if active != nil {
		active.Close()
	}
	v.wg.Wait()
}
func (v *executionReadView) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, port.ErrReadOnly
}
func (v *executionReadView) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	release, err := v.admit(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := (transactionQuery{v.tx}).Query(ctx, sql, args...)
	if err != nil {
		release()
		return nil, err
	}
	r := &executionReadRows{Rows: rows, view: v, release: release}
	v.mu.Lock()
	if v.closing {
		v.mu.Unlock()
		r.Close()
		return nil, port.ErrClosed
	}
	v.active = r
	v.mu.Unlock()
	return r, nil
}
func (v *executionReadView) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	rows, err := v.Query(ctx, sql, args...)
	if err != nil {
		return errRow{err}
	}
	return executionReadRow{rows}
}

type executionReadRow struct{ rows pgx.Rows }

func (r executionReadRow) Scan(dest ...any) error {
	defer r.rows.Close()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return pgx.ErrNoRows
	}
	return r.rows.Scan(dest...)
}

type executionReadRows struct {
	pgx.Rows
	view    *executionReadView
	release func()
	mu      sync.Mutex
	closed  bool
}

func (r *executionReadRows) closeLocked() {
	if r.closed {
		return
	}
	r.closed = true
	r.Rows.Close()
	r.view.mu.Lock()
	if r.view.active == r {
		r.view.active = nil
	}
	r.view.mu.Unlock()
	r.release()
}
func (r *executionReadRows) Close() { r.mu.Lock(); defer r.mu.Unlock(); r.closeLocked() }
func (r *executionReadRows) Next() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	if r.Rows.Next() {
		return true
	}
	r.closeLocked()
	return false
}
func (r *executionReadRows) Scan(dest ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return port.ErrClosed
	}
	return r.Rows.Scan(dest...)
}
func (r *executionReadRows) Err() error { r.mu.Lock(); defer r.mu.Unlock(); return r.Rows.Err() }
func (r *executionReadRows) Values() ([]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, port.ErrClosed
	}
	return r.Rows.Values()
}
func (r *executionReadRows) RawValues() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	raw := r.Rows.RawValues()
	out := make([][]byte, len(raw))
	for i := range raw {
		out[i] = bytes.Clone(raw[i])
	}
	return out
}
func (r *executionReadRows) CommandTag() pgconn.CommandTag {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Rows.CommandTag()
}
func (r *executionReadRows) FieldDescriptions() []pgconn.FieldDescription {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Rows.FieldDescriptions()
}
func (r *executionReadRows) Conn() *pgx.Conn { r.mu.Lock(); defer r.mu.Unlock(); return r.Rows.Conn() }

var _ port.QuerySnapshotter = (*Store)(nil)

func (s *Store) ExecutionSnapshotsEnabled() bool { return s.executionMode.Load() }
