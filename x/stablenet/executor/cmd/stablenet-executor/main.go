package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/nodebackend"
	"os"
	"os/signal"
	"syscall"
)

func finishError(ctx context.Context, err error) error {
	if ctx.Err() != nil && cancellationOnly(err, ctx.Err()) {
		return nil
	}
	return err
}
func cancellationOnly(err, target error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !cancellationOnly(child, target) {
				return false
			}
		}
		return true
	}
	if child := errors.Unwrap(err); child != nil {
		return cancellationOnly(child, target)
	}
	return err == target
}

func main() {
	path := flag.String("config", "", "path to explicit execution-node JSON configuration")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "--config is required")
		os.Exit(2)
	}
	c, err := nodebackend.LoadConfig(*path)
	if err == nil {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		err = finishError(ctx, nodebackend.Run(ctx, c))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
