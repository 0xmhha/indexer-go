// Package rpc is the JSON-RPC block source: it fetches blocks from a node as
// raw JSON and decodes them with the node's chain profile into the
// chain-neutral model.
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// Errors are shared by every source (pkg/source).
var (
	ErrNotFound             = source.ErrNotFound
	ErrInconsistentReceipts = source.ErrInconsistentReceipts
)

var _ source.Source = (*Source)(nil)

// Source reads blocks and receipts from one node.
type Source struct {
	rpc     *gethrpc.Client
	profile chains.Profile

	// perTxReceipts is set once the node rejects eth_getBlockReceipts.
	perTxReceipts atomic.Bool
}

// New returns a Source that decodes with profile.
func New(c *gethrpc.Client, profile chains.Profile) *Source {
	return &Source{rpc: c, profile: profile}
}

// NodeInfo asks the node for the facts profiles detect it by.
func NodeInfo(ctx context.Context, c *gethrpc.Client) (chains.NodeInfo, error) {
	var info chains.NodeInfo
	if err := c.CallContext(ctx, &info.ClientVersion, "web3_clientVersion"); err != nil {
		return info, fmt.Errorf("source: web3_clientVersion: %w", err)
	}
	var id hexutil.Uint64
	if err := c.CallContext(ctx, &id, "eth_chainId"); err != nil {
		return info, fmt.Errorf("source: eth_chainId: %w", err)
	}
	info.ChainID = uint64(id)
	return info, nil
}

// Detect returns a Source using the highest-priority profile that accepts
// the node.
func Detect(ctx context.Context, c *gethrpc.Client) (*Source, error) {
	info, err := NodeInfo(ctx, c)
	if err != nil {
		return nil, err
	}
	p, err := chains.Detect(info)
	if err != nil {
		return nil, err
	}
	return New(c, p), nil
}

// Select returns a Source using the profile named name (an id or alias),
// or the detected one when name is empty or names no profile (an adapter
// type such as "anvil" that has no profile of its own).
func Select(ctx context.Context, c *gethrpc.Client, name string) (*Source, error) {
	if name != "" {
		if p, ok := chains.Lookup(name); ok {
			return New(c, p), nil
		}
	}
	return Detect(ctx, c)
}

// Profile returns the profile the source decodes with.
func (s *Source) Profile() chains.Profile { return s.profile }

// Head returns the node's latest block number.
func (s *Source) Head(ctx context.Context) (uint64, error) {
	var n hexutil.Uint64
	if err := s.rpc.CallContext(ctx, &n, "eth_blockNumber"); err != nil {
		return 0, fmt.Errorf("source: eth_blockNumber: %w", err)
	}
	return uint64(n), nil
}

// HashAt returns the hash the node reports for its block at height n,
// without transactions. It is meant for comparing chains (reorg checks).
func (s *Source) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	var head struct {
		Hash common.Hash `json:"hash"`
	}
	var raw json.RawMessage
	if err := s.rpc.CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeUint64(n), false); err != nil {
		return common.Hash{}, fmt.Errorf("source: eth_getBlockByNumber %d: %w", n, err)
	}
	if isNull(raw) {
		return common.Hash{}, fmt.Errorf("%w: %d", ErrNotFound, n)
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return common.Hash{}, fmt.Errorf("source: block %d: %w", n, err)
	}
	return head.Hash, nil
}

// Block fetches block n with full transactions.
func (s *Source) Block(ctx context.Context, n uint64) (*model.Block, error) {
	var raw json.RawMessage
	if err := s.rpc.CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeUint64(n), true); err != nil {
		return nil, fmt.Errorf("source: eth_getBlockByNumber %d: %w", n, err)
	}
	if isNull(raw) {
		return nil, fmt.Errorf("%w: %d", ErrNotFound, n)
	}
	return s.decodeBlock(n, raw)
}

func (s *Source) decodeBlock(n uint64, raw json.RawMessage) (*model.Block, error) {
	b, err := s.profile.DecodeBlock(raw)
	if err != nil {
		return nil, fmt.Errorf("source: block %d: %w", n, err)
	}
	if b.Number != n {
		return nil, fmt.Errorf("source: asked for block %d, node returned %d", n, b.Number)
	}
	return b, nil
}

// BlockWithReceipts fetches block n and its receipts in one JSON-RPC batch
// (one round trip). Receipts are checked as in Receipts. On nodes without
// eth_getBlockReceipts it falls back to per-transaction receipts.
func (s *Source) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	if s.perTxReceipts.Load() {
		b, err := s.Block(ctx, n)
		if err != nil {
			return nil, nil, err
		}
		rs, err := s.Receipts(ctx, b)
		return b, rs, err
	}

	var rawBlock, rawReceipts json.RawMessage
	num := hexutil.EncodeUint64(n)
	batch := []gethrpc.BatchElem{
		{Method: "eth_getBlockByNumber", Args: []any{num, true}, Result: &rawBlock},
		{Method: "eth_getBlockReceipts", Args: []any{num}, Result: &rawReceipts},
	}
	if err := s.rpc.BatchCallContext(ctx, batch); err != nil {
		return nil, nil, fmt.Errorf("source: block %d: %w", n, err)
	}
	if batch[0].Error != nil {
		return nil, nil, fmt.Errorf("source: eth_getBlockByNumber %d: %w", n, batch[0].Error)
	}
	if isNull(rawBlock) {
		return nil, nil, fmt.Errorf("%w: %d", ErrNotFound, n)
	}
	// Decode the receipts while the block decodes; they are checked
	// against the block once both are done.
	type decoded struct {
		rs  []*model.Receipt
		err error
	}
	var receiptsDone chan decoded
	if batch[1].Error == nil && !isNull(rawReceipts) {
		receiptsDone = make(chan decoded, 1)
		go func() {
			rs, err := s.profile.DecodeReceipts(rawReceipts)
			receiptsDone <- decoded{rs, err}
		}()
	}
	b, err := s.decodeBlock(n, rawBlock)
	var pre *decoded
	if receiptsDone != nil {
		d := <-receiptsDone
		pre = &d
	}
	if err != nil {
		return nil, nil, err
	}
	unsupported := batch[1].Error != nil && isMethodNotFound(batch[1].Error)
	if unsupported {
		s.perTxReceipts.Store(true)
	}
	if len(b.Transactions) == 0 {
		return b, nil, nil
	}
	switch {
	case unsupported:
		rs, err := s.Receipts(ctx, b)
		return b, rs, err
	case batch[1].Error != nil:
		return nil, nil, fmt.Errorf("source: eth_getBlockReceipts %d: %w", n, batch[1].Error)
	case isNull(rawReceipts):
		// The node has the block but not yet its receipts: ask per block again.
		rs, err := s.Receipts(ctx, b)
		return b, rs, err
	}
	if pre.err != nil {
		return nil, nil, fmt.Errorf("source: receipts of block %d: %w", b.Number, pre.err)
	}
	rs, err := checkReceipts(b, pre.rs)
	return b, rs, err
}

// Receipts fetches the receipts of b and checks that they belong to it: one
// receipt per transaction, in order, with matching hashes.
func (s *Source) Receipts(ctx context.Context, b *model.Block) ([]*model.Receipt, error) {
	if len(b.Transactions) == 0 {
		return nil, nil
	}
	raw, err := s.receiptsJSON(ctx, b)
	if err != nil {
		return nil, err
	}
	return s.decodeReceipts(b, raw)
}

// decodeReceipts decodes a block's receipts and checks that they belong to
// it: one receipt per transaction, in order, with matching hashes.
func (s *Source) decodeReceipts(b *model.Block, raw json.RawMessage) ([]*model.Receipt, error) {
	rs, err := s.profile.DecodeReceipts(raw)
	if err != nil {
		return nil, fmt.Errorf("source: receipts of block %d: %w", b.Number, err)
	}
	return checkReceipts(b, rs)
}

// checkReceipts requires one receipt per transaction of b, in order, with
// matching transaction and block hashes.
func checkReceipts(b *model.Block, rs []*model.Receipt) ([]*model.Receipt, error) {
	if len(rs) != len(b.Transactions) {
		return nil, fmt.Errorf("%w: block %d has %d transactions, %d receipts", ErrInconsistentReceipts, b.Number, len(b.Transactions), len(rs))
	}
	for i, r := range rs {
		if r.TxHash != b.Transactions[i].Hash || r.BlockHash != b.Hash {
			return nil, fmt.Errorf("%w: block %d receipt %d is for tx %s in block %s", ErrInconsistentReceipts, b.Number, i, r.TxHash.Hex(), r.BlockHash.Hex())
		}
	}
	return rs, nil
}

// receiptsJSON returns the block's receipts as one JSON array, from
// eth_getBlockReceipts or, on nodes without it, one receipt per transaction.
func (s *Source) receiptsJSON(ctx context.Context, b *model.Block) (json.RawMessage, error) {
	if !s.perTxReceipts.Load() {
		var raw json.RawMessage
		err := s.rpc.CallContext(ctx, &raw, "eth_getBlockReceipts", hexutil.EncodeUint64(b.Number))
		if err == nil && !isNull(raw) {
			return raw, nil
		}
		if err != nil && !isMethodNotFound(err) {
			return nil, fmt.Errorf("source: eth_getBlockReceipts %d: %w", b.Number, err)
		}
		if err != nil {
			s.perTxReceipts.Store(true)
		}
	}

	batch := make([]gethrpc.BatchElem, len(b.Transactions))
	out := make([]json.RawMessage, len(b.Transactions))
	for i, tx := range b.Transactions {
		batch[i] = gethrpc.BatchElem{Method: "eth_getTransactionReceipt", Args: []any{tx.Hash}, Result: &out[i]}
	}
	if err := s.rpc.BatchCallContext(ctx, batch); err != nil {
		return nil, fmt.Errorf("source: receipts of block %d: %w", b.Number, err)
	}
	for i, e := range batch {
		if e.Error != nil {
			return nil, fmt.Errorf("source: receipt of %s: %w", b.Transactions[i].Hash.Hex(), e.Error)
		}
		if isNull(out[i]) {
			return nil, fmt.Errorf("%w: block %d has no receipt for %s", ErrInconsistentReceipts, b.Number, b.Transactions[i].Hash.Hex())
		}
	}
	return json.Marshal(out)
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

func isMethodNotFound(err error) bool {
	var re gethrpc.Error
	if errors.As(err, &re) && re.ErrorCode() == -32601 {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "method not found") ||
		strings.Contains(strings.ToLower(err.Error()), "does not exist")
}
