package eventbus

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// ShutdownManager handles graceful shutdown of distributed EventBus components
type ShutdownManager struct {
	mu sync.Mutex

	// Components to shut down
	eventBus      EventBus
	kafkaProducer *KafkaProducer

	// Configuration
	shutdownTimeout time.Duration

	// Logger
	logger *slog.Logger

	// State
	shutdownStarted bool
}

// NewShutdownManager creates a new shutdown manager
func NewShutdownManager(shutdownTimeout time.Duration) *ShutdownManager {
	if shutdownTimeout <= 0 {
		shutdownTimeout = 30 * time.Second
	}

	return &ShutdownManager{
		shutdownTimeout: shutdownTimeout,
		logger:          slog.Default().With("component", "shutdown-manager"),
	}
}

// RegisterEventBus registers an EventBus for shutdown
func (sm *ShutdownManager) RegisterEventBus(eb EventBus) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.eventBus = eb
}

// RegisterKafkaProducer registers a Kafka producer for shutdown
func (sm *ShutdownManager) RegisterKafkaProducer(kp *KafkaProducer) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.kafkaProducer = kp
}

// Shutdown performs graceful shutdown of all registered components
func (sm *ShutdownManager) Shutdown(ctx context.Context) error {
	sm.mu.Lock()
	if sm.shutdownStarted {
		sm.mu.Unlock()
		return nil
	}
	sm.shutdownStarted = true
	sm.mu.Unlock()

	sm.logger.Info("initiating graceful shutdown",
		"timeout", sm.shutdownTimeout.String(),
	)

	// Create a context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, sm.shutdownTimeout)
	defer cancel()

	// Use a WaitGroup to track shutdown progress
	var wg sync.WaitGroup

	// Track errors
	errCh := make(chan error, 3)

	// Shutdown EventBus
	if sm.eventBus != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.shutdownEventBus(shutdownCtx, errCh)
		}()
	}

	// Shutdown Kafka Producer
	if sm.kafkaProducer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.shutdownKafkaProducer(shutdownCtx, errCh)
		}()
	}

	// Wait for all shutdowns to complete or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		sm.logger.Info("graceful shutdown completed")
	case <-shutdownCtx.Done():
		sm.logger.Warn("shutdown timed out, some components may not have shut down cleanly")
		return shutdownCtx.Err()
	}

	// Check for errors
	close(errCh)
	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
		sm.logger.Error("shutdown error", "error", err)
	}

	return firstErr
}

// shutdownEventBus shuts down the EventBus
func (sm *ShutdownManager) shutdownEventBus(ctx context.Context, errCh chan<- error) {
	sm.mu.Lock()
	eb := sm.eventBus
	sm.mu.Unlock()

	if eb == nil {
		return
	}

	sm.logger.Info("shutting down EventBus", "type", eb.Type())

	// If it's a distributed EventBus, disconnect first
	if deb, ok := eb.(DistributedEventBus); ok {
		if deb.IsConnected() {
			if err := deb.Disconnect(ctx); err != nil {
				sm.logger.Error("error disconnecting from distributed backend", "error", err)
				errCh <- err
			}
		}
	}

	// Stop the EventBus
	eb.Stop()

	sm.logger.Info("EventBus shutdown complete")
}

// shutdownKafkaProducer shuts down the Kafka producer
func (sm *ShutdownManager) shutdownKafkaProducer(ctx context.Context, errCh chan<- error) {
	sm.mu.Lock()
	kp := sm.kafkaProducer
	sm.mu.Unlock()

	if kp == nil {
		return
	}

	sm.logger.Info("shutting down Kafka producer")

	// Disconnect
	if kp.IsConnected() {
		if err := kp.Disconnect(ctx); err != nil {
			sm.logger.Error("error disconnecting Kafka producer", "error", err)
			errCh <- err
		}
	}

	// Stop the producer (flushes pending messages)
	kp.Stop()

	sm.logger.Info("Kafka producer shutdown complete")
}

// ShutdownHook represents a function to call during shutdown
type ShutdownHook func(ctx context.Context) error

// MultiComponentShutdown handles shutdown of multiple components with ordering
type MultiComponentShutdown struct {
	hooks   []shutdownEntry
	mu      sync.Mutex
	logger  *slog.Logger
	timeout time.Duration
}

type shutdownEntry struct {
	name     string
	priority int
	hook     ShutdownHook
}

// NewMultiComponentShutdown creates a new multi-component shutdown handler
func NewMultiComponentShutdown(timeout time.Duration) *MultiComponentShutdown {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &MultiComponentShutdown{
		hooks:   make([]shutdownEntry, 0),
		logger:  slog.Default().With("component", "multi-shutdown"),
		timeout: timeout,
	}
}

// RegisterHook adds a shutdown hook with a priority
// Higher priority hooks are executed first
func (mcs *MultiComponentShutdown) RegisterHook(name string, priority int, hook ShutdownHook) {
	mcs.mu.Lock()
	defer mcs.mu.Unlock()

	mcs.hooks = append(mcs.hooks, shutdownEntry{
		name:     name,
		priority: priority,
		hook:     hook,
	})

	// Sort by priority (descending)
	for i := len(mcs.hooks) - 1; i > 0; i-- {
		if mcs.hooks[i].priority > mcs.hooks[i-1].priority {
			mcs.hooks[i], mcs.hooks[i-1] = mcs.hooks[i-1], mcs.hooks[i]
		} else {
			break
		}
	}
}

// Shutdown executes all shutdown hooks in priority order
func (mcs *MultiComponentShutdown) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, mcs.timeout)
	defer cancel()

	mcs.mu.Lock()
	hooks := make([]shutdownEntry, len(mcs.hooks))
	copy(hooks, mcs.hooks)
	mcs.mu.Unlock()

	mcs.logger.Info("starting multi-component shutdown",
		"components", len(hooks),
		"timeout", mcs.timeout.String(),
	)

	var firstErr error
	for _, entry := range hooks {
		mcs.logger.Info("shutting down component",
			"name", entry.name,
			"priority", entry.priority,
		)

		if err := entry.hook(ctx); err != nil {
			mcs.logger.Error("component shutdown error",
				"name", entry.name,
				"error", err,
			)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			mcs.logger.Info("component shutdown complete", "name", entry.name)
		}

		// Check if context is cancelled
		if ctx.Err() != nil {
			mcs.logger.Warn("shutdown timeout reached", "remaining", len(hooks))
			return ctx.Err()
		}
	}

	mcs.logger.Info("all components shut down")
	return firstErr
}

// Common shutdown priorities
const (
	ShutdownPriorityEventBus     = 100 // High priority - shut down event bus first
	ShutdownPriorityKafka        = 90  // High priority - flush Kafka messages
	ShutdownPriorityRedis        = 80  // Medium-high priority
	ShutdownPriorityAPI          = 50  // Medium priority - stop accepting new requests
	ShutdownPriorityStorage      = 10  // Low priority - close storage last
	ShutdownPriorityCleanup      = 0   // Lowest priority - final cleanup
)
