// execution-monitor evaluates saved observations; it never starts a chain.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xmhha/indexer-go/internal/nativemonitor"
)

func main() {
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := nativemonitor.Run(ctx, os.Args[1:], os.Stdout)
	stop()
	os.Exit(code)
}
