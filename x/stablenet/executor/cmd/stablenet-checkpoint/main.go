// stablenet-checkpoint exports/inspects existing state without starting node services.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	snapshot "github.com/0xmhha/indexer-go/x/stablenet/executor/checkpoint"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/nodebackend"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
)

func run(ctx context.Context, action, config, path, ancients, selection, output string) error {
	c, err := nodebackend.LoadConfig(config)
	if err != nil {
		return err
	}
	if action == "inspect" {
		n, e := nodebackend.InspectCheckpoint(ctx, c, path, ancients)
		s, se := spool.InspectMetadata(ctx, c.SpoolDir)
		obs := format.SourceObservation{Chain: s.Chain, Anchor: s.Anchor, Committed: s.Committed, Parent: n.Ref, ParentRoot: n.Root, HeaderAvailable: n.HeaderAvailable, StateAvailable: n.StateAvailable, TransitionPending: s.TransitionPending, MaintenancePending: s.MaintenancePending}
		if err = json.NewEncoder(os.Stdout).Encode(obs); err != nil {
			return err
		}
		return errors.Join(e, se)
	}
	if action != "genesis" && action != "node" {
		return fmt.Errorf("action must be genesis, node or inspect")
	}
	var targets []snapshot.Target
	if selection != "" {
		f, e := os.Open(selection)
		if e != nil {
			return e
		}
		d := json.NewDecoder(io.LimitReader(f, 1<<20))
		d.DisallowUnknownFields()
		e = d.Decode(&targets)
		var extra any
		if e == nil && d.Decode(&extra) != io.EOF {
			e = fmt.Errorf("trailing selection data")
		}
		ce := f.Close()
		if e = errors.Join(e, ce); e != nil {
			return e
		}
		if targets == nil {
			targets = []snapshot.Target{}
		}
	}
	if output == "" {
		return fmt.Errorf("new output file required")
	}
	// O_EXCL preserves existing files and rejects symlink destinations. Failure
	// leaves a private incomplete artifact for diagnosis; verification rejects it.
	f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if action == "genesis" {
		if selection != "" {
			err = fmt.Errorf("genesis requires full coverage")
		} else {
			err = nodebackend.ExportGenesis(ctx, c, f)
		}
	} else {
		err = nodebackend.ExportCheckpoint(ctx, c, path, ancients, f, targets)
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
func main() {
	action := flag.String("action", "inspect", "genesis, node or inspect; no node startup")
	cfg := flag.String("config", "", "explicit executor config")
	path := flag.String("chaindata", "", "existing absolute chaindata directory")
	ancients := flag.String("ancients", "", "existing absolute freezer directory when used")
	selection := flag.String("selection", "", "optional selected account/slot JSON (partial only)")
	output := flag.String("output", "", "new private snapshot file")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *action, *cfg, *path, *ancients, *selection, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
