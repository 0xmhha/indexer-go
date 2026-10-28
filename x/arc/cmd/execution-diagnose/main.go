// execution-diagnose samples existing metadata without starting services.
package main

import (
	"context"
	"github.com/0xmhha/indexer-go/internal/nativediagnostics"
	"github.com/0xmhha/indexer-go/x/arc/diagnostics"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := nativediagnostics.Run(ctx, os.Args[1:], os.Stdout, diagnostics.Product())
	stop()
	os.Exit(code)
}
