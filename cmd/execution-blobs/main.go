// execution-blobs composes retained full sidecars into a new private EXR1.
// It is a generic EVM tool; chain-specific producers use the library with their
// own binary profile, including its audited blob-admission policy.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type attachFunc func(context.Context, chains.BinaryProfile, execwire.Chain, string, string, string, string) (execution.BlobAttachment, error)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := run(ctx, os.Args[1:], os.Stdout, execution.AttachBlobFiles); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, out io.Writer, attach attachFunc) error {
	f := flag.NewFlagSet("execution-blobs", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	id := f.String("chain-id", "", "exact canonical decimal chain ID")
	genesis := f.String("genesis", "", "exact lowercase 0x genesis hash")
	input := f.String("result", "", "absolute private original EXR1, before spool admission")
	sidecar := f.String("sidecars", "", "absolute private full-blob relay RLP; omit only for a no-reference block")
	output := f.String("output", "", "new absolute private EXR1; never overwritten")
	provider := f.String("provider", "", "fixed provenance label, not a URL or credential")
	timeout := f.Duration("timeout", 15*time.Second, "offline composition deadline, at most one hour")
	if e := f.Parse(args); e != nil {
		if e == flag.ErrHelp {
			f.SetOutput(out)
			f.Usage()
			return nil
		}
		return e
	}
	if f.NArg() != 0 || *id == "" || *input == "" || *output == "" || *provider == "" || *timeout <= 0 || *timeout > time.Hour || attach == nil {
		return fmt.Errorf("provide --chain-id, --genesis, --result, --output, --provider and a valid --timeout")
	}
	var hash execwire.Hash
	if e := hash.UnmarshalText([]byte(*genesis)); e != nil || hash == (execwire.Hash{}) {
		return fmt.Errorf("invalid genesis hash")
	}
	canonical, _ := hash.MarshalText()
	if string(canonical) != *genesis {
		return fmt.Errorf("genesis must use canonical lowercase hex")
	}
	chain := execwire.Chain{ID: *id, Genesis: hash}
	if e := execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: execwire.Ref{Generation: 1, Hash: hash}}}); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	info, e := attach(ctx, evm.New("evm"), chain, *input, *sidecar, *output, *provider)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(info)
}
