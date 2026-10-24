// execution-checkpoint verifies/imports offline StableNet bootstrap artifacts.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/0xmhha/indexer-go/internal/executionops"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var o executionops.Options
	flag.StringVar(&o.Action, "action", "verify", "verify, import or inspect (never starts the app)")
	flag.StringVar(&o.Input, "input", "", "snapshot NDJSON file")
	flag.StringVar(&o.Expected, "expected", "", "independently trusted manifest JSON")
	flag.StringVar(&o.Schema, "schema", "", "explicit non-public PostgreSQL schema")
	flag.StringVar(&o.Source, "source", "", "optional source observation JSON for diagnosis")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := executionops.Run(ctx, o, stablenet.New(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
