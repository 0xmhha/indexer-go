package storage

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// BackendType identifies the type of storage backend
type BackendType string

const (
	// BackendTypePebble represents PebbleDB backend
	BackendTypePebble BackendType = "pebble"

	// BackendTypeRocksDB represents RocksDB backend (future)
	BackendTypeRocksDB BackendType = "rocksdb"

	// BackendTypePostgres represents PostgreSQL backend (future)
	BackendTypePostgres BackendType = "postgres"

	// BackendTypeMemory represents in-memory backend (for testing)
	BackendTypeMemory BackendType = "memory"
)

// Backend defines the low-level key-value store interface.
// This is the minimal interface that storage backends must implement.
type Backend interface {
	// Get retrieves a value by key
	Get(key []byte) ([]byte, error)

	// Set stores a key-value pair
	Set(key, value []byte) error

	// Delete removes a key
	Delete(key []byte) error

	// Has checks if a key exists
	Has(key []byte) (bool, error)

	// NewIterator creates an iterator for range scans
	NewIterator(start, end []byte) Iterator

	// NewBatch creates a new batch for atomic writes
	NewBatch() BackendBatch

	// Close closes the backend
	Close() error

	// Type returns the backend type
	Type() BackendType
}

// Iterator provides iteration over key-value pairs
type Iterator interface {
	// Valid returns true if the iterator is positioned at a valid item
	Valid() bool

	// Next advances the iterator to the next item
	Next()

	// Key returns the current key
	Key() []byte

	// Value returns the current value
	Value() []byte

	// Close releases iterator resources
	Close() error
}

// BackendBatch provides atomic batch write operations
type BackendBatch interface {
	// Set adds a set operation to the batch
	Set(key, value []byte) error

	// Delete adds a delete operation to the batch
	Delete(key []byte) error

	// Commit writes all batched operations atomically
	Commit() error

	// Reset clears all operations in the batch
	Reset()

	// Count returns the number of operations in the batch
	Count() int

	// Close releases batch resources without committing
	Close() error
}

// BackendConfig holds backend-specific configuration
type BackendConfig struct {
	// Type specifies the backend type
	Type BackendType

	// Path is the database path (for file-based backends)
	Path string

	// ConnectionString for database backends (PostgreSQL, etc.)
	ConnectionString string

	// Cache size in MB
	Cache int

	// MaxOpenFiles for file-based backends
	MaxOpenFiles int

	// WriteBuffer size in MB
	WriteBuffer int

	// ReadOnly opens the backend in read-only mode
	ReadOnly bool

	// Additional backend-specific options
	Options map[string]interface{}
}

// DefaultBackendConfig returns default backend configuration
func DefaultBackendConfig(backendType BackendType, path string) *BackendConfig {
	return &BackendConfig{
		Type:         backendType,
		Path:         path,
		Cache:        128,
		MaxOpenFiles: 1000,
		WriteBuffer:  64,
		ReadOnly:     false,
		Options:      make(map[string]interface{}),
	}
}

// BackendFactory creates a Backend instance
type BackendFactory func(config *BackendConfig, logger *zap.Logger) (Backend, error)

// BackendMetadata contains information about a registered backend
type BackendMetadata struct {
	// Name is the human-readable name
	Name string

	// Description describes the backend
	Description string

	// Version is the backend implementation version
	Version string

	// Features lists supported features
	Features []string
}

// BackendRegistry manages storage backend registrations
type BackendRegistry struct {
	mu        sync.RWMutex
	factories map[BackendType]BackendFactory
	metadata  map[BackendType]*BackendMetadata
}

// global backend registry instance
var (
	globalBackendRegistry     *BackendRegistry
	globalBackendRegistryOnce sync.Once
)

// GlobalBackendRegistry returns the global backend registry instance
func GlobalBackendRegistry() *BackendRegistry {
	globalBackendRegistryOnce.Do(func() {
		globalBackendRegistry = NewBackendRegistry()
	})
	return globalBackendRegistry
}

// NewBackendRegistry creates a new backend registry
func NewBackendRegistry() *BackendRegistry {
	return &BackendRegistry{
		factories: make(map[BackendType]BackendFactory),
		metadata:  make(map[BackendType]*BackendMetadata),
	}
}

// Register adds a backend factory to the registry
func (r *BackendRegistry) Register(backendType BackendType, factory BackendFactory, metadata *BackendMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[backendType]; exists {
		return fmt.Errorf("backend type %s is already registered", backendType)
	}

	r.factories[backendType] = factory
	if metadata != nil {
		r.metadata[backendType] = metadata
	}

	return nil
}

// MustRegister registers a backend factory and panics on error
func (r *BackendRegistry) MustRegister(backendType BackendType, factory BackendFactory, metadata *BackendMetadata) {
	if err := r.Register(backendType, factory, metadata); err != nil {
		panic(fmt.Sprintf("failed to register storage backend: %v", err))
	}
}

// Create creates a new backend instance
func (r *BackendRegistry) Create(config *BackendConfig, logger *zap.Logger) (Backend, error) {
	r.mu.RLock()
	factory, exists := r.factories[config.Type]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown backend type: %s (available: %v)", config.Type, r.SupportedTypes())
	}

	return factory(config, logger)
}

// Has checks if a backend type is registered
func (r *BackendRegistry) Has(backendType BackendType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.factories[backendType]
	return exists
}

// SupportedTypes returns all registered backend types
func (r *BackendRegistry) SupportedTypes() []BackendType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]BackendType, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

// GetMetadata returns metadata for a registered backend type
func (r *BackendRegistry) GetMetadata(backendType BackendType) (*BackendMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	meta, exists := r.metadata[backendType]
	return meta, exists
}

// =============================================================================
// Global convenience functions
// =============================================================================

// RegisterBackend adds a backend factory to the global registry
func RegisterBackend(backendType BackendType, factory BackendFactory, metadata *BackendMetadata) error {
	return GlobalBackendRegistry().Register(backendType, factory, metadata)
}

// MustRegisterBackend registers a backend factory and panics on error
func MustRegisterBackend(backendType BackendType, factory BackendFactory, metadata *BackendMetadata) {
	GlobalBackendRegistry().MustRegister(backendType, factory, metadata)
}

// CreateBackend creates a new backend instance from the global registry
func CreateBackend(config *BackendConfig, logger *zap.Logger) (Backend, error) {
	return GlobalBackendRegistry().Create(config, logger)
}

// HasBackend checks if a backend type is registered
func HasBackend(backendType BackendType) bool {
	return GlobalBackendRegistry().Has(backendType)
}

// SupportedBackends returns all registered backend types
func SupportedBackends() []BackendType {
	return GlobalBackendRegistry().SupportedTypes()
}

// =============================================================================
// Storage Factory using Backend
// =============================================================================

// NewStorageWithBackend creates a Storage instance using the specified backend type.
// This is the recommended way to create storage instances.
func NewStorageWithBackend(ctx context.Context, backendType BackendType, config *Config, logger *zap.Logger) (Storage, error) {
	switch backendType {
	case BackendTypePebble:
		return NewPebbleStorage(config)
	default:
		return nil, fmt.Errorf("unsupported backend type: %s", backendType)
	}
}

// NewStorage creates a Storage instance with the default backend (PebbleDB).
// This maintains backward compatibility.
func NewStorage(config *Config) (Storage, error) {
	return NewPebbleStorage(config)
}
