package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/0xmhha/indexer-go/cmd/flags"
)

// setHelp replaces the root's help with one that lists the flags by group
// and says where settings come from; subcommands keep cobra's help.
func (c *cli) setHelp(root *cobra.Command) {
	subHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd != root {
			subHelp(cmd, args)
			return
		}
		if err := c.writeHelp(cmd.OutOrStdout(), root); err != nil {
			cmd.PrintErrln(err)
		}
	})
}

func (c *cli) writeHelp(w io.Writer, root *cobra.Command) error {
	var b strings.Builder
	n := c.name
	fmt.Fprintf(&b, "%s: %s.\n\n", n, strings.ToLower(root.Short[:1])+root.Short[1:])
	fmt.Fprintf(&b, "Usage:\n  %s [flags]              index the chain and serve the API\n  %s <command> [flags]\n\n", n, n)

	b.WriteString("Commands:\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, sub := range root.Commands() {
		if sub.IsAvailableCommand() || sub.Name() == "help" {
			if _, err := fmt.Fprintf(tw, "  %s\t%s\n", sub.Name(), sub.Short); err != nil {
				return err
			}
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(&b, `
Settings are applied in this order, later ones winning:
  defaults < --config file (%s if present) < INDEXER_* environment variables < flags given here
Each flag names the setting it sets and its environment variable in parentheses.
Settings without a flag are set in the file or the environment (see configs/*.yaml).

`, flags.DefaultConfigFile)
	if err := c.global.WriteUsage(&b); err != nil {
		return err
	}
	if err := c.local.WriteUsage(&b); err != nil {
		return err
	}
	fmt.Fprintf(&b, `Other:
  -h, --help      Show this help ('%[1]s <command> --help' for a command)
  -v, --version   Print the version

Examples:
  %[1]s --config config.yaml
  %[1]s --rpc http://127.0.0.1:8545 --db ./data --api
  %[1]s --config config.yaml --reindex
  %[1]s resume-check --config /etc/indexer/config.yaml

Documentation: README.md and docs/README.md in the source repository.
`, n)
	_, err := io.WriteString(w, b.String())
	return err
}
