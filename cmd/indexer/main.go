// Command indexer runs the indexer engine (pkg/app) with the generic EVM
// profile and the common features, through the command line of cmd/cli.
// Chains with rules of their own have their own binaries under x/; this one
// refuses their nodes and names the binary to run.
package main

import (
	"os"
	"slices"
	"strings"

	"github.com/0xmhha/indexer-go/cmd/cli"
	"github.com/0xmhha/indexer-go/pkg/chains"
)

// otherProducts describe the nodes of the chains built under x/. They
// repeat those profiles' detection rules, which main_test checks, because
// importing the profiles would link them in.
var otherProducts = []cli.Product{
	{Binary: "indexer-stablenet", Matches: func(n chains.NodeInfo) bool {
		v := strings.ToLower(n.ClientVersion)
		return strings.Contains(v, "gstable") || strings.Contains(v, "stablenet") || strings.Contains(v, "stableone")
	}},
	{Binary: "indexer-arc", Matches: func(n chains.NodeInfo) bool {
		return slices.Contains([]uint64{5042, 5042002}, n.ChainID)
	}},
}

func main() {
	os.Exit(cli.Main(cli.WithName("indexer"), cli.WithOtherProducts(otherProducts...)))
}
