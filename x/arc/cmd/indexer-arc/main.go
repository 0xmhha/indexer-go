// Command indexer-arc is the indexer built for Arc chains: the engine and
// common features with the Arc bundle. It refuses nodes of other chains.
package main

import (
	"os"

	"github.com/0xmhha/indexer-go/cmd/cli"
	"github.com/0xmhha/indexer-go/x/arc"
	_ "github.com/0xmhha/indexer-go/x/arc/bundle"
)

func main() {
	os.Exit(cli.Main(cli.WithName("indexer-arc"), cli.WithChain(arc.ID)))
}
