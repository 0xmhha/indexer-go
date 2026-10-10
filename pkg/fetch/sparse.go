package fetch

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// The sparse ingest of the declared mode with finalized blocks
// (refactoring plan R6, after P07): a range of up to LogRange blocks is read
// with one eth_getLogs call and the headers of the blocks that have declared
// logs, and committed in one transaction with the cursor at the range's
// end. Blocks without declared logs are not stored, so there is no parent
// hash chain to check: the mode needs finality "finalized" (no
// reorganizations below the finalized block).

// LogRange is the most blocks one eth_getLogs call covers.
const LogRange = 1000

// RangeSource reads a block range's declared logs and the headers of
// chosen blocks (sourcerpc.Logs).
type RangeSource interface {
	LogsInRange(ctx context.Context, from, to uint64) ([]*model.Log, error)
	Headers(ctx context.Context, heights []uint64) (map[uint64]*model.Block, error)
}

// SetSparse selects the sparse ingest (see LogRange). The source must be a
// RangeSource.
func (f *Fetcher) SetSparse(sparse bool) { f.sparse = sparse }

// batchSize is how many blocks the live loop asks for at once.
func (f *Fetcher) batchSize() uint64 {
	if f.sparse {
		return LogRange
	}
	return uint64(max(1, f.config.BatchSize))
}

func (f *Fetcher) rangeSource() (RangeSource, error) {
	rs, ok := f.src.(RangeSource)
	if !ok {
		return nil, errors.New("fetch: the block source cannot read log ranges")
	}
	return rs, nil
}

// maxLogSplits bounds how often a refused log range is halved: from
// LogRange blocks down to one.
const maxLogSplits = 10

// rangeLogs reads the declared logs of [from, to], halving the range when
// the node refuses it (providers cap ranges and result sizes).
func rangeLogs(ctx context.Context, rs RangeSource, from, to uint64, splits int) ([]*model.Log, error) {
	logs, err := rs.LogsInRange(ctx, from, to)
	if err == nil || from == to || splits >= maxLogSplits || ctx.Err() != nil {
		return logs, err
	}
	mid := from + (to-from)/2
	left, err := rangeLogs(ctx, rs, from, mid, splits+1)
	if err != nil {
		return nil, err
	}
	right, err := rangeLogs(ctx, rs, mid+1, to, splits+1)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

// readLogBlocks returns the blocks of [from, to] that have declared logs,
// each its header with receipts holding only those logs, in height order.
func (f *Fetcher) readLogBlocks(ctx context.Context, from, to uint64) ([]*fetchedBlock, error) {
	rs, err := f.rangeSource()
	if err != nil {
		return nil, err
	}
	rctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	logs, err := rangeLogs(rctx, rs, from, to, 0)
	if err != nil {
		return nil, err
	}
	byBlock := map[uint64][]*model.Log{}
	var heights []uint64
	for _, l := range logs {
		if l.Removed {
			continue
		}
		if _, ok := byBlock[l.BlockNumber]; !ok {
			heights = append(heights, l.BlockNumber)
		}
		byBlock[l.BlockNumber] = append(byBlock[l.BlockNumber], l)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	headers, err := rs.Headers(rctx, heights)
	if err != nil {
		return nil, err
	}
	out := make([]*fetchedBlock, 0, len(heights))
	for _, n := range heights {
		h := headers[n]
		var receipts []*model.Receipt
		byTx := map[common.Hash]*model.Receipt{}
		for _, l := range byBlock[n] {
			if l.BlockHash != h.Hash {
				return nil, fmt.Errorf("fetch: a log of block %d belongs to %s, the header is %s (the node switched branches)",
					n, l.BlockHash.Hex(), h.Hash.Hex())
			}
			r, ok := byTx[l.TxHash]
			if !ok {
				r = &model.Receipt{TxHash: l.TxHash, TxIndex: l.TxIndex, BlockHash: h.Hash, BlockNumber: n, Status: types.ReceiptStatusSuccessful}
				byTx[l.TxHash] = r
				receipts = append(receipts, r)
			}
			r.Logs = append(r.Logs, l)
		}
		fb, err := fetchedFromModel(h, receipts)
		if err != nil {
			return nil, err
		}
		out = append(out, fb)
	}
	return out, nil
}

// indexSparse indexes [from, to] a LogRange at a time.
func (f *Fetcher) indexSparse(ctx context.Context, from, to uint64) error {
	if f.txr == nil {
		return errNoBlockTransactions
	}
	for start := from; start <= to; {
		end := min(to, start+LogRange-1)
		blocks, err := f.readLogBlocks(ctx, start, end)
		if err != nil {
			return fmt.Errorf("read blocks %d..%d: %w", start, end, err)
		}
		for _, fb := range blocks {
			f.offerBlock(fb) // fast path: before the range's commit
		}
		if err := f.write().do(ctx, "indexSparse", func(ctx context.Context) error {
			return f.writeSparse(ctx, blocks, end)
		}); err != nil {
			return err
		}
		if end == to {
			break
		}
		start = end + 1
	}
	return nil
}
