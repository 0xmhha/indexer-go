package era

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// Logs is the source of the declared ingest mode over era1 files: a block
// is its header and the logs of the declared contracts and events only,
// grouped by transaction into receipts that hold nothing but those logs,
// as rpc.Logs reads them from a node. The files have no index by address
// or topic, so every block of a read is decoded.
type Logs struct {
	src       *Source
	addresses map[common.Address]bool
	topics    map[common.Hash]bool
}

var _ source.LogRangeSource = (*Logs)(nil)

// NewLogs returns a Logs source over src for the logs of addresses whose
// first topic is one of topics.
func NewLogs(src *Source, addresses []common.Address, topics []common.Hash) *Logs {
	l := &Logs{src: src, addresses: map[common.Address]bool{}, topics: map[common.Hash]bool{}}
	for _, a := range addresses {
		l.addresses[a] = true
	}
	for _, t := range topics {
		l.topics[t] = true
	}
	return l
}

// Profile implements source.Source.
func (l *Logs) Profile() chains.Profile { return l.src.Profile() }

// Range implements source.Ranged.
func (l *Logs) Range() (first, last uint64) { return l.src.Range() }

// Head implements source.Source.
func (l *Logs) Head(ctx context.Context) (uint64, error) { return l.src.Head(ctx) }

// HashAt implements source.Source.
func (l *Logs) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	return l.src.HashAt(ctx, n)
}

func (l *Logs) declared(lg *model.Log) bool {
	return l.addresses[lg.Address] && len(lg.Topics) > 0 && l.topics[lg.Topics[0]]
}

// BlockWithReceipts implements source.Source: the block's header (without
// transactions) and its declared logs as receipts.
func (l *Logs) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	b, rs, err := l.src.BlockWithReceipts(ctx, n)
	if err != nil {
		return nil, nil, err
	}
	var receipts []*model.Receipt
	for _, r := range rs {
		var logs []*model.Log
		for _, lg := range r.Logs {
			if l.declared(lg) {
				logs = append(logs, lg)
			}
		}
		if len(logs) == 0 {
			continue
		}
		receipts = append(receipts, &model.Receipt{TxHash: r.TxHash, TxIndex: r.TxIndex, BlockHash: b.Hash,
			BlockNumber: n, Status: r.Status, Logs: logs})
	}
	return header(b), receipts, nil
}

// header is b without its transactions, as a node returns a block read
// without them (its size stays the full block's).
func header(b *model.Block) *model.Block {
	h := *b
	h.Transactions = []*model.Transaction{}
	return &h
}

// LogsInRange returns the declared logs of blocks [from, to] in chain
// order.
func (l *Logs) LogsInRange(ctx context.Context, from, to uint64) ([]*model.Log, error) {
	var out []*model.Log
	for n := from; n <= to; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, rs, err := l.BlockWithReceipts(ctx, n)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, r.Logs...)
		}
	}
	return out, nil
}

// Headers returns the headers (blocks without transactions) of the given
// heights.
func (l *Logs) Headers(ctx context.Context, heights []uint64) (map[uint64]*model.Block, error) {
	out := make(map[uint64]*model.Block, len(heights))
	for _, n := range heights {
		b, _, err := l.src.BlockWithReceipts(ctx, n)
		if err != nil {
			return nil, err
		}
		out[n] = header(b)
	}
	return out, nil
}
