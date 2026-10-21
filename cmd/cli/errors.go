package cli

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Exit codes of Main.
const (
	exitFailure = 1
	exitUsage   = 2
)

// maxSuggestDistance is the largest edit distance of a suggested flag.
const maxSuggestDistance = 2

// usageError is a wrong command line: the indexer did not start.
type usageError struct {
	err  error
	hint string
}

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// configError is a configuration that cannot be loaded or is invalid.
type configError struct{ err error }

func (e configError) Error() string { return e.err.Error() }
func (e configError) Unwrap() error { return e.err }

// report writes err with what to do about it and returns the exit code.
func report(w io.Writer, name string, err error) int {
	var b strings.Builder
	code := exitFailure
	fmt.Fprintf(&b, "Error: %v\n", err)
	var u usageError
	var c configError
	switch {
	case errors.As(err, &u):
		if u.hint != "" {
			b.WriteString(u.hint + "\n")
		}
		fmt.Fprintf(&b, "Run '%s --help' for usage.\n", name)
		code = exitUsage
	case errors.As(err, &c):
		fmt.Fprintf(&b, "Settings come from defaults, the --config file, INDEXER_* environment variables and flags, later ones winning.\nRun '%s --help' for the flags and the settings they set.\n", name)
	}
	_, _ = io.WriteString(w, b.String())
	return code
}

// noArgs rejects positional arguments: the commands take flags only.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	hint := "The indexer takes flags, not arguments: start it without a command, or give one of its commands."
	if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
		hint = fmt.Sprintf("Did you mean '%s %s'?", cmd.CommandPath(), s[0])
	}
	return usageError{err: fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath()), hint: hint}
}

var (
	unknownFlag      = regexp.MustCompile(`^unknown flag: --([^ =]+)`)
	unknownShorthand = regexp.MustCompile(`^unknown shorthand flag: '.' in -([^ =]+)`)
)

// flagError turns a parse error into a usage error that names the flag
// meant, when one is close.
func flagError(cmd *cobra.Command, err error) error {
	msg := err.Error()
	hint := ""
	if m := unknownShorthand.FindStringSubmatch(msg); m != nil && cmd.Flags().Lookup(m[1]) != nil {
		hint = fmt.Sprintf("Long flags take two dashes: --%s.", m[1])
	} else if m := unknownFlag.FindStringSubmatch(msg); m != nil {
		if s := closestFlag(cmd.Flags(), m[1]); s != "" {
			hint = fmt.Sprintf("Did you mean --%s?", s)
		}
	}
	return usageError{err: err, hint: hint}
}

// closestFlag returns the flag name nearest to name, or "" if none is
// close enough.
func closestFlag(fs *pflag.FlagSet, name string) string {
	best, bestD := "", maxSuggestDistance+1
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		d := distance(name, f.Name)
		if strings.HasPrefix(f.Name, name) {
			d = min(d, 1)
		}
		if d < bestD || (d == bestD && f.Name < best) {
			best, bestD = f.Name, d
		}
	})
	return best
}

// distance is the Levenshtein distance of a and b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
