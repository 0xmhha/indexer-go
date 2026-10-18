package postgres

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestTransactionQueryIOCancellation(t *testing.T) {
	for _, operation := range []string{"exec", "row", "query", "stream"} {
		t.Run(operation, func(t *testing.T) {
			s := newTestStore(t)
			tx, err := s.pool.Begin(t.Context())
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(context.Background()) }()
			q := transactionQuery{tx}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			switch operation {
			case "exec":
				_, err = q.Exec(ctx, "SELECT pg_sleep(10)")
			case "row":
				var v any
				err = q.QueryRow(ctx, "SELECT pg_sleep(10)").Scan(&v)
			case "query":
				rows, e := q.Query(ctx, "SELECT pg_sleep(10)")
				err = e
				if rows != nil {
					rows.Close()
					if err == nil {
						err = rows.Err()
					}
				}
			case "stream":
				rows, e := q.Query(ctx, "SELECT generate_series(1,1000000)")
				require.NoError(t, e)
				defer rows.Close()
				require.True(t, rows.Next())
				cancel()
				for rows.Next() {
				}
				err = rows.Err()
			}
			if operation == "stream" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			require.Less(t, time.Since(started), 3*time.Second)
			_ = tx.Rollback(context.Background())
			var value int
			require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT 7").Scan(&value))
			require.Equal(t, 7, value)
			require.Eventually(t, func() bool { return s.pool.Stat().AcquiredConns() == 0 }, time.Second, time.Millisecond)
			require.LessOrEqual(t, s.pool.Stat().TotalConns(), int32(4))
		})
	}
}

func TestTransactionQueryIOOwnsOnlyStatement(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := transactionQuery{tx}
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		var v int
		require.NoError(t, q.QueryRow(ctx, "SELECT 7").Scan(&v))
		cancel()
		// The same transaction/socket survives cancellation of a completed
		// statement. No stale callback may poison the next statement.
		require.NoError(t, q.QueryRow(t.Context(), "SELECT 9").Scan(&v))
		require.Equal(t, 9, v)
	}
	ctx, cancel := context.WithCancel(t.Context())
	rows, err := q.Query(ctx, "SELECT 1 WHERE false")
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
	rows.Close()
	cancel()
	var v int
	require.ErrorIs(t, q.QueryRow(t.Context(), "SELECT 1 WHERE false").Scan(&v), pgx.ErrNoRows)
	var queries atomic.Int32
	observed := ObserveQueries(t.Context(), func(o QueryObservation) {
		if o.Stage == "query" {
			queries.Add(1)
		}
	})
	require.NoError(t, q.QueryRow(observed, "SELECT 11").Scan(&v))
	require.Equal(t, int32(1), queries.Load(), "context values survive statement watcher replacement")
	preCanceled, stop := context.WithCancel(observed)
	stop()
	require.ErrorIs(t, q.QueryRow(preCanceled, "SELECT 12").Scan(&v), context.Canceled)
	require.Equal(t, int32(1), queries.Load(), "already canceled queries issue no SQL")
	var sqlError *pgconn.PgError
	require.ErrorAs(t, q.QueryRow(t.Context(), "SELECT missing_column").Scan(&v), &sqlError)
}
