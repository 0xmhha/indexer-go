package postgres

import (
	"context"
	"errors"
	"runtime/trace"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryReaderKey struct{}

var _ port.QueryReaderReserver = (*Store)(nil)

// ReserveQueryReader pins one of the EXISTING pool's connections. Background
// work cannot occupy this slot while waiting for RPC/fresh or transaction
// locks. Read-only API processes and pools smaller than four keep all their
// original capacity. Multiple API servers share one reservation by reference.
func (s *Store) ReserveQueryReader(ctx context.Context) (port.QueryReaderLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.queryMu.Lock()
	defer s.queryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.queryClosing {
		return nil, port.ErrClosed
	}
	if s.readOnly || s.pool.Config().MaxConns < 4 {
		return nil, nil
	}
	if s.queryReader == nil {
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		r := &reservedQueryReader{owner: s, conn: conn, gate: make(chan struct{}, 1), stopped: make(chan struct{}), idle: make(chan struct{})}
		r.gate <- struct{}{}
		s.queryReader = r
	}
	r := s.queryReader
	r.refs++ // protected by queryMu; query operations use gate instead
	return &queryReaderLease{reader: r}, nil
}

type queryReaderLease struct {
	reader *reservedQueryReader
	once   sync.Once
}

func (l *queryReaderLease) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, queryReaderKey{}, l.reader)
}
func (l *queryReaderLease) Close() {
	l.once.Do(func() {
		s := l.reader.owner
		s.queryMu.Lock()
		defer s.queryMu.Unlock()
		if s.queryReader != l.reader {
			return // Store.Close already released it
		}
		l.reader.refs--
		if l.reader.refs == 0 {
			l.reader.close()
			s.queryReader = nil
		}
	})
}

type reservedQueryReader struct {
	owner   *Store
	conn    *pgxpool.Conn
	gate    chan struct{}
	stateMu sync.Mutex
	closing bool
	active  int
	stopped chan struct{}
	idle    chan struct{}
	refs    int
}

// begin/finish include admission waiters and both pinned and borrowed rows,
// so Close can stop new work and wait for every active connection safely.
func (r *reservedQueryReader) begin() error {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.closing {
		return port.ErrClosed
	}
	r.active++
	return nil
}
func (r *reservedQueryReader) finish() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.active--
	if r.closing && r.active == 0 {
		close(r.idle)
	}
}
func quietAcquire(ctx context.Context) context.Context {
	// Admission is measured once around the entire selection, not twice by
	// the nested pool tracer. The actual SQL still uses the original context.
	if f, _ := ctx.Value(observerKey{}).(func(QueryObservation)); f != nil {
		return context.WithValue(ctx, observerKey{}, (func(QueryObservation))(nil))
	}
	return ctx
}
func (r *reservedQueryReader) acquire(ctx context.Context) (result *pgxpool.Conn, release func(), resultErr error) {
	path, probes := "unavailable", 0
	if observe := readObserver(ctx); observe != nil {
		start := time.Now()
		defer func() {
			observe(ReadObservation{Stage: "admission", Elapsed: time.Since(start), Path: path, BorrowProbes: probes, Failed: resultErr != nil})
		}()
	}
	region := trace.StartRegion(ctx, "postgres.apiAdmission")
	defer region.End()
	observed := observationStart(ctx, acquireStartKey{})
	defer observationEnd(observed, acquireStartKey{}, "acquire")
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := r.begin(); err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*pgxpool.Conn, func(), error) { r.finish(); return nil, nil, err }
	// Prefer the guaranteed slot, then borrow otherwise available pool
	// capacity. Retry admission only (never SQL), so nested short queries can
	// progress when background capacity becomes free while the pin is busy.
	for {
		select {
		case <-r.stopped:
			return fail(port.ErrClosed)
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-r.gate:
			path = "pinned"
			return r.pinned(ctx)
		default:
		}
		short, cancel := context.WithTimeout(quietAcquire(ctx), time.Millisecond)
		probes++
		conn, err := r.owner.pool.Acquire(short)
		cancel()
		if err == nil {
			path = "borrowed"
			select {
			case <-r.stopped:
				conn.Release()
				return fail(port.ErrClosed)
			default:
			}
			return conn, func() { conn.Release(); r.finish() }, nil
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			// Failed connection probes must not spin during a DB outage.
			retry := time.NewTimer(50 * time.Millisecond)
			select {
			case <-r.stopped:
				retry.Stop()
				return fail(port.ErrClosed)
			case <-ctx.Done():
				retry.Stop()
				return fail(ctx.Err())
			case <-r.gate:
				retry.Stop()
				path = "pinned"
				return r.pinned(ctx)
			case <-retry.C:
			}
		}
	}
}

func (r *reservedQueryReader) pinned(ctx context.Context) (*pgxpool.Conn, func(), error) {
	release := func() { r.gate <- struct{}{}; r.finish() }
	select {
	case <-r.stopped:
		release()
		return nil, nil, port.ErrClosed
	default:
	}
	if r.conn == nil || r.conn.Conn().IsClosed() {
		if r.conn != nil {
			r.conn.Release()
			r.conn = nil
		}
		conn, err := r.owner.pool.Acquire(quietAcquire(ctx))
		if err != nil {
			release()
			return nil, nil, err
		}
		r.conn = conn
	}
	return r.conn, release, nil
}
func (r *reservedQueryReader) close() {
	r.stateMu.Lock()
	if !r.closing {
		r.closing = true
		close(r.stopped)
		if r.active == 0 {
			close(r.idle)
		}
	}
	idle := r.idle
	r.stateMu.Unlock()
	<-idle
	<-r.gate
	if r.conn != nil {
		r.conn.Release()
		r.conn = nil
	}
	r.gate <- struct{}{}
}
func (r *reservedQueryReader) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	conn, release, err := r.acquire(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer release()
	queryCtx, io := watchQueryIO(ctx, conn)
	defer io.finish()
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := conn.Exec(queryCtx, sql, args...)
	return tag, io.err(err)
}
func (r *reservedQueryReader) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	conn, release, err := r.acquire(ctx)
	if err != nil {
		return nil, err
	}
	queryCtx, io := watchQueryIO(ctx, conn)
	if err := ctx.Err(); err != nil {
		io.finish()
		release()
		return nil, err
	}
	rows, err := conn.Query(queryCtx, sql, args...)
	if err != nil {
		io.finish()
		release()
		return nil, io.err(err)
	}
	return &queryReaderRows{Rows: rows, release: release, io: io}, nil
}
func (r *reservedQueryReader) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	conn, release, err := r.acquire(ctx)
	if err != nil {
		return queryReaderError{err}
	}
	queryCtx, io := watchQueryIO(ctx, conn)
	if err := ctx.Err(); err != nil {
		io.finish()
		release()
		return queryReaderError{err}
	}
	return &queryReaderRow{Row: conn.QueryRow(queryCtx, sql, args...), release: release, io: io}
}

type queryReaderError struct{ err error }

func (r queryReaderError) Scan(...any) error { return r.err }

type queryReaderRow struct {
	pgx.Row
	release func()
	once    sync.Once
	io      queryIO
}

func (r *queryReaderRow) Scan(dest ...any) error {
	defer r.once.Do(func() { r.io.finish(); r.release() })
	return r.io.err(r.Row.Scan(dest...))
}

type queryReaderRows struct {
	pgx.Rows
	release func()
	once    sync.Once
	io      queryIO
}

func (r *queryReaderRows) Close() {
	r.once.Do(func() { r.Rows.Close(); r.io.finish(); r.release() })
}
func (r *queryReaderRows) Next() bool {
	if r.Rows.Next() {
		return true
	}
	r.Close()
	return false
}

func (r *queryReaderRows) Err() error { return r.io.err(r.Rows.Err()) }

func (r *queryReaderRows) Scan(dest ...any) error { return r.io.err(r.Rows.Scan(dest...)) }

func (r *queryReaderRows) Values() ([]any, error) {
	values, err := r.Rows.Values()
	return values, r.io.err(err)
}
