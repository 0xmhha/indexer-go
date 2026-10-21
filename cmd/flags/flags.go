// Package flags defines the indexer's command-line flags in groups. A
// configuration group applies to the configuration only the flags given on
// the command line, so a flag default never replaces a value from the file
// or the environment, and a boolean flag can switch a setting off as well
// as on. Groups are composed into a Set, which also lays out the help.
package flags

import (
	"fmt"
	"io"

	"github.com/spf13/pflag"

	"github.com/0xmhha/indexer-go/internal/config"
)

// Group is one titled set of related flags.
type Group interface {
	// Name titles the group in help.
	Name() string
	// AddFlags defines the group's flags in fs.
	AddFlags(fs *pflag.FlagSet)
}

// Applier is a group whose flags set configuration fields.
type Applier interface {
	// ApplyTo copies into cfg the flags for which changed reports true.
	ApplyTo(cfg *config.Config, changed func(name string) bool)
}

// Set composes groups. Each group's flags live in a flag set of their own,
// which Add merges into the command's, so help can list them by group.
type Set struct {
	groups []Group
	sets   []*pflag.FlagSet
}

// NewSet returns a set of the given groups, in help order.
func NewSet(groups ...Group) *Set { return &Set{groups: groups} }

// Add defines every group's flags in fs.
func (s *Set) Add(fs *pflag.FlagSet) {
	s.sets = s.sets[:0]
	for _, g := range s.groups {
		gs := pflag.NewFlagSet(g.Name(), pflag.ContinueOnError)
		g.AddFlags(gs)
		fs.AddFlagSet(gs)
		s.sets = append(s.sets, gs)
	}
}

// ApplyTo applies every configuration group in order.
func (s *Set) ApplyTo(cfg *config.Config, changed func(name string) bool) {
	for _, g := range s.groups {
		if a, ok := g.(Applier); ok {
			a.ApplyTo(cfg, changed)
		}
	}
}

// WriteUsage writes the flags group by group. It must follow Add.
func (s *Set) WriteUsage(w io.Writer) error {
	for i, g := range s.groups {
		if _, err := fmt.Fprintf(w, "%s:\n%s\n", g.Name(), s.sets[i].FlagUsages()); err != nil {
			return err
		}
	}
	return nil
}

// Configuration returns the configuration groups of the indexing command,
// in help order. Log is separate: every command takes it.
func Configuration() []Group {
	return []Group{RPC(), Database(), Indexer(), API()}
}
