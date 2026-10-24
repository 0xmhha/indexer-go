// execution-submit sends one retained native Arc replay result to an already
// running indexer. It starts no node/indexer service and removes no source file.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/x/arc"
)

type submitFunc func(context.Context, chains.BinaryProfile, execwire.Chain, string, string) (execwire.Frame, error)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, execution.SubmitFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, submit submitFunc) error {
	f := flag.NewFlagSet("execution-submit", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	id := f.String("chain-id", "", "Arc chain ID (5042 or 5042002)")
	genesis := f.String("genesis", "", "exact lowercase 0x genesis hash")
	path := f.String("result", "", "absolute private retained EXR1 file")
	socket := f.String("socket", "", "absolute existing private indexer socket")
	timeout := f.Duration("timeout", time.Minute, "transfer deadline, at most one hour")
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			f.SetOutput(out)
			f.Usage()
			return nil
		}
		return err
	}
	if f.NArg() != 0 || (*id != "5042" && *id != "5042002") || *path == "" || *socket == "" || *timeout <= 0 || *timeout > time.Hour || submit == nil {
		return fmt.Errorf("provide --chain-id, --genesis, --result, --socket and a valid --timeout")
	}
	var hash execwire.Hash
	if len(*genesis) != 66 || (*genesis)[:2] != "0x" {
		return fmt.Errorf("invalid genesis hash")
	}
	if _, err := hex.Decode(hash[:], []byte((*genesis)[2:])); err != nil || hash == (execwire.Hash{}) {
		return fmt.Errorf("invalid genesis hash")
	}
	encoded, _ := hash.MarshalText()
	if string(encoded) != *genesis {
		return fmt.Errorf("genesis must use canonical lowercase hex")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	ack, err := submit(ctx, arc.New(), execwire.Chain{ID: *id, Genesis: hash}, *path, *socket)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(ack)
}
