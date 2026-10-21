// Command indexer runs the indexer engine (pkg/app) with the features and
// chain profiles it links in, through the command line of cmd/cli.
package main

import (
	"os"

	"github.com/0xmhha/indexer-go/cmd/cli"
)

func main() { os.Exit(cli.Main()) }
