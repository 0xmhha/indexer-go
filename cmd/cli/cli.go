// Package cli is the indexer's command line: it composes the flag groups of
// cmd/flags into cobra commands, loads the configuration, and runs the
// engine (pkg/app). A binary's main calls Main; the options name the binary.
package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/cmd/flags"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/app"
)

// Version information, injected at build time with
// -X github.com/0xmhha/indexer-go/cmd/cli.version=...
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// defaultName names the command when no option does.
const defaultName = "indexer"

// Option configures the command line.
type Option func(*cli)

// WithName names the binary in help, errors and the startup log.
func WithName(name string) Option { return func(c *cli) { c.name = name } }

// cli holds the command line's flag values and what its commands run;
// tests replace start and resume.
type cli struct {
	name   string
	start  func(context.Context, *config.Config, *zap.Logger, app.StartOptions) error
	resume func(context.Context, *config.Config, *zap.Logger) error

	file   flags.File
	global *flags.Set // every command: configuration file and logging
	local  *flags.Set // the indexing command: configuration groups
	run    flags.Start
}

func newCLI(opts ...Option) *cli {
	c := &cli{name: defaultName, start: app.Start, resume: app.ResumeCheck}
	for _, o := range opts {
		o(c)
	}
	c.global = flags.NewSet(&c.file, flags.Log())
	c.local = flags.NewSet(append(flags.Configuration(), &c.run)...)
	return c
}

// New returns the root command: it indexes and serves, and its
// subcommands are resume-check and version.
func New(opts ...Option) *cobra.Command { return newCLI(opts...).command() }

// Main runs the command line of the process and returns its exit code:
// 0 on success, 1 when the indexer fails, 2 when the command line is
// wrong. SIGINT and SIGTERM stop the indexer normally.
func Main(opts ...Option) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return execute(ctx, New(opts...), os.Args[1:], os.Stderr)
}

// execute runs cmd with args and reports an error to w.
func execute(ctx context.Context, cmd *cobra.Command, args []string, w io.Writer) int {
	cmd.SetArgs(args)
	failed, err := cmd.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	return report(w, failed.CommandPath(), err)
}
