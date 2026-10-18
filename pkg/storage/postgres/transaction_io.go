package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// transactionQuery owns only statement I/O on an already-owned transaction.
// The original context still drives scope/cancellation/fresh checks. Only the
// driver's per-statement watcher is replaced; BEGIN/COMMIT/ROLLBACK, SendBatch,
// pool admission and transaction/generation fences keep their existing paths.
// A row/rows must be consumed or closed before another transaction operation.
type transactionQuery struct{ tx pgx.Tx }

func (q transactionQuery) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	queryCtx, io := watchConnectionIO(ctx, q.tx.Conn().PgConn())
	defer io.finish()
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := q.tx.Exec(queryCtx, sql, args...)
	return tag, io.err(err)
}

func (q transactionQuery) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	queryCtx, io := watchConnectionIO(ctx, q.tx.Conn().PgConn())
	if err := ctx.Err(); err != nil {
		io.finish()
		return nil, err
	}
	rows, err := q.tx.Query(queryCtx, sql, args...)
	if err != nil {
		io.finish()
		return nil, io.err(err)
	}
	return &queryReaderRows{Rows: rows, release: func() {}, io: io}, nil
}

func (q transactionQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	queryCtx, io := watchConnectionIO(ctx, q.tx.Conn().PgConn())
	if err := ctx.Err(); err != nil {
		io.finish()
		return queryReaderError{err}
	}
	return &queryReaderRow{Row: q.tx.QueryRow(queryCtx, sql, args...), release: func() {}, io: io}
}
