// Package executionops prepares and inspects execution state without starting an app.
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
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/internal/client"
	"github.com/0xmhha/indexer-go/pkg/chains"
	checkpoint "github.com/0xmhha/indexer-go/pkg/execution/checkpoint"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
)

type Options struct{ Action, Input, Expected, Schema, Source string }

func openInput(path string) (*os.File, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("checkpoint input must be a regular file")
	}
	return f, nil
}
func readJSON(path string, v any) error {
	f, e := openInput(path)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	info, e := f.Stat()
	if e != nil {
		return e
	}
	if info.Size() > 2<<20 {
		return execwire.ErrLimit
	}
	d := json.NewDecoder(io.LimitReader(f, 2<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return execwire.ErrProtocol
	}
	return nil
}

// Run requires explicit file/schema inputs. DSN and RPC credentials are supplied
// only by environment; it does not print them. Verification performs no I/O to DB/RPC.
func Run(ctx context.Context, o Options, p chains.HeaderCodec, out io.Writer) error {
	encode := func(v any) error { return json.NewEncoder(out).Encode(v) }
	if o.Action != "verify" && o.Action != "import" && o.Action != "inspect" {
		return fmt.Errorf("action must be verify, import or inspect")
	}
	var m format.Manifest
	if o.Action != "inspect" {
		if o.Expected == "" || o.Input == "" {
			return fmt.Errorf("input and independent trusted expected manifest are required")
		}
		if err := readJSON(o.Expected, &m); err != nil {
			return fmt.Errorf("invalid expected identity")
		}
	}
	if o.Action == "verify" {
		f, err := openInput(o.Input)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		r, err := checkpoint.Verify(ctx, f, m, p, nil)
		if err != nil {
			return err
		}
		return encode(r)
	}
	dsn := os.Getenv("INDEXER_DB_POSTGRES_DSN")
	if o.Schema == "" || (o.Action == "import" && o.Schema == "public") || dsn == "" {
		return fmt.Errorf("explicit schema (non-public for import) and private INDEXER_DB_POSTGRES_DSN required")
	}
	s, err := postgres.Open(ctx, postgres.Options{DSN: dsn, Schema: o.Schema, MaxConns: 4, ReadOnly: o.Action == "inspect"})
	if err != nil {
		return fmt.Errorf("cannot open checkpoint database (connection details redacted)")
	}
	defer func() { _ = s.Close() }()
	if o.Action == "inspect" {
		var result struct {
			Control   any
			Bootstrap any
			Recovery  *checkpoint.RecoveryReport
			Scope     string
		}
		err = s.ReadSnapshot(ctx, func(bound context.Context) error {
			c, e := s.ExecutionStatus(bound)
			if e != nil {
				return e
			}
			b, e := s.ExecutionBootstrapStatus(bound)
			if e != nil {
				return e
			}
			result.Control, result.Bootstrap = c, b
			if o.Source != "" {
				var src format.SourceObservation
				if e = readJSON(o.Source, &src); e != nil {
					return e
				}
				r := checkpoint.Diagnose(c, src)
				result.Recovery = &r
			}
			return nil
		})
		if err != nil {
			return err
		}
		result.Scope = "read-only DB snapshot; optional source file is separate observation, not ACK/GC/start authorization"
		return encode(result)
	}
	endpoint := os.Getenv("INDEXER_CHECKPOINT_RPC")
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		return fmt.Errorf("private INDEXER_CHECKPOINT_RPC must be HTTP(S)")
	}
	c, err := client.NewClient(&client.Config{Endpoint: endpoint, RateLimit: 5, Timeout: 15 * time.Second})
	if err != nil {
		return fmt.Errorf("cannot create canonical RPC client (endpoint redacted)")
	}
	defer c.Close()
	bp, ok := p.(chains.Profile)
	if !ok {
		return execwire.ErrProtocol
	}
	src := sourcerpc.New(c.RPCClient(), bp)
	id, err := c.GetChainID(ctx)
	if err != nil {
		return fmt.Errorf("canonical chain identity unavailable")
	}
	genesis, err := src.HashAt(ctx, 0)
	if err != nil {
		return fmt.Errorf("canonical genesis unavailable")
	}
	if id.String() != m.Chain.ID || execwire.Hash(genesis) != m.Chain.Genesis {
		return fmt.Errorf("canonical chain identity mismatch")
	}
	f, err := openInput(o.Input)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	r, err := s.BootstrapExecution(ctx, f, m, p, func(ctx context.Context, ref execwire.Ref) error {
		h, e := src.HashAt(ctx, ref.Height)
		if e != nil {
			return errors.New("checkpoint fresh canonical request failed")
		}
		if execwire.Hash(h) != ref.Hash {
			return errors.New("checkpoint fresh canonical mismatch")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return encode(r)
}
