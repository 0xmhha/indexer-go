package multichain

import (
	"sync"

	"go.uber.org/zap"
)

// Registry manages the registration of chain instances.
type Registry struct {
	chains map[string]*ChainInstance
	mu     sync.RWMutex
	logger *zap.Logger
}

// NewRegistry creates a new chain registry.
func NewRegistry(logger *zap.Logger) *Registry {
	return &Registry{
		chains: make(map[string]*ChainInstance),
		logger: logger.Named("registry"),
	}
}

// Register adds a chain instance to the registry.
func (r *Registry) Register(instance *ChainInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.chains[instance.Config.ID]; exists {
		return ErrChainAlreadyExists
	}

	r.chains[instance.Config.ID] = instance
	r.logger.Info("chain registered",
		zap.String("id", instance.Config.ID),
		zap.String("name", instance.Config.Name),
	)

	return nil
}

// Unregister removes a chain instance from the registry.
func (r *Registry) Unregister(chainID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.chains[chainID]; !exists {
		return ErrChainNotFound
	}

	delete(r.chains, chainID)
	r.logger.Info("chain unregistered", zap.String("id", chainID))

	return nil
}

// Get returns a chain instance by ID.
func (r *Registry) Get(chainID string) (*ChainInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instance, exists := r.chains[chainID]
	if !exists {
		return nil, ErrChainNotFound
	}

	return instance, nil
}

// List returns all registered chain instances.
func (r *Registry) List() []*ChainInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instances := make([]*ChainInstance, 0, len(r.chains))
	for _, instance := range r.chains {
		instances = append(instances, instance)
	}

	return instances
}

// ListByStatus returns chain instances with the given status.
func (r *Registry) ListByStatus(status ChainStatus) []*ChainInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var instances []*ChainInstance
	for _, instance := range r.chains {
		if instance.Status() == status {
			instances = append(instances, instance)
		}
	}

	return instances
}

// Count returns the number of registered chains.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.chains)
}

// CountByStatus returns the count of chains with the given status.
func (r *Registry) CountByStatus(status ChainStatus) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, instance := range r.chains {
		if instance.Status() == status {
			count++
		}
	}

	return count
}

// Exists checks if a chain is registered.
func (r *Registry) Exists(chainID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.chains[chainID]
	return exists
}

// GetAll returns a map of all chains.
func (r *Registry) GetAll() map[string]*ChainInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*ChainInstance, len(r.chains))
	for k, v := range r.chains {
		result[k] = v
	}

	return result
}
