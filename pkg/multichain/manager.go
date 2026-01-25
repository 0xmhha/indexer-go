package multichain

import (
	"context"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"go.uber.org/zap"
)

// Manager is the main entry point for multi-chain management.
// It coordinates multiple chain instances, handling their lifecycle and health monitoring.
type Manager struct {
	config        *ManagerConfig
	registry      *Registry
	healthChecker *HealthChecker
	storage       storage.Storage
	eventBus      *events.EventBus
	logger        *zap.Logger

	ctx        context.Context
	cancelFunc context.CancelFunc
	runningWg  sync.WaitGroup
	mu         sync.RWMutex

	isRunning bool
}

// NewManager creates a new multi-chain manager.
func NewManager(
	config *ManagerConfig,
	globalStorage storage.Storage,
	globalEventBus *events.EventBus,
	logger *zap.Logger,
) (*Manager, error) {
	if config == nil {
		config = DefaultManagerConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	m := &Manager{
		config:   config,
		registry: NewRegistry(logger),
		storage:  globalStorage,
		eventBus: globalEventBus,
		logger:   logger.Named("multichain"),
	}

	// Create health checker
	m.healthChecker = NewHealthChecker(m, config.HealthCheckInterval, logger)

	return m, nil
}

// Start initializes and starts all enabled chains.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return nil
	}
	m.ctx, m.cancelFunc = context.WithCancel(ctx)
	m.isRunning = true
	m.mu.Unlock()

	m.logger.Info("starting multi-chain manager",
		zap.Int("chainCount", len(m.config.Chains)),
	)

	// Register and start all enabled chains
	for _, chainCfg := range m.config.GetEnabledChains() {
		cfg := chainCfg // Create copy for closure
		if _, err := m.RegisterChain(m.ctx, &cfg); err != nil {
			m.logger.Error("failed to register chain",
				zap.String("chainId", cfg.ID),
				zap.Error(err),
			)
			continue
		}

		if err := m.StartChain(m.ctx, cfg.ID); err != nil {
			m.logger.Error("failed to start chain",
				zap.String("chainId", cfg.ID),
				zap.Error(err),
			)
		}
	}

	// Start health checker
	m.healthChecker.Start(m.ctx)

	// Start auto-restart monitor if enabled
	if m.config.AutoRestart {
		m.runningWg.Add(1)
		go m.autoRestartMonitor()
	}

	m.logger.Info("multi-chain manager started",
		zap.Int("activeChains", m.registry.CountByStatus(StatusActive)+m.registry.CountByStatus(StatusSyncing)),
	)

	return nil
}

// Stop gracefully stops all chains and the manager.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.isRunning {
		m.mu.Unlock()
		return nil
	}
	m.isRunning = false
	m.mu.Unlock()

	m.logger.Info("stopping multi-chain manager")

	// Stop health checker first
	m.healthChecker.Stop()

	// Cancel context to stop auto-restart monitor
	if m.cancelFunc != nil {
		m.cancelFunc()
	}

	// Stop all chains
	for _, instance := range m.registry.List() {
		if err := instance.Stop(ctx); err != nil {
			m.logger.Error("error stopping chain",
				zap.String("chainId", instance.Config.ID),
				zap.Error(err),
			)
		}
	}

	// Wait for background goroutines
	done := make(chan struct{})
	go func() {
		m.runningWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("multi-chain manager stopped gracefully")
	case <-ctx.Done():
		m.logger.Warn("multi-chain manager stop timed out")
	}

	return nil
}

// RegisterChain registers a new chain with the manager.
func (m *Manager) RegisterChain(ctx context.Context, config *ChainConfig) (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}

	if m.registry.Exists(config.ID) {
		return "", ErrChainAlreadyExists
	}

	instance := NewChainInstance(config, m.storage, m.eventBus, m.logger)
	if err := m.registry.Register(instance); err != nil {
		return "", err
	}

	m.logger.Info("chain registered",
		zap.String("chainId", config.ID),
		zap.String("name", config.Name),
	)

	return config.ID, nil
}

// UnregisterChain removes a chain from the manager.
// The chain must be stopped before unregistering.
func (m *Manager) UnregisterChain(ctx context.Context, chainID string) error {
	instance, err := m.registry.Get(chainID)
	if err != nil {
		return err
	}

	// Ensure chain is stopped
	if instance.Status() != StatusStopped && instance.Status() != StatusRegistered {
		if err := instance.Stop(ctx); err != nil {
			return err
		}
	}

	return m.registry.Unregister(chainID)
}

// StartChain starts a specific chain.
func (m *Manager) StartChain(ctx context.Context, chainID string) error {
	instance, err := m.registry.Get(chainID)
	if err != nil {
		return err
	}

	return instance.Start(ctx)
}

// StopChain stops a specific chain.
func (m *Manager) StopChain(ctx context.Context, chainID string) error {
	instance, err := m.registry.Get(chainID)
	if err != nil {
		return err
	}

	return instance.Stop(ctx)
}

// GetChain returns a chain instance by ID.
func (m *Manager) GetChain(chainID string) (*ChainInstance, error) {
	return m.registry.Get(chainID)
}

// ListChains returns status information for all chains.
func (m *Manager) ListChains() []*ChainInfo {
	instances := m.registry.List()
	infos := make([]*ChainInfo, 0, len(instances))
	for _, instance := range instances {
		infos = append(infos, instance.Info())
	}
	return infos
}

// HealthCheck returns health status for all chains.
func (m *Manager) HealthCheck(ctx context.Context) map[string]*HealthStatus {
	instances := m.registry.List()
	statuses := make(map[string]*HealthStatus, len(instances))

	for _, instance := range instances {
		statuses[instance.Config.ID] = instance.HealthCheck(ctx)
	}

	return statuses
}

// GetMetrics returns metrics for all chains.
func (m *Manager) GetMetrics() map[string]*ChainMetrics {
	instances := m.registry.List()
	metrics := make(map[string]*ChainMetrics, len(instances))

	for _, instance := range instances {
		metrics[instance.Config.ID] = instance.GetMetrics()
	}

	return metrics
}

// ChainCount returns the number of registered chains.
func (m *Manager) ChainCount() int {
	return m.registry.Count()
}

// ActiveChainCount returns the number of active/syncing chains.
func (m *Manager) ActiveChainCount() int {
	return m.registry.CountByStatus(StatusActive) + m.registry.CountByStatus(StatusSyncing)
}

// autoRestartMonitor monitors chains and restarts failed ones.
func (m *Manager) autoRestartMonitor() {
	defer m.runningWg.Done()

	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkAndRestartFailedChains()
		}
	}
}

// checkAndRestartFailedChains restarts chains that have failed.
func (m *Manager) checkAndRestartFailedChains() {
	errorChains := m.registry.ListByStatus(StatusError)

	for _, instance := range errorChains {
		// Wait for restart delay
		if instance.lastErrorAt != nil {
			elapsed := time.Since(*instance.lastErrorAt)
			if elapsed < m.config.AutoRestartDelay {
				continue
			}
		}

		m.logger.Info("auto-restarting failed chain",
			zap.String("chainId", instance.Config.ID),
		)

		// Stop and restart
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		_ = instance.Stop(ctx)
		cancel()

		ctx, cancel = context.WithTimeout(m.ctx, 60*time.Second)
		if err := instance.Start(ctx); err != nil {
			m.logger.Error("failed to auto-restart chain",
				zap.String("chainId", instance.Config.ID),
				zap.Error(err),
			)
		} else {
			m.logger.Info("chain auto-restarted successfully",
				zap.String("chainId", instance.Config.ID),
			)
		}
		cancel()
	}
}

// IsEnabled returns whether multichain mode is enabled.
func (m *Manager) IsEnabled() bool {
	return m.config.Enabled
}

// WaitForSync waits until all chains are synced.
func (m *Manager) WaitForSync(ctx context.Context) error {
	return m.healthChecker.WaitForAllHealthy(ctx)
}
