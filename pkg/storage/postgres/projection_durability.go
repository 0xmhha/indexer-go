package postgres

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func (s *Store) RequireBlockDurability(ctx context.Context) error {
	bound := s.boundTx(ctx)
	if bound == nil {
		return port.ErrInvalidData
	}
	q := bound.q()
	var fsync string
	if e := q.QueryRow(ctx, "SHOW fsync").Scan(&fsync); e != nil {
		return e
	}
	if fsync != "on" {
		return errors.New("projection: PostgreSQL fsync must be on")
	}
	_, e := q.Exec(ctx, "SET LOCAL synchronous_commit = on")
	return e
}

var _ port.BlockDurability = (*Store)(nil)
