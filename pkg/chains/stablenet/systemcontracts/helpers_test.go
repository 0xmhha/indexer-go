package systemcontracts

import (
	"testing"

	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// testDB is a Pebble storage with the system contract store over it.
type testDB struct {
	*storagepkg.PebbleStorage
	*Store
}

func newTestDB(cfg *storagepkg.Config) (*testDB, error) {
	db, err := storagepkg.NewPebbleStorage(cfg)
	if err != nil {
		return nil, err
	}
	return &testDB{PebbleStorage: db, Store: NewStore(db, zap.NewNop())}, nil
}

func newTestPebble(t *testing.T) *testDB {
	t.Helper()
	db, err := newTestDB(storagepkg.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func setupTestStorage(t *testing.T) (*testDB, func()) {
	t.Helper()
	db, err := newTestDB(storagepkg.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	return db, func() { _ = db.Close() }
}
