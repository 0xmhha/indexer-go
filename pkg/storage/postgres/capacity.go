package postgres

import (
	"context"
	"fmt"
)

// DatabaseBytes returns the size of the store's database
// (pg_database_size), shared by every schema and process using it. It never
// runs in a block transaction bound to ctx.
func (s *Store) DatabaseBytes(ctx context.Context) (int64, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: database size: %w", err)
	}
	return n, nil
}

// WALBytes returns the size of the server's WAL directory (pg_ls_waldir).
// The role needs pg_monitor or EXECUTE on pg_ls_waldir(); without it the
// error is returned.
func (s *Store) WALBytes(ctx context.Context) (int64, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, "SELECT COALESCE(sum(size), 0)::bigint FROM pg_ls_waldir()").Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: WAL size: %w", err)
	}
	return n, nil
}
