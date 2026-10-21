package flags

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"github.com/0xmhha/indexer-go/pkg/app"
	"github.com/0xmhha/indexer-go/pkg/chains"
)

// DefaultConfigFile is read when --config is not given, if it exists.
const DefaultConfigFile = "config.yaml"

// File is the configuration file group.
type File struct{ Path string }

// Name implements Group.
func (*File) Name() string { return "Configuration file" }

// AddFlags implements Group.
func (f *File) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&f.Path, "config", DefaultConfigFile, "YAML configuration file; the default is skipped if it does not exist, a given file must exist")
}

// Start is the group of choices for one start, which the configuration
// does not hold.
type Start struct{ opts app.StartOptions }

// Name implements Group.
func (*Start) Name() string { return "Start" }

// AddFlags implements Group.
func (s *Start) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.opts.Adapter, "adapter", "", fmt.Sprintf("Chain profile id (%s) or alias; detected from the node if empty or %q", strings.Join(chains.IDs(), ", "), chains.DetectName))
	fs.BoolVar(&s.opts.GapRecovery, "gap-recovery", false, "Detect and fill missing blocks at startup")
	fs.BoolVar(&s.opts.ClearData, "clear-data", false, "Delete the databases before starting")
	fs.BoolVar(&s.opts.Reindex, "reindex", false, "Delete the blockchain data before starting, keeping verification data (ABIs, sources)")
}

// ErrClearAndReindex rejects --clear-data with --reindex: one deletes all
// data, the other keeps some, so together they say two different things.
var ErrClearAndReindex = errors.New("--clear-data deletes all data and --reindex keeps verification data; give only one of them")

// Options validates the choices and returns them.
func (s *Start) Options() (app.StartOptions, error) {
	if s.opts.ClearData && s.opts.Reindex {
		return app.StartOptions{}, ErrClearAndReindex
	}
	if !chains.Selectable(s.opts.Adapter) {
		return app.StartOptions{}, fmt.Errorf("--adapter %q is not a chain profile of this binary (profiles: %s; %q detects from the node)",
			s.opts.Adapter, strings.Join(chains.IDs(), ", "), chains.DetectName)
	}
	return s.opts, nil
}
