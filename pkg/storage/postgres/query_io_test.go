package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestReservedQueryIOCancellation(t *testing.T) {
	for _, operation := range []string{"exec", "row", "query", "stream"} {
		t.Run(operation, func(t *testing.T) {
			s := newTestStore(t)
			lease, err := s.ReserveQueryReader(t.Context())
			require.NoError(t, err)
			defer lease.Close()
			ctx, cancel := context.WithTimeout(lease.Context(t.Context()), 100*time.Millisecond)
			defer cancel()
			q := s.q(ctx)
			started := time.Now()
			switch operation {
			case "exec":
				_, err = q.Exec(ctx, "SELECT pg_sleep(10)")
			case "row":
				var value any
				err = q.QueryRow(ctx, "SELECT pg_sleep(10)").Scan(&value)
			case "query":
				rows, queryErr := q.Query(ctx, "SELECT pg_sleep(10)")
				err = queryErr
				if rows != nil {
					rows.Close()
					if err == nil {
						err = rows.Err()
					}
				}
			case "stream":
				rows, queryErr := q.Query(ctx, "SELECT generate_series(1,1000000)")
				require.NoError(t, queryErr)
				defer rows.Close()
				require.True(t, rows.Next())
				// Cancellation after Query returns must still interrupt row IO.
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
			var value int
			apiCtx := lease.Context(t.Context())
			require.NoError(t, s.q(apiCtx).QueryRow(apiCtx, "SELECT 7").Scan(&value), "canceled socket must recover within the same pool")
			require.Equal(t, 7, value)
			require.Eventually(t, func() bool { return s.pool.Stat().AcquiredConns() == 1 }, time.Second, time.Millisecond,
				"pgx destroys the canceled pin asynchronously; only the replacement reservation remains")
			require.LessOrEqual(t, s.pool.Stat().TotalConns(), int32(4))
		})
	}
}

func TestReservedQueryIOCompletionDoesNotCancelNextRead(t *testing.T) {
	s := newTestStore(t)
	lease, err := s.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	defer lease.Close()
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(lease.Context(t.Context()))
		var value int
		require.NoError(t, s.q(ctx).QueryRow(ctx, "SELECT 7").Scan(&value))
		cancel()
		// Cancellation of completed request must not set the reused socket's
		// deadline; use an independent context on the very same pinned slot.
		next := lease.Context(t.Context())
		require.NoError(t, s.q(next).QueryRow(next, "SELECT 9").Scan(&value))
		require.Equal(t, 9, value)
	}
	ctx, cancel := context.WithCancel(lease.Context(t.Context()))
	rows, err := s.q(ctx).Query(ctx, "SELECT 1 WHERE false")
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
	rows.Close()
	cancel()
	var value int
	apiCtx := lease.Context(t.Context())
	require.ErrorIs(t, s.q(apiCtx).QueryRow(apiCtx, "SELECT 1 WHERE false").Scan(&value), pgx.ErrNoRows)
	err = s.q(apiCtx).QueryRow(apiCtx, "SELECT missing_column").Scan(&value)
	var sqlError *pgconn.PgError
	require.ErrorAs(t, err, &sqlError)
}

func TestReservedQueryIOBorrowedCancellation(t *testing.T) {
	s := newTestStore(t)
	lease, err := s.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	defer lease.Close()
	apiCtx := lease.Context(t.Context())
	pinned, err := s.q(apiCtx).Query(apiCtx, "SELECT 1")
	require.NoError(t, err)
	defer pinned.Close()
	ctx, cancel := context.WithTimeout(apiCtx, 100*time.Millisecond)
	defer cancel()
	var value any
	err = s.q(ctx).QueryRow(ctx, "SELECT pg_sleep(10)").Scan(&value)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	pinned.Close()
	var number int
	require.NoError(t, s.q(apiCtx).QueryRow(apiCtx, "SELECT 9").Scan(&number))
	require.Equal(t, 9, number)
	require.Eventually(t, func() bool { return s.pool.Stat().AcquiredConns() == 1 }, time.Second, time.Millisecond, "pgx destroys a canceled borrowed connection asynchronously")
	require.LessOrEqual(t, s.pool.Stat().TotalConns(), int32(4))
}
