package app

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/opsguard"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// initOperations builds the capacity monitor, which Run starts. Chain Apps of
// multichain mode have no operations section and share the root's monitor.
func (a *App) initOperations() error {
	ops := a.config.Operations.WithDefaults()
	if !ops.Enabled() {
		return nil
	}
	m, err := opsguard.NewMonitor(ops, a.haltProcess, opsguard.WithLogger(a.logger.Named("capacity")))
	if err != nil {
		return fmt.Errorf("operations: %w", err)
	}
	a.opsMonitor = m
	a.rawObserver = m.ObserveRaw
	return nil
}

// observeRaw passes raw window usage to the capacity monitor, if any. The
// observer is set before Run starts the sessions that call this.
func (a *App) observeRaw(u window.Usage) {
	if a.rawObserver != nil {
		a.rawObserver(u)
	}
}

// attachPostgresStats lets m sample the statistics of this App's store, if
// it is PostgreSQL, until closeOperations.
func (a *App) attachPostgresStats(m *opsguard.Monitor) {
	if m == nil || a.statsDetach != nil {
		return
	}
	if s, ok := postgresStats(a.storage); ok {
		a.statsDetach = m.AttachPostgresStats(s)
	}
}

// closeOperations runs before the store closes.
func (a *App) closeOperations() {
	if a.statsDetach != nil {
		a.statsDetach()
	}
	if a.opsMonitor == nil {
		return
	}
	if err := a.opsMonitor.Close(); err != nil {
		a.logger.Warn("Failed to close the capacity journal", zap.Error(err))
	}
}

// haltProcess ends Run with h as its result. The latch is already written.
func (a *App) haltProcess(h *opsguard.HaltError) {
	a.haltMu.Lock()
	cancel := a.haltRun
	a.haltMu.Unlock()
	if cancel != nil {
		cancel(h)
	}
}

// chainFailure is the multichain failure policy: a classified failure is
// not restarted automatically, and a capacity or storage failure, shared by
// every chain of the process, latches and stops the whole process.
func (a *App) chainFailure(chain string, err error) bool {
	reason, ok := opsguard.Classify(err)
	if !ok {
		return false
	}
	if reason.ProcessWide() {
		a.latchFailure(chain, err)
		a.haltProcess(&opsguard.HaltError{Reason: reason, Detail: fmt.Sprintf("chain %s: %v", chain, err)})
	}
	return true
}

// latchFailure records a process-wide failure in the halt latch, so that a
// restart waits for the resume check. Other errors are left to the caller.
func (a *App) latchFailure(chain string, err error) {
	reason, ok := opsguard.Classify(err)
	path := a.config.Operations.HaltFile
	if !ok || !reason.ProcessWide() || path == "" {
		return
	}
	l := opsguard.Latch{Reason: reason, Chain: chain, Detail: err.Error(), At: time.Now(), StopDatabase: opsguard.StopsDatabase(err)}
	if werr := opsguard.WriteLatch(path, l); werr != nil {
		a.logger.Error("Failed to write the halt latch", zap.String("path", path), zap.Error(werr))
	}
}

// ResumeCheck runs the operator's resume check (the resume-check command):
// it samples the resume conditions of operations.capacity and removes the
// halt latch if they hold. It never starts indexing.
func ResumeCheck(ctx context.Context, cfg *config.Config, log *zap.Logger) error {
	if cfg.Operations.HaltFile == "" {
		return fmt.Errorf("resume-check needs operations.halt_file")
	}
	report, err := opsguard.Resume(ctx, cfg.Operations, opsguard.ResumeOptions{})
	fields := []zap.Field{zap.String("latch", cfg.Operations.HaltFile), zap.Int64s("free_bytes", report.FreeBytes), zap.Bool("cleared", report.Cleared)}
	if report.Postgres != nil {
		fields = append(fields, zap.Int64("postgres_bytes", *report.Postgres))
	}
	if report.Latch != nil {
		fields = append(fields, zap.String("reason", string(report.Latch.Reason)), zap.String("source", string(report.Latch.Source)), zap.String("chain", report.Latch.Chain))
	}
	switch {
	case err != nil:
		log.Error("Resume check refused", append(fields, zap.Error(err))...)
		return err
	case report.Latch == nil:
		log.Info("No halt latch; nothing to resume", fields...)
	default:
		log.Info("Resume conditions hold; halt latch removed. Start the indexer normally.", fields...)
	}
	return nil
}
