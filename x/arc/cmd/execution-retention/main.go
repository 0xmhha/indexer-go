// execution-retention pins/completes offline Arc producer maintenance.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xmhha/indexer-go/internal/executionops"
	"github.com/0xmhha/indexer-go/x/arc"
)

func main() {
	var o executionops.RetentionOptions
	flag.StringVar(&o.Action, "action", "inspect", "inspect, authorize or collect; never starts the app")
	flag.StringVar(&o.Schema, "schema", "", "explicit PostgreSQL schema")
	flag.StringVar(&o.Request, "request", "", "offline producer request JSON")
	flag.StringVar(&o.Permit, "permit", "", "DB-issued permit JSON")
	flag.StringVar(&o.Output, "output", "", "new artifact in a private directory (otherwise stdout)")
	flag.StringVar(&o.Producer, "producer", "", "absolute compiled arc-native-retention binary for supervised collect")
	flag.StringVar(&o.ProducerConfig, "producer-config", "", "explicit private Arc native retention config for supervised collect")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := executionops.RunRetention(ctx, o, arc.New(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
