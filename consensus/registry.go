// Package consensus provides a plugin registry for consensus parsers.
// This allows different consensus mechanisms (WBFT, PoA, Tendermint, etc.)
// to be registered and selected dynamically based on configuration.
package consensus

import (
	"fmt"
	"sync"

	"github.com/0xmhha/indexer-go/types/chain"
	"go.uber.org/zap"
)

// Config holds configuration for consensus parser creation
type Config struct {
	// EpochLength is the number of blocks per epoch
	EpochLength uint64

	// ChainID is the chain identifier
	ChainID uint64

	// Additional consensus-specific settings
	Settings map[string]interface{}
}

// DefaultConfig returns default consensus configuration
func DefaultConfig() *Config {
	return &Config{
		EpochLength: 10,
		Settings:    make(map[string]interface{}),
	}
}

// ParserFactory is a function that creates a ConsensusParser
type ParserFactory func(config *Config, logger *zap.Logger) (chain.ConsensusParser, error)

// Registry manages consensus parser registrations
type Registry struct {
	mu        sync.RWMutex
	factories map[chain.ConsensusType]ParserFactory
	metadata  map[chain.ConsensusType]*ParserMetadata
}

// ParserMetadata contains information about a registered parser
type ParserMetadata struct {
	// Name is the human-readable name of the consensus mechanism
	Name string

	// Description describes the consensus mechanism
	Description string

	// Version is the parser implementation version
	Version string

	// SupportedChainTypes lists chain types this parser supports
	SupportedChainTypes []chain.ChainType
}

// global registry instance
var (
	globalRegistry     *Registry
	globalRegistryOnce sync.Once
)

// Global returns the global registry instance
func Global() *Registry {
	globalRegistryOnce.Do(func() {
		globalRegistry = NewRegistry()
	})
	return globalRegistry
}

// NewRegistry creates a new consensus parser registry
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[chain.ConsensusType]ParserFactory),
		metadata:  make(map[chain.ConsensusType]*ParserMetadata),
	}
}

// Register adds a consensus parser factory to the registry
func (r *Registry) Register(consensusType chain.ConsensusType, factory ParserFactory, metadata *ParserMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[consensusType]; exists {
		return fmt.Errorf("consensus type %s is already registered", consensusType)
	}

	r.factories[consensusType] = factory
	if metadata != nil {
		r.metadata[consensusType] = metadata
	}

	return nil
}

// MustRegister registers a parser factory and panics on error
// This is useful for init() functions
func (r *Registry) MustRegister(consensusType chain.ConsensusType, factory ParserFactory, metadata *ParserMetadata) {
	if err := r.Register(consensusType, factory, metadata); err != nil {
		panic(fmt.Sprintf("failed to register consensus parser: %v", err))
	}
}

// Get creates a new consensus parser instance
func (r *Registry) Get(consensusType chain.ConsensusType, config *Config, logger *zap.Logger) (chain.ConsensusParser, error) {
	r.mu.RLock()
	factory, exists := r.factories[consensusType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown consensus type: %s (available: %v)", consensusType, r.SupportedTypes())
	}

	if config == nil {
		config = DefaultConfig()
	}

	return factory(config, logger)
}

// Has checks if a consensus type is registered
func (r *Registry) Has(consensusType chain.ConsensusType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.factories[consensusType]
	return exists
}

// SupportedTypes returns all registered consensus types
func (r *Registry) SupportedTypes() []chain.ConsensusType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]chain.ConsensusType, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

// GetMetadata returns metadata for a registered consensus type
func (r *Registry) GetMetadata(consensusType chain.ConsensusType) (*ParserMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	meta, exists := r.metadata[consensusType]
	return meta, exists
}

// Unregister removes a consensus parser from the registry
// This is mainly useful for testing
func (r *Registry) Unregister(consensusType chain.ConsensusType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.factories, consensusType)
	delete(r.metadata, consensusType)
}

// Clear removes all registered parsers
// This is mainly useful for testing
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories = make(map[chain.ConsensusType]ParserFactory)
	r.metadata = make(map[chain.ConsensusType]*ParserMetadata)
}

// =============================================================================
// Global convenience functions
// =============================================================================

// Register adds a consensus parser factory to the global registry
func Register(consensusType chain.ConsensusType, factory ParserFactory, metadata *ParserMetadata) error {
	return Global().Register(consensusType, factory, metadata)
}

// MustRegister registers a parser factory to the global registry and panics on error
func MustRegister(consensusType chain.ConsensusType, factory ParserFactory, metadata *ParserMetadata) {
	Global().MustRegister(consensusType, factory, metadata)
}

// Get creates a new consensus parser instance from the global registry
func Get(consensusType chain.ConsensusType, config *Config, logger *zap.Logger) (chain.ConsensusParser, error) {
	return Global().Get(consensusType, config, logger)
}

// Has checks if a consensus type is registered in the global registry
func Has(consensusType chain.ConsensusType) bool {
	return Global().Has(consensusType)
}

// SupportedTypes returns all registered consensus types from the global registry
func SupportedTypes() []chain.ConsensusType {
	return Global().SupportedTypes()
}
