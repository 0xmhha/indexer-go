// execution-preflight validates explicit files; it never starts services.
package main

import (
	"context"
	"github.com/0xmhha/indexer-go/internal/nativepreflight"
	"github.com/0xmhha/indexer-go/x/arc/preflight"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(nativepreflight.Run(ctx, os.Args[1:], os.Stdout, preflight.Product()))
}
