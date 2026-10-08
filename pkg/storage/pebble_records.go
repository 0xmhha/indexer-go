package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.RecordReader = (*PebbleStorage)(nil)
	_ port.RecordWriter = (*PebbleStorage)(nil)
)

// Record keys (refactoring plan R6-1). Table names are lower-case
// identifiers and key IDs field names joined with ",", so "/" separates
// them; a key's values are hashed into a fixed-length part.
//
//	/rec/<table>/<position>                                 record (JSON)
//	/reckey/<table>/<key id>/<keccak of values><position>   -> position
const (
	prefixRecord    = "/rec/"
	prefixRecordKey = "/reckey/"
)

func init() {
	RegisterKeyspace("records", ChainData, prefixRecord, prefixRecordKey)
}

func recordPrefix(table string) ([]byte, error) {
	if table == "" || strings.Contains(table, "/") {
		return nil, fmt.Errorf("record table %q", table)
	}
	return []byte(prefixRecord + table + "/"), nil
}

// keyValuesHash identifies a key's values; values are joined with a byte
// that formatted values do not contain.
func keyValuesHash(values []string) []byte {
	return crypto.Keccak256([]byte(strings.Join(values, "\x00")))
}

func recordKeyPrefix(table string, key port.RecordKey) ([]byte, error) {
	if table == "" || strings.Contains(table, "/") || key.ID == "" || strings.Contains(key.ID, "/") {
		return nil, fmt.Errorf("record key %q of table %q", key.ID, table)
	}
	return append([]byte(prefixRecordKey+table+"/"+key.ID+"/"), keyValuesHash(key.Values)...), nil
}

// ListRecords implements port.RecordReader.
func (s *PebbleStorage) ListRecords(ctx context.Context, table string, page port.Page) ([]*port.Record, string, error) {
	prefix, err := recordPrefix(table)
	if err != nil {
		return nil, "", err
	}
	return aggPage[port.Record](ctx, s, prefix, prefixUpperBound(prefix), page)
}

// ListRecordsByKey implements port.RecordReader.
func (s *PebbleStorage) ListRecordsByKey(ctx context.Context, table string, key port.RecordKey, page port.Page) ([]*port.Record, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	prefix, err := recordKeyPrefix(table, key)
	if err != nil {
		return nil, "", err
	}
	records, _ := recordPrefix(table)
	limit := min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, limit, nil)
	if err != nil {
		return nil, "", err
	}
	out := make([]*port.Record, 0, len(entries))
	for _, e := range entries {
		r, err := getDexJSON[port.Record](ctx, s, append(append([]byte{}, records...), e.Value...))
		if err != nil {
			return nil, "", fmt.Errorf("record of key entry %q: %w", e.Key, err)
		}
		out = append(out, r)
	}
	return out, next, nil
}

// SaveRecord implements port.RecordWriter.
func (s *PebbleStorage) SaveRecord(ctx context.Context, r *port.Record, keys []port.RecordKey) error {
	prefix, err := recordPrefix(r.Table)
	if err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	position := dexPosition(r.BlockNumber, r.LogIndex)
	entries := [][2][]byte{{append(bytes.Clone(prefix), position...), data}}
	for _, k := range keys {
		kp, err := recordKeyPrefix(r.Table, k)
		if err != nil {
			return err
		}
		entries = append(entries, [2][]byte{append(kp, position...), position})
	}
	return s.putDex(ctx, entries...)
}
