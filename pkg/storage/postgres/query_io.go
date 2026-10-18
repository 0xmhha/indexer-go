package postgres

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryIO owns cancellation until the rows close, before the connection is
// returned. pgx 5.7's watcher starts a goroutine and rendezvous on every query,
// including successful short reads. AfterFunc runs only on cancellation.
// The caller must own the connection until finish, including row consumption.
// This also permits transaction-local queries to avoid the rendezvous without
// changing transaction ownership, commit contexts, or the caller's fresh check.
type queryIO struct {
	ctx    context.Context
	finish func()
}

func watchQueryIO(ctx context.Context, conn *pgxpool.Conn) (context.Context, queryIO) {
	return watchConnectionIO(ctx, conn.Conn().PgConn())
}

func watchConnectionIO(ctx context.Context, conn *pgconn.PgConn) (context.Context, queryIO) {
	if ctx.Done() == nil {
		return ctx, queryIO{ctx: ctx, finish: func() {}}
	}
	handler := &pgconn.DeadlineContextWatcherHandler{Conn: conn.Conn()}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		handler.HandleCancel(ctx)
		close(done)
	})
	finish := sync.OnceFunc(func() {
		if !stop() {
			<-done
			handler.HandleUnwatchAfterCancel()
		}
	})
	// Values (including query diagnostics) survive. The socket watcher above
	// owns cancellation; do not also start pgx's per-query watcher.
	return context.WithoutCancel(ctx), queryIO{ctx: ctx, finish: finish}
}

func (q queryIO) err(err error) error {
	// The socket deadline produces the same timeout pgx normally normalizes
	// using its context. Preserve SQL/Scan/no-row errors and successful reads,
	// even if the request is canceled just after they finish.
	if err != nil && pgconn.Timeout(err) && q.ctx.Err() != nil {
		return errors.Join(err, q.ctx.Err())
	}
	return err
}
