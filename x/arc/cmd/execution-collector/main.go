// execution-collector owns a read-only Arc adapter and a durable source queue.
// It connects to an already prepared indexer and starts no node service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/x/arc/collector"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, collector.Run); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, start func(context.Context, collector.Config) error) error {
	f := flag.NewFlagSet("execution-collector", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	configPath := f.String("config", "", "explicit collector JSON; exclusive of other flags")
	id := f.String("chain-id", "", "Arc chain ID: 5042 or 5042002")
	genesis := f.String("genesis", "", "exact lowercase genesis hash")
	executable := f.String("native-executable", "", "absolute pinned Arc executor binary")
	datadir := f.String("native-datadir", "", "absolute existing native data directory (read-only)")
	spool := f.String("spool-dir", "", "absolute private durable source directory; separate from native data")
	socket := f.String("socket", "", "absolute existing private indexer socket")
	height := f.Uint64("anchor-height", 0, "explicit original verified checkpoint height (including 0)")
	hash := f.String("anchor-hash", "", "exact lowercase original checkpoint hash")
	generation := f.Uint64("generation", 0, "original prepared checkpoint generation; never replace it with a guessed Hello")
	maxBytes := f.Uint64("spool-bytes", 4<<30, "hard source spool quota including quarantined files")
	maxRecords := f.Int("spool-records", 262144, "maximum retained source records")
	window := f.Int("window", 8, "bounded in-flight results (1..64)")
	poll := f.Duration("poll", 100*time.Millisecond, "native head poll interval")
	retry := f.Duration("retry", 500*time.Millisecond, "indexer transport reconnect interval")
	proofs := f.String("state-proofs", "", "absolute private JSON selection; requires a fresh source spool when enabled or changed")
	timeout := f.Duration("timeout", time.Minute, "individual native/transport call deadline, at most one hour")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			f.SetOutput(out)
			f.Usage()
			return nil
		}
		return err
	}
	configSupplied := false
	f.Visit(func(v *flag.Flag) {
		if v.Name == "config" {
			configSupplied = true
		}
	})
	if configSupplied {
		if *configPath == "" || f.NFlag() != 1 || f.NArg() != 0 || start == nil {
			return fmt.Errorf("config is exclusive of overrides")
		}
		c, err := collector.LoadConfig(*configPath)
		if err != nil {
			return err
		}
		return start(ctx, c)
	}
	explicitHeight := false
	f.Visit(func(v *flag.Flag) {
		if v.Name == "anchor-height" {
			explicitHeight = true
		}
	})
	if f.NArg() != 0 || start == nil || !explicitHeight || (*id != "5042" && *id != "5042002") || *executable == "" || *datadir == "" || *spool == "" || *socket == "" || *generation == 0 || *timeout <= 0 || *timeout > time.Hour || *poll <= 0 || *retry <= 0 || *window < 1 || *window > 64 || *maxBytes <= 64<<10 || *maxRecords < 1 || *maxRecords > 1<<20 {
		return fmt.Errorf("provide explicit chain, native/spool/socket paths and original checkpoint; invalid limits are refused")
	}
	parseHash := func(s string) (execwire.Hash, error) {
		var h execwire.Hash
		if err := h.UnmarshalText([]byte(s)); err != nil {
			return h, err
		}
		text, _ := h.MarshalText()
		if h == (execwire.Hash{}) || string(text) != s {
			return h, fmt.Errorf("hash must be nonzero canonical lowercase hex")
		}
		return h, nil
	}
	gen, err := parseHash(*genesis)
	if err != nil {
		return err
	}
	anchor, err := parseHash(*hash)
	if err != nil {
		return err
	}
	var selection *stateproof.Selection
	if *proofs != "" {
		s, e := execution.ReadStateProofSelection(ctx, *proofs)
		if e != nil {
			return e
		}
		selection = &s
	}
	return start(ctx, collector.Config{Native: collector.Options{Executable: *executable, DataDir: *datadir, Chain: execwire.Chain{ID: *id, Genesis: gen}, Timeout: *timeout, StateProofs: selection}, Anchor: execwire.Ref{Generation: *generation, Height: *height, Hash: anchor}, SpoolDir: *spool, Socket: *socket, MaxBytes: *maxBytes, MaxRecords: *maxRecords, Window: *window, Poll: *poll, Retry: *retry})
}
