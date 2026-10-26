// execution-blobs composes retained full sidecars into a new private EXR1.
// It is a generic EVM tool; chain-specific producers use the library with their
// own binary profile, including its audited blob-admission policy.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	"github.com/ethereum/go-ethereum/rpc"
	"io"
	"net/http"
	"net/url"
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
	engine := f.String("engine-rpc", "", "opt-in Engine V1 HTTP endpoint, sharing one bounded RPC5 client for all calls")
	jwtFile := f.String("engine-jwt-file", "", "private 0600 Engine JWT hex secret file (optional for offline mocks)")
	contract := f.String("blob-contract", execution.EngineBlobContract, "audited full-blob source contract; native mocks use evm-dev-cancun-full-blobs.engine-v1")
	maxLag := f.Uint64("max-lag-blocks", 64, "maximum head distance, 1..8192; pool pruning can make any blob unavailable")
	timeout := f.Duration("timeout", 15*time.Second, "offline composition deadline, at most one hour")
	if e := f.Parse(args); e != nil {
		if e == flag.ErrHelp {
			f.SetOutput(out)
			f.Usage()
			return nil
		}
		return e
	}
	if f.NArg() != 0 || *id == "" || *input == "" || *output == "" || *provider == "" || *timeout <= 0 || *timeout > time.Hour {
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
	var info execution.BlobAttachment
	var e error
	if *engine != "" {
		if *sidecar != "" {
			return fmt.Errorf("choose --sidecars or --engine-rpc")
		}
		policy := execution.BlobSupplyPolicy{Contract: *contract, Chain: chain, Provider: *provider, MaxLagBlocks: *maxLag}
		if _, e = policy.Admission(); e != nil {
			return e
		}
		endpoint, parseErr := url.Parse(*engine)
		if parseErr != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Host == "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			return fmt.Errorf("invalid Engine HTTP endpoint")
		}
		pool, poolErr := rpcpool.New(rpcpool.Config{Endpoints: []string{*engine}, RateLimit: 5, Burst: 1, Timeout: *timeout, MaxResponseBytes: 10 << 20})
		if poolErr != nil {
			return poolErr
		}
		defer pool.Close()
		options := []rpc.ClientOption{rpc.WithHTTPClient(&http.Client{Transport: pool})}
		if *jwtFile != "" {
			secret, readErr := execution.ReadEngineJWTSecret(ctx, *jwtFile)
			if readErr != nil {
				return readErr
			}
			options = append(options, rpc.WithHTTPAuth(func(header http.Header) error {
				payload, marshalErr := json.Marshal(map[string]int64{"iat": time.Now().Unix()})
				if marshalErr != nil {
					return marshalErr
				}
				token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(payload)
				mac := hmac.New(sha256.New, secret[:])
				_, _ = mac.Write([]byte(token))
				header.Set("Authorization", "Bearer "+token+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
				return nil
			}))
		}
		client, dialErr := rpc.DialOptions(ctx, *engine, options...)
		if dialErr != nil {
			return fmt.Errorf("engine client initialization failed")
		}
		defer client.Close()
		info, e = execution.AcquireEngineBlobFiles(ctx, evm.New("evm"), policy, *input, *output, client)
	} else {
		if *jwtFile != "" || attach == nil {
			return fmt.Errorf("engine authentication requires --engine-rpc")
		}
		info, e = attach(ctx, evm.New("evm"), chain, *input, *sidecar, *output, *provider)
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(info)
}
