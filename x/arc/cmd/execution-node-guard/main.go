// execution-node-guard owns the native run lease for an explicitly invoked
// unmodified Arc node. Collection/maintenance never calls this command.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/0xmhha/indexer-go/x/arc/nativeops"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	executable := flag.String("executable", "", "absolute official Arc node executable")
	datadir := flag.String("datadir", "", "absolute existing managed native directory")
	chain := flag.String("chain", "", "arc-mainnet or arc-testnet")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := nativeops.RunGuard(ctx, *executable, *datadir, *chain, flag.Args(), os.Stdin, os.Stdout, os.Stderr); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
