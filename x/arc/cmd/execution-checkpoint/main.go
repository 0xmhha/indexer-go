// execution-checkpoint verifies/imports offline Arc state artifacts using the
// shared bounded checkpoint tools. It starts no node or application service.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xmhha/indexer-go/internal/executionops"
	"github.com/0xmhha/indexer-go/x/arc"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	f := flag.NewFlagSet("arc-execution-checkpoint", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var o executionops.Options
	f.StringVar(&o.Action, "action", "verify", "verify, import or inspect; never starts the app")
	f.StringVar(&o.Input, "input", "", "snapshot NDJSON file")
	f.StringVar(&o.Expected, "expected", "", "independently trusted manifest JSON")
	f.StringVar(&o.Schema, "schema", "", "explicit non-public PostgreSQL schema")
	f.StringVar(&o.Source, "source", "", "optional source observation JSON for diagnosis")
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			f.SetOutput(out)
			f.Usage()
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	return executionops.Run(ctx, o, arc.New(), out)
}
