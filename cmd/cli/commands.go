package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/logger"
	"github.com/0xmhha/indexer-go/pkg/app"
)

func (c *cli) command() *cobra.Command {
	root := &cobra.Command{
		Use:     c.name,
		Short:   "Index an EVM chain and serve it over GraphQL, JSON-RPC, REST and WebSocket",
		Version: version,
		Args:    noArgs,
		// cobra uses its default distance only for its own messages.
		SuggestionsMinimumDistance: 2,
		SilenceErrors:              true,
		SilenceUsage:               true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: c.runIndexer,
	}
	root.SetVersionTemplate(versionText(c.name))
	root.SetFlagErrorFunc(flagError)
	c.global.Add(root.PersistentFlags())
	c.local.Add(root.Flags())
	root.AddCommand(c.resumeCommand(), c.versionCommand())
	c.setHelp(root)
	return root
}

func (c *cli) resumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resume-check",
		Short: "Check the resume conditions after a capacity halt and remove the halt latch",
		Long: `resume-check samples the resume conditions of operations.capacity (three
samples 30s apart) and removes the halt latch only if they all hold. It never
starts indexing: start the indexer normally afterwards. Halts caused by the
raw window or by integrity failures are not cleared by it.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, log, err := c.load(cmd, false)
			if err != nil {
				return err
			}
			defer func() { _ = log.Sync() }()
			return c.resume(cmd.Context(), cfg, log)
		},
	}
}

func (c *cli) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), versionText(c.name))
			return err
		},
	}
}

func versionText(name string) string {
	return fmt.Sprintf("%s version %s\n  commit: %s\n  built:  %s\n", name, version, commit, buildTime)
}

func (c *cli) runIndexer(cmd *cobra.Command, _ []string) error {
	opts, err := c.run.Options()
	if err != nil {
		return usageError{err: err}
	}
	opts.Build = app.BuildInfo{Name: c.name, Version: version, Commit: commit, Time: buildTime}
	cfg, log, err := c.load(cmd, true)
	if err != nil {
		return err
	}
	defer func() { _ = log.Sync() }()
	return c.start(cmd.Context(), cfg, log, opts)
}

// load reads the configuration (defaults, file, environment, then the
// flags given), validates it and builds the logger. withLocal applies the
// indexing command's own flags too.
func (c *cli) load(cmd *cobra.Command, withLocal bool) (*config.Config, *zap.Logger, error) {
	cfg, err := c.config(cmd, withLocal)
	if err != nil {
		return nil, nil, err
	}
	log, err := newLogger(cfg.Log)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize logger: %w", err)
	}
	return cfg, log, nil
}

func (c *cli) config(cmd *cobra.Command, withLocal bool) (*config.Config, error) {
	changed := cmd.Flags().Changed
	// The default file is optional; a file given with --config must exist.
	path := c.file.Path
	if !changed("config") {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			path = ""
		}
	}
	cfg, err := config.LoadUnvalidated(path)
	if err != nil {
		return nil, configError{fmt.Errorf("failed to load configuration: %w", err)}
	}
	c.global.ApplyTo(cfg, changed)
	if withLocal {
		c.local.ApplyTo(cfg, changed)
	}
	if err := app.ValidateConfig(cfg); err != nil {
		return nil, configError{fmt.Errorf("invalid configuration: %w", err)}
	}
	return cfg, nil
}

func newLogger(l config.LogConfig) (*zap.Logger, error) {
	if l.Format == "json" || l.Format == "production" {
		return logger.NewProduction()
	}
	return logger.NewWithConfig(&logger.Config{Level: l.Level, Encoding: "console", Development: true})
}
