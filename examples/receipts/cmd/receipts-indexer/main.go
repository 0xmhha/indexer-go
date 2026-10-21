// Command receipts-indexer is the indexer with the receipts handlers
// linked in: the standard indexer, configured by its config file, plus
// GET /receipts/{merchant}/{orderId} and the receipts.totals feature.
package main

import (
	"os"

	"github.com/0xmhha/indexer-go/cmd/cli"
	_ "github.com/0xmhha/indexer-go/examples/receipts" // registers the handlers
)

func main() { os.Exit(cli.Main(cli.WithName("receipts-indexer"))) }
