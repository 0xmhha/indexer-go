// Package source defines where indexed blocks come from. A Source returns
// blocks and their receipts as the chain-neutral model, decoded by the chain
// profile. Implementations: rpc (a node's JSON-RPC), era (era1 archives
// exported by a node client) and Chained (one source for a range of heights,
// another after it).
package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// ErrNotFound means the source does not have the requested block.
var ErrNotFound = errors.New("source: block not found")

// ErrInconsistentReceipts means the receipts do not belong, one to one and in
// order, to the block's transactions.
var ErrInconsistentReceipts = errors.New("source: receipts do not match block transactions")

// ErrDifferentChain means two sources disagree on a block both have.
var ErrDifferentChain = errors.New("source: sources hold different chains")

// Source returns blocks with their receipts.
type Source interface {
	// Profile is the chain profile the source decodes with.
	Profile() chains.Profile
	// Head is the highest block the source has.
	Head(ctx context.Context) (uint64, error)
	// BlockWithReceipts returns block n and its receipts, checked to belong
	// to it.
	BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error)
	// HashAt returns the hash of block n, for comparing chains.
	HashAt(ctx context.Context, n uint64) (common.Hash, error)
}

// Ranged is a source that holds a fixed range of blocks (an archive).
type Ranged interface {
	Source
	Range() (first, last uint64)
}

// LogRangeSource is a source of the declared ingest mode (headers and the
// declared logs) that also reads ranges: the logs of many blocks at once
// and the headers of chosen blocks (fetch.RangeSource).
type LogRangeSource interface {
	Source
	LogsInRange(ctx context.Context, from, to uint64) ([]*model.Log, error)
	Headers(ctx context.Context, heights []uint64) (map[uint64]*model.Block, error)
}

// RangedLogSource is a LogRangeSource of a fixed range (an archive).
type RangedLogSource interface {
	LogRangeSource
	Range() (first, last uint64)
}

// ChainedLogs is Chained for declared-mode sources: an archive's declared
// logs for history and the node's after it, by block and by range.
type ChainedLogs struct {
	*Chained
	first RangedLogSource
	then  LogRangeSource
}

var _ LogRangeSource = (*ChainedLogs)(nil)

// NewChainedLogs returns the declared-mode source reading first's range
// from first and every other block from then.
func NewChainedLogs(first RangedLogSource, then LogRangeSource) *ChainedLogs {
	return &ChainedLogs{Chained: &Chained{First: first, Then: then}, first: first, then: then}
}

// LogsInRange implements LogRangeSource: the part of [from, to] inside the
// archive's range from the archive, the rest from the node, in chain order.
func (c *ChainedLogs) LogsInRange(ctx context.Context, from, to uint64) ([]*model.Log, error) {
	first, last := c.first.Range()
	var out []*model.Log
	read := func(s LogRangeSource, a, b uint64) error {
		if a > b {
			return nil
		}
		logs, err := s.LogsInRange(ctx, a, b)
		out = append(out, logs...)
		return err
	}
	if from < first {
		if err := read(c.then, from, min(to, first-1)); err != nil {
			return nil, err
		}
	}
	if err := read(c.first, max(from, first), min(to, last)); err != nil {
		return nil, err
	}
	if to > last {
		if err := read(c.then, max(from, last+1), to); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Headers implements LogRangeSource: each height from the source that
// holds it.
func (c *ChainedLogs) Headers(ctx context.Context, heights []uint64) (map[uint64]*model.Block, error) {
	first, last := c.first.Range()
	var inArchive, onNode []uint64
	for _, n := range heights {
		if n >= first && n <= last {
			inArchive = append(inArchive, n)
		} else {
			onNode = append(onNode, n)
		}
	}
	out := make(map[uint64]*model.Block, len(heights))
	for _, part := range []struct {
		s       LogRangeSource
		heights []uint64
	}{{c.first, inArchive}, {c.then, onNode}} {
		if len(part.heights) == 0 {
			continue
		}
		hs, err := part.s.Headers(ctx, part.heights)
		if err != nil {
			return nil, err
		}
		for n, b := range hs {
			out[n] = b
		}
	}
	return out, nil
}

// Chained reads blocks within First's range from First and every other block
// from Then: an archive for history, a node for what follows.
type Chained struct {
	First Ranged
	Then  Source
}

var _ Source = (*Chained)(nil)

func (c *Chained) pick(n uint64) Source {
	first, last := c.First.Range()
	if n >= first && n <= last {
		return c.First
	}
	return c.Then
}

// Profile implements Source.
func (c *Chained) Profile() chains.Profile { return c.Then.Profile() }

// Head implements Source: the later of both.
func (c *Chained) Head(ctx context.Context) (uint64, error) {
	h, err := c.Then.Head(ctx)
	if err != nil {
		return 0, err
	}
	if _, last := c.First.Range(); last > h {
		return last, nil
	}
	return h, nil
}

// BlockWithReceipts implements Source.
func (c *Chained) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	return c.pick(n).BlockWithReceipts(ctx, n)
}

// HashAt implements Source.
func (c *Chained) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	return c.pick(n).HashAt(ctx, n)
}

// CheckJoin compares the hash of the highest block both sources have, so an
// archive of another chain (or of a fork the node abandoned) is rejected
// before anything is indexed from it. When the node has none of the
// archive's blocks yet there is nothing to compare.
func (c *Chained) CheckJoin(ctx context.Context) error {
	first, last := c.First.Range()
	h, err := c.Then.Head(ctx)
	if err != nil {
		return err
	}
	if h < first {
		return nil
	}
	n := min(h, last)
	a, err := c.First.HashAt(ctx, n)
	if err != nil {
		return err
	}
	b, err := c.Then.HashAt(ctx, n)
	if err != nil {
		return err
	}
	if a != b {
		return fmt.Errorf("%w: block %d is %s in the archive and %s on the node", ErrDifferentChain, n, a.Hex(), b.Hex())
	}
	return nil
}
