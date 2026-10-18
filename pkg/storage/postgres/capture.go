package postgres

import (
	"bytes"
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type captureWriteMode uint8

const (
	captureExclusive captureWriteMode = iota
	captureAppend
	captureReceipt
)

func (s *Store) CaptureUpdate(ctx context.Context, chain model.FeedChain, fn func(port.CaptureWriter) error) error {
	return s.captureWrite(ctx, chain, fn, captureExclusive)
}

func (s *Store) CaptureAppend(ctx context.Context, chain model.FeedChain, fn func(port.CaptureWriter) error) error {
	return s.captureWrite(ctx, chain, fn, captureAppend)
}

func (s *Store) CaptureReceipt(ctx context.Context, chain model.FeedChain, fn func(port.CaptureWriter) error) error {
	return s.captureWrite(ctx, chain, fn, captureReceipt)
}

func (s *Store) captureWrite(ctx context.Context, chain model.FeedChain, fn func(port.CaptureWriter) error, mode captureWriteMode) error {
	if err := chain.Validate(); err != nil {
		return err
	}
	if s.boundTx(ctx) != nil {
		return port.ErrInvalidData
	}
	if err := s.write(); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := transactionQuery{tx}
		// Capture success is a durability ACK, even if the DSN disabled
		// synchronous_commit. A server with fsync off cannot fulfill it.
		var fsync string
		if err := q.QueryRow(ctx, "SHOW fsync").Scan(&fsync); err != nil {
			return err
		}
		if fsync != "on" {
			return errors.New("capture: PostgreSQL fsync must be on")
		}
		if _, err := q.Exec(ctx, "SET LOCAL synchronous_commit = on"); err != nil {
			return err
		}
		id := captureChain(chain)
		lock := "SELECT chain FROM capture_locks WHERE chain = $1 FOR UPDATE"
		switch mode {
		case captureExclusive:
			if _, err := q.Exec(ctx, "INSERT INTO capture_locks (chain) VALUES ($1) ON CONFLICT DO NOTHING", id); err != nil {
				return err
			}
		case captureAppend:
			// Serialize appends but permit KEY SHARE holders to validate
			// already published facts. They cannot observe partial new facts.
			lock = "SELECT chain FROM capture_locks WHERE chain = $1 FOR NO KEY UPDATE"
		case captureReceipt:
			lock = "SELECT chain FROM capture_locks WHERE chain = $1 FOR KEY SHARE"
		default:
			return port.ErrInvalidData
		}
		if err := q.QueryRow(ctx, lock, id).Scan(&id); err != nil {
			return notFound(err)
		}
		t := &captureKV{ctx: ctx, q: q, chain: id, active: true, writable: true, mode: mode}
		defer func() { t.active = false }()
		if err := fn(t); err != nil {
			return err
		}
		return errors.Join(t.err, ctx.Err())
	})
}

func (s *Store) CaptureView(ctx context.Context, chain model.FeedChain, fn func(port.CaptureReader) error) error {
	return s.captureView(ctx, chain, fn, false)
}

func (s *Store) CaptureProjectionView(ctx context.Context, chain model.FeedChain, fn func(port.CaptureReader) error) error {
	return s.captureView(ctx, chain, fn, true)
}

func (s *Store) captureView(ctx context.Context, chain model.FeedChain, fn func(port.CaptureReader) error, prefixOnly bool) error {
	if err := chain.Validate(); err != nil {
		return err
	}
	if bound := s.boundTx(ctx); bound != nil {
		// A writer may validate its facts using this same Store, even with a
		// one-connection pool. Reuse that transaction instead of nesting one.
		// Generic views freeze the entire namespace, including append state.
		// Prefix-only views exclude CaptureUpdate with KEY SHARE through
		// projection commit, but permit immutable future inputs to be added.
		q := bound.q()
		var id []byte
		lock := "SELECT chain FROM capture_locks WHERE chain = $1 FOR UPDATE"
		if prefixOnly {
			lock = "SELECT chain FROM capture_locks WHERE chain = $1 FOR KEY SHARE"
		}
		if err := q.QueryRow(ctx, lock, captureChain(chain)).Scan(&id); err != nil {
			return notFound(err)
		}
		t := &captureKV{ctx: ctx, q: q, chain: id, active: true}
		defer func() { t.active = false }()
		if err := fn(t); err != nil {
			return err
		}
		return errors.Join(t.err, ctx.Err())
	}
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		t := &captureKV{ctx: ctx, q: transactionQuery{tx}, chain: captureChain(chain), active: true}
		defer func() { t.active = false }()
		if err := fn(t); err != nil {
			return err
		}
		return errors.Join(t.err, ctx.Err())
	})
}

func captureChain(c model.FeedChain) []byte { return []byte(c.ID + "/" + c.GenesisHash.Hex()) }

type captureKV struct {
	ctx              context.Context
	q                querier
	chain            []byte
	active, writable bool
	mode             captureWriteMode
	used             int
	err              error
}

func (t *captureKV) checkScope() error {
	if !t.active {
		return port.ErrClosed
	}
	if t.err != nil {
		return t.err
	}
	if err := t.ctx.Err(); err != nil {
		t.err = err
		return err
	}
	return nil
}

func (t *captureKV) check(k []byte) error {
	if err := t.checkScope(); err != nil {
		return err
	}
	if len(k) == 0 || len(k) > port.CaptureKeyLimit {
		t.err = port.ErrInvalidData
	}
	return t.err
}

// GetMany keeps the chain predicate, snapshot and parameter-binding contract
// of Get. CASE prevents an oversized DB value from being sent to the client.
func (t *captureKV) GetMany(keys [][]byte) (map[string][]byte, error) {
	if err := t.checkScope(); err != nil {
		return nil, err
	}
	if len(keys) > port.CaptureBatchKeyLimit {
		t.err = port.ErrInvalidData
		return nil, t.err
	}
	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		if err := t.check(k); err != nil {
			return nil, err
		}
		wanted[string(k)] = true
	}
	result := make(map[string][]byte, len(wanted))
	if len(keys) == 0 {
		return result, nil
	}
	rows, err := t.q.Query(t.ctx, "SELECT key, CASE WHEN octet_length(value) <= $3 THEN value ELSE NULL END FROM capture_records WHERE chain = $1 AND key = ANY($2::bytea[])", t.chain, keys, port.CaptureBatchValueLimit)
	if err != nil {
		t.err = err
		return nil, err
	}
	defer rows.Close()
	used := 0
	for rows.Next() {
		var key, value []byte
		if err = rows.Scan(&key, &value); err != nil {
			t.err = err
			return nil, err
		}
		_, duplicate := result[string(key)]
		used += len(key) + len(value)
		if !wanted[string(key)] || duplicate || value == nil || len(value) > port.CaptureBatchValueLimit || used > port.CaptureBatchBytesLimit {
			t.err = port.ErrInvalidData
			return nil, t.err
		}
		result[string(key)] = value
	}
	if err = rows.Err(); err != nil {
		t.err = err
		return nil, err
	}
	return result, nil
}

func (t *captureKV) Get(k []byte) ([]byte, error) {
	if err := t.check(k); err != nil {
		return nil, err
	}
	var v []byte
	err := t.q.QueryRow(t.ctx, "SELECT value FROM capture_records WHERE chain = $1 AND key = $2", t.chain, k).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		t.err = err
		return nil, err
	}
	if len(v) > port.CaptureValueLimit {
		t.err = port.ErrInvalidData
		return nil, t.err
	}
	return v, nil
}

func (t *captureKV) Put(k, v []byte) error {
	if err := t.check(k); err != nil {
		return err
	}
	if !t.writable {
		t.err = port.ErrReadOnly
		return t.err
	}
	if len(v) > port.CaptureValueLimit || t.used+len(k)+len(v) > port.CaptureTransactionLimit {
		t.err = port.ErrInvalidData
		return t.err
	}
	t.used += len(k) + len(v)
	if v == nil {
		v = []byte{}
	}
	if t.mode != captureExclusive {
		allowed := bytes.HasPrefix(k, []byte("projection/"))
		mutable := false
		if t.mode == captureAppend {
			// Commit verifies next-height/parent/generation before writes. A
			// reset can leave an old fork pointer ABOVE the current Through;
			// publishing its replacement must be able to replace that pointer.
			mutable = bytes.Equal(k, []byte("state")) || bytes.HasPrefix(k, []byte("canonical/"))
			allowed = mutable || bytes.HasPrefix(k, []byte("facts/")) || bytes.HasPrefix(k, []byte("events/")) || bytes.HasPrefix(k, []byte("objects/"))
		}
		if !allowed {
			t.err = port.ErrInvalidData
			return t.err
		}
		if !mutable {
			// Immutable publication must not overwrite bytes, even when two
			// receipt callbacks both observed an absent key concurrently.
			tag, err := t.q.Exec(t.ctx, "INSERT INTO capture_records (chain, key, value) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", t.chain, k, v)
			t.err = err
			if err == nil && tag.RowsAffected() == 0 {
				var prior []byte
				prior, t.err = t.Get(k)
				if t.err == nil && !bytes.Equal(prior, v) {
					t.err = port.ErrInvalidData
				}
			}
			return t.err
		}
	}
	_, t.err = t.q.Exec(t.ctx, "INSERT INTO capture_records (chain, key, value) VALUES ($1, $2, $3) ON CONFLICT (chain, key) DO UPDATE SET value = EXCLUDED.value", t.chain, k, v)
	return t.err
}

var _ port.CaptureDatabase = (*Store)(nil)
var _ port.CaptureAppender = (*Store)(nil)
var _ port.CaptureReceipter = (*Store)(nil)
var _ port.CaptureProjectionViewer = (*Store)(nil)
