package app

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/opsguard"
)

// BuildInfo identifies the running binary in the startup log.
type BuildInfo struct {
	Name    string
	Version string
	Commit  string
	Time    string
}

// StartOptions are the choices of one start that the configuration does
// not hold.
type StartOptions struct {
	// GapRecovery detects and fills missing blocks at startup.
	GapRecovery bool
	// ClearData deletes the databases before starting.
	ClearData bool
	// Reindex deletes the blockchain data but keeps verification data.
	// It is ignored with ClearData, which deletes everything.
	Reindex bool
	// Adapter names a chain profile id or alias; empty detects it.
	Adapter string
	// CheckChain, if set, refuses a node the binary is not built for. It
	// runs once the profile is selected, for every chain of the process.
	CheckChain ChainCheck
	Build      BuildInfo
}

// ChainCheck accepts or refuses the chain a run connects to: info is what
// the node reports and profile the profile selected for it.
type ChainCheck func(info chains.NodeInfo, profile chains.Profile) error

// ValidateConfig checks a loaded configuration: the configuration's own
// rules, then the ones this engine adds.
func ValidateConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	return validateConfig(cfg)
}

// Start runs the indexer with a validated configuration until ctx ends,
// which is a normal stop (nil), or the indexer fails. It refuses to start
// while the operations halt latch exists.
func Start(ctx context.Context, cfg *config.Config, log *zap.Logger, opts StartOptions) error {
	for _, msg := range cfg.UnsupportedSettings() {
		log.Warn("Unsupported setting ignored", zap.String("detail", msg))
	}
	if err := opsguard.CheckStartup(cfg.Operations.HaltFile); err != nil {
		return err
	}
	logStartupInfo(log, cfg, opts)
	if err := preflightFeeds(cfg, opts.GapRecovery); err != nil {
		return err
	}
	if opts.ClearData {
		if err := clearDatabases(cfg, log); err != nil {
			return fmt.Errorf("failed to clear data folder: %w", err)
		}
	} else if opts.Reindex {
		if err := reindexDatabases(cfg, log); err != nil {
			return fmt.Errorf("failed to reindex data: %w", err)
		}
	}

	a, err := newApp(cfg, log, opts)
	if err != nil {
		return fmt.Errorf("failed to create application: %w", err)
	}
	defer a.Shutdown()

	errc := make(chan error, 1)
	go func() { errc <- a.Run(ctx) }()
	// A stop request returns at once, as the process always has: Shutdown
	// then closes what Run still holds.
	select {
	case <-ctx.Done():
		log.Info("Received shutdown signal", zap.Error(context.Cause(ctx)))
	case err := <-errc:
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Error("Application stopped with error", zap.Error(err))
			return err
		}
	}
	log.Info("Shutting down gracefully...")
	return nil
}

// logStartupInfo logs what this start runs with.
func logStartupInfo(log *zap.Logger, cfg *config.Config, opts StartOptions) {
	profileInfo := "auto-detect"
	if opts.Adapter != "" {
		profileInfo = opts.Adapter + " (forced)"
	}
	log.Info("Starting indexer",
		zap.String("binary", opts.Build.Name),
		zap.String("version", opts.Build.Version),
		zap.String("commit", opts.Build.Commit),
		zap.String("build_time", opts.Build.Time),
		zap.String("rpc_endpoint", cfg.RPC.Endpoint),
		zap.String("db_driver", cfg.Database.Driver),
		zap.String("db_path", cfg.Database.Path),
		zap.Uint64("start_height", cfg.Indexer.StartHeight),
		zap.Int("workers", cfg.Indexer.Workers),
		zap.Int("chunk_size", cfg.Indexer.ChunkSize),
		zap.Bool("gap_recovery", opts.GapRecovery),
		zap.Bool("clear_data", opts.ClearData),
		zap.Bool("reindex", opts.Reindex),
		zap.String("chain_profile", profileInfo),
	)
}
