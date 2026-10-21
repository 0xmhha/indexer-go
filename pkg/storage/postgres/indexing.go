package postgres

import (
	"context"
	"github.com/0xmhha/indexer-go/internal/progress"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ReadIndexingPage(ctx context.Context, after string, limit int) (out port.IndexingPage, err error) {
	if s.boundTx(ctx) != nil {
		return out, port.ErrInvalidData
	}
	if limit < 1 || limit > 100 || (after != "" && !progress.ValidName(after)) {
		return out, port.ErrInvalidData
	}
	err = pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		get := func(k []byte) ([]byte, error) {
			var b []byte
			e := tx.QueryRow(ctx, "SELECT value FROM kv WHERE key = $1", k).Scan(&b)
			return b, notFound(e)
		}
		scan := func(after string, n int) ([][2][]byte, error) {
			rows, e := tx.Query(ctx, "SELECT key,value FROM kv WHERE key >= $1 AND key < $2 AND key > $3 ORDER BY key LIMIT $4", []byte(progress.StatePrefix), prefixEnd([]byte(progress.StatePrefix)), []byte(progress.StatePrefix+after), n)
			if e != nil {
				return nil, e
			}
			defer rows.Close()
			out := make([][2][]byte, 0, n)
			for rows.Next() {
				var p [2][]byte
				if e := rows.Scan(&p[0], &p[1]); e != nil {
					return nil, e
				}
				if len(p[0]) > len(progress.StatePrefix)+256 || len(p[1]) > progress.MaxValue {
					return nil, port.ErrInvalidData
				}
				out = append(out, p)
			}
			return out, rows.Err()
		}
		var e error
		out, e = progress.ReadPage(get, scan, after, limit)
		return e
	})
	return out, err
}
