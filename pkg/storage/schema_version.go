package storage

import (
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
)

// SchemaVersion is the storage layout this build reads and writes.
//
//	1: blocks, transactions and receipts in go-ethereum RLP (no marker key)
//	2: blocks, transactions and receipts in the chain-neutral model encoding
const SchemaVersion uint64 = 2

const keySchemaVersion = "/meta/schema"

// ErrSchemaMismatch means the database was written with another storage
// layout. The data is not migrated in place: index into a new data directory
// and switch over once it has caught up (chain profile design, section 5).
var ErrSchemaMismatch = errors.New("storage: database schema differs from this build; reindex into a new data directory")

// SchemaVersionKey returns the key holding the database's schema version.
func SchemaVersionKey() []byte { return []byte(keySchemaVersion) }

// checkSchema stamps an empty database with SchemaVersion and refuses a
// database written with another version.
func (s *PebbleStorage) checkSchema() error {
	value, closer, err := s.db.Get(SchemaVersionKey())
	if err == nil {
		defer func() { _ = closer.Close() }()
		got, derr := DecodeUint64(value)
		if derr != nil {
			return fmt.Errorf("decode schema version: %w", derr)
		}
		if got != SchemaVersion {
			return fmt.Errorf("%w: database has %d, build uses %d", ErrSchemaMismatch, got, SchemaVersion)
		}
		return nil
	}
	if !errors.Is(err, pebble.ErrNotFound) {
		return fmt.Errorf("read schema version: %w", err)
	}

	empty, err := s.isEmpty()
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("%w: database has 1 (no schema marker), build uses %d", ErrSchemaMismatch, SchemaVersion)
	}
	if s.config.ReadOnly {
		return nil
	}
	if err := s.db.Set(SchemaVersionKey(), EncodeUint64(SchemaVersion), pebble.Sync); err != nil {
		return fmt.Errorf("write schema version: %w", err)
	}
	return nil
}

func (s *PebbleStorage) isEmpty() (bool, error) {
	it, err := s.db.NewIter(nil)
	if err != nil {
		return false, fmt.Errorf("open iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	return !it.First(), nil
}
