package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.KV                = (*Store)(nil)
	_ port.FeatureStateStore = (*Store)(nil)
)

// The kv table keeps the data of port.KV owners (chain packages,
// notifications) under their own key prefixes. bytea compares bytewise, so
// key order matches the Pebble store's.

// kvBatch is how many rows scans and cursors read at a time. Rows are read
// in full batches, so callbacks may use the store (and the block
// transaction) while a scan is in progress.
const kvBatch = 256

// Put implements port.KVStore.
func (s *Store) Put(ctx context.Context, key, value []byte) error {
	if err := s.writeKey(key); err != nil {
		return err
	}
	if value == nil {
		value = []byte{}
	}
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO kv (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// Get implements port.KVStore.
func (s *Store) Get(ctx context.Context, key []byte) ([]byte, error) {
	var v []byte
	if err := s.q(ctx).QueryRow(ctx, "SELECT value FROM kv WHERE key = $1", key).Scan(&v); err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

// Delete implements port.KVStore.
func (s *Store) Delete(ctx context.Context, key []byte) error {
	if err := s.writeKey(key); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, "DELETE FROM kv WHERE key = $1", key)
	return err
}

// Has implements port.KVStore.
func (s *Store) Has(ctx context.Context, key []byte) (bool, error) {
	return s.exists(ctx, "SELECT EXISTS (SELECT 1 FROM kv WHERE key = $1)", key)
}

// Iterate implements port.KVStore.
func (s *Store) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	return s.Scan(ctx, prefix, prefixEnd(prefix), false, fn)
}

// Scan implements port.KV.
func (s *Store) Scan(ctx context.Context, lower, upper []byte, reverse bool, fn func(key, value []byte) bool) error {
	c := &kvCursor{s: s, ctx: ctx, lower: lower, upper: upper}
	var ok bool
	if reverse {
		ok = c.Last()
	} else {
		ok = c.First()
	}
	for ; ok; ok = c.step(reverse) {
		if !fn(bytes.Clone(c.Key()), bytes.Clone(c.Value())) {
			break
		}
	}
	return c.Error()
}

// NewCursor implements port.KV.
func (s *Store) NewCursor(ctx context.Context, lower, upper []byte) (port.Cursor, error) {
	return &kvCursor{s: s, ctx: ctx, lower: lower, upper: upper}, nil
}

// prefixEnd returns the first key after every key with the prefix, nil if
// there is none.
func prefixEnd(prefix []byte) []byte {
	end := bytes.Clone(prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

type kvPair struct{ key, value []byte }

// kvCursor reads [lower, upper) in batches, forward or backward from the
// current key.
type kvCursor struct {
	s            *Store
	ctx          context.Context
	lower, upper []byte
	buf          []kvPair // in key order
	i            int      // current position in buf; -1 or len(buf) when invalid
	err          error
}

func (c *kvCursor) step(reverse bool) bool {
	if reverse {
		return c.Prev()
	}
	return c.Next()
}

// load reads a batch: the keys after (or before, when backward) the given
// key within the bounds; nil from starts at the bound.
func (c *kvCursor) load(from []byte, backward bool) bool {
	where := "key >= $1"
	args := []any{c.lower}
	if c.lower == nil {
		args[0] = []byte{}
	}
	if c.upper != nil {
		where += " AND key < $2"
		args = append(args, c.upper)
	}
	order := "ASC"
	if from != nil {
		args = append(args, from)
		if backward {
			where += fmt.Sprintf(" AND key < $%d", len(args))
		} else {
			where += fmt.Sprintf(" AND key > $%d", len(args))
		}
	}
	if backward {
		order = "DESC"
	}
	rows, err := c.s.q(c.ctx).Query(c.ctx, fmt.Sprintf("SELECT key, value FROM kv WHERE %s ORDER BY key %s LIMIT %d", where, order, kvBatch), args...)
	if err != nil {
		c.err = err
		c.buf = nil
		return false
	}
	pairs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (kvPair, error) {
		var p kvPair
		err := r.Scan(&p.key, &p.value)
		return p, err
	})
	if err != nil {
		c.err = err
		c.buf = nil
		return false
	}
	if backward {
		for i, j := 0, len(pairs)-1; i < j; i, j = i+1, j-1 {
			pairs[i], pairs[j] = pairs[j], pairs[i]
		}
	}
	c.buf = pairs
	if len(pairs) == 0 {
		c.i = -1
		return false
	}
	if backward {
		c.i = len(pairs) - 1
	} else {
		c.i = 0
	}
	return true
}

func (c *kvCursor) First() bool { return c.load(nil, false) }
func (c *kvCursor) Last() bool  { return c.load(nil, true) }

func (c *kvCursor) Next() bool {
	if !c.Valid() {
		return false
	}
	if c.i+1 < len(c.buf) {
		c.i++
		return true
	}
	return c.load(c.buf[c.i].key, false)
}

func (c *kvCursor) Prev() bool {
	if !c.Valid() {
		return false
	}
	if c.i > 0 {
		c.i--
		return true
	}
	return c.load(c.buf[c.i].key, true)
}

func (c *kvCursor) Valid() bool   { return c.err == nil && c.i >= 0 && c.i < len(c.buf) }
func (c *kvCursor) Key() []byte   { return c.buf[c.i].key }
func (c *kvCursor) Value() []byte { return c.buf[c.i].value }
func (c *kvCursor) Error() error  { return c.err }
func (c *kvCursor) Close() error  { c.buf = nil; return nil }

// FeatureStates implements port.FeatureStateStore.
func (s *Store) FeatureStates(ctx context.Context) (map[string]port.FeatureState, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT name, state FROM feature_states")
	if err != nil {
		return nil, err
	}
	type row struct {
		Name  string
		State []byte
	}
	rs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return nil, err
	}
	out := make(map[string]port.FeatureState, len(rs))
	for _, r := range rs {
		var st port.FeatureState
		if err := json.Unmarshal(r.State, &st); err != nil {
			return nil, fmt.Errorf("feature %s: %w", r.Name, err)
		}
		out[r.Name] = st
	}
	return out, nil
}

// SetFeatureState implements port.FeatureStateStore.
func (s *Store) SetFeatureState(ctx context.Context, name string, st port.FeatureState) error {
	if err := s.write(); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO feature_states (name, state) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET state = EXCLUDED.state`, name, data)
	return err
}
