package postgres

import (
	"context"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.RecordReader = (*Store)(nil)
	_ port.RecordWriter = (*Store)(nil)
)

// recordValuesHash identifies a key's values, as the Pebble store does.
func recordValuesHash(values []string) []byte {
	return crypto.Keccak256([]byte(strings.Join(values, "\x00")))
}

var recordPositionKeys = []keyCol{{"block_number", kindInt, false}, {"log_index", kindInt, false}}

func recordPosition(r *port.Record) []string {
	return []string{u64s(r.BlockNumber), u64s(uint64(r.LogIndex))}
}

// ListRecords implements port.RecordReader.
func (s *Store) ListRecords(ctx context.Context, table string, page port.Page) ([]*port.Record, string, error) {
	return listQuery[*port.Record]{
		list:  "records:" + table,
		sql:   "SELECT data FROM records WHERE tbl = $1",
		args:  []any{table},
		keys:  recordPositionKeys,
		scan:  scanJSON[port.Record],
		keyOf: recordPosition,
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// ListRecordsByKey implements port.RecordReader.
func (s *Store) ListRecordsByKey(ctx context.Context, table string, key port.RecordKey, page port.Page) ([]*port.Record, string, error) {
	return listQuery[*port.Record]{
		list: "record-key:" + table + ":" + key.ID,
		sql: `SELECT r.data FROM record_keys k JOIN records r USING (tbl, block_number, log_index)
			WHERE k.tbl = $1 AND k.key_id = $2 AND k.value_hash = $3`,
		args:  []any{table, key.ID, recordValuesHash(key.Values)},
		keys:  []keyCol{{"k.block_number", kindInt, false}, {"k.log_index", kindInt, false}},
		scan:  scanJSON[port.Record],
		keyOf: recordPosition,
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// SaveRecord implements port.RecordWriter.
func (s *Store) SaveRecord(ctx context.Context, r *port.Record, keys []port.RecordKey) error {
	err := s.saveDex(ctx, "record", r, `INSERT INTO records (tbl, block_number, log_index, data) VALUES ($1, $2, $3, $4)
		ON CONFLICT (tbl, block_number, log_index) DO UPDATE SET data = EXCLUDED.data`,
		r.Table, i64(r.BlockNumber), int64(r.LogIndex))
	if err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := s.q(ctx).Exec(ctx, `INSERT INTO record_keys (tbl, key_id, value_hash, block_number, log_index)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
			r.Table, k.ID, recordValuesHash(k.Values), i64(r.BlockNumber), int64(r.LogIndex)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRecords implements port.RecordWriter.
func (s *Store) DeleteRecords(ctx context.Context, table string) error {
	if err := s.write(); err != nil {
		return err
	}
	for _, stmt := range []string{`DELETE FROM record_keys WHERE tbl = $1`, `DELETE FROM records WHERE tbl = $1`} {
		if _, err := s.q(ctx).Exec(ctx, stmt, table); err != nil {
			return err
		}
	}
	return nil
}
