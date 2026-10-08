package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var _ port.Outbox = (*Store)(nil)

// The outbox tables are not tracked for undo (migration 0006): a rollback
// keeps entries that may have been delivered and appends the
// reorganization's entries instead.

// AppendOutbox implements port.Outbox.
func (s *Store) AppendOutbox(ctx context.Context, entries []port.OutboxEntry) error {
	if len(entries) == 0 {
		return nil
	}
	bt := s.boundTx(ctx)
	if bt == nil {
		return errors.New("postgres: outbox entries must be appended in a block transaction")
	}
	last, err := lastOutboxSeq(ctx, bt.tx)
	if err != nil {
		return fmt.Errorf("read outbox sequence: %w", err)
	}
	b := &pgx.Batch{}
	for _, e := range entries {
		last++
		data := e.Data
		if data == nil {
			data = []byte{}
		}
		b.Queue("INSERT INTO outbox (seq, type, data) VALUES ($1, $2, $3)", i64(last), e.Type, data)
	}
	b.Queue(`INSERT INTO outbox_sequence (last) VALUES ($1)
		ON CONFLICT (id) DO UPDATE SET last = EXCLUDED.last`, i64(last))
	return sendBatch(ctx, bt.tx, b)
}

func lastOutboxSeq(ctx context.Context, q querier) (uint64, error) {
	var last int64
	err := q.QueryRow(ctx, "SELECT last FROM outbox_sequence").Scan(&last)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return uint64(last), err
}

// ReadOutbox implements port.Outbox; limit <= 0 reads every entry.
func (s *Store) ReadOutbox(ctx context.Context, after uint64, limit int) ([]port.OutboxEntry, error) {
	if after >= math.MaxInt64 {
		return nil, nil
	}
	sql := "SELECT seq, type, data FROM outbox WHERE seq > $1 ORDER BY seq"
	if limit > 0 {
		sql += " LIMIT " + itoa(limit)
	}
	rows, err := s.q(ctx).Query(ctx, sql, i64(after))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (port.OutboxEntry, error) {
		var (
			e   port.OutboxEntry
			seq int64
		)
		err := row.Scan(&seq, &e.Type, &e.Data)
		e.Seq = uint64(seq)
		return e, err
	})
}

// LastOutboxSeq implements port.Outbox.
func (s *Store) LastOutboxSeq(ctx context.Context) (uint64, error) {
	return lastOutboxSeq(ctx, s.q(ctx))
}

// OutboxCursor implements port.Outbox.
func (s *Store) OutboxCursor(ctx context.Context, name string) (uint64, bool, error) {
	var seq int64
	err := s.q(ctx).QueryRow(ctx, "SELECT seq FROM outbox_cursors WHERE name = $1", name).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return uint64(seq), true, nil
}

// SetOutboxCursor implements port.Outbox.
func (s *Store) SetOutboxCursor(ctx context.Context, name string, seq uint64) error {
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO outbox_cursors (name, seq) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET seq = EXCLUDED.seq`, name, i64(seq))
	return err
}

// PruneOutbox implements port.Outbox.
func (s *Store) PruneOutbox(ctx context.Context, before uint64) error {
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, "DELETE FROM outbox WHERE seq < $1", bigintOf(before))
	return err
}
