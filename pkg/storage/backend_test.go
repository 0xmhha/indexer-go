package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestBackendRegistry_RegisterAndCreate(t *testing.T) {
	registry := NewBackendRegistry()

	// Create a mock factory
	mockFactory := func(config *BackendConfig, logger *zap.Logger) (Backend, error) {
		return &mockBackend{backendType: config.Type}, nil
	}

	// Register the mock backend
	err := registry.Register(BackendTypeMemory, mockFactory, &BackendMetadata{
		Name:        "Mock Memory Backend",
		Description: "A mock in-memory backend for testing",
		Version:     "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to register backend: %v", err)
	}

	// Verify it's registered
	if !registry.Has(BackendTypeMemory) {
		t.Error("Expected memory backend to be registered")
	}

	// Create the backend
	config := &BackendConfig{Type: BackendTypeMemory}
	backend, err := registry.Create(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}

	if backend == nil {
		t.Error("Expected non-nil backend")
	}

	if backend.Type() != BackendTypeMemory {
		t.Errorf("Expected backend type %s, got %s", BackendTypeMemory, backend.Type())
	}
}

func TestBackendRegistry_DuplicateRegistration(t *testing.T) {
	registry := NewBackendRegistry()

	mockFactory := func(config *BackendConfig, logger *zap.Logger) (Backend, error) {
		return &mockBackend{}, nil
	}

	// First registration should succeed
	err := registry.Register(BackendTypeMemory, mockFactory, nil)
	if err != nil {
		t.Fatalf("First registration should succeed: %v", err)
	}

	// Second registration should fail
	err = registry.Register(BackendTypeMemory, mockFactory, nil)
	if err == nil {
		t.Error("Expected error for duplicate registration")
	}
}

func TestBackendRegistry_UnknownType(t *testing.T) {
	registry := NewBackendRegistry()

	config := &BackendConfig{Type: BackendTypeRocksDB}
	_, err := registry.Create(config, zap.NewNop())
	if err == nil {
		t.Error("Expected error for unknown backend type")
	}
}

func TestBackendRegistry_SupportedTypes(t *testing.T) {
	registry := NewBackendRegistry()

	mockFactory := func(config *BackendConfig, logger *zap.Logger) (Backend, error) {
		return &mockBackend{backendType: config.Type}, nil
	}

	// Register multiple types
	registry.MustRegister(BackendTypeMemory, mockFactory, nil)
	registry.MustRegister(BackendTypeRocksDB, mockFactory, nil)

	types := registry.SupportedTypes()
	if len(types) != 2 {
		t.Errorf("Expected 2 supported types, got %d", len(types))
	}
}

func TestPebbleBackend_BasicOperations(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pebble-backend-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &BackendConfig{
		Type:         BackendTypePebble,
		Path:         filepath.Join(tmpDir, "testdb"),
		Cache:        8,
		MaxOpenFiles: 100,
		WriteBuffer:  4,
	}

	backend, err := NewPebbleBackend(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create pebble backend: %v", err)
	}
	defer backend.Close()

	// Test Set and Get
	key := []byte("test-key")
	value := []byte("test-value")

	if err := backend.Set(key, value); err != nil {
		t.Fatalf("Failed to set key: %v", err)
	}

	got, err := backend.Get(key)
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}

	if string(got) != string(value) {
		t.Errorf("Expected value %s, got %s", value, got)
	}

	// Test Has
	exists, err := backend.Has(key)
	if err != nil {
		t.Fatalf("Failed to check key existence: %v", err)
	}
	if !exists {
		t.Error("Expected key to exist")
	}

	// Test Delete
	if err := backend.Delete(key); err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}

	exists, err = backend.Has(key)
	if err != nil {
		t.Fatalf("Failed to check key existence after delete: %v", err)
	}
	if exists {
		t.Error("Expected key to not exist after delete")
	}
}

func TestPebbleBackend_Batch(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pebble-batch-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &BackendConfig{
		Type:         BackendTypePebble,
		Path:         filepath.Join(tmpDir, "testdb"),
		Cache:        8,
		MaxOpenFiles: 100,
		WriteBuffer:  4,
	}

	backend, err := NewPebbleBackend(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create pebble backend: %v", err)
	}
	defer backend.Close()

	// Create batch
	batch := backend.NewBatch()

	// Add operations
	batch.Set([]byte("key1"), []byte("value1"))
	batch.Set([]byte("key2"), []byte("value2"))
	batch.Set([]byte("key3"), []byte("value3"))

	if batch.Count() != 3 {
		t.Errorf("Expected batch count 3, got %d", batch.Count())
	}

	// Commit batch
	if err := batch.Commit(); err != nil {
		t.Fatalf("Failed to commit batch: %v", err)
	}

	// Verify values
	for i := 1; i <= 3; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		expectedValue := []byte(fmt.Sprintf("value%d", i))

		got, err := backend.Get(key)
		if err != nil {
			t.Errorf("Failed to get key%d: %v", i, err)
			continue
		}

		if string(got) != string(expectedValue) {
			t.Errorf("Expected value%d, got %s", i, got)
		}
	}
}

func TestPebbleBackend_Iterator(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pebble-iter-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &BackendConfig{
		Type:         BackendTypePebble,
		Path:         filepath.Join(tmpDir, "testdb"),
		Cache:        8,
		MaxOpenFiles: 100,
		WriteBuffer:  4,
	}

	backend, err := NewPebbleBackend(config, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create pebble backend: %v", err)
	}
	defer backend.Close()

	// Insert test data with common prefix
	prefix := []byte("prefix:")
	for i := 0; i < 5; i++ {
		key := append(prefix, byte('0'+i))
		value := []byte(fmt.Sprintf("value%d", i))
		if err := backend.Set(key, value); err != nil {
			t.Fatalf("Failed to set key: %v", err)
		}
	}

	// Create iterator with prefix range
	endPrefix := []byte("prefix;") // ':' + 1 = ';'
	iter := backend.NewIterator(prefix, endPrefix)
	defer iter.Close()

	count := 0
	for iter.Valid() {
		count++
		iter.Next()
	}

	if count != 5 {
		t.Errorf("Expected 5 items, got %d", count)
	}
}

func TestGlobalBackendRegistry(t *testing.T) {
	// The global registry should have PebbleDB registered via init()
	if !HasBackend(BackendTypePebble) {
		t.Error("Expected PebbleDB to be registered in global registry")
	}

	types := SupportedBackends()
	found := false
	for _, typ := range types {
		if typ == BackendTypePebble {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected PebbleDB in supported backends list")
	}
}

// mockBackend is a mock implementation for testing
type mockBackend struct {
	backendType BackendType
}

func (m *mockBackend) Type() BackendType                      { return m.backendType }
func (m *mockBackend) Get(key []byte) ([]byte, error)         { return nil, ErrNotFound }
func (m *mockBackend) Set(key, value []byte) error            { return nil }
func (m *mockBackend) Delete(key []byte) error                { return nil }
func (m *mockBackend) Has(key []byte) (bool, error)           { return false, nil }
func (m *mockBackend) NewIterator(start, end []byte) Iterator { return &mockIterator{} }
func (m *mockBackend) NewBatch() BackendBatch                 { return &mockBatch{} }
func (m *mockBackend) Close() error                           { return nil }

type mockIterator struct{}

func (m *mockIterator) Valid() bool   { return false }
func (m *mockIterator) Next()         {}
func (m *mockIterator) Key() []byte   { return nil }
func (m *mockIterator) Value() []byte { return nil }
func (m *mockIterator) Close() error  { return nil }

type mockBatch struct{ count int }

func (m *mockBatch) Set(key, value []byte) error { m.count++; return nil }
func (m *mockBatch) Delete(key []byte) error     { m.count++; return nil }
func (m *mockBatch) Commit() error               { return nil }
func (m *mockBatch) Reset()                      { m.count = 0 }
func (m *mockBatch) Count() int                  { return m.count }
func (m *mockBatch) Close() error                { return nil }
