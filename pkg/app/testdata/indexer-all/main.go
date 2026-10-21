// Command indexer-all is a test binary: the indexer command with every
// chain bundle linked in, the wiring the end-to-end tests of pkg/app use
// in process (chains_test.go). Tests build it to run the indexer as a
// process.
package main

import (
	"os"

	"github.com/0xmhha/indexer-go/cmd/cli"
	_ "github.com/0xmhha/indexer-go/x/arc/bundle"
	_ "github.com/0xmhha/indexer-go/x/stablenet/bundle"
)

func main() { os.Exit(cli.Main(cli.WithName("indexer-all"))) }
