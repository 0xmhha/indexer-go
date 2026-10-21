// Command indexer-stablenet is the indexer built for StableNet chains: the
// engine and common features with the StableNet bundle. It refuses nodes of
// other chains.
package main

import (
	"os"

	"github.com/0xmhha/indexer-go/cmd/cli"
	"github.com/0xmhha/indexer-go/x/stablenet"
	_ "github.com/0xmhha/indexer-go/x/stablenet/bundle"
)

func main() {
	os.Exit(cli.Main(cli.WithName("indexer-stablenet"), cli.WithChain(stablenet.ID)))
}
