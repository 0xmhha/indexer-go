package fetch

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// rpcCtx bounds one RPC call by Config.RPCTimeout so a stalled node cannot
// block indexing indefinitely.
func (f *Fetcher) rpcCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if f.config.RPCTimeout > 0 {
		return context.WithTimeout(ctx, f.config.RPCTimeout)
	}
	return ctx, func() {}
}

// getBlock fetches a block through the chain adapter when one is set.
func (f *Fetcher) getBlock(ctx context.Context, height uint64) (*types.Block, error) {
	ctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	if f.chainAdapter != nil {
		return f.chainAdapter.BlockFetcher().GetBlockByNumber(ctx, height)
	}
	return f.client.GetBlockByNumber(ctx, height)
}

// getReceipts fetches a block's receipts through the chain adapter when set.
func (f *Fetcher) getReceipts(ctx context.Context, height uint64) (types.Receipts, error) {
	ctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	if f.chainAdapter != nil {
		return f.chainAdapter.BlockFetcher().GetBlockReceipts(ctx, height)
	}
	return f.client.GetBlockReceipts(ctx, height)
}

func (f *Fetcher) latestBlockNumber(ctx context.Context) (uint64, error) {
	ctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	return f.client.GetLatestBlockNumber(ctx)
}

func (f *Fetcher) balanceAt(ctx context.Context, addr common.Address, block *big.Int) (*big.Int, error) {
	ctx, cancel := f.rpcCtx(ctx)
	defer cancel()
	return f.client.BalanceAt(ctx, addr, block)
}
