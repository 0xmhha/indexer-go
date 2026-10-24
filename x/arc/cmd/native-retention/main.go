// arc-native-retention performs explicit offline producer maintenance.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xmhha/indexer-go/x/arc/nativeops"
)

func main() {
	var o nativeops.CLI
	flag.StringVar(&o.Action, "action", "plan", "plan, inspect, apply or release; never starts a node")
	flag.StringVar(&o.Config, "config", "", "explicit private Arc native retention config")
	flag.StringVar(&o.Permit, "permit", "", "DB-issued permit JSON")
	flag.StringVar(&o.Release, "release", "", "DB-issued release JSON")
	flag.StringVar(&o.Output, "output", "", "new artifact in a private directory (otherwise stdout)")
	flag.Uint64Var(&o.Keep, "keep-blocks", 86400, "acknowledged blocks retained, at least129")
	flag.Parse()
	if o.Action == "apply" {
		o.Supervisor = os.NewFile(3, "retention-supervisor")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := nativeops.RunRetention(ctx, o, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
