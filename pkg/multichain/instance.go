package multichain

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"go.uber.org/zap"
)

// ChainInstance represents a single blockchain connection with all its components.
type ChainInstance struct {
	// Configuration
	Config *ChainConfig

	// Indexer is the chain's pipeline and storage while it runs (nil
	// before Start and after Stop).
	Indexer Indexer
	factory IndexerFactory

	// State
	status       ChainStatus
	statusMu     sync.RWMutex
	registeredAt time.Time  // When the chain was registered
	startedAt    *time.Time // When the chain was started (nil if never started)
	lastError    error
	lastErrorAt  *time.Time

	// Metrics (atomic for concurrent access)
	blocksIndexed       atomic.Uint64
	transactionsIndexed atomic.Uint64
	logsIndexed         atomic.Uint64
	rpcCalls            atomic.Uint64
	rpcErrors           atomic.Uint64

	// Control
	cancelFunc context.CancelFunc
	runningWg  sync.WaitGroup
	logger     *zap.Logger
}

// NewChainInstance creates a new chain instance with the given configuration.
func NewChainInstance(cfg *ChainConfig, factory IndexerFactory, logger *zap.Logger) *ChainInstance {
	return &ChainInstance{
		Config:       cfg,
		factory:      factory,
		status:       StatusRegistered,
		registeredAt: time.Now(),
		logger:       logger.With(zap.String("chain", cfg.ID)),
	}
}

// Start initializes and starts the chain instance.
func (ci *ChainInstance) Start(ctx context.Context) error {
	ci.statusMu.Lock()
	if ci.status != StatusRegistered && ci.status != StatusStopped && ci.status != StatusError {
		ci.statusMu.Unlock()
		return ErrChainAlreadyRunning
	}
	ci.setStatusLocked(StatusStarting)
	ci.statusMu.Unlock()

	if ci.factory == nil {
		ci.setError(ErrIndexerRequired)
		return ErrIndexerRequired
	}

	// Create instance-specific context
	runCtx, cancel := context.WithCancel(ctx)
	ci.cancelFunc = cancel

	ci.logger.Info("starting chain instance",
		zap.String("rpc", ci.Config.RPCEndpoint),
		zap.Uint64("chainId", ci.Config.ChainID),
	)

	idx, err := ci.factory(runCtx, ci.Config)
	if err != nil {
		cancel()
		err = NewChainError(ci.Config.ID, ErrSourceInitFailed, err)
		ci.setError(err)
		return err
	}
	now := time.Now()
	ci.statusMu.Lock()
	ci.Indexer = idx
	ci.startedAt = &now
	ci.setStatusLocked(StatusSyncing)
	ci.statusMu.Unlock()

	// Index in the background
	ci.runningWg.Add(1)
	go ci.run(runCtx, idx)

	ci.logger.Info("chain instance started successfully")

	return nil
}

// Stop gracefully stops the chain instance.
func (ci *ChainInstance) Stop(ctx context.Context) error {
	ci.statusMu.Lock()
	if ci.status == StatusStopped || ci.status == StatusStopping {
		ci.statusMu.Unlock()
		return nil
	}
	ci.setStatusLocked(StatusStopping)
	ci.statusMu.Unlock()

	ci.logger.Info("stopping chain instance")

	// Cancel the context to signal all goroutines
	if ci.cancelFunc != nil {
		ci.cancelFunc()
	}

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		ci.runningWg.Wait()
		close(done)
	}()

	// Release the chain's storage and connections once its pipeline has
	// stopped; closing them under a running pipeline would fail its writes.
	ci.statusMu.Lock()
	idx := ci.Indexer
	ci.Indexer = nil
	ci.statusMu.Unlock()
	select {
	case <-done:
		ci.logger.Info("chain instance stopped gracefully")
		if idx != nil {
			idx.Close()
		}
	case <-ctx.Done():
		ci.logger.Warn("chain instance stop timed out; closing it when its pipeline stops")
		if idx != nil {
			go func() { <-done; idx.Close() }()
		}
	}

	ci.setStatus(StatusStopped)
	return nil
}

// IndexedHeight returns the chain's latest indexed block, if it is running
// and has indexed one.
func (ci *ChainInstance) IndexedHeight(ctx context.Context) (uint64, bool) {
	idx := ci.runningIndexer()
	if idx == nil {
		return 0, false
	}
	h, err := idx.IndexedHeight(ctx)
	return h, err == nil
}

// Store returns the chain's store and event bus while it runs.
func (ci *ChainInstance) Store() (port.QueryStore, *events.EventBus, bool) {
	idx := ci.runningIndexer()
	if idx == nil {
		return nil, nil, false
	}
	return idx.Store(), idx.EventBus(), true
}

// runningIndexer returns the chain's indexer while it runs, else nil.
func (ci *ChainInstance) runningIndexer() Indexer {
	ci.statusMu.RLock()
	defer ci.statusMu.RUnlock()
	return ci.Indexer
}

// Status returns the current status of the chain instance.
func (ci *ChainInstance) Status() ChainStatus {
	ci.statusMu.RLock()
	defer ci.statusMu.RUnlock()
	return ci.status
}

// RegisteredAt returns the time when the chain was registered.
func (ci *ChainInstance) RegisteredAt() time.Time {
	return ci.registeredAt
}

// Info returns the chain info.
func (ci *ChainInstance) Info() *ChainInfo {
	ci.statusMu.RLock()
	defer ci.statusMu.RUnlock()

	return &ChainInfo{
		ID:          ci.Config.ID,
		Name:        ci.Config.Name,
		ChainID:     ci.Config.ChainID,
		RPCEndpoint: ci.Config.RPCEndpoint,
		WSEndpoint:  ci.Config.WSEndpoint,
		AdapterType: ci.Config.AdapterType,
		Status:      ci.status,
		StartHeight: ci.Config.StartHeight,
		CreatedAt:   ci.registeredAt,
		StartedAt:   ci.startedAt,
	}
}

// HealthCheck performs a health check on the chain instance.
func (ci *ChainInstance) HealthCheck(ctx context.Context) *HealthStatus {
	status := &HealthStatus{
		ChainID:   ci.Config.ID,
		Status:    ci.Status(),
		CheckedAt: time.Now(),
	}

	ci.statusMu.RLock()
	startedAt := ci.startedAt
	ci.statusMu.RUnlock()
	if startedAt != nil {
		status.Uptime = time.Since(*startedAt)
	}

	// Check if we can get the latest block
	if idx := ci.runningIndexer(); idx != nil {
		start := time.Now()
		latestHeight, err := idx.NodeHeight(ctx)
		status.RPCLatency = time.Since(start)

		if err != nil {
			status.IsHealthy = false
			status.LastError = err.Error()
			now := time.Now()
			status.LastErrorTime = &now
		} else {
			status.LatestHeight = latestHeight

			if indexedHeight, err := idx.IndexedHeight(ctx); err == nil {
				status.IndexedHeight = indexedHeight
				if latestHeight > indexedHeight {
					status.SyncLag = latestHeight - indexedHeight
				}
			}

			// Consider healthy if sync lag is reasonable and RPC is responsive
			status.IsHealthy = status.SyncLag < 100 && status.RPCLatency < 10*time.Second
		}
	}

	// Capture last error
	ci.statusMu.RLock()
	if ci.lastError != nil {
		status.LastError = ci.lastError.Error()
		status.LastErrorTime = ci.lastErrorAt
	}
	ci.statusMu.RUnlock()

	return status
}

// GetMetrics returns the current metrics for the chain.
func (ci *ChainInstance) GetMetrics() *ChainMetrics {
	return &ChainMetrics{
		ChainID:             ci.Config.ID,
		BlocksIndexed:       ci.blocksIndexed.Load(),
		TransactionsIndexed: ci.transactionsIndexed.Load(),
		LogsIndexed:         ci.logsIndexed.Load(),
		RPCCalls:            ci.rpcCalls.Load(),
		RPCErrors:           ci.rpcErrors.Load(),
	}
}

// run indexes the chain until it stops, tracking metrics from its events.
func (ci *ChainInstance) run(ctx context.Context, idx Indexer) {
	defer ci.runningWg.Done()
	ci.logger.Info("indexer started")

	if bus := idx.EventBus(); bus != nil {
		subID := events.SubscriptionID("chain-" + ci.Config.ID + "-metrics")
		sub := bus.Subscribe(subID,
			[]events.EventType{events.EventTypeBlock, events.EventTypeTransaction, events.EventTypeLog},
			nil, 100)
		defer bus.Unsubscribe(subID)
		go ci.trackMetrics(ctx, sub)
	}

	if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
		ci.setError(err)
		ci.logger.Error("indexer error", zap.Error(err))
	}
	ci.logger.Info("indexer stopped")
}

// trackMetrics tracks block/tx/log counts from events.
func (ci *ChainInstance) trackMetrics(ctx context.Context, sub *events.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Channel:
			if !ok {
				return
			}
			switch event.Type() {
			case events.EventTypeBlock:
				ci.blocksIndexed.Add(1)
				// Check if we've caught up
				if ci.Status() == StatusSyncing {
					if health := ci.HealthCheck(ctx); health.SyncLag < 10 {
						ci.setStatus(StatusActive)
					}
				}
			case events.EventTypeTransaction:
				ci.transactionsIndexed.Add(1)
			case events.EventTypeLog:
				ci.logsIndexed.Add(1)
			}
		}
	}
}

// setStatus sets the chain status (thread-safe).
func (ci *ChainInstance) setStatus(status ChainStatus) {
	ci.statusMu.Lock()
	defer ci.statusMu.Unlock()
	ci.setStatusLocked(status)
}

// setStatusLocked sets the chain status (must hold lock).
func (ci *ChainInstance) setStatusLocked(status ChainStatus) {
	if ci.status != status {
		ci.logger.Info("status changed",
			zap.String("from", string(ci.status)),
			zap.String("to", string(status)),
		)
		ci.status = status
	}
}

// setError sets the error state.
func (ci *ChainInstance) setError(err error) {
	ci.statusMu.Lock()
	defer ci.statusMu.Unlock()
	ci.lastError = err
	now := time.Now()
	ci.lastErrorAt = &now
	ci.status = StatusError
}
