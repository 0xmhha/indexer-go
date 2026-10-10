package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
)

var _ source.LogRangeSource = (*Logs)(nil)

// Logs is the source of the declared ingest mode (indexer.mode: declared,
// refactoring plan R6-1): a block is its header and the logs of the
// declared contracts and events only, grouped by transaction into receipts
// that hold nothing but those logs. Both are read in one JSON-RPC batch;
// logs that belong to another block (the node switched branches between
// the two reads) fail the read, which is retried.
type Logs struct {
	src       *Source
	addresses []common.Address
	topics    []common.Hash
}

// NewLogs returns a Logs source over src for the logs of addresses whose
// first topic is one of topics.
func NewLogs(src *Source, addresses []common.Address, topics []common.Hash) *Logs {
	return &Logs{src: src, addresses: addresses, topics: topics}
}

// Profile implements source.Source.
func (l *Logs) Profile() chains.Profile { return l.src.Profile() }

// Head implements source.Source.
func (l *Logs) Head(ctx context.Context) (uint64, error) { return l.src.Head(ctx) }

// HashAt implements source.Source.
func (l *Logs) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	return l.src.HashAt(ctx, n)
}

// logFilter is an eth_getLogs filter object.
type logFilter struct {
	FromBlock string           `json:"fromBlock"`
	ToBlock   string           `json:"toBlock"`
	Address   []common.Address `json:"address"`
	Topics    [][]common.Hash  `json:"topics"`
}

// BlockWithReceipts implements source.Source: the block's header (without
// transactions) and its declared logs as receipts.
func (l *Logs) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	num := hexutil.EncodeUint64(n)
	var (
		rawBlock json.RawMessage
		logs     []*types.Log
	)
	batch := []gethrpc.BatchElem{
		{Method: "eth_getBlockByNumber", Args: []interface{}{num, false}, Result: &rawBlock},
		{Method: "eth_getLogs", Args: []interface{}{logFilter{FromBlock: num, ToBlock: num, Address: l.addresses, Topics: [][]common.Hash{l.topics}}}, Result: &logs},
	}
	if err := l.src.rpc.BatchCallContext(ctx, batch); err != nil {
		return nil, nil, fmt.Errorf("source: block %d header and logs: %w", n, err)
	}
	for _, e := range batch {
		if e.Error != nil {
			return nil, nil, fmt.Errorf("source: %s %d: %w", e.Method, n, e.Error)
		}
	}
	if isNull(rawBlock) {
		return nil, nil, fmt.Errorf("%w: %d", ErrNotFound, n)
	}
	b, err := l.header(n, rawBlock)
	if err != nil {
		return nil, nil, err
	}
	var receipts []*model.Receipt
	byTx := map[common.Hash]*model.Receipt{}
	for _, gl := range logs {
		if gl.BlockHash != b.Hash || gl.BlockNumber != n {
			return nil, nil, fmt.Errorf("source: a log of block %d belongs to block %d %s, not %s (the node switched branches)",
				n, gl.BlockNumber, gl.BlockHash.Hex(), b.Hash.Hex())
		}
		r, ok := byTx[gl.TxHash]
		if !ok {
			r = &model.Receipt{TxHash: gl.TxHash, TxIndex: gl.TxIndex, BlockHash: b.Hash, BlockNumber: n, Status: types.ReceiptStatusSuccessful}
			byTx[gl.TxHash] = r
			receipts = append(receipts, r)
		}
		r.Logs = append(r.Logs, gethconv.LogFromGeth(gl))
	}
	return b, receipts, nil
}

// LogsInRange returns the declared logs of blocks [from, to] in chain
// order, with one eth_getLogs call. A node may refuse a range it finds too
// large; the caller splits it.
func (l *Logs) LogsInRange(ctx context.Context, from, to uint64) ([]*model.Log, error) {
	var logs []*types.Log
	filter := logFilter{FromBlock: hexutil.EncodeUint64(from), ToBlock: hexutil.EncodeUint64(to), Address: l.addresses, Topics: [][]common.Hash{l.topics}}
	if err := l.src.rpc.CallContext(ctx, &logs, "eth_getLogs", filter); err != nil {
		return nil, fmt.Errorf("source: eth_getLogs %d..%d: %w", from, to, err)
	}
	out := make([]*model.Log, 0, len(logs))
	for _, gl := range logs {
		if gl.BlockNumber < from || gl.BlockNumber > to {
			return nil, fmt.Errorf("source: eth_getLogs %d..%d returned a log of block %d", from, to, gl.BlockNumber)
		}
		out = append(out, gethconv.LogFromGeth(gl))
	}
	return out, nil
}

// Headers returns the headers (blocks without transactions) of the given
// heights, read in one JSON-RPC batch.
func (l *Logs) Headers(ctx context.Context, heights []uint64) (map[uint64]*model.Block, error) {
	raws := make([]json.RawMessage, len(heights))
	batch := make([]gethrpc.BatchElem, len(heights))
	for i, n := range heights {
		batch[i] = gethrpc.BatchElem{Method: "eth_getBlockByNumber", Args: []interface{}{hexutil.EncodeUint64(n), false}, Result: &raws[i]}
	}
	if len(batch) > 0 {
		if err := l.src.rpc.BatchCallContext(ctx, batch); err != nil {
			return nil, fmt.Errorf("source: %d headers: %w", len(heights), err)
		}
	}
	out := make(map[uint64]*model.Block, len(heights))
	for i, n := range heights {
		if batch[i].Error != nil {
			return nil, fmt.Errorf("source: header %d: %w", n, batch[i].Error)
		}
		if isNull(raws[i]) {
			return nil, fmt.Errorf("%w: %d", ErrNotFound, n)
		}
		b, err := l.header(n, raws[i])
		if err != nil {
			return nil, err
		}
		out[n] = b
	}
	return out, nil
}

// header decodes a block returned without transactions: the profile
// decodes it as a block with none.
func (l *Logs) header(n uint64, raw json.RawMessage) (*model.Block, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("source: block %d: %w", n, err)
	}
	fields["transactions"] = json.RawMessage("[]")
	stripped, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return l.src.decodeBlock(n, stripped)
}
