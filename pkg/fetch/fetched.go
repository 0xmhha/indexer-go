package fetch

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// fetchedBlock is one block as the indexing pipeline works on it.
//
// block and receipts are the chain-neutral model: hashes, transaction types
// and chain-specific fields exactly as the chain profile decoded them. They
// are what is stored and what identity-bearing indexes (block hash,
// transaction hash, fee payer) are built from.
//
// geth and gethReceipts are a go-ethereum view of the same block for feature
// processors that have not moved to the model yet. The view cannot represent
// everything (a StableNet fee delegation transaction appears as its inner
// transaction and the block hash is recomputed with the Ethereum rule), so
// code that records a hash must take it from the model. Transactions and
// receipts are aligned by index in both views.
type fetchedBlock struct {
	block    *model.Block
	receipts []*model.Receipt

	geth         *types.Block
	gethReceipts types.Receipts
}

func (fb *fetchedBlock) height() uint64 { return fb.block.Number }

// fetchedFromModel wraps a block decoded by a chain profile.
func fetchedFromModel(b *model.Block, rs []*model.Receipt) (*fetchedBlock, error) {
	g, err := gethconv.BlockToGeth(b)
	if err != nil {
		return nil, fmt.Errorf("go-ethereum view of block %d: %w", b.Number, err)
	}
	return &fetchedBlock{block: b, receipts: rs, geth: g, gethReceipts: gethconv.ReceiptsToGeth(rs)}, nil
}

// errNoBlockTransactions is returned when the storage cannot index a block
// in one transaction (port.BlockTransactor).
var errNoBlockTransactions = errors.New("fetch: storage does not support block transactions")

// errNoSource is returned when blocks are read before SetSource.
var errNoSource = errors.New("fetch: no block source (SetSource)")

// SetSource sets where the fetcher reads blocks: raw JSON from the node
// decoded by its chain profile, or archives. It must be set before
// indexing.
func (f *Fetcher) SetSource(src source.Source) {
	f.src = src
}

// fetchOnce reads one block and its receipts without retrying.
func (f *Fetcher) fetchOnce(ctx context.Context, height uint64) (*fetchedBlock, error) {
	if f.src == nil {
		return nil, errNoSource
	}
	rctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	b, rs, err := f.src.BlockWithReceipts(rctx, height)
	if err != nil {
		return nil, err
	}
	return fetchedFromModel(b, rs)
}

// txWithReceipt is one transaction of a fetched block with its receipt, in
// both views.
type txWithReceipt struct {
	index       int
	tx          *model.Transaction
	receipt     *model.Receipt
	gethTx      *types.Transaction
	gethReceipt *types.Receipt
}

// transactions pairs each transaction with its receipt by the chain's
// transaction hash. Transactions without a receipt are left out.
func (fb *fetchedBlock) transactions() []txWithReceipt {
	byHash := make(map[common.Hash]int, len(fb.receipts))
	for i, r := range fb.receipts {
		byHash[r.TxHash] = i
	}
	gethTxs := fb.geth.Transactions()
	out := make([]txWithReceipt, 0, len(fb.block.Transactions))
	for i, tx := range fb.block.Transactions {
		ri, ok := byHash[tx.Hash]
		if !ok || i >= len(gethTxs) {
			continue
		}
		out = append(out, txWithReceipt{
			index: i, tx: tx, receipt: fb.receipts[ri],
			gethTx: gethTxs[i], gethReceipt: fb.gethReceipts[ri],
		})
	}
	return out
}

// SetFeatures sets the handlers of the enabled features. They run for every
// block after its core data is stored, inside the block's transaction.
func (f *Fetcher) SetFeatures(p *feature.Pipeline) {
	f.features = p
}

// Publish sends an event to subscribers. While a block is being indexed the
// event is held back until the block commits.
func (f *Fetcher) Publish(ev events.Event) bool {
	return f.publish(ev)
}

// runFeatures runs the enabled features' handlers for fb.
func (f *Fetcher) runFeatures(ctx context.Context, fb *fetchedBlock) error {
	if f.features == nil {
		return nil
	}
	return f.features.HandleBlock(ctx, &feature.Block{
		Model: fb.block, Receipts: fb.receipts, Geth: fb.geth, GethReceipts: fb.gethReceipts,
	})
}

// BlockAt reads block n from the node as the model (for chain rules that
// need earlier headers than the index holds).
func (f *Fetcher) BlockAt(ctx context.Context, n uint64) (*model.Block, error) {
	fb, err := f.fetchOnce(ctx, n)
	if err != nil {
		return nil, err
	}
	return fb.block, nil
}

// BalanceAt reads an account's native balance from the node at a block,
// bounded by the RPC timeout.
func (f *Fetcher) BalanceAt(ctx context.Context, addr common.Address, block *big.Int) (*big.Int, error) {
	return f.balanceAt(ctx, addr, block)
}
