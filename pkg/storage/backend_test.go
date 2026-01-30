package storage

import (
	"context"
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

func TestDefaultBackendConfig(t *testing.T) {
	config := DefaultBackendConfig(BackendTypePebble, "/data/pebble")

	if config.Type != BackendTypePebble {
		t.Errorf("expected type %s, got %s", BackendTypePebble, config.Type)
	}
	if config.Path != "/data/pebble" {
		t.Errorf("expected path '/data/pebble', got '%s'", config.Path)
	}
	if config.Cache != 128 {
		t.Errorf("expected cache 128, got %d", config.Cache)
	}
	if config.MaxOpenFiles != 1000 {
		t.Errorf("expected MaxOpenFiles 1000, got %d", config.MaxOpenFiles)
	}
	if config.WriteBuffer != 64 {
		t.Errorf("expected WriteBuffer 64, got %d", config.WriteBuffer)
	}
	if config.ReadOnly {
		t.Error("expected ReadOnly to be false")
	}
	if config.Options == nil {
		t.Error("Options should be initialized")
	}
}

func TestBackendRegistry_GetMetadata(t *testing.T) {
	registry := NewBackendRegistry()

	mockFactory := func(config *BackendConfig, logger *zap.Logger) (Backend, error) {
		return &mockBackend{backendType: config.Type}, nil
	}

	metadata := &BackendMetadata{
		Name:        "Test Backend",
		Description: "A test backend",
		Version:     "1.0.0",
		Features:    []string{"feature1", "feature2"},
	}

	// Register with metadata
	err := registry.Register(BackendTypeMemory, mockFactory, metadata)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Get metadata
	meta, exists := registry.GetMetadata(BackendTypeMemory)
	if !exists {
		t.Error("Expected metadata to exist")
	}
	if meta.Name != "Test Backend" {
		t.Errorf("expected name 'Test Backend', got '%s'", meta.Name)
	}
	if meta.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", meta.Version)
	}
	if len(meta.Features) != 2 {
		t.Errorf("expected 2 features, got %d", len(meta.Features))
	}

	// Get metadata for unregistered type
	_, exists = registry.GetMetadata(BackendTypeRocksDB)
	if exists {
		t.Error("Expected no metadata for unregistered type")
	}
}

func TestRegisterBackend(t *testing.T) {
	// Note: This uses the global registry
	mockFactory := func(config *BackendConfig, logger *zap.Logger) (Backend, error) {
		return &mockBackend{backendType: config.Type}, nil
	}

	// First registration should succeed (or already exist from other tests)
	err := RegisterBackend("test-backend", mockFactory, nil)
	// If it already exists, that's okay - we're just testing the function works
	if err != nil && !HasBackend("test-backend") {
		t.Errorf("RegisterBackend failed: %v", err)
	}
}

func TestCreateBackend(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "create-backend-test")
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

	backend, err := CreateBackend(config, zap.NewNop())
	if err != nil {
		t.Fatalf("CreateBackend failed: %v", err)
	}
	defer backend.Close()

	if backend.Type() != BackendTypePebble {
		t.Errorf("Expected backend type %s, got %s", BackendTypePebble, backend.Type())
	}
}

func TestPebbleBackend_Type(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pebble-type-test")
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

	if backend.Type() != BackendTypePebble {
		t.Errorf("Expected type %s, got %s", BackendTypePebble, backend.Type())
	}
}

func TestPebbleBackend_GetDB(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pebble-getdb-test")
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

	db := backend.GetDB()
	if db == nil {
		t.Error("Expected non-nil database")
	}
}

func TestPebbleIterator_KeyValue(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pebble-keyvalue-test")
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

	// Insert test data
	testKey := []byte("test-key")
	testValue := []byte("test-value")
	if err := backend.Set(testKey, testValue); err != nil {
		t.Fatalf("Failed to set key: %v", err)
	}

	// Create iterator
	iter := backend.NewIterator([]byte("test"), []byte("tesu"))
	defer iter.Close()

	if !iter.Valid() {
		t.Fatal("Expected iterator to be valid")
	}

	// Test Key method
	key := iter.Key()
	if string(key) != "test-key" {
		t.Errorf("Expected key 'test-key', got '%s'", key)
	}

	// Test Value method
	value := iter.Value()
	if string(value) != "test-value" {
		t.Errorf("Expected value 'test-value', got '%s'", value)
	}
}

func TestPebbleBatch_DeleteResetClose(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pebble-batch-ops-test")
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

	// First, set up some data
	if err := backend.Set([]byte("key1"), []byte("value1")); err != nil {
		t.Fatalf("Failed to set key: %v", err)
	}

	// Create batch with Delete operation
	batch := backend.NewBatch()

	// Test Delete in batch
	if err := batch.Delete([]byte("key1")); err != nil {
		t.Fatalf("batch Delete failed: %v", err)
	}

	if batch.Count() != 1 {
		t.Errorf("Expected batch count 1 after Delete, got %d", batch.Count())
	}

	// Test Reset
	batch.Reset()
	if batch.Count() != 0 {
		t.Errorf("Expected batch count 0 after Reset, got %d", batch.Count())
	}

	// Add some operations and test Close without Commit
	batch.Set([]byte("key2"), []byte("value2"))
	if err := batch.Close(); err != nil {
		t.Errorf("batch Close failed: %v", err)
	}

	// Verify key2 was not committed (batch was closed without commit)
	exists, err := backend.Has([]byte("key2"))
	if err != nil {
		t.Fatalf("Has failed: %v", err)
	}
	if exists {
		t.Error("Expected key2 to not exist (batch was closed without commit)")
	}

	// Verify key1 still exists (delete was reset)
	exists, err = backend.Has([]byte("key1"))
	if err != nil {
		t.Fatalf("Has failed: %v", err)
	}
	if !exists {
		t.Error("Expected key1 to still exist (delete was reset)")
	}
}

func TestMustRegister_Panic(t *testing.T) {
	registry := NewBackendRegistry()

	mockFactory := func(config *BackendConfig, logger *zap.Logger) (Backend, error) {
		return &mockBackend{}, nil
	}

	// First registration should succeed
	registry.MustRegister(BackendTypeMemory, mockFactory, nil)

	// Second registration should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for duplicate MustRegister")
		}
	}()

	registry.MustRegister(BackendTypeMemory, mockFactory, nil)
}

func TestNewStorageWithBackend_Pebble(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "storage-with-backend-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultConfig(filepath.Join(tmpDir, "testdb"))
	ctx := context.Background()

	storage, err := NewStorageWithBackend(ctx, BackendTypePebble, config, zap.NewNop())
	if err != nil {
		t.Fatalf("NewStorageWithBackend failed: %v", err)
	}
	defer storage.Close()

	// Verify storage works
	if storage == nil {
		t.Fatal("Expected non-nil storage")
	}
}

func TestNewStorageWithBackend_UnsupportedType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-unsupported-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultConfig(filepath.Join(tmpDir, "testdb"))
	ctx := context.Background()

	_, err = NewStorageWithBackend(ctx, "unsupported", config, zap.NewNop())
	if err == nil {
		t.Error("Expected error for unsupported backend type")
	}
}

func TestNewStorage(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "new-storage-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultConfig(filepath.Join(tmpDir, "testdb"))

	storage, err := NewStorage(config)
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer storage.Close()

	// Verify storage works
	if storage == nil {
		t.Fatal("Expected non-nil storage")
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
