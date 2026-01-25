package multichain

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HealthChecker performs periodic health checks on chain instances.
type HealthChecker struct {
	manager  *Manager
	interval time.Duration
	logger   *zap.Logger

	ctx        context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(manager *Manager, interval time.Duration, logger *zap.Logger) *HealthChecker {
	return &HealthChecker{
		manager:  manager,
		interval: interval,
		logger:   logger.Named("health"),
	}
}

// Start begins periodic health checking.
func (hc *HealthChecker) Start(ctx context.Context) {
	hc.ctx, hc.cancelFunc = context.WithCancel(ctx)

	hc.wg.Add(1)
	go hc.run()

	hc.logger.Info("health checker started", zap.Duration("interval", hc.interval))
}

// Stop stops the health checker.
func (hc *HealthChecker) Stop() {
	if hc.cancelFunc != nil {
		hc.cancelFunc()
	}
	hc.wg.Wait()
	hc.logger.Info("health checker stopped")
}

// run is the main health check loop.
func (hc *HealthChecker) run() {
	defer hc.wg.Done()

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			hc.checkAll()
		}
	}
}

// checkAll checks the health of all chains.
func (hc *HealthChecker) checkAll() {
	ctx, cancel := context.WithTimeout(hc.ctx, hc.interval)
	defer cancel()

	statuses := hc.manager.HealthCheck(ctx)

	var healthy, unhealthy int
	for chainID, status := range statuses {
		if status.IsHealthy {
			healthy++
		} else {
			unhealthy++
			hc.logger.Warn("chain unhealthy",
				zap.String("chainId", chainID),
				zap.String("status", string(status.Status)),
				zap.String("error", status.LastError),
				zap.Uint64("syncLag", status.SyncLag),
			)
		}
	}

	hc.logger.Debug("health check complete",
		zap.Int("healthy", healthy),
		zap.Int("unhealthy", unhealthy),
	)
}

// CheckChain performs a health check on a specific chain.
func (hc *HealthChecker) CheckChain(ctx context.Context, chainID string) (*HealthStatus, error) {
	instance, err := hc.manager.GetChain(chainID)
	if err != nil {
		return nil, err
	}
	return instance.HealthCheck(ctx), nil
}

// WaitForHealthy waits until a chain becomes healthy or the context is cancelled.
func (hc *HealthChecker) WaitForHealthy(ctx context.Context, chainID string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := hc.CheckChain(ctx, chainID)
			if err != nil {
				return err
			}
			if status.IsHealthy {
				return nil
			}
		}
	}
}

// WaitForAllHealthy waits until all chains become healthy.
func (hc *HealthChecker) WaitForAllHealthy(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			statuses := hc.manager.HealthCheck(ctx)
			allHealthy := true
			for _, status := range statuses {
				if !status.IsHealthy {
					allHealthy = false
					break
				}
			}
			if allHealthy {
				return nil
			}
		}
	}
}
