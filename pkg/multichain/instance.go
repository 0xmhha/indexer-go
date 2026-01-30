package multichain

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/pkg/adapters/factory"
	"github.com/0xmhha/indexer-go/pkg/client"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"go.uber.org/zap"
)

// ChainInstance represents a single blockchain connection with all its components.
type ChainInstance struct {
	// Configuration
	Config *ChainConfig

	// Core components
	Client   *client.Client
	Adapter  chain.Adapter
	Fetcher  *fetch.Fetcher
	Storage  storage.Storage   // Shared storage with chain-scoped prefixing
	EventBus *events.EventBus  // Global event bus (shared across chains)

	// State
	status      ChainStatus
	statusMu    sync.RWMutex
	startedAt   *time.Time
	lastError   error
	lastErrorAt *time.Time

	// Metrics (atomic for concurrent access)
	blocksIndexed       atomic.Uint64
	transactionsIndexed atomic.Uint64
	logsIndexed         atomic.Uint64
	rpcCalls            atomic.Uint64
	rpcErrors           atomic.Uint64

	// Control
	ctx        context.Context
	cancelFunc context.CancelFunc
	runningWg  sync.WaitGroup
	logger     *zap.Logger
}

// NewChainInstance creates a new chain instance with the given configuration.
func NewChainInstance(
	cfg *ChainConfig,
	globalStorage storage.Storage,
	globalEventBus *events.EventBus,
	logger *zap.Logger,
) *ChainInstance {
	return &ChainInstance{
		Config:   cfg,
		Storage:  globalStorage,
		EventBus: globalEventBus,
		status:   StatusRegistered,
		logger:   logger.With(zap.String("chain", cfg.ID)),
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

	// Validate required dependencies
	if ci.Storage == nil {
		ci.setError(ErrStorageRequired)
		return ErrStorageRequired
	}

	// Create instance-specific context
	ci.ctx, ci.cancelFunc = context.WithCancel(ctx)

	ci.logger.Info("starting chain instance",
		zap.String("rpc", ci.Config.RPCEndpoint),
		zap.Uint64("chainId", ci.Config.ChainID),
	)

	// Initialize client
	if err := ci.initClient(); err != nil {
		ci.setError(err)
		return err
	}

	// Initialize adapter
	if err := ci.initAdapter(ctx); err != nil {
		ci.setError(err)
		return err
	}

	// Initialize fetcher
	if err := ci.initFetcher(); err != nil {
		ci.setError(err)
		return err
	}

	// Start the fetcher in background
	ci.runningWg.Add(1)
	go ci.runFetcher()

	now := time.Now()
	ci.startedAt = &now

	ci.setStatus(StatusSyncing)
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

	select {
	case <-done:
		ci.logger.Info("chain instance stopped gracefully")
	case <-ctx.Done():
		ci.logger.Warn("chain instance stop timed out")
	}

	// Clean up resources
	if ci.Adapter != nil {
		ci.Adapter.Close()
	}
	if ci.Client != nil {
		ci.Client.Close()
	}

	ci.setStatus(StatusStopped)
	return nil
}

// Status returns the current status of the chain instance.
func (ci *ChainInstance) Status() ChainStatus {
	ci.statusMu.RLock()
	defer ci.statusMu.RUnlock()
	return ci.status
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

	if ci.startedAt != nil {
		status.Uptime = time.Since(*ci.startedAt)
	}

	// Check if we can get the latest block
	if ci.Client != nil {
		start := time.Now()
		latestHeight, err := ci.Client.GetLatestBlockNumber(ctx)
		status.RPCLatency = time.Since(start)

		if err != nil {
			status.IsHealthy = false
			status.LastError = err.Error()
			now := time.Now()
			status.LastErrorTime = &now
		} else {
			status.LatestHeight = latestHeight

			// Get indexed height from storage (only if storage is available)
			if ci.Storage != nil {
				indexedHeight, err := ci.Storage.GetLatestHeight(ctx)
				if err == nil {
					status.IndexedHeight = indexedHeight
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

// initClient initializes the RPC client.
func (ci *ChainInstance) initClient() error {
	clientCfg := &client.Config{
		Endpoint: ci.Config.RPCEndpoint,
		Timeout:  ci.Config.RPCTimeout,
		Logger:   ci.logger,
	}

	var err error
	ci.Client, err = client.NewClient(clientCfg)
	if err != nil {
		return NewChainError(ci.Config.ID, ErrClientInitFailed, err)
	}
	return nil
}

// initAdapter initializes the chain adapter.
func (ci *ChainInstance) initAdapter(ctx context.Context) error {
	factoryConfig := &factory.Config{
		RPCEndpoint:      ci.Config.RPCEndpoint,
		WSEndpoint:       ci.Config.WSEndpoint,
		ForceAdapterType: ci.Config.AdapterType,
		ChainID:          big.NewInt(int64(ci.Config.ChainID)),
	}

	f := factory.NewFactory(factoryConfig, ci.logger)
	result, err := f.Create(ctx)
	if err != nil {
		return NewChainError(ci.Config.ID, ErrAdapterInitFailed, err)
	}

	ci.Adapter = result.Adapter
	ci.logger.Info("adapter initialized",
		zap.String("type", result.AdapterType),
		zap.Uint64("chainId", result.NodeInfo.ChainID),
	)

	return nil
}

// initFetcher initializes the block fetcher.
func (ci *ChainInstance) initFetcher() error {
	fetcherConfig := &fetch.Config{
		StartHeight: ci.Config.StartHeight,
		BatchSize:   ci.Config.BatchSize,
		NumWorkers:  ci.Config.Workers,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}

	ci.Fetcher = fetch.NewFetcherWithAdapter(
		ci.Adapter.BlockFetcher(),
		ci.Storage,
		fetcherConfig,
		ci.logger,
		ci.EventBus,
		ci.Adapter,
	)

	return nil
}

// runFetcher runs the fetcher in a goroutine.
func (ci *ChainInstance) runFetcher() {
	defer ci.runningWg.Done()

	ci.logger.Info("fetcher started")

	// Subscribe to block events to track metrics (only if EventBus is available)
	if ci.EventBus != nil {
		subID := events.SubscriptionID("chain-" + ci.Config.ID + "-metrics")
		sub := ci.EventBus.Subscribe(
			subID,
			[]events.EventType{events.EventTypeBlock, events.EventTypeTransaction, events.EventTypeLog},
			nil,
			100,
		)
		defer ci.EventBus.Unsubscribe(subID)

		// Start metrics tracking goroutine
		go ci.trackMetrics(sub)
	} else {
		ci.logger.Warn("EventBus not available, metrics tracking disabled")
	}

	// Run the fetcher with gap recovery
	if err := ci.Fetcher.RunWithGapRecovery(ci.ctx); err != nil {
		if ci.ctx.Err() == nil {
			// Not a cancellation error
			ci.setError(err)
			ci.logger.Error("fetcher error", zap.Error(err))
		}
	}

	ci.logger.Info("fetcher stopped")
}

// trackMetrics tracks block/tx/log counts from events.
func (ci *ChainInstance) trackMetrics(sub *events.Subscription) {
	for {
		select {
		case <-ci.ctx.Done():
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
					if health := ci.HealthCheck(ci.ctx); health.SyncLag < 10 {
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
