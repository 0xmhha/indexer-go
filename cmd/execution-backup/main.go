// execution-backup authenticates external backup evidence without starting services.
package main

import (
	"context"
	"github.com/0xmhha/indexer-go/internal/executionbackup"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Let the encoder report a broken diagnostic pipe as exit1.
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := executionbackup.Run(ctx, os.Args[1:], os.Stdout)
	stop()
	os.Exit(code)
}
