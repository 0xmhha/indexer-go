// Package exporter sends durable second-pass results to the indexer. The
// backend is the owned execution node; this package never opens PostgreSQL.
package exporter

import (
	"context"
	"errors"
	"os"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
)

var ErrCanonicalChanged = errors.New("exporter: canonical chain changed")

type CanonicalChange struct{ Ref execwire.Ref }

func (*CanonicalChange) Error() string { return ErrCanonicalChanged.Error() }
func (*CanonicalChange) Unwrap() error { return ErrCanonicalChanged }

type Backend interface {
	Head(context.Context) (execwire.Ref, error)
	Canonical(context.Context, uint64) (execwire.Hash, error)
	Replay(context.Context, uint64, uint64) (execwire.Result, error)
}
type Collector struct {
	Backend Backend
	Spool   *spool.Store
	Limit   int
}

func (c *Collector) CatchUp(ctx context.Context) (int, error) {
	if c == nil || c.Backend == nil || c.Spool == nil {
		return 0, spool.ErrConflict
	}
	limit := c.Limit
	if limit == 0 {
		limit = 64
	}
	if limit < 1 || limit > 4096 {
		return 0, execwire.ErrLimit
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tail, err := c.Spool.Tail(ctx)
	if err != nil {
		return 0, err
	}
	head, err := c.Backend.Head(ctx)
	if err != nil {
		return 0, err
	}
	if head.Height < tail.Height {
		known, ok, err := c.Spool.Reference(ctx, head.Height)
		if err != nil {
			return 0, err
		}
		if !ok || known.Hash != head.Hash {
			return 0, &CanonicalChange{Ref: execwire.Ref{Generation: tail.Generation, Height: head.Height, Hash: head.Hash}}
		}
		return 0, nil
	}
	hash, err := c.Backend.Canonical(ctx, tail.Height)
	if err != nil {
		return 0, err
	}
	if hash != tail.Hash {
		return 0, &CanonicalChange{Ref: execwire.Ref{Generation: tail.Generation, Height: tail.Height, Hash: hash}}
	}
	captured := 0
	for tail.Height < head.Height && captured < limit {
		if err := ctx.Err(); err != nil {
			return captured, err
		}
		height := tail.Height + 1
		hash, err := c.Backend.Canonical(ctx, height)
		if err != nil {
			return captured, err
		}
		ref := execwire.Ref{Generation: tail.Generation, Height: height, Hash: hash}
		r, err := c.Spool.Retained(ctx, ref)
		if errors.Is(err, os.ErrNotExist) {
			r, err = c.Backend.Replay(ctx, height, ref.Generation)
		}
		if err != nil {
			return captured, err
		}
		if r.Ref != ref || r.Parent != tail.Hash {
			return captured, spool.ErrConflict
		}
		after, err := c.Backend.Canonical(ctx, height)
		if err != nil {
			return captured, err
		}
		if after != hash {
			return captured, &CanonicalChange{Ref: execwire.Ref{Generation: tail.Generation, Height: height, Hash: after}}
		}
		if _, err = c.Spool.Append(ctx, r); err != nil {
			return captured, err
		}
		tail = r.Ref
		captured++
	}
	return captured, nil
}
