package executionops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/internal/client"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
)

type RetentionOptions struct{ Action, Request, Permit, Schema, Output, Producer, ProducerConfig string }

// RunRetention uses one existing-size pool4 and no application services. DSN
// and canonical RPC credentials are accepted only through private environment.
// Source/node observations never release the durable DB fence by themselves.
func RunRetention(ctx context.Context, o RetentionOptions, profile chains.Profile, out io.Writer) error {
	if o.Action != "inspect" && o.Action != "authorize" && o.Action != "collect" {
		return fmt.Errorf("action must be inspect, authorize or collect")
	}
	emit := func(v any) error {
		if o.Output != "" {
			return retention.WriteArtifact(o.Output, v)
		}
		return json.NewEncoder(out).Encode(v)
	}
	var request retention.Request
	var permit retention.Permit
	var receipt retention.Receipt
	if o.Action == "authorize" {
		if e := retention.ReadArtifact(o.Request, &request); e != nil {
			return e
		}
		if e := request.Validate(); e != nil {
			return e
		}
	}
	if o.Action == "collect" {
		if e := retention.ReadArtifact(o.Permit, &permit); e != nil {
			return e
		}
		if e := permit.Validate(); e != nil {
			return e
		}

	}
	dsn := os.Getenv("INDEXER_DB_POSTGRES_DSN")
	if o.Schema == "" || dsn == "" {
		return fmt.Errorf("explicit schema and private INDEXER_DB_POSTGRES_DSN required")
	}
	s, e := postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: o.Schema, MaxConns: 4, ReadOnly: o.Action == "inspect"})
	if e != nil {
		return fmt.Errorf("cannot open retention database (connection details redacted)")
	}
	defer func() { _ = s.Close() }()
	if o.Action == "inspect" {
		control, e := s.ExecutionStatus(ctx)
		if e != nil {
			return e
		}
		p, pending, r, e := s.ExecutionRetentionStatus(ctx)
		if e != nil && !errors.Is(e, port.ErrNotFound) {
			return e
		}
		return emit(struct {
			Control port.ExecutionControl
			Permit  retention.Permit
			Pending bool
			Receipt *retention.Receipt
			Scope   string
		}{control, p, pending, r, "separate read-only DB observations; no source/node deletion or recovery"})
	}
	endpoint := os.Getenv("INDEXER_CHECKPOINT_RPC")
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		return fmt.Errorf("private INDEXER_CHECKPOINT_RPC must be HTTP(S)")
	}
	cl, e := client.NewClient(&client.Config{Endpoint: endpoint, RateLimit: 5, Timeout: 15 * time.Second})
	if e != nil {
		return fmt.Errorf("cannot create retention canonical client (endpoint redacted)")
	}
	defer cl.Close()
	source := sourcerpc.New(cl.RPCClient(), profile)
	control, e := s.ExecutionStatus(ctx)
	if e != nil {
		return e
	}
	id, e := cl.GetChainID(ctx)
	if e != nil {
		return fmt.Errorf("canonical chain identity unavailable")
	}
	genesis, e := source.HashAt(ctx, 0)
	if e != nil {
		return fmt.Errorf("canonical genesis unavailable")
	}
	if id.String() != control.Chain.ID || execwire.Hash(genesis) != control.Chain.Genesis {
		return port.ErrExecutionConflict
	}
	fresh := func(bound context.Context) error {
		c, e := s.ExecutionStatus(bound)
		if e != nil {
			return e
		}
		// HashAt issues a separate physical RPC read without a cached canonical hit.
		hash, e := source.HashAt(bound, c.Visible.Height)
		if e != nil {
			return fmt.Errorf("retention fresh canonical request failed")
		}
		if execwire.Hash(hash) != c.Visible.Hash {
			return port.ErrExecutionConflict
		}
		return nil
	}
	if o.Action == "authorize" {
		p, e := s.BeginExecutionRetention(ctx, request, fresh)
		if e != nil {
			return e
		}
		return emit(p)
	}
	current, pending, stored, e := s.ExecutionRetentionStatus(ctx)
	if e != nil {
		return e
	}
	if current != permit {
		return port.ErrExecutionConflict
	}
	if pending {
		e = s.WithExecutionRetention(ctx, permit, func(bound context.Context) error {
			var err error
			receipt, err = runRetentionProducer(bound, o, permit)
			return err
		})
		if e != nil {
			return e
		}
	} else {
		// Completed permit replay returns the existing release, NEVER launches GC.
		if stored == nil {
			return retention.ErrUnsafe
		}
		receipt = *stored
	}
	if e = s.CompleteExecutionRetention(ctx, permit, receipt, fresh); e != nil {
		return e
	}
	pd, e := retention.Digest(permit)
	if e != nil {
		return e
	}
	rd, e := retention.Digest(receipt)
	if e != nil {
		return e
	}
	return emit(retention.Release{Version: 1, PermitDigest: pd, ReceiptDigest: rd})
}
