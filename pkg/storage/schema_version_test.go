package storage

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/require"
)

func TestSchemaVersion(t *testing.T) {
	t.Run("new database is stamped and reopens", func(t *testing.T) {
		dir := t.TempDir()
		s, err := NewPebbleStorage(DefaultConfig(dir))
		require.NoError(t, err)
		v, closer, err := s.db.Get(SchemaVersionKey())
		require.NoError(t, err)
		got, err := DecodeUint64(v)
		require.NoError(t, err)
		_ = closer.Close()
		require.Equal(t, SchemaVersion, got)
		require.NoError(t, s.Close())

		s, err = NewPebbleStorage(DefaultConfig(dir))
		require.NoError(t, err)
		require.NoError(t, s.Close())
	})

	writeRaw := func(t *testing.T, dir string, key, value []byte) {
		t.Helper()
		db, err := pebble.Open(dir, &pebble.Options{})
		require.NoError(t, err)
		require.NoError(t, db.Set(key, value, pebble.Sync))
		require.NoError(t, db.Close())
	}

	t.Run("data without a marker is schema 1", func(t *testing.T) {
		dir := t.TempDir()
		writeRaw(t, dir, LatestHeightKey(), EncodeUint64(10))
		_, err := NewPebbleStorage(DefaultConfig(dir))
		require.ErrorIs(t, err, ErrSchemaMismatch)
	})

	t.Run("other version is refused", func(t *testing.T) {
		dir := t.TempDir()
		writeRaw(t, dir, SchemaVersionKey(), EncodeUint64(SchemaVersion+1))
		_, err := NewPebbleStorage(DefaultConfig(dir))
		require.ErrorIs(t, err, ErrSchemaMismatch)
	})
}
