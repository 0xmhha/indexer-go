package fetch

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// rpcCtx bounds one RPC call by Config.RPCTimeout so a stalled node cannot
// block indexing indefinitely.
func (f *Fetcher) rpcCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if f.config.RPCTimeout > 0 {
		return context.WithTimeout(ctx, f.config.RPCTimeout)
	}
	return ctx, func() {}
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
